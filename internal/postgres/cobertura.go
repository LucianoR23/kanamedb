package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Uncovered lista lo que hay en estos esquemas y el volcado de estructura NO
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
func Uncovered(ctx context.Context, pool *pgxpool.Pool, esquemas []string) ([]schema.Object, error) {
	if len(esquemas) == 0 {
		return nil, nil
	}
	var out []schema.Object

	for _, q := range consultasDeCobertura {
		filas, err := pool.Query(ctx, q.sql, esquemas)
		if err != nil {
			// Un catálogo que no se puede leer NO se traga: quedarse callado
			// acá produce exactamente el archivo silencioso que este código
			// existe para evitar.
			return nil, fmt.Errorf("leer %s del catálogo: %w", q.que, err)
		}
		for filas.Next() {
			o := schema.Object{Kind: q.kind}
			var tabla *string
			if err := filas.Scan(&o.Schema, &o.Name, &tabla); err != nil {
				filas.Close()
				return nil, fmt.Errorf("leer %s del catálogo: %w", q.que, err)
			}
			if tabla != nil {
				o.Table = *tabla
			}
			out = append(out, o)
		}
		if err := filas.Err(); err != nil {
			filas.Close()
			return nil, fmt.Errorf("leer %s del catálogo: %w", q.que, err)
		}
		filas.Close()
	}
	return out, nil
}

// consultaDeCobertura es una clase de objeto y cómo encontrarla. Las tres
// columnas son siempre las mismas —esquema, nombre, tabla o NULL— para que el
// lector sea uno solo.
type consultaDeCobertura struct {
	kind schema.ObjectKind
	que  string
	sql  string
}

var consultasDeCobertura = []consultaDeCobertura{
	{
		kind: schema.ObjView,
		que:  "las vistas",
		sql: `SELECT n.nspname, c.relname, NULL::text
		      FROM pg_catalog.pg_class c
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE c.relkind = 'v' AND n.nspname = ANY($1)`,
	},
	{
		kind: schema.ObjMatView,
		que:  "las vistas materializadas",
		sql: `SELECT n.nspname, c.relname, NULL::text
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
		sql: `SELECT n.nspname, p.proname, NULL::text
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
		sql: `SELECT n.nspname, p.proname, NULL::text
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
		sql: `SELECT n.nspname, t.tgname, c.relname
		      FROM pg_catalog.pg_trigger t
		      JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE NOT t.tgisinternal AND n.nspname = ANY($1)`,
	},
	{
		kind: schema.ObjPolicy,
		que:  "las políticas de RLS",
		sql: `SELECT n.nspname, p.polname, c.relname
		      FROM pg_catalog.pg_policy p
		      JOIN pg_catalog.pg_class c ON c.oid = p.polrelid
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE n.nspname = ANY($1)`,
	},
	{
		// Enums, dominios y compuestos. Se excluyen los tipos de fila que
		// Postgres crea solo para cada tabla —`relkind` de la clase asociada—
		// porque no son objetos propios.
		kind: schema.ObjType,
		que:  "los tipos",
		sql: `SELECT n.nspname, t.typname, NULL::text
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
		sql: `SELECT n.nspname, c.relname, NULL::text
		      FROM pg_catalog.pg_class c
		      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		      WHERE c.relkind = 'S' AND n.nspname = ANY($1)
		        AND NOT EXISTS (
		          SELECT 1 FROM pg_catalog.pg_depend d
		          WHERE d.objid = c.oid AND d.deptype IN ('a', 'i'))`,
	},
}
