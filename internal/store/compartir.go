package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/LucianoR23/kanamedb/internal/connection"
)

// MaxSharedFileBytes es lo más que se lee de un archivo compartido. Una
// libreta grande son decenas de kilobytes; un megabyte no es una libreta, y
// el archivo lo eligió una persona en un selector: puede ser cualquier cosa.
const MaxSharedFileBytes = 1 << 20

// ErrSharedFileTooBig lo devuelve Decode cuando el archivo pasa el límite.
var ErrSharedFileTooBig = errors.New("el archivo es demasiado grande para ser una libreta de conexiones")

// codificar escribe el cuerpo del archivo. Es lo que comparten la libreta y
// un archivo exportado: el mismo formato, con distinto encabezado.
func codificar(buf *strings.Builder, f *file) error {
	if err := toml.NewEncoder(buf).Encode(f); err != nil {
		return fmt.Errorf("serializar las conexiones: %w", err)
	}
	return nil
}

// Encode escribe conexiones en el formato de la libreta, para compartirlas.
//
// Es el mismo formato que `connections.toml` a propósito: quien recibe el
// archivo puede importarlo desde el gestor o pegar sus entradas a mano en su
// libreta. Y por lo mismo no puede llevar un secreto: el modelo no tiene
// dónde ponerlo. Los IDs viajan tal cual —son aleatorios y no dicen nada—
// para que el archivo pegado a mano siga siendo una libreta válida; al
// importar se ignoran y cada conexión recibe uno nuevo.
func Encode(conns []connection.Connection, ahora time.Time) ([]byte, error) {
	var buf strings.Builder
	buf.WriteString("# Conexiones de Kaname, exportadas el " + ahora.Format("2006-01-02 15:04") + ".\n")
	buf.WriteString("# Sin contraseñas: se piden al conectar y quedan en el keychain de cada máquina.\n")
	buf.WriteString("# Se importa desde «Importar…» en el gestor de conexiones, o se pega en connections.toml.\n\n")
	if err := codificar(&buf, &file{Version: currentVersion, Connections: conns}); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// Decoded es lo que trae un archivo compartido.
type Decoded struct {
	// Connections vienen normalizadas pero SIN validar y con los IDs del
	// archivo: quien importa decide qué hacer con cada una, y el ID lo cambia.
	Connections []connection.Connection

	// Ignored son las claves del archivo que Kaname no conoce y no leyó, con su
	// ruta (`connection.password`). Se devuelven para decirlo: un archivo
	// escrito a mano con `password = "…"` no importa la contraseña, y la
	// persona tiene que enterarse de que no la importó, no descubrirlo al
	// conectar.
	Ignored []string
}

// Decode lee un archivo compartido.
//
// No es `read`: no exige IDs ni que sean únicos, porque al importar se
// reemplazan, y no falla por una conexión rota, porque la vista previa tiene
// que poder mostrarla con sus problemas.
func Decode(data []byte) (Decoded, error) {
	if len(data) > MaxSharedFileBytes {
		return Decoded{}, ErrSharedFileTooBig
	}
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		// Sin el contenido: aunque no debería haber secretos ahí, un error no
		// es lugar para volcar un archivo que eligió otra persona.
		return Decoded{}, fmt.Errorf("el archivo no es una libreta de conexiones válida: %w", err)
	}
	if f.Version > currentVersion {
		return Decoded{}, fmt.Errorf(
			"el archivo es de la versión %d de la libreta y esta build entiende hasta la %d: actualizá Kaname",
			f.Version, currentVersion)
	}

	out := Decoded{Connections: make([]connection.Connection, 0, len(f.Connections))}
	for _, c := range f.Connections {
		out.Connections = append(out.Connections, c.Normalize())
	}
	for _, k := range md.Undecoded() {
		out.Ignored = append(out.Ignored, k.String())
	}
	slices.Sort(out.Ignored)
	out.Ignored = slices.Compact(out.Ignored)
	return out, nil
}
