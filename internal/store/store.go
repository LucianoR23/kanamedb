// Package store persiste la lista de conexiones en un archivo TOML.
//
// El archivo no contiene secretos a propósito: está pensado para sincronizarse
// entre máquinas con Syncthing o un repo git privado. Las contraseñas viven en
// el keychain de cada máquina. Ver CLAUDE.md.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/LucianoR23/kanamedb/internal/connection"
)

// ErrNotFound lo devuelven Get, Update y Delete cuando el ID no existe.
var ErrNotFound = errors.New("la conexión no existe")

// ErrDuplicateID lo devuelve Add cuando el ID ya está en uso.
var ErrDuplicateID = errors.New("ya existe una conexión con ese id")

// file es la forma del archivo en disco. Va en su propio tipo para que el
// formato del archivo pueda cambiar sin arrastrar al modelo de dominio.
type file struct {
	// Version permite migrar el formato más adelante sin adivinar.
	Version     int                     `toml:"version"`
	Connections []connection.Connection `toml:"connection"`
}

const currentVersion = 1

// Store lee y escribe el archivo de conexiones.
//
// Es seguro para uso concurrente: los servicios de Wails corren en goroutines
// distintas y varias ventanas pueden tocar la lista a la vez.
type Store struct {
	path string
	mu   sync.RWMutex
}

// New construye un Store sobre el archivo indicado. No toca el disco todavía.
func New(path string) *Store {
	return &Store{path: path}
}

// Path devuelve la ruta del archivo.
func (s *Store) Path() string { return s.path }

// List devuelve todas las conexiones, ordenadas por nombre.
//
// Si el archivo no existe devuelve una lista vacía y ningún error: no haber
// configurado nada todavía es un estado normal, no una falla.
func (s *Store) List() ([]connection.Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, err := s.read()
	if err != nil {
		return nil, err
	}
	sort.Slice(f.Connections, func(i, j int) bool {
		a, b := f.Connections[i], f.Connections[j]
		if x := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); x != 0 {
			return x < 0
		}
		return a.ID < b.ID
	})
	return f.Connections, nil
}

// Get devuelve una conexión por ID.
func (s *Store) Get(id string) (connection.Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, err := s.read()
	if err != nil {
		return connection.Connection{}, err
	}
	for _, c := range f.Connections {
		if c.ID == id {
			return c, nil
		}
	}
	return connection.Connection{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Add agrega una conexión nueva. Falla si el ID ya existe.
func (s *Store) Add(c connection.Connection) error {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return err
	}
	for _, existing := range f.Connections {
		if existing.ID == c.ID {
			return fmt.Errorf("%w: %s", ErrDuplicateID, c.ID)
		}
	}
	f.Connections = append(f.Connections, c)
	return s.write(f)
}

// Update reemplaza una conexión existente, identificada por su ID.
func (s *Store) Update(c connection.Connection) error {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return err
	}
	for i, existing := range f.Connections {
		if existing.ID == c.ID {
			f.Connections[i] = c
			return s.write(f)
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, c.ID)
}

// Delete borra una conexión. No toca el keychain: quien llama decide qué hacer
// con la contraseña, porque borrarla es irreversible.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return err
	}
	for i, c := range f.Connections {
		if c.ID == id {
			f.Connections = append(f.Connections[:i], f.Connections[i+1:]...)
			return s.write(f)
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

// read carga el archivo. Un archivo ausente equivale a una lista vacía.
func (s *Store) read() (*file, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return &file{Version: currentVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leer %s: %w", s.path, err)
	}

	var f file
	if _, err := toml.Decode(string(data), &f); err != nil {
		// El mensaje no incluye el contenido del archivo: aunque no debería
		// haber secretos ahí, un error no es lugar para volcarlo.
		return nil, fmt.Errorf("el archivo de conexiones %s está corrupto: %w", s.path, err)
	}
	if f.Version > currentVersion {
		return nil, fmt.Errorf(
			"el archivo de conexiones %s es de la versión %d y esta build entiende hasta la %d: actualizá Kaname",
			s.path, f.Version, currentVersion)
	}

	// El archivo se edita a mano, así que se normaliza al leer: sin esto, un
	// `engine = " Postgres"` o un `ssl_mode` ausente llegan crudos a la UI y a
	// Warnings, que entonces avisan mal o no avisan.
	vistos := make(map[string]int, len(f.Connections))
	for i := range f.Connections {
		f.Connections[i] = f.Connections[i].Normalize()

		id := f.Connections[i].ID
		if id == "" {
			return nil, fmt.Errorf(
				"el archivo de conexiones %s tiene una entrada sin id (%q): el id es la clave de la contraseña en el keychain",
				s.path, f.Connections[i].Name)
		}
		if prev, dup := vistos[id]; dup {
			return nil, fmt.Errorf(
				"el archivo de conexiones %s repite el id %q en %q y %q: editar o borrar una afectaría a la otra",
				s.path, id, f.Connections[prev].Name, f.Connections[i].Name)
		}
		vistos[id] = i
	}
	return &f, nil
}

// write guarda el archivo de forma atómica: escribe a un temporal en el mismo
// directorio y lo renombra encima. Sin esto, un corte a mitad de escritura deja
// el archivo truncado y el usuario pierde todas sus conexiones.
func (s *Store) write(f *file) error {
	f.Version = currentVersion

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("crear %s: %w", dir, err)
	}

	var buf strings.Builder
	buf.WriteString("# Conexiones de Kaname.\n")
	buf.WriteString("# Este archivo NO contiene contraseñas: viven en el keychain del sistema.\n")
	buf.WriteString("# Se puede editar a mano y sincronizar entre máquinas.\n\n")
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("serializar las conexiones: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".connections-*.toml")
	if err != nil {
		return fmt.Errorf("crear archivo temporal en %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Si algo falla de acá en adelante, no dejamos basura en el directorio.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(buf.String()); err != nil {
		tmp.Close()
		return fmt.Errorf("escribir %s: %w", tmpName, err)
	}
	// Sync antes del rename: el rename puede completarse y el contenido seguir
	// en el caché del sistema si se corta la luz justo ahí.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sincronizar %s a disco: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cerrar %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("ajustar permisos de %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("reemplazar %s: %w", s.path, err)
	}
	return nil
}
