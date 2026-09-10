package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Objects lista lo que hay en estas bases y el volcado de estructura NO sabe
// escribir. Ver el equivalente de Postgres por el porqué.
//
// En MySQL y MariaDB un «esquema» ES una base, así que lo que en Postgres se
// consulta contra `pg_namespace` acá va contra `TABLE_SCHEMA` de
// `information_schema`. Los eventos son propios de estos dos motores y por eso
// existe `ObjEvent`: sin él, un evento programado desaparecería del aviso.
func Objects(ctx context.Context, db *sql.DB, esquemas []string) ([]schema.Object, error) {
	if len(esquemas) == 0 {
		return nil, nil
	}
	marcadores := strings.TrimSuffix(strings.Repeat("?,", len(esquemas)), ",")
	args := make([]any, 0, len(esquemas))
	for _, e := range esquemas {
		args = append(args, e)
	}

	var out []schema.Object
	for _, q := range consultasDeCobertura {
		filas, err := db.QueryContext(ctx, fmt.Sprintf(q.sql, marcadores), args...)
		if err != nil {
			// `information_schema.EVENTS` no existe si el scheduler nunca se
			// habilitó en algunas variantes; se avisa igual en vez de tragarlo,
			// porque quedarse callado produce el archivo silencioso que esto
			// existe para evitar.
			return nil, fmt.Errorf("leer %s del catálogo: %w", q.que, err)
		}
		for filas.Next() {
			o := schema.Object{Kind: q.kind}
			var tabla sql.NullString
			if err := filas.Scan(&o.Schema, &o.Name, &tabla); err != nil {
				filas.Close()
				return nil, fmt.Errorf("leer %s del catálogo: %w", q.que, err)
			}
			o.Table = tabla.String
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

type consultaDeCobertura struct {
	kind schema.ObjectKind
	que  string
	// sql lleva UN `%s`, donde van los marcadores de los esquemas.
	sql string
}

var consultasDeCobertura = []consultaDeCobertura{
	{
		kind: schema.ObjView,
		que:  "las vistas",
		sql: `SELECT TABLE_SCHEMA, TABLE_NAME, NULL
		      FROM information_schema.VIEWS WHERE TABLE_SCHEMA IN (%s)`,
	},
	{
		kind: schema.ObjFunction,
		que:  "las funciones",
		sql: `SELECT ROUTINE_SCHEMA, ROUTINE_NAME, NULL
		      FROM information_schema.ROUTINES
		      WHERE ROUTINE_TYPE = 'FUNCTION' AND ROUTINE_SCHEMA IN (%s)`,
	},
	{
		kind: schema.ObjProcedure,
		que:  "los procedimientos",
		sql: `SELECT ROUTINE_SCHEMA, ROUTINE_NAME, NULL
		      FROM information_schema.ROUTINES
		      WHERE ROUTINE_TYPE = 'PROCEDURE' AND ROUTINE_SCHEMA IN (%s)`,
	},
	{
		kind: schema.ObjTrigger,
		que:  "los triggers",
		sql: `SELECT TRIGGER_SCHEMA, TRIGGER_NAME, EVENT_OBJECT_TABLE
		      FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA IN (%s)`,
	},
	{
		kind: schema.ObjEvent,
		que:  "los eventos",
		sql: `SELECT EVENT_SCHEMA, EVENT_NAME, NULL
		      FROM information_schema.EVENTS WHERE EVENT_SCHEMA IN (%s)`,
	},
}

// AutoIncrement escribe una columna que se numera sola.
//
// En MySQL y MariaDB `AUTO_INCREMENT` va pegado al tipo, adentro de la
// definición de la columna, y exige que la columna sea clave — que lo es,
// porque el volcado escribe la clave primaria adentro del mismo CREATE TABLE.
func AutoIncrement(col schema.DetailColumn) (string, bool) {
	if col.Identity == "" {
		return "", false
	}
	return col.DataType + " AUTO_INCREMENT", true
}
