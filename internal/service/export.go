package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// Exports formatea y guarda lo que sale de la aplicación.
//
// Son dos caminos con el mismo escritor. El resultado del editor ya está en
// memoria del lado de la interfaz y vuelve entero para formatearse acá:
// formateo puro, sin consulta. Una TABLA no vuelve por el puente —dos millones
// de filas no caben en un mensaje— y se lee del motor a medida que se escribe
// al archivo, con `engine.Conn.Scan`.
//
// Se apoya en Queries y no solo en Session por el registro de cancelaciones:
// una exportación larga ES una consulta corriendo, y «Cancelar» tiene que
// poder cortarla con el mismo runID que corta cualquier otra.
type Exports struct {
	queries *Queries
}

func NewExports(q *Queries) *Exports { return &Exports{queries: q} }

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

// Save escribe el resultado del editor en path.
func (e *Exports) Save(r ResultExport, path string) (SaveInfo, error) {
	return e.guardar(path, func(w io.Writer) (int, error) {
		return export.Write(r.Format, w, r.Options, r.Columns, r.Rows, 0)
	})
}

// guardar escribe en un archivo temporal del mismo directorio y lo renombra al
// final.
//
// Un fallo a mitad de camino —disco lleno, permiso, el servidor que corta el
// recorrido— no deja un archivo cortado con el nombre que la persona eligió,
// que se vería igual que uno entero. Con una tabla grande esto no es teórico:
// el archivo se escribe durante minutos y cualquier cosa puede pasar en el
// medio.
func (e *Exports) guardar(path string, escribir func(io.Writer) (int, error)) (SaveInfo, error) {
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

	n, err := escribir(tmp)
	if err != nil {
		tmp.Close()
		return SaveInfo{}, err
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

/* -------------------------------------------------------------- una tabla */

// TableExport es una tabla que se exporta entera.
//
// No trae las filas: las lee Go del motor. Eso es lo que separa este camino del
// del editor —una tabla puede no entrar en memoria— y por eso la interfaz solo
// dice CUÁL tabla, no QUÉ filas.
type TableExport struct {
	// RunID lo elige la interfaz y sirve para cancelar esta exportación y no
	// otra, con el mismo Queries.Cancel que corta una consulta.
	RunID  string `json:"runId"`
	Schema string `json:"schema"`
	Table  string `json:"table"`

	Format  export.Format  `json:"format"`
	Options export.Options `json:"options"`

	// OrderBy vacío deja que el motor devuelva las filas en el orden que
	// quiera, que para un archivo completo da lo mismo y es más rápido. Se
	// ordena por la clave primaria cuando el archivo tiene que ser comparable
	// entre dos corridas.
	OrderBy    []string `json:"orderBy"`
	Descending bool     `json:"descending"`

	// Where es el filtro que está puesto en la grilla. Exportar «lo que estoy
	// mirando» es exportar la tabla con el mismo filtro, y sin límite.
	Where []query.Condition `json:"where"`
}

// PreviewTable devuelve el texto de las primeras filas de una tabla.
//
// Lee con el mismo Scan que la exportación y corta apenas tiene las filas que
// necesita: así lo que se ve en la vista previa sale del mismo camino que lo
// que se va a escribir, y no de una lectura parecida.
func (e *Exports) PreviewTable(ctx context.Context, r TableExport, limit int) (string, error) {
	if limit <= 0 {
		return "", errors.New("la vista previa necesita un límite de filas")
	}
	var b strings.Builder
	opts := r.Options
	// El gzip se ignora al renderizar: el texto es para leerlo.
	opts.Gzip = false
	if _, err := e.volcar(ctx, r, opts, &b, limit); err != nil {
		return "", err
	}
	return b.String(), nil
}

// SaveTable escribe una tabla entera en path, leyéndola a medida que la escribe.
func (e *Exports) SaveTable(ctx context.Context, r TableExport, path string) (SaveInfo, error) {
	return e.guardar(path, func(w io.Writer) (int, error) {
		return e.volcar(ctx, r, r.Options, w, 0)
	})
}

// volcar lee la tabla y la escribe. Con limit mayor que cero corta ahí.
func (e *Exports) volcar(
	ctx context.Context, r TableExport, opts export.Options, w io.Writer, limit int,
) (int, error) {
	sesion, err := e.queries.session.abierta()
	if err != nil {
		return 0, err
	}
	ctx, listo := e.queries.registrar(ctx, r.RunID)
	defer listo()

	flujo, err := sesion.db.Scan(ctx, r.Schema, r.Table, engine.ScanOptions{
		OrderBy:    r.OrderBy,
		Descending: r.Descending,
		Where:      r.Where,
	})
	if err != nil {
		return 0, fmt.Errorf("leer %s: %w", nombreDeTabla(r.Schema, r.Table), err)
	}
	defer flujo.Close()

	esc, err := export.New(r.Format, w, opts)
	if err != nil {
		return 0, err
	}
	n, err := volcarFlujo(flujo, esc, limit)
	if err != nil {
		return n, fmt.Errorf("exportar %s: %w", nombreDeTabla(r.Schema, r.Table), err)
	}
	return n, nil
}

// volcarFlujo pasa las filas del recorrido al escritor. Con limit mayor que
// cero corta ahí, sin que eso sea un error: es lo que hace la vista previa.
//
// Está separado de volcar para poder probar con un recorrido que falla A LA
// MITAD, que es el caso que ninguna prueba contra un motor real puede provocar
// cuando quiera. Lo que protege es concreto: si el servidor corta —se cae la
// conexión, salta el statement_timeout— lo que hay escrito no es la tabla, y
// terminar el archivo como si lo fuera daría un archivo cortado idéntico a uno
// entero.
func volcarFlujo(flujo engine.RowStream, esc export.Writer, limit int) (int, error) {
	if err := esc.Begin(flujo.Columns()); err != nil {
		return 0, err
	}
	n := 0
	for flujo.Next() {
		if limit > 0 && n >= limit {
			break
		}
		if err := esc.Row(flujo.Row()); err != nil {
			return n, err
		}
		n++
	}
	// El error del recorrido se mira ANTES de cerrar el escritor.
	if err := flujo.Err(); err != nil {
		return n, err
	}
	if err := esc.End(); err != nil {
		return n, err
	}
	return n, nil
}

func nombreDeTabla(esquema, tabla string) string {
	if esquema == "" {
		return tabla
	}
	return esquema + "." + tabla
}
