package export

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// escritorCSV escribe RFC 4180 con las mismas convenciones que `COPY … CSV`
// de Postgres, que son las que S18 va a leer de vuelta:
//
//   - la cadena vacía va como `""`, entre comillas, y NULL va como `\N` —o
//     como nada, con NullAsEmpty—. Así las dos se distinguen en el archivo,
//     que es la diferencia que la grilla también muestra;
//   - un campo se cita cuando lleva el delimitador, comillas o saltos de
//     línea, o cuando su texto coincidiría con la marca de NULL: un `\N` que
//     es un valor de verdad se escribe `"\N"` para que no se lea como nulo;
//   - las comillas adentro de un campo se duplican; el final de línea es `\n`.
//
// No usa encoding/csv porque no sabe citar todo ni distinguir NULL de vacío,
// y las dos cosas son opciones del diálogo.
type escritorCSV struct {
	w *bufio.Writer
	o Options

	columnas int
	buf      []byte
}

func (e *escritorCSV) Begin(cols []query.Column) error {
	e.columnas = len(cols)
	if e.o.BOM {
		if _, err := e.w.WriteString("\uFEFF"); err != nil {
			return fmt.Errorf("escribir el CSV: %w", err)
		}
	}
	if e.o.NoHeader {
		return nil
	}
	nombres := make([]*string, len(cols))
	for i := range cols {
		nombres[i] = &cols[i].Name
	}
	return e.Row(nombres)
}

func (e *escritorCSV) Row(vals []*string) error {
	if len(vals) != e.columnas {
		return fmt.Errorf("la fila tiene %d valores y el resultado %d columnas", len(vals), e.columnas)
	}
	e.buf = e.buf[:0]
	for i, v := range vals {
		if i > 0 {
			e.buf = append(e.buf, e.o.Delimiter...)
		}
		e.buf = e.campo(e.buf, v)
	}
	e.buf = append(e.buf, '\n')
	if _, err := e.w.Write(e.buf); err != nil {
		return fmt.Errorf("escribir el CSV: %w", err)
	}
	return nil
}

func (e *escritorCSV) End() error { return nil }

// campo agrega un valor citado si hace falta.
func (e *escritorCSV) campo(b []byte, v *string) []byte {
	if v == nil {
		// NULL nunca se cita: citado dejaría de ser NULL para quien lo lea.
		return append(b, e.o.nulo()...)
	}
	s := *v
	if e.o.NeutralizeFormulas && esFormula(s) {
		// El apóstrofo va ADENTRO de las comillas: es parte del valor para la
		// planilla, que lo interpreta como «esto es texto».
		b = append(b, '"', '\'')
		b = citado(b, s)
		return append(b, '"')
	}
	if !e.o.QuoteAll && s != "" && s != e.o.nulo() && !strings.ContainsAny(s, e.o.Delimiter+"\"\r\n") {
		return append(b, s...)
	}
	b = append(b, '"')
	b = citado(b, s)
	return append(b, '"')
}

// citado agrega el texto con las comillas dobladas, sin las comillas de afuera.
func citado(b []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			b = append(b, '"')
		}
		b = append(b, s[i])
	}
	return b
}

// esFormula dice si una planilla trataría el campo como fórmula y no como
// texto. Son los cuatro caracteres que arrancan una expresión, y la tabulación
// y el retorno, que la planilla saltea antes de mirar el siguiente.
func esFormula(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\t', '\r':
			continue
		case '=', '+', '-', '@':
			return true
		default:
			return false
		}
	}
	return false
}
