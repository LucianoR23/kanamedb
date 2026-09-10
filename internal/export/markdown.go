package export

import (
	"bufio"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// escritorMarkdown escribe una tabla de GitHub Flavored Markdown: la fila de
// nombres, la de alineación y una por fila de datos.
//
// Las columnas numéricas van alineadas a la derecha, como en la grilla. Una
// barra vertical adentro de un valor se escapa como `\|`, y un salto de línea
// se vuelve `<br>`: una tabla de Markdown vive en una línea por fila y un
// salto crudo la parte en dos. NULL se escribe `NULL` —o nada, con
// NullAsEmpty—, que es lo que se lee en un documento; `\N` es de máquinas.
type escritorMarkdown struct {
	w *bufio.Writer
	o Options

	columnas int
	buf      []byte
}

func (e *escritorMarkdown) Begin(cols []query.Column) error {
	e.columnas = len(cols)
	e.buf = e.buf[:0]
	e.buf = append(e.buf, '|')
	for _, c := range cols {
		e.buf = append(e.buf, ' ')
		e.buf = escaparMarkdown(e.buf, c.Name)
		e.buf = append(e.buf, " |"...)
	}
	e.buf = append(e.buf, "\n|"...)
	for _, c := range cols {
		if c.Class == query.ClassNumber {
			e.buf = append(e.buf, " --: |"...)
		} else {
			e.buf = append(e.buf, " --- |"...)
		}
	}
	e.buf = append(e.buf, '\n')
	if _, err := e.w.Write(e.buf); err != nil {
		return fmt.Errorf("escribir el Markdown: %w", err)
	}
	return nil
}

func (e *escritorMarkdown) Row(vals []*string) error {
	if len(vals) != e.columnas {
		return fmt.Errorf("la fila tiene %d valores y el resultado %d columnas", len(vals), e.columnas)
	}
	e.buf = e.buf[:0]
	e.buf = append(e.buf, '|')
	for _, v := range vals {
		e.buf = append(e.buf, ' ')
		switch {
		case v == nil && !e.o.NullAsEmpty:
			e.buf = append(e.buf, "NULL"...)
		case v == nil:
		default:
			e.buf = escaparMarkdown(e.buf, *v)
		}
		e.buf = append(e.buf, " |"...)
	}
	e.buf = append(e.buf, '\n')
	if _, err := e.w.Write(e.buf); err != nil {
		return fmt.Errorf("escribir el Markdown: %w", err)
	}
	return nil
}

func (e *escritorMarkdown) End() error { return nil }

// escaparMarkdown agrega el texto sin lo que rompería la tabla.
func escaparMarkdown(b []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '|':
			b = append(b, '\\', '|')
		case '\r':
			// Un \r\n es UN salto, no dos.
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			b = append(b, "<br>"...)
		case '\n':
			b = append(b, "<br>"...)
		default:
			b = append(b, s[i])
		}
	}
	return b
}
