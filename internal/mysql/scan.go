package mysql

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
// El driver de MySQL entrega las filas del servidor de a una, así que leer dos
// millones no las junta en memoria mientras el consumidor las escriba. Lo que
// sí hay que cuidar es el copiado: sql.RawBytes apunta al búfer del driver y se
// invalida en el Next siguiente.
func scan(
	ctx context.Context, db *sql.DB, base, tabla string, opts engine.ScanOptions,
) (engine.RowStream, error) {
	var b strings.Builder
	b.WriteString("SELECT " + listaDeColumnas(opts.Columns) + " FROM ")
	b.WriteString(QualifiedName(base, tabla))
	filtro, args, err := dml.Where(opts.Where, dialectoDML(QuoteString), 0)
	if err != nil {
		return nil, err
	}
	if filtro != "" {
		b.WriteString(" WHERE " + filtro)
	}
	if len(opts.OrderBy) > 0 {
		// El DESC va pegado a CADA columna, como en page.
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
		return nil, fmt.Errorf("leer %s.%s: %w", base, tabla, err)
	}
	tipos, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("leer las columnas de %s.%s: %w", base, tabla, err)
	}
	columnas := make([]query.Column, len(tipos))
	nombres := make([]string, len(tipos))
	for i, t := range tipos {
		columnas[i] = query.Column{
			Name:     t.Name(),
			DataType: strings.ToLower(t.DatabaseTypeName()),
			Class:    claseDe(t.DatabaseTypeName()),
		}
		nombres[i] = t.DatabaseTypeName()
	}
	return &flujo{rows: rows, columnas: columnas, tipos: nombres}, nil
}

// flujo adapta *sql.Rows a engine.RowStream.
type flujo struct {
	rows     *sql.Rows
	columnas []query.Column
	// tipos son los nombres de tipo del driver, para convertir los binarios y
	// los BIT igual que en la grilla (textoDe).
	tipos   []string
	fila    []*string
	cerrado bool
	err     error
}

func (f *flujo) Columns() []query.Column { return f.columnas }

func (f *flujo) Next() bool {
	if f.cerrado || !f.rows.Next() {
		return false
	}
	n := len(f.columnas)
	crudo := make([]sql.RawBytes, n)
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
	for i, b := range crudo {
		if b == nil {
			continue
		}
		// RawBytes apunta al búfer del driver y se invalida en el Next
		// siguiente: hay que copiar. Sin la copia, todas las filas del archivo
		// terminarían con el valor de la última.
		s := textoDe(b, f.tipos[i])
		fila[i] = &s
	}
	f.fila = fila
	return true
}

func (f *flujo) Row() []*string { return f.fila }

// Err es el error del recorrido: el de Scan si hubo, y si no el de *sql.Rows,
// que ya distingue «se terminó» de «falló a la mitad».
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

// listaDeColumnas escribe qué leer: `*`, o los nombres citados.
//
// Los nombres son IDENTIFICADORES y no valores: no se pueden parametrizar, así
// que van citados por el motor. Y salen de la introspección, no de nada que
// alguien escriba. Ver CLAUDE.md.
func listaDeColumnas(cols []string) string {
	if len(cols) == 0 {
		return "*"
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, QuoteIdent(c))
	}
	return strings.Join(out, ", ")
}
