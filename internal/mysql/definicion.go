package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// ErrSinDefinicion es que Kaname no sabe reconstruir ese tipo de objeto.
var ErrSinDefinicion = errors.New("Kaname todavía no sabe leer esta clase de objeto")

// Definition devuelve la definición de un objeto como un CREATE completo.
//
// MySQL y MariaDB tienen la respuesta hecha: `SHOW CREATE …` devuelve el CREATE
// entero para las cinco clases de objeto que este motor sabe listar. No hay que
// normalizar nada.
//
// Lo que devuelve lleva el `DEFINER=usuario@host` y el `ALGORITHM` que el motor
// le puso, y se deja TAL CUAL. Sacarlos daría un texto más portable y sería
// mentir: el editor promete que lo que se ve es lo que se ejecuta, y una vista
// con DEFINER se comporta distinto de una sin él. Que ese usuario no exista en
// otro servidor es información que conviene ver antes de mover el objeto, no
// después.
//
// `SHOW CREATE` no acepta parámetros: el nombre va citado y concatenado, que es
// la excepción declarada para identificadores en CLAUDE.md.
func Definition(ctx context.Context, db *sql.DB, o schema.Object) (schema.ObjectDefinition, error) {
	def := schema.ObjectDefinition{Object: o}

	var que string
	switch o.Kind {
	case schema.ObjView:
		que = "VIEW"
	case schema.ObjFunction:
		que = "FUNCTION"
	case schema.ObjProcedure:
		que = "PROCEDURE"
	case schema.ObjTrigger:
		que = "TRIGGER"
	case schema.ObjEvent:
		que = "EVENT"
	default:
		return def, fmt.Errorf("este motor no tiene %s: %w", o.Kind, ErrSinDefinicion)
	}

	nombre := QualifiedName(o.Schema, o.Name)
	sqlTexto, err := showCreate(ctx, db, "SHOW CREATE "+que+" "+nombre)
	if err != nil {
		return def, fmt.Errorf("leer la definición de %s: %w", o.Completo(), err)
	}
	// `SHOW CREATE VIEW` devuelve `CREATE ALGORITHM=… VIEW`, sin OR REPLACE, y
	// correr eso sobre la vista que existe falla con «already exists». Como
	// esta definición ES la que el editor va a ejecutar, se muestra ya en la
	// forma re-ejecutable. Solo para vistas: las rutinas y los triggers de esta
	// familia no admiten OR REPLACE —MariaDB sí, pero ver la nota de
	// PuedeReemplazarEnElLugar—.
	if o.Kind == schema.ObjView {
		sqlTexto = change.ConOrReplace(sqlTexto)
	}
	def.SQL = sqlTexto
	return def, nil
}

// showCreate corre un `SHOW CREATE …` y saca la columna que trae el texto.
//
// La columna se busca POR NOMBRE y no por posición: `SHOW CREATE VIEW` devuelve
// cuatro columnas, `SHOW CREATE FUNCTION` seis y `SHOW CREATE TRIGGER` siete, y
// MariaDB no promete las mismas que MySQL ni entre sus propias versiones.
// Contar posiciones acá sería una constante que se rompe con una actualización
// del servidor, en silencio y devolviendo un `sql_mode` donde iba el cuerpo.
func showCreate(ctx context.Context, db *sql.DB, consulta string) (string, error) {
	filas, err := db.QueryContext(ctx, consulta)
	if err != nil {
		return "", err
	}
	defer filas.Close()

	cols, err := filas.Columns()
	if err != nil {
		return "", err
	}
	cual := -1
	for i, c := range cols {
		// «Create View», «Create Function»… y, en los triggers, «SQL Original
		// Statement», que es el mismo texto con otro nombre.
		if strings.HasPrefix(c, "Create ") || c == "SQL Original Statement" {
			cual = i
			break
		}
	}
	if cual < 0 {
		return "", fmt.Errorf("la respuesta no trae la definición, solo %s", strings.Join(cols, ", "))
	}

	if !filas.Next() {
		if err := filas.Err(); err != nil {
			return "", err
		}
		return "", errors.New("ya no está en la base")
	}
	destino := make([]any, len(cols))
	var texto sql.NullString
	descarte := make([]sql.RawBytes, len(cols))
	for i := range destino {
		if i == cual {
			destino[i] = &texto
			continue
		}
		destino[i] = &descarte[i]
	}
	if err := filas.Scan(destino...); err != nil {
		return "", err
	}
	if err := filas.Err(); err != nil {
		return "", err
	}
	if !texto.Valid {
		// Pasa cuando el usuario ve el objeto en el catálogo pero no tiene
		// permiso para ver su cuerpo: el motor devuelve NULL en vez de fallar.
		return "", errors.New("el servidor no devolvió la definición (¿falta permiso SHOW VIEW o el objeto es de otro usuario?)")
	}
	return texto.String, nil
}
