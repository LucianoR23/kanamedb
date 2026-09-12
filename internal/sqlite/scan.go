package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/dml"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// scan recorre una tabla entera con UNA consulta que se lee a medida que llega.
//
// Los valores se escanean a `any` y no a *string, igual que en leerFilas: el
// driver entrega el tipo del VALOR —int64, float64, string, []byte o nil— y un
// *string obligaría al driver a convertir, que es donde se pierden los blobs.
func scan(
	ctx context.Context, db *sql.DB, tabla string, opts engine.ScanOptions,
) (engine.RowStream, error) {
	declaradas, err := columnasDeclaradas(ctx, db, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer la definición de %s: %w", tabla, err)
	}
	var b strings.Builder
	// Las columnas se leen tal como están guardadas: ver lecturaFiel.
	b.WriteString("SELECT " + lecturaFiel(declaradas, opts.Columns) + " FROM ")
	b.WriteString(QuoteIdent(tabla))
	filtro, args, err := dml.Where(opts.Where, dialectoDML, 0)
	if err != nil {
		return nil, err
	}
	if filtro != "" {
		b.WriteString(" WHERE " + filtro)
	}
	if len(opts.OrderBy) > 0 {
		b.WriteString(" ORDER BY ")
		for i, col := range opts.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(QuoteIdent(col))
			if opts.Descending {
				b.WriteString(" DESC")
			}
		}
	}

	if opts.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", opts.Limit)
	}

	rows, err := db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("leer %s: %w", tabla, err)
	}
	tipos, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("leer las columnas de %s: %w", tabla, err)
	}
	columnas := make([]query.Column, len(tipos))
	for i, t := range tipos {
		columnas[i] = query.Column{
			Name:     t.Name(),
			DataType: strings.ToLower(t.DatabaseTypeName()),
			Class:    claseDe(t.DatabaseTypeName()),
		}
	}
	reponerDeclarados(columnas, declaradas)
	return &flujo{rows: rows, columnas: columnas}, nil
}

// claseDelValor es la clase de lo que el driver entregó para UNA celda.
func claseDelValor(v any) query.Class {
	switch v.(type) {
	case []byte:
		return query.ClassBinary
	case int64, float64:
		return query.ClassNumber
	}
	return query.ClassText
}

// flujo adapta *sql.Rows a engine.RowStream.
type flujo struct {
	rows     *sql.Rows
	columnas []query.Column
	fila     []*string
	// clases es la clase de cada VALOR de `fila`: el tipo en SQLite es del
	// valor, y el escritor SQL tiene que saber qué va como X'…', qué sin
	// comillas y qué como texto.
	clases  []query.Class
	cerrado bool
	err     error
}

func (f *flujo) Columns() []query.Column    { return f.columnas }
func (f *flujo) CellClasses() []query.Class { return f.clases }

func (f *flujo) Next() bool {
	if f.cerrado || !f.rows.Next() {
		return false
	}
	n := len(f.columnas)
	crudo := make([]any, n)
	punteros := make([]any, n)
	for i := range crudo {
		punteros[i] = &crudo[i]
	}
	if err := f.rows.Scan(punteros...); err != nil {
		// Se guarda: Scan devuelve su error de vuelta y NO lo deja en
		// rows.Err(), así que sin esto el recorrido terminaría como si hubiera
		// llegado al final y el archivo quedaría cortado sin decirlo.
		f.err = err
		f.Close()
		return false
	}
	fila := make([]*string, n)
	clases := make([]query.Class, n)
	for i, v := range crudo {
		// Los BLOB salen en hexadecimal: la exportación tiene que ser fiel.
		s, ok := valorATexto(v, true, false)
		if !ok {
			continue
		}
		clases[i] = claseDelValor(v)
		fila[i] = &s
	}
	f.fila, f.clases = fila, clases
	return true
}

func (f *flujo) Row() []*string { return f.fila }

// Err es el error del recorrido: el de Scan si hubo, y si no el de *sql.Rows.
func (f *flujo) Err() error {
	if f.err != nil {
		return f.err
	}
	return f.rows.Err()
}

func (f *flujo) Close() {
	if f.cerrado {
		return
	}
	f.cerrado = true
	f.rows.Close()
}
