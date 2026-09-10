package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/csvimport"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Imports mete un archivo CSV en una tabla.
//
// Es el otro lado de Exports, y comparte con él la forma de trabajar: el
// archivo se lee de a una fila y no se junta en memoria, y los valores viajan
// SIEMPRE como parámetros. La conversión al tipo de la columna la hace el
// servidor: interpretar acá una fecha o un número sería inventar una regla que
// no es la del motor.
type Imports struct {
	queries *Queries
}

func NewImports(q *Queries) *Imports { return &Imports{queries: q} }

// Inspect mira el archivo sin tocar la base.
func (i *Imports) Inspect(path string, o csvimport.Options) (*csvimport.Inspection, error) {
	return csvimport.Inspect(path, o)
}

// OnConflict es qué hacer con una fila que choca con una que ya está.
type OnConflict string

const (
	// ConflictFail aborta la importación entera. Es el default porque es el
	// único que no pierde información en silencio: si hay choques, quien
	// importa se entera y decide.
	ConflictFail OnConflict = "fail"
	// ConflictSkip deja la fila que ya estaba y sigue. Es `ON CONFLICT DO
	// NOTHING` en Postgres y SQLite, e `INSERT IGNORE` en MySQL.
	ConflictSkip OnConflict = "skip"
)

// ImportPlan es una importación lista para correr.
type ImportPlan struct {
	RunID  string `json:"runId"`
	Path   string `json:"path"`
	Schema string `json:"schema"`
	Table  string `json:"table"`

	Options csvimport.Options `json:"options"`

	// Mapping dice a qué columna de la TABLA va cada columna del archivo, en
	// orden. La cadena vacía saltea esa columna del archivo.
	//
	// Es una lista y no un mapa porque el orden importa: lo que identifica a
	// una columna del CSV es su posición, y dos columnas del archivo se pueden
	// llamar igual.
	Mapping []string `json:"mapping"`

	OnConflict OnConflict `json:"onConflict"`
}

// ImportResult es cómo terminó.
type ImportResult struct {
	OK bool `json:"ok"`
	// Inserted es cuántas filas quedaron. Con «saltear», las que chocaron no
	// se cuentan acá.
	Inserted int64 `json:"inserted"`
	// Read es cuántas filas de datos tenía el archivo.
	Read      int   `json:"read"`
	ElapsedMs int64 `json:"elapsedMs"`

	// Failure es el fallo, si lo hubo. Nada quedó escrito: la importación va en
	// UNA transacción.
	Failure *engine.Failure `json:"failure,omitempty"`
	// Line es la línea del archivo donde estaba el problema, si se sabe.
	Line int `json:"line,omitempty"`
}

// filasPorLote es cuántas filas van en cada INSERT.
//
// Mil, como dice el diseño. Una por fila hace un viaje al servidor por fila;
// todas juntas se pasa del tamaño máximo de paquete y además del límite de
// parámetros —Postgres admite 65535 por sentencia, así que con veinte columnas
// el tope real está en 3276 filas—. Mil deja margen para tablas anchas.
const filasPorLote = 1000

// Run corre la importación entera adentro de UNA transacción.
//
// Todo o nada: si un lote falla, no queda ninguna fila. Es lo que el diálogo
// promete y lo que hace que se pueda intentar de nuevo sin mirar qué entró.
func (i *Imports) Run(ctx context.Context, p ImportPlan) ImportResult {
	return i.correr(ctx, p, false)
}

// DryRun hace exactamente lo mismo y revierte al final.
//
// Es el paso de validación del asistente, y responde lo que ninguna
// comprobación nuestra podría: si el SERVIDOR acepta estas filas. Comprobarlo
// del lado de Kaname —«esto parece una fecha»— sería inventar las reglas del
// motor y equivocarse justo en los casos raros.
func (i *Imports) DryRun(ctx context.Context, p ImportPlan) ImportResult {
	return i.correr(ctx, p, true)
}

func (i *Imports) correr(ctx context.Context, p ImportPlan, ensayo bool) ImportResult {
	arranque := time.Now()
	sesion, err := i.queries.session.abierta()
	if err != nil {
		return ImportResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}
	if sesion.conn.Safety.ReadOnly {
		return ImportResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "Esta conexión está abierta en modo solo lectura.",
			Hint:    "El interruptor está en el gestor de conexiones, en «Abrir en solo lectura».",
		}}
	}

	destino, err := columnasDestino(p.Mapping)
	if err != nil {
		return ImportResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}

	ctx, listo := i.queries.registrar(ctx, p.RunID)
	defer listo()

	tx, err := sesion.db.Begin(ctx, engine.TxOptions{})
	if err != nil {
		return ImportResult{Failure: sesion.db.ClassifyStatement(err, "abrir la transacción de la importación")}
	}
	// Un ensayo SIEMPRE revierte, haya salido bien o mal.
	commiteado := false
	defer func() {
		if !commiteado {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	res := ImportResult{}
	lote := make([][]*string, 0, filasPorLote)
	primeraDelLote := 0

	descargar := func() error {
		if len(lote) == 0 {
			return nil
		}
		sql, args := sesion.db.InsertBatch(p.Schema, p.Table, destino, lote, p.OnConflict == ConflictSkip)
		n, err := tx.Modify(ctx, sql, args)
		if err != nil {
			return &erroDeLinea{linea: primeraDelLote, err: err}
		}
		res.Inserted += n
		lote = lote[:0]
		return nil
	}

	err = csvimport.Rows(p.Path, p.Options, func(linea int, fila []*string) error {
		res.Read++
		valores, err := elegir(fila, p.Mapping, len(destino))
		if err != nil {
			return &erroDeLinea{linea: linea, err: err}
		}
		if len(lote) == 0 {
			primeraDelLote = linea
		}
		lote = append(lote, valores)
		if len(lote) >= filasPorLote {
			return descargar()
		}
		return nil
	})
	if err == nil {
		err = descargar()
	}
	if err != nil {
		res.ElapsedMs = time.Since(arranque).Milliseconds()
		var el *erroDeLinea
		if errors.As(err, &el) {
			res.Line = el.linea
			res.Failure = fallaDeImportacion(sesion, el.err, el.linea)
		} else {
			res.Failure = &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}
		}
		return res
	}

	if !ensayo {
		if err := tx.Commit(ctx); err != nil {
			res.ElapsedMs = time.Since(arranque).Milliseconds()
			res.Failure = sesion.db.ClassifyStatement(err, "confirmar la importación")
			return res
		}
		commiteado = true
	}
	res.OK = true
	res.ElapsedMs = time.Since(arranque).Milliseconds()
	return res
}

// erroDeLinea lleva en qué línea del archivo pasó algo.
//
// Es lo que convierte «falló la importación» en «falló la línea 118», que es la
// diferencia entre poder arreglar el archivo y tener que adivinar.
type erroDeLinea struct {
	linea int
	err   error
}

func (e *erroDeLinea) Error() string { return e.err.Error() }
func (e *erroDeLinea) Unwrap() error { return e.err }

// fallaDeImportacion clasifica el error del motor y le agrega la línea.
//
// La línea del lote es la PRIMERA del lote, no la culpable: un INSERT de mil
// filas que falla no dice cuál fue. Se dice así, en vez de señalar una fila que
// puede no ser la que rompió.
func fallaDeImportacion(sesion *openSession, err error, linea int) *engine.Failure {
	f := sesion.db.ClassifyStatement(err, "la importación")
	if f == nil {
		f = &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}
	}
	if linea > 0 {
		f.Hint = strings.TrimSpace(f.Hint + fmt.Sprintf(
			" El lote que falló empieza en la línea %d del archivo; el error puede estar en cualquiera de las %d siguientes.",
			linea, filasPorLote))
	}
	return f
}

// columnasDestino saca del mapeo las columnas de la tabla, en orden.
func columnasDestino(mapping []string) ([]string, error) {
	var out []string
	vistas := map[string]bool{}
	for _, c := range mapping {
		if c == "" {
			continue
		}
		if vistas[c] {
			return nil, fmt.Errorf("la columna %q está elegida dos veces: cada columna de la tabla puede recibir una sola del archivo", c)
		}
		vistas[c] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("no se eligió ninguna columna: la importación no escribiría nada")
	}
	return out, nil
}

// elegir se queda con los campos que van a alguna columna.
func elegir(fila []*string, mapping []string, n int) ([]*string, error) {
	out := make([]*string, 0, n)
	for i, c := range mapping {
		if c == "" {
			continue
		}
		if i >= len(fila) {
			return nil, fmt.Errorf("la línea tiene %d campos y el mapeo espera al menos %d", len(fila), i+1)
		}
		out = append(out, fila[i])
	}
	return out, nil
}
