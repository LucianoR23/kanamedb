package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// columnTypesQuery lista los tipos que se pueden usar como tipo de una columna.
//
// Se leen del catálogo y no de una lista escrita a mano, por la misma razón que
// todo lo demás en este paquete: los tipos de Postgres NO son un conjunto
// cerrado. Cada extensión agrega los suyos —citext, hstore, vector, los de
// PostGIS— y cada enum o dominio que alguien define es un tipo más. Una lista
// fija sería peor que dejar escribir a mano, porque no habría forma de elegir un
// tipo propio de esta base.
//
// Qué se deja afuera y por qué:
//
//   - typtype 'c' son los tipos compuestos, y casi todos son el tipo fila de una
//     tabla: listarlos llenaría el desplegable con una entrada por tabla.
//   - typtype 'p' son pseudotipos —anyelement, record, trigger—: no se pueden
//     usar como tipo de columna.
//   - Los arreglos se excluyen y se ofrecen con una casilla, porque si no cada
//     tipo aparecería dos veces.
//
// format_type devuelve el nombre SQL canónico: `integer` y no `int4`,
// `character varying` y no `varchar`. Es el que se lee en cualquier otra
// herramienta.
const columnTypesQuery = `
	SELECT n.nspname,
	       pg_catalog.format_type(t.oid, NULL),
	       t.typtype,
	       t.typmodin <> 0,
	       coalesce(obj_description(t.oid, 'pg_type'), '')
	FROM pg_catalog.pg_type t
	JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
	WHERE t.typisdefined
	  AND t.typtype IN ('b', 'e', 'd', 'r', 'm')
	  AND t.typcategory <> 'A'
	  AND n.nspname <> 'information_schema'
	  AND (n.nspname = 'pg_catalog' OR pg_catalog.has_schema_privilege(n.oid, 'USAGE'))
	  AND pg_catalog.has_type_privilege(t.oid, 'USAGE')
	ORDER BY n.nspname, 2`

// comunes son los tipos que se eligen casi siempre, en el orden en que conviene
// ofrecerlos.
//
// No es una lista de qué se permite —eso lo decide el catálogo— sino de qué va
// primero. Sin esto, `abstime` y `aclitem` aparecen antes que `text`, y elegir
// un tipo se convierte en buscar entre cuatrocientos.
var comunes = []string{
	"text",
	"bigint",
	"integer",
	"boolean",
	"numeric",
	"timestamp with time zone",
	"date",
	"uuid",
	"jsonb",
	"character varying",
	"smallint",
	"double precision",
	"bytea",
	"time with time zone",
	"timestamp without time zone",
	"interval",
	"inet",
}

// ColumnTypes lee del catálogo los tipos usables como tipo de columna.
func ColumnTypes(ctx context.Context, pool *pgxpool.Pool) ([]schema.TypeOption, error) {
	rows, err := pool.Query(ctx, columnTypesQuery)
	if err != nil {
		return nil, fmt.Errorf("listar los tipos: %w", err)
	}
	defer rows.Close()

	var out []schema.TypeOption
	for rows.Next() {
		var esquema, nombre, clase, comentario string
		var aceptaModificador bool
		if err := rows.Scan(&esquema, &nombre, &clase, &aceptaModificador, &comentario); err != nil {
			return nil, fmt.Errorf("leer un tipo: %w", err)
		}
		out = append(out, schema.TypeOption{
			Name:            nombre,
			Schema:          esquema,
			BuiltIn:         esquema == "pg_catalog",
			Kind:            claseDeTipo(clase),
			AcceptsModifier: aceptaModificador,
			Comment:         comentario,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer los tipos: %w", err)
	}

	ordenarTipos(out)
	return out, nil
}

func claseDeTipo(c string) string {
	switch c {
	case "e":
		return "enum"
	case "d":
		return "domain"
	case "r", "m":
		return "range"
	default:
		return "base"
	}
}

// ordenarTipos deja arriba lo que se usa: primero los comunes en su orden, y
// después todo lo demás alfabético, con los tipos propios de la base antes que
// los del sistema.
//
// Los propios van antes porque son los que nadie recuerda de memoria: un enum
// que se definió la semana pasada es exactamente lo que hay que poder encontrar
// sin escribirlo.
func ordenarTipos(ts []schema.TypeOption) {
	rango := map[string]int{}
	for i, n := range comunes {
		rango[n] = i
	}
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		ra, oka := rango[a.Name]
		rb, okb := rango[b.Name]
		if oka != okb {
			return oka
		}
		if oka && okb {
			return ra < rb
		}
		if a.BuiltIn != b.BuiltIn {
			return !a.BuiltIn
		}
		return a.Name < b.Name
	})
}
