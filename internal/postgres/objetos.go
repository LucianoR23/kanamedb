package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Objects lista lo que hay en estos esquemas y el volcado de estructura NO
// sabe escribir.
//
// Es la mitad que hace honesto al volcado de estructura. Kaname renderiza DDL
// de tablas, columnas, claves, restricciones e índices —lo que el changeset
// sabe expresar— y nada más. Todo lo demás que exista en el esquema queda
// afuera del archivo, y este método es lo que permite decirlo con nombre y
// apellido en vez de con un «puede faltar algo».
//
// La lista se arma de lo que el catálogo TIENE, no de lo que se nos ocurrió
// que podía haber: si mañana aparece algo que no está contemplado, va a
// aparecer acá como su propio tipo en vez de desaparecer. Ver `dump.Cobertura`.
func Objects(ctx context.Context, pool *pgxpool.Pool, esquemas []string) ([]schema.Object, error) {
	if len(esquemas) == 0 {
		return nil, nil
	}
	var out []schema.Object
	// Los fallos se juntan y NO cortan el recorrido, y lo que se alcanzó a leer
	// se devuelve igual.
	//
	// Un catálogo que no se puede leer no se traga —quedarse callado produce el
	// archivo silencioso que este código existe para evitar— pero cortar en la
	// primera consulta que falla tiraba TODO lo demás: la de los eventos va
	// última y no siempre se puede leer, y por ella desaparecían del árbol las
	// vistas, las funciones y los triggers que ya se habían leído bien. Quien
	// llama decide qué hacer con las dos mitades: el volcado se niega a
	// escribir un archivo incompleto, el árbol muestra lo que hay y dice qué
	// faltó.
	var fallos []error

	for _, q := range consultasDeCobertura {
		filas, err := pool.Query(ctx, q.sql, esquemas)
		if err != nil {
			fallos = append(fallos, fmt.Errorf("leer %s del catálogo: %w", q.que, err))
			continue
		}
		for filas.Next() {
			o := schema.Object{Kind: q.kind}
			var tabla, args, clase *string
			if err := filas.Scan(&o.Schema, &o.Name, &tabla, &args, &clase); err != nil {
				fallos = append(fallos, fmt.Errorf("leer %s del catálogo: %w", q.que, err))
				break
			}
			if tabla != nil {
				o.Table = *tabla
			}
			if args != nil {
				o.Args = *args
			}
			// Casi todas las consultas traen una sola clase de objeto y la
			// declaran arriba. La de los tipos no puede: enum, dominio y
			// compuesto salen de la MISMA fila del catálogo y se distinguen por
			// una columna, así que ahí la clase viaja con el dato.
			if clase != nil {
				o.Kind = schema.ObjectKind(*clase)
			}
			out = append(out, o)
		}
		if err := filas.Err(); err != nil {
			fallos = append(fallos, fmt.Errorf("leer %s del catálogo: %w", q.que, err))
		}
		filas.Close()
	}
	return out, errors.Join(fallos...)
}

// consultaDeCobertura es una clase de objeto y cómo encontrarla. Las cinco
// columnas son siempre las mismas —esquema, nombre, tabla o NULL, argumentos o
// NULL, clase o NULL— para que el lector sea uno solo.
type consultaDeCobertura struct {
	kind schema.ObjectKind
	que  string
	sql  string
}

var consultasDeCobertura = []consultaDeCobertura{
	{
		kind: schema.ObjView,
		que:  "las vistas",
		sql: `SELECT n.nspname, c.relname, NULL::text, NULL::text, NULL::text
		      FROM pg_catalog.pg_class c
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE c.relkind = 'v' AND n.nspname = ANY($1)`,
	},
	{
		kind: schema.ObjMatView,
		que:  "las vistas materializadas",
		sql: `SELECT n.nspname, c.relname, NULL::text, NULL::text, NULL::text
		      FROM pg_catalog.pg_class c
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE c.relkind = 'm' AND n.nspname = ANY($1)`,
	},
	{
		// `prokind` separa funciones de procedimientos desde PostgreSQL 11, y
		// se excluyen las de agregado y las de ventana porque no son objetos
		// que alguien vaya a extrañar por su cuenta.
		kind: schema.ObjFunction,
		que:  "las funciones",
		sql: `SELECT n.nspname, p.proname, NULL::text,
		             pg_catalog.pg_get_function_identity_arguments(p.oid), NULL::text
		      FROM pg_catalog.pg_proc p
		      JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		      WHERE p.prokind = 'f' AND n.nspname = ANY($1)
		        AND NOT EXISTS (
		          SELECT 1 FROM pg_catalog.pg_depend d
		          WHERE d.objid = p.oid AND d.deptype = 'e')`,
	},
	{
		kind: schema.ObjProcedure,
		que:  "los procedimientos",
		sql: `SELECT n.nspname, p.proname, NULL::text,
		             pg_catalog.pg_get_function_identity_arguments(p.oid), NULL::text
		      FROM pg_catalog.pg_proc p
		      JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		      WHERE p.prokind = 'p' AND n.nspname = ANY($1)
		        AND NOT EXISTS (
		          SELECT 1 FROM pg_catalog.pg_depend d
		          WHERE d.objid = p.oid AND d.deptype = 'e')`,
	},
	{
		// `tgisinternal` saca los triggers que Postgres crea solo para hacer
		// cumplir una clave foránea: esos SÍ están cubiertos, porque la clave
		// se renderiza.
		kind: schema.ObjTrigger,
		que:  "los triggers",
		sql: `SELECT n.nspname, t.tgname, c.relname, NULL::text, NULL::text
		      FROM pg_catalog.pg_trigger t
		      JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE NOT t.tgisinternal AND n.nspname = ANY($1)`,
	},
	{
		kind: schema.ObjPolicy,
		que:  "las políticas de RLS",
		sql: `SELECT n.nspname, p.polname, c.relname, NULL::text, NULL::text
		      FROM pg_catalog.pg_policy p
		      JOIN pg_catalog.pg_class c ON c.oid = p.polrelid
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE n.nspname = ANY($1)`,
	},
	{
		// Enums, dominios y compuestos. Se excluyen los tipos de fila que
		// Postgres crea solo para cada tabla —`relkind` de la clase asociada—
		// porque no son objetos propios.
		que: "los tipos",
		sql: `SELECT n.nspname, t.typname, NULL::text, NULL::text,
		             CASE t.typtype WHEN 'e' THEN 'enum'
		                            WHEN 'd' THEN 'domain'
		                            ELSE 'composite' END
		      FROM pg_catalog.pg_type t
		      JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		      WHERE n.nspname = ANY($1)
		        AND t.typtype IN ('e', 'd', 'c')
		        AND NOT EXISTS (
		          SELECT 1 FROM pg_catalog.pg_class c
		          WHERE c.oid = t.typrelid AND c.relkind <> 'c')
		        AND NOT EXISTS (
		          SELECT 1 FROM pg_catalog.pg_depend d
		          WHERE d.objid = t.oid AND d.deptype = 'e')`,
	},
	{
		// Solo las secuencias INDEPENDIENTES: la de una columna `serial` o
		// `identity` viene con la columna, que sí se renderiza.
		kind: schema.ObjSequence,
		que:  "las secuencias",
		sql: `SELECT n.nspname, c.relname, NULL::text, NULL::text, NULL::text
		      FROM pg_catalog.pg_class c
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE c.relkind = 'S' AND n.nspname = ANY($1)
		        AND NOT EXISTS (
		          SELECT 1 FROM pg_catalog.pg_depend d
		          WHERE d.objid = c.oid AND d.deptype IN ('a', 'i'))`,
	},
}

// AutoIncrement escribe una columna que se numera sola.
//
// Postgres tiene dos formas y las dos entran en el tipo de la columna, así que
// el renderizador del changeset las escribe sin saber que existen:
//
//   - `serial`: llega como `integer` con un default `nextval('…')`. Se escribe
//     como `serial`, y Postgres crea la secuencia solo. Escribir el default
//     tal cual —que es lo que se hacía— deja un `nextval` apuntando a una
//     secuencia que el archivo nunca crea: el volcado no se puede correr.
//   - `identity`: `GENERATED ALWAYS AS IDENTITY` va pegado al tipo y es válido
//     adentro de la definición de la columna.
//
// DumpHints es lo que los INSERT de un volcado necesitan en Postgres.
//
//   - `OVERRIDING SYSTEM VALUE` si alguna columna es identity ALWAYS: sin eso
//     el servidor rechaza el valor explícito (SQLSTATE 428C9).
//   - Un `setval` por columna que se numera sola —identity o serial—, con el
//     máximo que quedó: sin eso el primer INSERT sin id sobre la base
//     restaurada choca con una clave que ya existe, una vez por fila vieja.
//     `pg_get_serial_sequence` resuelve el nombre de la secuencia en la base
//     destino, que es donde el archivo se corre; el nombre de la tabla va
//     citado adentro del literal porque esa función lo parsea como
//     identificador, y el de la columna tal cual porque lo toma literal.
//
// Hallazgo C-05 de la auditoría del 2026-09-11.
func DumpHints(d schema.TableDetail) engine.DumpHints {
	var h engine.DumpHints
	tabla := QualifiedName(d.Schema, d.Name)
	for _, col := range d.Columns {
		if col.Identity == "always" {
			h.InsertModifier = "OVERRIDING SYSTEM VALUE"
		}
		if _, numerada := AutoIncrement(col); !numerada {
			continue
		}
		h.AfterData = append(h.AfterData, fmt.Sprintf(
			"SELECT setval(pg_get_serial_sequence(%s, %s), COALESCE(max(%s), 1), max(%s) IS NOT NULL) FROM %s",
			dialectoDML.QuoteLiteral(tabla), dialectoDML.QuoteLiteral(col.Name), QuoteIdent(col.Name), QuoteIdent(col.Name), tabla))
	}
	return h
}

func AutoIncrement(col schema.DetailColumn) (string, bool) {
	switch col.Identity {
	case "always":
		return col.DataType + " GENERATED ALWAYS AS IDENTITY", true
	case "by default":
		return col.DataType + " GENERATED BY DEFAULT AS IDENTITY", true
	}
	if !strings.HasPrefix(col.Default, "nextval(") {
		return "", false
	}
	switch strings.ToLower(col.DataType) {
	case "smallint", "int2":
		return "smallserial", true
	case "bigint", "int8":
		return "bigserial", true
	default:
		return "serial", true
	}
}
