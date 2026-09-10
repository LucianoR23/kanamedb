package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// Exports formatea y guarda lo que sale de la aplicación.
//
// Hoy es el resultado del editor, que ya está en memoria del lado de la
// interfaz y vuelve entero para formatearse acá: es formateo puro, sin
// consulta. S19 agrega las tablas, que se leen del motor a medida que se
// escriben, con los mismos escritores de `internal/export`.
type Exports struct{}

func NewExports() *Exports { return &Exports{} }

// ResultExport es un resultado del editor con el formato pedido.
type ResultExport struct {
	Format  export.Format  `json:"format"`
	Options export.Options `json:"options"`
	Columns []query.Column `json:"columns"`
	Rows    [][]*string    `json:"rows"`
}

// FormatInfo describe un formato para que la interfaz no tenga que saber
// las extensiones.
type FormatInfo struct {
	Key       export.Format `json:"key"`
	Extension string        `json:"extension"`
}

// Formats lista los formatos en el orden en que se ofrecen.
func (e *Exports) Formats() []FormatInfo {
	out := make([]FormatInfo, 0, len(export.Formats))
	for _, f := range export.Formats {
		out = append(out, FormatInfo{Key: f, Extension: f.Extension()})
	}
	return out
}

// Preview devuelve el texto de las primeras filas, sin comprimir.
func (e *Exports) Preview(r ResultExport, limit int) (string, error) {
	if limit <= 0 {
		return "", errors.New("la vista previa necesita un límite de filas")
	}
	return export.Render(r.Format, r.Options, r.Columns, r.Rows, limit)
}

// Render devuelve el texto entero, para el portapapeles.
func (e *Exports) Render(r ResultExport) (string, error) {
	return export.Render(r.Format, r.Options, r.Columns, r.Rows, 0)
}

// SaveInfo cuenta lo que quedó escrito.
type SaveInfo struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Rows  int    `json:"rows"`
}

// Save escribe el resultado en path.
//
// Escribe en un archivo temporal del mismo directorio y lo renombra al final:
// un fallo a mitad de camino —disco lleno, permiso— no deja un archivo cortado
// con el nombre que la persona eligió, que se vería igual que uno entero.
func (e *Exports) Save(r ResultExport, path string) (SaveInfo, error) {
	if path == "" {
		return SaveInfo{}, errors.New("falta la ruta del archivo")
	}
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "."+base+"-*")
	if err != nil {
		return SaveInfo{}, fmt.Errorf("no se pudo crear el archivo en %s: %w", dir, err)
	}
	// Si algo falla, el temporal se va. Después del rename ya no existe con
	// ese nombre y el Remove no hace nada.
	defer os.Remove(tmp.Name())

	n, err := export.Write(r.Format, tmp, r.Options, r.Columns, r.Rows, 0)
	if err != nil {
		tmp.Close()
		return SaveInfo{}, fmt.Errorf("no se pudo escribir %s: %w", base, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return SaveInfo{}, fmt.Errorf("no se pudo escribir %s: %w", base, err)
	}
	info, err := tmp.Stat()
	if err != nil {
		tmp.Close()
		return SaveInfo{}, fmt.Errorf("no se pudo escribir %s: %w", base, err)
	}
	if err := tmp.Close(); err != nil {
		return SaveInfo{}, fmt.Errorf("no se pudo escribir %s: %w", base, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return SaveInfo{}, fmt.Errorf("no se pudo reemplazar %s: %w", base, err)
	}
	return SaveInfo{Path: path, Bytes: info.Size(), Rows: n}, nil
}
