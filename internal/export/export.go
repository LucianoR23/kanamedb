// Package export escribe filas en los formatos que salen de Kaname: CSV, JSON,
// JSON Lines y Markdown.
//
// Es formateo puro: no consulta nada. Lo usan dos caminos que no se parecen en
// volumen —el resultado del editor, que ya está en memoria, y S19, que lee una
// tabla entera del motor— y por eso la interfaz va de a una fila: Begin, Row,
// Row, …, End. Un escritor que pidiera todas las filas juntas obligaría a S19
// a juntarlas, y una tabla de dos millones de filas no pasa por un [][]string.
//
// Los valores llegan como los muestra la grilla: texto del servidor, y nil
// para NULL. El paquete no interpreta el texto salvo donde el formato lo
// exige —JSON tiene números, booleanos y null propios— y ahí solo cuando el
// texto es inequívoco; en la duda, va como cadena. Un archivo que no se puede
// volver a leer es peor que uno que dice "128.40" entre comillas.
package export

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// Format es el formato de salida.
type Format string

const (
	CSV Format = "csv"
	// JSON es un array de objetos, uno por fila.
	JSON Format = "json"
	// JSONL es JSON Lines: un objeto por línea, sin array alrededor. Es lo que
	// leen jq, DuckDB y pandas de a pedazos, y lo que S19 va a preferir para
	// una tabla grande.
	JSONL    Format = "jsonl"
	Markdown Format = "markdown"
)

// Formats es la lista en el orden en que se ofrece.
var Formats = []Format{CSV, JSON, JSONL, Markdown}

// Extension es la extensión del archivo, con punto.
func (f Format) Extension() string {
	switch f {
	case CSV:
		return ".csv"
	case JSON:
		return ".json"
	case JSONL:
		return ".jsonl"
	case Markdown:
		return ".md"
	}
	return ""
}

// Options es lo que S19 deja elegir. El valor cero es la salida por defecto:
// coma, con encabezado, NULL como `\N`, comillas solo donde hacen falta.
type Options struct {
	// Delimiter separa los campos del CSV: "," (por defecto), ";" o "\t".
	// Los demás formatos lo ignoran.
	Delimiter string `json:"delimiter"`

	// NoHeader omite la fila de nombres del CSV. Markdown la lleva siempre
	// —una tabla sin encabezado no es una tabla— y JSON usa los nombres como
	// claves.
	NoHeader bool `json:"noHeader"`

	// NullAsEmpty escribe NULL como campo vacío en vez de `\N`. La cadena
	// vacía sigue distinguiéndose: en CSV va entre comillas, como en `COPY …
	// CSV`. JSON no lo mira: NULL es null.
	NullAsEmpty bool `json:"nullAsEmpty"`

	// QuoteAll pone comillas a todos los campos del CSV, no solo a los que las
	// necesitan. Algunas planillas leen mejor así.
	QuoteAll bool `json:"quoteAll"`

	// BOM antepone la marca de orden de bytes de UTF-8 al CSV. Excel en
	// Windows abre un CSV sin marca con la página de códigos del sistema, y
	// «señal» se vuelve «seÃ±al». Solo CSV: JSON la prohíbe y Markdown no la
	// necesita.
	BOM bool `json:"bom"`

	// Gzip comprime la salida al escribir.
	Gzip bool `json:"gzip"`

	// NeutralizeFormulas antepone un apóstrofo a los campos que Excel y
	// LibreOffice ejecutarían como fórmula al abrir el archivo: los que
	// empiezan con `=`, `+`, `-`, `@`, tabulación o retorno de carro.
	//
	// El valor de una celda es dato NO CONFIABLE —lo dice CLAUDE.md— y este es
	// el único camino que se lo entrega a una planilla, donde `=cmd|…` no es
	// texto sino una orden. Citar no alcanza: la planilla mira el contenido del
	// campo, no las comillas.
	//
	// Va apagado por defecto porque CAMBIA EL VALOR: un `-5` exportado así se
	// lee después como `'-5`, y un archivo que se va a volver a importar tiene
	// que decir lo que decía. Se enciende cuando el destino es una planilla,
	// que es cuando el riesgo existe.
	NeutralizeFormulas bool `json:"neutralizeFormulas"`
}

// Writer escribe filas a medida que llegan.
//
// End es obligatorio: cierra el array de JSON, vacía el búfer y termina el
// gzip. Un archivo sin End queda cortado aunque no haya habido error.
type Writer interface {
	Begin(columns []query.Column) error
	Row(values []*string) error
	End() error
}

// New arma el escritor de un formato sobre w.
func New(f Format, w io.Writer, o Options) (Writer, error) {
	o, err := o.normalizada()
	if err != nil {
		return nil, err
	}
	var gz *gzip.Writer
	if o.Gzip {
		gz = gzip.NewWriter(w)
		w = gz
	}
	bw := bufio.NewWriterSize(w, 64<<10)

	var interno Writer
	switch f {
	case CSV:
		interno = &escritorCSV{w: bw, o: o}
	case JSON:
		interno = &escritorJSON{w: bw}
	case JSONL:
		interno = &escritorJSON{w: bw, lineas: true}
	case Markdown:
		interno = &escritorMarkdown{w: bw, o: o}
	default:
		return nil, fmt.Errorf("formato de exportación desconocido: %q", string(f))
	}
	return &conCierre{Writer: interno, bw: bw, gz: gz}, nil
}

// conCierre agrega al escritor de formato lo que todos comparten: vaciar el
// búfer y cerrar el gzip, en ese orden.
type conCierre struct {
	Writer
	bw *bufio.Writer
	gz *gzip.Writer
}

func (c *conCierre) End() error {
	if err := c.Writer.End(); err != nil {
		return err
	}
	if err := c.bw.Flush(); err != nil {
		return fmt.Errorf("escribir la exportación: %w", err)
	}
	if c.gz != nil {
		if err := c.gz.Close(); err != nil {
			return fmt.Errorf("cerrar el gzip: %w", err)
		}
	}
	return nil
}

// normalizada completa los valores por defecto y rechaza lo que no puede ser
// un delimitador.
func (o Options) normalizada() (Options, error) {
	switch o.Delimiter {
	case "":
		o.Delimiter = ","
	case ",", ";", "\t":
	default:
		return o, fmt.Errorf("el delimitador tiene que ser coma, punto y coma o tabulación, no %q", o.Delimiter)
	}
	return o, nil
}

// Write vuelca columnas y filas que ya están en memoria. Es el camino del
// resultado del editor; S19 no lo usa porque no tiene las filas juntas.
//
// Si limit es mayor que cero, escribe a lo sumo esa cantidad de filas: es lo
// que muestra la vista previa. Devuelve cuántas escribió.
func Write(f Format, w io.Writer, o Options, columns []query.Column, rows [][]*string, limit int) (int, error) {
	esc, err := New(f, w, o)
	if err != nil {
		return 0, err
	}
	if err := esc.Begin(columns); err != nil {
		return 0, err
	}
	n := 0
	for _, fila := range rows {
		if limit > 0 && n >= limit {
			break
		}
		if err := esc.Row(fila); err != nil {
			return n, err
		}
		n++
	}
	if err := esc.End(); err != nil {
		return n, err
	}
	return n, nil
}

// Render devuelve el texto de las primeras filas. Sirve para la vista previa
// y para el portapapeles, que son texto y no un archivo: el gzip se ignora.
func Render(f Format, o Options, columns []query.Column, rows [][]*string, limit int) (string, error) {
	o.Gzip = false
	var b strings.Builder
	if _, err := Write(f, &b, o, columns, rows, limit); err != nil {
		return "", err
	}
	return b.String(), nil
}

// nulo es cómo se escribe NULL en los formatos de texto.
func (o Options) nulo() string {
	if o.NullAsEmpty {
		return ""
	}
	return `\N`
}
