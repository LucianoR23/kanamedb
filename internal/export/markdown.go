package export

import (
	"bufio"
	"fmt"
	"strings"
	"unicode/utf8"

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
		e.buf = celdaMarkdown(e.buf, v, e.o)
		e.buf = append(e.buf, " |"...)
	}
	e.buf = append(e.buf, '\n')
	if _, err := e.w.Write(e.buf); err != nil {
		return fmt.Errorf("escribir el Markdown: %w", err)
	}
	return nil
}

func (e *escritorMarkdown) End() error { return nil }

// celdaMarkdown escribe UN valor: el texto escapado, o cómo se ve un NULL.
//
// Está separado porque lo usan los dos caminos —el escritor que va de a una
// fila y el renderizador que alinea— y las reglas de escape tienen que ser las
// mismas en los dos. Dos copias serían dos verdades sobre cómo se escapa una
// barra vertical, y la segunda se atrasaría sin que nadie lo note.
func celdaMarkdown(b []byte, v *string, o Options) []byte {
	switch {
	case v == nil && !o.NullAsEmpty:
		return append(b, "NULL"...)
	case v == nil:
		return b
	default:
		return escaparMarkdown(b, *v)
	}
}

// escaparMarkdown agrega el texto sin lo que rompería la tabla.
//
// La barra invertida también se escapa: sin eso, `a\|b` salía como `a\\|b`,
// que GFM lee como una barra escapada seguida de un separador de celda, y la
// fila se partía (C-26 de la auditoría del 2026-09-11).
func escaparMarkdown(b []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b = append(b, '\\', '\\')
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

// anchoMaximoAlineado es hasta cuánto se rellena una columna.
//
// Cuarenta caracteres. Sin tope, una columna con un JWT de trescientos deja a
// todas las demás con doscientos noventa espacios de relleno y la tabla se
// vuelve ilegible — que es lo contrario de para qué se alinea. Las columnas más
// anchas que esto se escriben sin rellenar: desalinean su renglón y dejan el
// resto legible.
const anchoMaximoAlineado = 40

// RenderMarkdownAlineado arma la tabla con las columnas rellenadas al ancho de
// su contenido.
//
// Es para el PORTAPAPELES, no para un archivo: alinear exige conocer el ancho
// de cada columna, y para eso hay que tener todas las filas antes de escribir
// la primera. Con dos millones de filas eso no entra en memoria, y por eso el
// escritor de a una fila —el que usa la exportación a archivo— no alinea y
// `NewInto` rechaza la opción en vez de aceptarla y quedarse sin memoria.
//
// Lo que se gana: una tabla pegada en un ticket o en un pull request se lee
// ANTES de renderizarse. Markdown la dibuja igual con o sin relleno.
//
// Un valor con una TABULACIÓN adentro corre su renglón, porque en una terminal
// el tab salta al siguiente tope. No se toca: cambiarlo por un espacio
// alinearía la vista y falsearía el dato, y el dato es lo que se está copiando.
// Markdown lo dibuja como un espacio de todos modos.
func RenderMarkdownAlineado(o Options, columns []query.Column, rows [][]*string, limit int) string {
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}

	// El texto de cada celda, una vez. Volver a escaparlo para medirlo y otra
	// para escribirlo daría dos oportunidades de que las reglas se separen.
	celdas := make([][]string, 0, len(rows)+1)
	cabecera := make([]string, len(columns))
	for i, c := range columns {
		cabecera[i] = string(escaparMarkdown(nil, c.Name))
	}
	celdas = append(celdas, cabecera)
	for _, fila := range rows {
		texto := make([]string, len(columns))
		for i := range columns {
			var v *string
			if i < len(fila) {
				v = fila[i]
			}
			texto[i] = string(celdaMarkdown(nil, v, o))
		}
		celdas = append(celdas, texto)
	}

	anchos := make([]int, len(columns))
	for _, fila := range celdas {
		for i, t := range fila {
			// Se mide en RUNAS y no en bytes: con bytes, «ADRIANA ITATÍ BAEZ»
			// contaría la í como dos y la columna quedaría corrida justo en las
			// tablas con acentos.
			if n := utf8.RuneCountInString(t); n > anchos[i] && n <= anchoMaximoAlineado {
				anchos[i] = n
			}
		}
	}
	// La línea de alineación mide tres —`---`— así que ninguna columna puede
	// ser más angosta que eso sin que la tabla quede torcida.
	for i := range anchos {
		if anchos[i] < 3 {
			anchos[i] = 3
		}
	}

	var b strings.Builder
	renglon := func(textos []string) {
		b.WriteByte('|')
		for i, t := range textos {
			b.WriteByte(' ')
			b.WriteString(t)
			b.WriteString(relleno(anchos[i] - utf8.RuneCountInString(t)))
			b.WriteString(" |")
		}
		b.WriteByte('\n')
	}

	renglon(celdas[0])
	b.WriteByte('|')
	for i, c := range columns {
		b.WriteByte(' ')
		// La alineación a la derecha de los números se conserva: es lo que la
		// grilla hace y lo que un lector espera de una columna de importes.
		if c.Class == query.ClassNumber {
			b.WriteString(strings.Repeat("-", anchos[i]-1))
			b.WriteString(":")
		} else {
			b.WriteString(strings.Repeat("-", anchos[i]))
		}
		b.WriteString(" |")
	}
	b.WriteByte('\n')
	for _, fila := range celdas[1:] {
		renglon(fila)
	}
	return b.String()
}

// relleno son n espacios, o ninguno si n es negativo —una celda más ancha que
// el tope, que se escribe sin rellenar—.
func relleno(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}
