// Package layout guarda dónde quedó cada tabla en el diagrama ERD.
//
// Va junto al archivo de conexiones y no en el directorio de estado por la misma
// razón por la que ese archivo está donde está: acomodar cuarenta tablas es
// trabajo, y quien sincroniza sus conexiones entre máquinas con Syncthing no
// quiere volver a hacerlo del otro lado.
//
// No guarda nada de la base, solo coordenadas: los nombres de tabla ya están
// implícitos en la conexión, y nunca se escribe nada del lado del servidor.
package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ErrIDInvalido lo devuelven Get y Save cuando el identificador de conexión no
// puede ser un nombre de archivo.
var ErrIDInvalido = errors.New("identificador de conexión inválido")

// maxTablas es el tope de posiciones que se guardan por esquema.
//
// No es una limitación del diagrama sino del archivo: sin tope, un esquema
// enorme —o un error del lado de la interfaz— escribiría megabytes en cada
// arrastre. Mil tablas en un diagrama ya es ilegible mucho antes del límite.
const maxTablas = 1000

const versionActual = 1

// Position es dónde quedó una tabla, en coordenadas del lienzo.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Positions mapea "esquema.tabla" a su posición.
type Positions map[string]Position

// archivo es la forma del JSON en disco.
type archivo struct {
	Version int                  `json:"version"`
	Schemas map[string]Positions `json:"schemas"`
}

// Store lee y escribe los diagramas guardados, uno por conexión.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// New construye un Store sobre el directorio indicado. No toca el disco.
func New(dir string) *Store { return &Store{dir: dir} }

// Dir devuelve el directorio donde se guardan los diagramas.
func (s *Store) Dir() string { return s.dir }

// Get devuelve las posiciones guardadas de un esquema.
//
// Que no haya archivo no es un error: nunca haber abierto el diagrama es el
// estado normal la primera vez, y quien llama acomoda solo.
func (s *Store) Get(conexion, esquema string) (Positions, error) {
	ruta, err := s.ruta(conexion)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	f, err := leer(ruta)
	if err != nil {
		return nil, err
	}
	if p, ok := f.Schemas[esquema]; ok {
		return p, nil
	}
	return Positions{}, nil
}

// Save reemplaza las posiciones de un esquema, dejando intactas las de los
// demás esquemas de la misma conexión.
//
// Guardar un mapa vacío borra el acomodado de ese esquema, que es lo que hace
// falta para volver al automático.
func (s *Store) Save(conexion, esquema string, p Positions) error {
	ruta, err := s.ruta(conexion)
	if err != nil {
		return err
	}
	if len(p) > maxTablas {
		return fmt.Errorf("el diagrama tiene %d tablas y el máximo es %d", len(p), maxTablas)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Leer y reescribir el archivo entero: son unos pocos kilobytes y así los
	// otros esquemas de la misma conexión no se pierden.
	f, err := leer(ruta)
	if err != nil {
		return err
	}
	if f.Schemas == nil {
		f.Schemas = map[string]Positions{}
	}
	if len(p) == 0 {
		delete(f.Schemas, esquema)
	} else {
		f.Schemas[esquema] = p
	}
	return escribir(ruta, f)
}

// Forget borra todo el diagrama de una conexión. Se usa al eliminarla: dejar el
// archivo huérfano acumula basura que nadie va a limpiar nunca.
func (s *Store) Forget(conexion string) error {
	ruta, err := s.ruta(conexion)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(ruta); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("borrar %s: %w", ruta, err)
	}
	return nil
}

// ruta arma el nombre de archivo de una conexión, verificando que el
// identificador no pueda salirse del directorio.
//
// El identificador llega desde el proceso de la interfaz, así que se valida acá
// aunque hoy lo genere `connection.NewID`: un `../../..` en ese lugar escribiría
// donde quisiera. Se exige el mismo alfabeto que produce NewID —hexadecimal— más
// guiones, por si el formato del identificador cambia.
func (s *Store) ruta(conexion string) (string, error) {
	if conexion == "" || len(conexion) > 64 {
		return "", fmt.Errorf("%w: %q", ErrIDInvalido, conexion)
	}
	for _, r := range conexion {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return "", fmt.Errorf("%w: %q", ErrIDInvalido, conexion)
		}
	}
	return filepath.Join(s.dir, conexion+".json"), nil
}

func leer(ruta string) (*archivo, error) {
	datos, err := os.ReadFile(ruta)
	if errors.Is(err, os.ErrNotExist) {
		return &archivo{Version: versionActual, Schemas: map[string]Positions{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leer %s: %w", ruta, err)
	}
	var f archivo
	if err := json.Unmarshal(datos, &f); err != nil {
		// Un archivo corrupto no puede tirar abajo el diagrama: se empieza de
		// cero y el primer guardado lo reemplaza. Perder el acomodado molesta;
		// no poder abrir el ERD, más.
		return &archivo{Version: versionActual, Schemas: map[string]Positions{}}, nil
	}
	if f.Schemas == nil {
		f.Schemas = map[string]Positions{}
	}
	return &f, nil
}

// escribir deja el archivo en disco de una sola vez.
//
// Es el mismo procedimiento que usa el archivo de conexiones: temporal, sync y
// rename. Un arrastre interrumpido no puede dejar un JSON a medias que después
// no se pueda leer.
func escribir(ruta string, f *archivo) error {
	f.Version = versionActual

	dir := filepath.Dir(ruta)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("crear %s: %w", dir, err)
	}

	datos, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("serializar el diagrama: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".layout-*.json")
	if err != nil {
		return fmt.Errorf("crear archivo temporal en %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(datos); err != nil {
		tmp.Close()
		return fmt.Errorf("escribir %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sincronizar %s a disco: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cerrar %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, ruta); err != nil {
		return fmt.Errorf("reemplazar %s: %w", ruta, err)
	}
	return nil
}
