package export

import (
	"bufio"
	"encoding/json"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// escritorJSON escribe un array de objetos, o un objeto por línea si es JSON
// Lines. Las claves son los nombres de las columnas, en su orden; si dos se
// llaman igual —`select 1, 1`— las dos van, como hace `row_to_json`.
//
// El array se escribe de a una fila, con la coma adelante de cada fila que no
// es la primera: así una tabla grande sale sin juntarla y el archivo cierra
// bien aunque tenga cero filas.
type escritorJSON struct {
	w      *bufio.Writer
	lineas bool

	columnas []query.Column
	claves   [][]byte
	n        int
	buf      []byte
}

func (e *escritorJSON) Begin(cols []query.Column) error {
	e.columnas = cols
	e.claves = make([][]byte, len(cols))
	for i, c := range cols {
		e.claves[i] = codificar(nil, c.Name)
	}
	if !e.lineas {
		if _, err := e.w.WriteString("["); err != nil {
			return fmt.Errorf("escribir el JSON: %w", err)
		}
	}
	return nil
}

func (e *escritorJSON) Row(vals []*string) error {
	if len(vals) != len(e.columnas) {
		return fmt.Errorf("la fila tiene %d valores y el resultado %d columnas", len(vals), len(e.columnas))
	}
	e.buf = e.buf[:0]
	if !e.lineas {
		if e.n > 0 {
			e.buf = append(e.buf, ',')
		}
		e.buf = append(e.buf, '\n')
	}
	e.buf = append(e.buf, '{')
	for i, v := range vals {
		if i > 0 {
			e.buf = append(e.buf, ',')
		}
		e.buf = append(e.buf, e.claves[i]...)
		e.buf = append(e.buf, ':')
		e.buf = valorJSON(e.buf, e.columnas[i].Class, v)
	}
	e.buf = append(e.buf, '}')
	if e.lineas {
		e.buf = append(e.buf, '\n')
	}
	e.n++
	if _, err := e.w.Write(e.buf); err != nil {
		return fmt.Errorf("escribir el JSON: %w", err)
	}
	return nil
}

func (e *escritorJSON) End() error {
	if e.lineas {
		return nil
	}
	cierre := "]\n"
	if e.n > 0 {
		cierre = "\n]\n"
	}
	if _, err := e.w.WriteString(cierre); err != nil {
		return fmt.Errorf("escribir el JSON: %w", err)
	}
	return nil
}

// numeroJSON es la gramática de número de JSON. Lo que no la cumple —NaN,
// Infinity, un numeric con notación que JSON no tiene— va como cadena.
var numeroJSON = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// valorJSON agrega un valor con el tipo que JSON sabe expresar, y solo cuando
// el texto del servidor es inequívoco:
//
//   - NULL es null;
//   - una columna numérica cuyo texto es un número JSON va sin comillas. Es lo
//     que hacen `row_to_json` y `JSON_OBJECT` en el propio servidor, y lo que
//     hace que la fila exportada sea la misma que el visor muestra como JSON;
//   - una booleana va como true o false si el texto es t/f, true/false o 1/0;
//   - una columna json o jsonb va tal cual si es JSON válido;
//   - todo lo demás, incluidos los casos raros de los anteriores, es una
//     cadena. Nunca se emite algo que no se pueda volver a leer.
func valorJSON(b []byte, clase query.Class, v *string) []byte {
	if v == nil {
		return append(b, "null"...)
	}
	s := *v
	switch clase {
	case query.ClassNumber:
		if numeroJSON.MatchString(s) {
			return append(b, s...)
		}
	case query.ClassBool:
		switch s {
		case "t", "true", "1":
			return append(b, "true"...)
		case "f", "false", "0":
			return append(b, "false"...)
		}
	case query.ClassJSON:
		if json.Valid([]byte(s)) {
			return append(b, s...)
		}
	}
	return codificar(b, s)
}

// codificar escribe una cadena JSON con lo mínimo escapado: comillas, barra
// invertida y los caracteres de control. No escapa <, > ni & —eso es para
// meter JSON adentro de HTML, y esto es un archivo— y reemplaza los bytes que
// no son UTF-8 válido por U+FFFD, como hace encoding/json.
//
// Está escrito a mano y no con json.Marshal por el volumen: son dos millones
// de filas por diez columnas, y una asignación por celda se nota.
func codificar(b []byte, s string) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b = append(b, '\\', byte(r))
		case r == '\n':
			b = append(b, '\\', 'n')
		case r == '\r':
			b = append(b, '\\', 'r')
		case r == '\t':
			b = append(b, '\\', 't')
		case r < 0x20:
			b = append(b, '\\', 'u', '0', '0', hex[r>>4], hex[r&0xf])
		default:
			// Un byte inválido llega como RuneError y se escribe como tal:
			// utf8.AppendRune lo codifica como U+FFFD.
			b = utf8.AppendRune(b, r)
		}
	}
	return append(b, '"')
}
