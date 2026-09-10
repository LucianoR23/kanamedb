package sqlite

import (
	"context"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Dependents dice que en SQLite no se puede saber.
//
// SQLite no guarda dependencias entre objetos: `sqlite_master` tiene el TEXTO
// con el que se creó cada uno y nada más. Se podría buscar el nombre de la
// vista adentro del texto de las otras, y sería adivinar: encontraría una
// coincidencia en un comentario o en una cadena, y se perdería una vista que la
// usa con otro alias.
//
// Devolver una lista vacía sería peor que no contestar, porque una lista vacía
// significa «no depende nada de esto» y es exactamente lo que alguien mira
// antes de apretar un botón destructivo.
func Dependents(_ context.Context, o schema.Object) (schema.Dependents, error) {
	return schema.Dependents{
		Unknown: true,
		Reason:  "SQLite no guarda dependencias entre objetos: el catálogo solo tiene el texto con el que se creó cada uno.",
	}, nil
}
