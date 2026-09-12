package export

import (
	"bufio"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// SQLTarget es lo que el formato SQL necesita y los demás no: a qué tabla
// insertar y cómo cita este motor.
//
// Se recibe como funciones y no como un `dml.Dialect` para que `export` no
// dependa de cómo se arma el DML: lo único que hace falta acá es citar, y
// citar es distinto en cada motor.
type SQLTarget struct {
	// Table es el nombre de la tabla ya calificado y citado por el motor.
	Table string
	// QuoteIdent cita el nombre de una columna.
	QuoteIdent func(string) string
	// QuoteLiteral cita un texto como literal.
	QuoteLiteral func(string) string
	// InsertModifier va entre la lista de columnas y VALUES, si el motor lo
	// necesita: `OVERRIDING SYSTEM VALUE` en Postgres con identity ALWAYS.
	InsertModifier string
	// QuoteBinary escribe el valor de una columna binaria. Nil cae en
	// QuoteLiteral, que es lo correcto cuando el motor ya entrega el binario
	// como texto que él mismo acepta (Postgres).
	QuoteBinary func(string) string
}

// filasPorSentencia es cuántas filas entran en cada INSERT.
//
// Ni una por fila —un millón de sentencias tarda una eternidad en volver a
// entrar— ni todas juntas: un INSERT gigante puede pasarse del tamaño máximo
// de paquete del servidor, y además un archivo de una sola línea de 200 MB no
// se puede ni mirar. Quinientas es lo que usan los volcados de MySQL.
const filasPorSentencia = 500

// escritorSQL escribe INSERTs que se pueden volver a correr.
//
// Es el ÚNICO lugar del proyecto donde el valor de una fila se escribe adentro
// de la SQL, y es legítimo porque acá la SQL no se ejecuta: es un archivo que
// alguien va a leer y, si quiere, correr en otro lado. Todo lo que Kaname
// ejecuta va con parámetros. Ver CLAUDE.md y `internal/dml`.
type escritorSQL struct {
	w *bufio.Writer
	o Options
	t SQLTarget

	columnas []query.Column
	cabecera string
	enLote   int
	buf      []byte
	// clases es, en la fila en curso, la clase de cada VALOR. Nil es
	// «decidilo por la clase de la columna».
	clases []query.Class
}

// RowClasses escribe la fila sabiendo de qué clase es cada celda.
func (e *escritorSQL) RowClasses(vals []*string, clases []query.Class) error {
	e.clases = clases
	defer func() { e.clases = nil }()
	return e.Row(vals)
}

func (e *escritorSQL) Begin(cols []query.Column) error {
	e.columnas = cols
	nombres := make([]byte, 0, 64)
	for i, c := range cols {
		if i > 0 {
			nombres = append(nombres, ", "...)
		}
		nombres = append(nombres, e.t.QuoteIdent(c.Name)...)
	}
	modificador := ""
	if e.t.InsertModifier != "" {
		modificador = " " + e.t.InsertModifier
	}
	e.cabecera = fmt.Sprintf("INSERT INTO %s (%s)%s VALUES\n", e.t.Table, nombres, modificador)
	return nil
}

func (e *escritorSQL) Row(vals []*string) error {
	if len(vals) != len(e.columnas) {
		return fmt.Errorf("la fila tiene %d valores y el resultado %d columnas", len(vals), len(e.columnas))
	}
	e.buf = e.buf[:0]
	switch {
	case e.enLote == 0:
		e.buf = append(e.buf, e.cabecera...)
	default:
		e.buf = append(e.buf, ",\n"...)
	}
	e.buf = append(e.buf, '(')
	for i, v := range vals {
		if i > 0 {
			e.buf = append(e.buf, ", "...)
		}
		clase := e.columnas[i].Class
		if e.clases != nil {
			clase = e.clases[i]
		}
		e.buf = e.literal(e.buf, clase, v)
	}
	e.buf = append(e.buf, ')')

	e.enLote++
	if e.enLote >= filasPorSentencia {
		e.buf = append(e.buf, ";\n"...)
		e.enLote = 0
	}
	if _, err := e.w.Write(e.buf); err != nil {
		return fmt.Errorf("escribir el SQL: %w", err)
	}
	return nil
}

func (e *escritorSQL) End() error {
	if e.enLote > 0 {
		if _, err := e.w.WriteString(";\n"); err != nil {
			return fmt.Errorf("escribir el SQL: %w", err)
		}
		e.enLote = 0
	}
	return nil
}

// literal escribe el valor como lo escribiría alguien a mano.
//
// La regla es la misma que en JSON, y por el mismo motivo: solo se escribe sin
// comillas lo que es inequívoco. Una columna numérica cuyo texto es un número
// va sin comillas; todo lo demás va citado, que es lo que hace que el archivo
// se pueda volver a correr aunque el tipo de la columna cambie. NULL es NULL.
func (e *escritorSQL) literal(b []byte, clase query.Class, v *string) []byte {
	if v == nil {
		return append(b, "NULL"...)
	}
	if clase == query.ClassBinary && e.t.QuoteBinary != nil {
		return append(b, e.t.QuoteBinary(*v)...)
	}
	if clase == query.ClassNumber && numeroJSON.MatchString(*v) {
		return append(b, *v...)
	}
	return append(b, e.t.QuoteLiteral(*v)...)
}
