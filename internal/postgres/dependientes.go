package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Dependents lista lo que se rompe si este objeto deja de existir.
//
// Postgres es el único de los cuatro motores que lleva el registro de verdad:
// `pg_depend` guarda cada dependencia entre objetos del catálogo. Solo se
// cuentan las NORMALES (`deptype = 'n'`), que son las que un DROP hace fallar y
// un DROP CASCADE arrastra; las automáticas y las internas —la secuencia de un
// `serial`, el índice de una restricción— son parte del objeto que las creó.
//
// La consulta tiene tres ramas y las tres hacen falta, porque `pg_depend` no
// apunta al objeto que a uno le interesa sino al que lo implementa:
//
//  1. Una vista no depende de sus tablas: depende a través de su regla
//     `_RETURN`, en `pg_rewrite`. Sin ese salto, una vista que usa a otra no
//     aparece.
//  2. Un trigger sí está directo, y hay que traer también SU TABLA: los nombres
//     de trigger son únicos por tabla y no por esquema, así que dos `auditar`
//     sobre tablas distintas son indistinguibles sin ella — y la tabla es
//     justamente lo que hace falta para ir a arreglarlo.
//  3. Todo lo demás. Una columna tipada con un enum se registra como una fila
//     de `pg_class` con `objsubid`, y el default `nextval(…)` de una columna
//     como una de `pg_attrdef`: ninguna de las dos entra por las ramas de
//     arriba. Sin esta tercera, un enum que usan cinco tablas contestaba «no
//     depende nada de esto» — la mentira exacta que este tipo existe para
//     impedir. `pg_identify_object` los nombra a todos, sea lo que sea.
func Dependents(ctx context.Context, pool *pgxpool.Pool, o schema.Object) (schema.Dependents, error) {
	var out schema.Dependents

	// El objeto se busca por su clase: una vista y una función viven en
	// catálogos distintos, y el oid de una no sirve para la otra. El catálogo
	// va como parámetro a la consulta: sin filtrar por `refclassid`, un
	// `refobjid` que coincida por número con otro catálogo produce un
	// dependiente fantasma. Los oids son únicos por catálogo, no globalmente.
	var oid uint32
	var catalogo string
	var err error
	switch o.Kind {
	case schema.ObjView, schema.ObjMatView, schema.ObjSequence:
		catalogo = "pg_class"
		err = pool.QueryRow(ctx, `
			SELECT c.oid FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2`, o.Schema, o.Name).Scan(&oid)
	case schema.ObjFunction, schema.ObjProcedure:
		catalogo = "pg_proc"
		err = pool.QueryRow(ctx, `
			SELECT p.oid FROM pg_catalog.pg_proc p
			JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = $1 AND p.proname = $2
			  AND ($3 = '' OR pg_catalog.pg_get_function_identity_arguments(p.oid) = $3)
			LIMIT 1`, o.Schema, o.Name, o.Args).Scan(&oid)
	case schema.ObjEnum, schema.ObjDomain, schema.ObjComposite:
		catalogo = "pg_type"
		err = pool.QueryRow(ctx, `
			SELECT t.oid FROM pg_catalog.pg_type t
			JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
			WHERE n.nspname = $1 AND t.typname = $2`, o.Schema, o.Name).Scan(&oid)
	case schema.ObjTrigger:
		// Un trigger es el único caso de «vacío y seguro»: nada puede colgar de
		// él. Decirlo como «no se sabe» sería asustar sin motivo.
		return out, nil
	default:
		// Todo lo demás —una política, una extensión, y lo que aparezca
		// mañana— NO se contesta con una lista vacía. `DROP EXTENSION postgis`
		// arrastra cientos de objetos, y decir «no depende nada» ahí es la
		// respuesta más cara que esta función puede dar.
		out.Unknown = true
		out.Reason = fmt.Sprintf(
			"Kaname todavía no sabe buscar de qué cuelga %s.", etiquetaDeTipo(o.Kind))
		return out, nil
	}
	if err != nil {
		return out, envolver(err, o)
	}

	const q = `
		SELECT n.nspname, c.relname,
		       CASE c.relkind WHEN 'm' THEN 'materializedView'
		                      WHEN 'v' THEN 'view'
		                      ELSE 'table' END,
		       ''
		FROM pg_catalog.pg_depend d
		JOIN pg_catalog.pg_rewrite r ON r.oid = d.objid
		JOIN pg_catalog.pg_class c ON c.oid = r.ev_class
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE d.refobjid = $1 AND d.refclassid = $2::regclass
		  AND d.classid = 'pg_catalog.pg_rewrite'::regclass
		  AND d.deptype = 'n' AND c.oid <> $1

		UNION

		SELECT n.nspname, t.tgname, 'trigger', c.relname
		FROM pg_catalog.pg_depend d
		JOIN pg_catalog.pg_trigger t ON t.oid = d.objid
		JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE d.refobjid = $1 AND d.refclassid = $2::regclass
		  AND d.classid = 'pg_catalog.pg_trigger'::regclass
		  AND d.deptype = 'n' AND NOT t.tgisinternal

		UNION

		SELECT '', COALESCE(io.identity, io.name, '?'), io.type, ''
		FROM pg_catalog.pg_depend d
		CROSS JOIN LATERAL pg_catalog.pg_identify_object(d.classid, d.objid, d.objsubid) io
		WHERE d.refobjid = $1 AND d.refclassid = $2::regclass
		  AND d.classid NOT IN ('pg_catalog.pg_rewrite'::regclass,
		                        'pg_catalog.pg_trigger'::regclass)
		  AND d.deptype = 'n'
		  AND d.classid NOT IN ('pg_catalog.pg_rewrite'::regclass,
		                        'pg_catalog.pg_trigger'::regclass)
		  AND d.deptype = 'n'`

	filas, err := pool.Query(ctx, q, oid, catalogo)
	if err != nil {
		return out, fmt.Errorf("leer las dependencias de %s: %w", o.Completo(), err)
	}
	defer filas.Close()

	for filas.Next() {
		var dep schema.Object
		var clase string
		if err := filas.Scan(&dep.Schema, &dep.Name, &clase, &dep.Table); err != nil {
			return out, fmt.Errorf("leer las dependencias de %s: %w", o.Completo(), err)
		}
		// La clase de la tercera rama es la que dice el catálogo —«table
		// column», «default value»— y se deja cruda. Traducirla a una de las
		// nuestras obligaría a mantener una tabla de equivalencias que se
		// desactualiza, y la de arriba tiene el mismo criterio que el resto del
		// proyecto: lo que no se conoce se muestra con su nombre, no se
		// esconde.
		dep.Kind = schema.ObjectKind(clase)
		out.Objects = append(out.Objects, dep)
	}
	if err := filas.Err(); err != nil {
		return out, fmt.Errorf("leer las dependencias de %s: %w", o.Completo(), err)
	}
	return out, nil
}
