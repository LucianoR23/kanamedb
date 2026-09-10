package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// ErrSinDefinicion es que Kaname no sabe reconstruir ese tipo de objeto.
var ErrSinDefinicion = errors.New("sin definición")

// Definition devuelve la definición de un objeto como un CREATE completo.
//
// SQLite es el caso más honesto de los tres: `sqlite_master.sql` guarda el
// texto EXACTO con el que se creó el objeto, comentarios y saltos de línea
// incluidos. No hay nada que normalizar ni que reconstruir, y el editor muestra
// lo que la persona escribió y no lo que un catálogo dedujo.
//
// A cambio, un objeto creado sin `IF NOT EXISTS` vuelve sin `IF NOT EXISTS`, y
// uno escrito con el nombre sin comillas vuelve así. Es lo correcto: reescribir
// el texto para uniformarlo sería cambiar el objeto de la persona por otro
// equivalente, y el editor dejaría de mostrar lo que hay.
//
// El nombre viaja como PARÁMETRO —es un valor en una consulta a una tabla, no
// un identificador— así que acá no hay concatenación de ninguna clase.
func Definition(ctx context.Context, db *sql.DB, o schema.Object) (schema.ObjectDefinition, error) {
	def := schema.ObjectDefinition{Object: o}

	var tipo string
	switch o.Kind {
	case schema.ObjView:
		tipo = "view"
	case schema.ObjTrigger:
		tipo = "trigger"
	default:
		return def, fmt.Errorf("%w: SQLite no tiene %s", ErrSinDefinicion, o.Kind)
	}

	var texto sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = ? AND name = ?`,
		tipo, o.Name).Scan(&texto)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return def, fmt.Errorf("%s ya no está en el archivo", o.Completo())
	case err != nil:
		return def, fmt.Errorf("leer la definición de %s: %w", o.Completo(), err)
	case !texto.Valid:
		// `sqlite_master.sql` es NULL en los objetos que crea el propio motor.
		// No debería llegar acá —esos se filtran al listar— pero devolver una
		// cadena vacía dejaría el editor en blanco, y guardar eso borraría el
		// objeto.
		return def, fmt.Errorf("%s no tiene definición guardada", o.Completo())
	}
	def.SQL = texto.String
	return def, nil
}
