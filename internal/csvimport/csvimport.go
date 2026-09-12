// Package csvimport lee un archivo CSV para meterlo en una tabla.
//
// Es la mitad de S18 que no toca la base: abrir el archivo, mirarlo, y decir
// qué tiene. La otra mitad —insertar— vive en `internal/service`, porque
// necesita la conexión.
//
// Todo lo que sale de acá es TEXTO. La conversión al tipo de la columna la hace
// el servidor, igual que en la grilla: interpretar acá una fecha o un número
// sería inventar una regla que no es la del motor, y la diferencia aparecería
// en los casos raros —el formato de fecha, la coma decimal, el infinito—.
package csvimport

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Options es cómo leer el archivo.
type Options struct {
	// Delimiter separa los campos: "," (por defecto), ";", "\t" o "|".
	Delimiter string `json:"delimiter"`
	// HasHeader dice que la primera línea son los nombres de las columnas.
	HasHeader bool `json:"hasHeader"`
	// EmptyAsNull trata el campo vacío como NULL.
	//
	// Es una decisión de quien importa y no se puede adivinar: un CSV que salió
	// de una planilla escribe la cadena vacía y el NULL igual, y solo quien
	// conoce los datos sabe cuál era. Por eso está acá y no hay un default
	// «inteligente».
	EmptyAsNull bool `json:"emptyAsNull"`
	// Trim recorta los espacios de los bordes de cada valor.
	Trim bool `json:"trim"`
}

func (o Options) coma() (rune, error) {
	switch o.Delimiter {
	case "", ",":
		return ',', nil
	case ";":
		return ';', nil
	case "\t":
		return '\t', nil
	case "|":
		return '|', nil
	}
	return 0, fmt.Errorf("el delimitador tiene que ser coma, punto y coma, tabulación o barra, no %q", o.Delimiter)
}

// FileInfo es lo que se sabe del archivo antes de leerlo entero.
type FileInfo struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// Inspect mira el archivo y devuelve con qué se va a trabajar.
//
// Recorre el archivo ENTERO para contar las filas, y es a propósito: «4.182
// filas» es lo que deja decidir si esto se importa o no, y una estimación ahí
// no sirve. Solo se guardan las primeras filas de muestra, así que un archivo
// de un giga no se junta en memoria.
type Inspection struct {
	File FileInfo `json:"file"`
	// Columns son los nombres de la primera línea, o `columna 1`, `columna 2`…
	// cuando no hay encabezado.
	Columns []string `json:"columns"`
	// Sample son las primeras filas de datos, para mostrar de qué se trata.
	Sample [][]string `json:"sample"`
	// Rows es cuántas filas de DATOS tiene, sin contar el encabezado.
	Rows int `json:"rows"`
	// Raw son las primeras líneas tal como están en el archivo, sin parsear.
	// Es lo que deja ver que el delimitador elegido es el que no es.
	Raw string `json:"raw"`

	// Ragged son las líneas cuya cantidad de campos no coincide con la de la
	// primera. Se avisan acá y no al importar: es el error más común de un CSV
	// y el más fácil de arreglar antes de empezar.
	Ragged []Ragged `json:"ragged"`

	// NotUTF8 dice que el archivo tiene bytes que no son UTF-8 válido.
	//
	// Se avisa y no se convierte: convertir necesita saber DE QUÉ codificación
	// —latin-1, windows-1252, big5— y adivinarlo mal escribe basura en la base
	// sin que nadie se entere hasta mucho después.
	NotUTF8 bool `json:"notUtf8"`
}

// Ragged es una línea con otra cantidad de campos.
type Ragged struct {
	Line   int `json:"line"`
	Fields int `json:"fields"`
}

// muestra es cuántas filas se guardan para mirar.
const muestra = 20

// lineasCrudas es cuántas líneas se muestran sin parsear.
const lineasCrudas = 6

// maxRagged es cuántas líneas desparejas se listan. Más que eso no ayuda a
// decidir: si hay cientos, el problema es el delimitador y no las líneas.
const maxRagged = 20

// Inspect abre el archivo y lo describe.
func Inspect(path string, o Options) (*Inspection, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir %s: %w", path, err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s es una carpeta, no un archivo", path)
	}
	coma, err := o.coma()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir %s: %w", path, err)
	}
	defer f.Close()

	insp := &Inspection{
		File:   FileInfo{Path: path, Name: nombreDe(path), Bytes: st.Size()},
		Sample: [][]string{},
		Ragged: []Ragged{},
	}
	insp.Raw, insp.NotUTF8, err = primerasLineas(f, lineasCrudas)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("no se pudo volver al principio de %s: %w", path, err)
	}
	src, err := sinBOM(f)
	if err != nil {
		return nil, err
	}

	r := lector(src, coma)
	// `linea` es la LÍNEA FÍSICA donde empieza el registro, no el número de
	// registro: un campo citado con un salto de línea adentro ocupa dos líneas
	// y desplazaba todos los números que vienen después —«el lote que falló
	// empieza en la línea N» apuntaba mal— (C-28 de la auditoría del
	// 2026-09-11). FieldPos la da después de cada Read.
	registro, linea := 0, 0
	campos := -1
	for {
		fila, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("no se pudo leer %s en la línea %d: %w", insp.File.Name, lineaDelError(err, linea), err)
		}
		registro++
		linea, _ = r.FieldPos(0)
		if campos < 0 {
			campos = len(fila)
		}
		// El aviso de codificación se decide sobre el archivo ENTERO, no sobre
		// el prefijo que se muestra: `Inspect` ya lo recorre todo para contar
		// las filas, así que mirar solo los primeros 64 KiB dejaba pasar un
		// archivo cuya basura empieza más adelante —y lo importaba como
		// caracteres rotos, que es justo lo que este aviso existe para evitar—.
		if !insp.NotUTF8 && !filaEsUTF8(fila) {
			insp.NotUTF8 = true
		}
		if len(fila) != campos && len(insp.Ragged) < maxRagged {
			insp.Ragged = append(insp.Ragged, Ragged{Line: linea, Fields: len(fila)})
		}
		if registro == 1 && o.HasHeader {
			insp.Columns = limpiar(fila, o)
			continue
		}
		if insp.Columns == nil {
			insp.Columns = genericas(len(fila))
		}
		insp.Rows++
		if len(insp.Sample) < muestra {
			insp.Sample = append(insp.Sample, limpiar(fila, o))
		}
	}
	if insp.Columns == nil {
		// Un archivo vacío, o con solo el encabezado.
		insp.Columns = []string{}
	}
	return insp, nil
}

// bom es la marca de orden de bytes de UTF-8, que Excel escribe al guardar un
// CSV y que hay que saltear: sin esto, el primer nombre de columna vendría con
// tres bytes invisibles adelante y no coincidiría con ninguna columna de la
// tabla — un error que se ve como «no existe la columna "id"» sobre una tabla
// que tiene `id`.
var bom = []byte{0xEF, 0xBB, 0xBF}

// sinBOM devuelve un lector que saltea la marca si está.
func sinBOM(r io.ReadSeeker) (io.Reader, error) {
	cabeza := make([]byte, len(bom))
	n, err := io.ReadFull(r, cabeza)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("leer el principio del archivo: %w", err)
	}
	desde := int64(0)
	if n == len(bom) && string(cabeza) == string(bom) {
		desde = int64(len(bom))
	}
	if _, err := r.Seek(desde, io.SeekStart); err != nil {
		return nil, fmt.Errorf("posicionar el archivo: %w", err)
	}
	return r, nil
}

// lector arma el lector con las reglas que este proyecto quiere.
func lector(r io.Reader, coma rune) *csv.Reader {
	c := csv.NewReader(r)
	c.Comma = coma
	// -1: las líneas con otra cantidad de campos NO son un error de lectura.
	// Se cuentan y se avisan, que es más útil que cortar en la primera.
	c.FieldsPerRecord = -1
	// Una comilla suelta en el medio de un campo es habitual en los CSV que
	// escribe la gente a mano, y cortar la lectura por eso deja el archivo
	// inservible en vez de importable.
	c.LazyQuotes = true
	c.ReuseRecord = false
	return c
}

// limpiar aplica el recorte de espacios si se pidió.
func limpiar(fila []string, o Options) []string {
	if !o.Trim {
		return fila
	}
	out := make([]string, len(fila))
	for i, v := range fila {
		out[i] = strings.TrimSpace(v)
	}
	return out
}

func genericas(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("columna %d", i+1)
	}
	return out
}

// primerasLineas devuelve el principio del archivo tal cual, y si tiene bytes
// que no son UTF-8.
func primerasLineas(r io.Reader, n int) (string, bool, error) {
	const tope = 64 << 10
	buf := make([]byte, tope)
	leidos, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", false, fmt.Errorf("leer el principio del archivo: %w", err)
	}
	buf = buf[:leidos]
	// El corte del búfer cae en un byte cualquiera, así que si un carácter de
	// varios bytes queda partido al final, `utf8.Valid` diría que el archivo no
	// es UTF-8 — de un archivo perfectamente válido, solo por ser más grande
	// que el búfer. Se descarta la secuencia incompleta del final antes de
	// mirar, salvo que el archivo entero haya entrado: ahí no hay corte y un
	// final truncado ES un archivo mal formado.
	mirar := buf
	if leidos == tope {
		mirar = sinColaPartida(buf)
	}
	malo := !utf8.Valid(mirar)

	texto := strings.TrimPrefix(string(buf), "\uFEFF")
	lineas := strings.SplitN(texto, "\n", n+1)
	if len(lineas) > n {
		lineas = lineas[:n]
	}
	return strings.TrimRight(strings.Join(lineas, "\n"), "\r\n"), malo, nil
}

// sinColaPartida recorta el carácter incompleto que pueda haber quedado al
// final de un corte arbitrario.
//
// Un carácter UTF-8 mide como mucho cuatro bytes, así que mirar los últimos
// tres alcanza: se busca hacia atrás el arranque de secuencia más cercano y, si
// lo que sigue no llega a estar completo, se corta ahí.
func sinColaPartida(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if r, n := utf8.DecodeRune(b[i:]); r == utf8.RuneError && n <= 1 {
				return b[:i]
			}
			return b
		}
	}
	return b
}

// filaEsUTF8 dice si todos los campos de la fila son UTF-8 válido.
//
// El lector de CSV entrega los bytes tal cual: Go no valida al construir la
// cadena, así que un byte de latin-1 llega intacto y `utf8.ValidString` lo ve.
func filaEsUTF8(fila []string) bool {
	for _, c := range fila {
		if !utf8.ValidString(c) {
			return false
		}
	}
	return true
}

func nombreDe(ruta string) string {
	if i := strings.LastIndexAny(ruta, `/\`); i >= 0 {
		return ruta[i+1:]
	}
	return ruta
}

// Rows recorre las filas de datos del archivo y las entrega de a una.
//
// Es lo que usa la importación: un archivo de dos millones de filas no se junta
// en memoria, igual que una tabla no se junta al exportarla. `fn` devuelve
// error para cortar.
func Rows(path string, o Options, fn func(linea int, fila []*string) error) error {
	coma, err := o.coma()
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("no se pudo abrir %s: %w", path, err)
	}
	defer f.Close()
	src, err := sinBOM(f)
	if err != nil {
		return err
	}

	r := lector(src, coma)
	registro, linea := 0, 0
	for {
		fila, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("no se pudo leer la línea %d: %w", lineaDelError(err, linea), err)
		}
		registro++
		// La línea física, como en Inspect: es lo que se muestra.
		linea, _ = r.FieldPos(0)
		if registro == 1 && o.HasHeader {
			continue
		}
		if err := fn(linea, valores(limpiar(fila, o), o)); err != nil {
			return err
		}
	}
}

// lineaDelError es la línea física donde empieza el registro que no se pudo
// leer: la trae el propio error del lector. Si no la trae, la siguiente a la
// del último registro bueno, que es lo mejor que se sabe.
func lineaDelError(err error, ultimaBuena int) int {
	var pe *csv.ParseError
	if errors.As(err, &pe) && pe.StartLine > 0 {
		return pe.StartLine
	}
	return ultimaBuena + 1
}

// valores pasa los campos a la forma que espera la base: nil es NULL.
func valores(fila []string, o Options) []*string {
	out := make([]*string, len(fila))
	for i := range fila {
		if o.EmptyAsNull && fila[i] == "" {
			continue
		}
		v := fila[i]
		out[i] = &v
	}
	return out
}
