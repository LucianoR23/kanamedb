package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Uncovered lista lo que hay en el archivo y el volcado de estructura NO sabe
// escribir. Ver el equivalente de Postgres por el porqué.
//
// SQLite tiene un solo catálogo —`sqlite_master`— y muchísimo menos que
// esconder: no hay funciones, ni procedimientos, ni políticas, ni tipos
// propios. Quedan las vistas y los triggers, y con eso la lista está completa.
// Los índices NO van: los renderiza el volcado.
//
// El esquema se ignora a propósito: en SQLite es siempre `main`, y filtrar por
// un nombre que el motor no tiene dejaría la lista vacía —y un archivo con
// vistas adentro se vería como uno que no las tiene—.
func Uncovered(ctx context.Context, db *sql.DB, _ []string) ([]schema.Object, error) {
	const q = `SELECT type, name, COALESCE(tbl_name, '')
	           FROM sqlite_master
	           WHERE type IN ('view', 'trigger')
	             AND name NOT LIKE 'sqlite_%'`
	filas, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("leer el catálogo: %w", err)
	}
	defer filas.Close()

	var out []schema.Object
	for filas.Next() {
		var tipo, nombre, tabla string
		if err := filas.Scan(&tipo, &nombre, &tabla); err != nil {
			return nil, fmt.Errorf("leer el catálogo: %w", err)
		}
		o := schema.Object{Schema: "main", Name: nombre}
		switch tipo {
		case "view":
			o.Kind = schema.ObjView
		case "trigger":
			o.Kind = schema.ObjTrigger
			// El nombre de la tabla solo tiene sentido en un trigger: en una
			// vista, `tbl_name` es la vista misma y repetirlo sería ruido.
			o.Table = tabla
		default:
			continue
		}
		out = append(out, o)
	}
	if err := filas.Err(); err != nil {
		return nil, fmt.Errorf("leer el catálogo: %w", err)
	}
	return out, nil
}
