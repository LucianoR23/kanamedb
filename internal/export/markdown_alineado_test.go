package export

import (
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/query"
)

func cols(nombres ...string) []query.Column {
	out := make([]query.Column, 0, len(nombres))
	for _, n := range nombres {
		out = append(out, query.Column{Name: n, Class: query.ClassText})
	}
	return out
}

func s(v string) *string { return &v }

// TestElMarkdownAlineadoSeLeeAntesDeRenderizarse.
//
// Es la razón de existir del alineado: una tabla pegada en un ticket o en un
// pull request se lee ANTES de que nada la dibuje. Markdown la renderiza igual
// con relleno o sin él, así que lo único que se gana es eso — y es todo.
func TestElMarkdownAlineadoSeLeeAntesDeRenderizarse(t *testing.T) {
	c := cols("id", "nombre")
	c[0].Class = query.ClassNumber
	filas := [][]*string{
		{s("1"), s("Ana")},
		{s("100"), s("Bernardino")},
	}
	got := RenderMarkdownAlineado(Options{}, c, filas, 0)

	// Todos los renglones miden lo mismo: eso es estar alineado.
	lineas := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lineas) != 4 {
		t.Fatalf("salieron %d renglones:\n%s", len(lineas), got)
	}
	ancho := len([]rune(lineas[0]))
	for i, l := range lineas {
		if n := len([]rune(l)); n != ancho {
			t.Errorf("el renglón %d mide %d y el primero %d:\n%s", i, n, ancho, got)
		}
	}
	// La alineación a la derecha de los números se conserva.
	if !strings.Contains(lineas[1], ":") {
		t.Errorf("la columna numérica perdió su alineación: %q", lineas[1])
	}
}

// TestUnaColumnaEnormeNoRellenaALasDemas.
//
// Sin tope, una columna con un JWT de trescientos caracteres deja a todas las
// otras con doscientos noventa espacios de relleno: la tabla se vuelve
// ilegible, que es lo contrario de para qué se alinea.
func TestUnaColumnaEnormeNoRellenaALasDemas(t *testing.T) {
	jwt := strings.Repeat("x", 300)
	got := RenderMarkdownAlineado(Options{}, cols("id", "token"), [][]*string{
		{s("1"), s(jwt)},
	}, 0)

	// El valor entero está: no se trunca nada.
	if !strings.Contains(got, jwt) {
		t.Error("el valor largo se truncó")
	}
	// Y ningún renglón se infla al ancho del monstruo.
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, jwt) {
			continue
		}
		if len([]rune(l)) > 2*anchoMaximoAlineado {
			t.Errorf("un renglón mide %d por culpa de la columna enorme: %q", len([]rune(l)), l)
		}
	}
}

// TestElAnchoSeMideEnRunasYNoEnBytes.
//
// Un acento son dos bytes y una runa. Midiendo o rellenando en bytes, la
// columna queda corrida justo en las tablas con castellano adentro.
//
// El valor acentuado NO puede ser el más ancho, y eso no es casual: si lo
// fuera, su relleno sería cero de las dos formas y el test no distinguiría
// nada. Tiene que ser uno que NECESITE relleno.
func TestElAnchoSeMideEnRunasYNoEnBytes(t *testing.T) {
	got := RenderMarkdownAlineado(Options{}, cols("v"), [][]*string{
		{s("ÍÍÍ")},      // 3 runas, 6 bytes: le faltan 5 espacios
		{s("ABCDEFGH")}, // 8 runas, 8 bytes: es la más ancha
	}, 0)
	lineas := strings.Split(strings.TrimRight(got, "\n"), "\n")
	ancho := len([]rune(lineas[0]))
	for i, l := range lineas {
		if n := len([]rune(l)); n != ancho {
			t.Errorf("el renglón %d mide %d y el primero %d:\n%s", i, n, ancho, got)
		}
	}
}

// TestElAlineadoEscapaIgualQueElEscritorDeAUnaFila.
//
// Son dos caminos y las reglas de escape tienen que ser las mismas: una barra
// vertical adentro de un valor parte la tabla en dos si no se escapa, y un
// salto de línea también.
func TestElAlineadoEscapaIgualQueElEscritorDeAUnaFila(t *testing.T) {
	c := cols("v")
	filas := [][]*string{{s("con | barra")}, {s("dos\nlíneas")}, {nil}}

	suelto, err := Render(Markdown, Options{}, c, filas, 0)
	if err != nil {
		t.Fatal(err)
	}
	alineado := RenderMarkdownAlineado(Options{}, c, filas, 0)

	for _, q := range []string{`con \| barra`, "dos<br>líneas", "NULL"} {
		if !strings.Contains(suelto, q) {
			t.Errorf("el escritor de a una fila no tiene %q", q)
		}
		if !strings.Contains(alineado, q) {
			t.Errorf("el alineado no tiene %q:\n%s", q, alineado)
		}
	}
}

// TestUnaFilaCortaNoRompeLaTabla: si una fila trae menos valores que columnas,
// las que faltan salen vacías en vez de reventar.
func TestUnaFilaCortaNoRompeLaTabla(t *testing.T) {
	got := RenderMarkdownAlineado(Options{}, cols("a", "b", "c"), [][]*string{
		{s("1")},
	}, 0)
	lineas := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lineas) != 3 {
		t.Fatalf("salieron %d renglones:\n%s", len(lineas), got)
	}
	if strings.Count(lineas[2], "|") != 4 {
		t.Errorf("la fila corta no tiene las tres columnas: %q", lineas[2])
	}
}

// TestElAlineadoNoSePuedeUsarAlEscribirUnArchivo.
//
// Es el candado que impide el fallo caro: un escritor de a una fila no puede
// alinear —no sabe el ancho de las columnas hasta la última fila— y aceptarlo
// significaría juntar dos millones de filas en memoria a mitad de archivo. Se
// rechaza en vez de intentarlo.
func TestElAlineadoNoSePuedeUsarAlEscribirUnArchivo(t *testing.T) {
	var b strings.Builder
	if _, err := NewInto(Markdown, &b, Options{Align: true}, nil); err == nil {
		t.Fatal("el escritor de a una fila aceptó alinear")
	}
	// Y sin la opción sigue funcionando: el candado no puede romper el camino
	// normal.
	if _, err := NewInto(Markdown, &b, Options{}, nil); err != nil {
		t.Fatalf("el escritor normal dejó de funcionar: %v", err)
	}
	// Render sí lo acepta, porque tiene todas las filas.
	texto, err := Render(Markdown, Options{Align: true}, cols("a"), [][]*string{{s("x")}}, 0)
	if err != nil {
		t.Fatalf("Render con alineado: %v", err)
	}
	if !strings.Contains(texto, "| x") {
		t.Errorf("Render no alineó:\n%s", texto)
	}
}
