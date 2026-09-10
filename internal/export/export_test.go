package export

import (
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/query"
)

func p(s string) *string { return &s }

var columnas = []query.Column{
	{Name: "id", DataType: "int4", Class: query.ClassNumber},
	{Name: "nombre", DataType: "text", Class: query.ClassText},
	{Name: "activo", DataType: "bool", Class: query.ClassBool},
	{Name: "extra", DataType: "jsonb", Class: query.ClassJSON},
}

var filas = [][]*string{
	{p("1"), p("Ana"), p("t"), p(`{"a": 1}`)},
	{p("2"), p(""), p("f"), nil},
	{p("3"), nil, nil, p("no es json")},
}

func render(t *testing.T, f Format, o Options, rows [][]*string) string {
	t.Helper()
	s, err := Render(f, o, columnas, rows, 0)
	if err != nil {
		t.Fatalf("Render(%s): %v", f, err)
	}
	return s
}

func TestCSVDistingueNuloDeVacioYVuelveALeerse(t *testing.T) {
	got := render(t, CSV, Options{}, filas)
	want := "id,nombre,activo,extra\n" +
		"1,Ana,t,\"{\"\"a\"\": 1}\"\n" +
		"2,\"\",f,\\N\n" +
		"3,\\N,\\N,no es json\n"
	if got != want {
		t.Fatalf("CSV:\n%s\nquería:\n%s", got, want)
	}
	// Lo que se escribe tiene que poder leerse con un lector estricto.
	r := csv.NewReader(strings.NewReader(got))
	regs, err := r.ReadAll()
	if err != nil {
		t.Fatalf("el CSV no se vuelve a leer: %v", err)
	}
	if len(regs) != 4 || regs[1][3] != `{"a": 1}` || regs[2][1] != "" || regs[2][3] != `\N` {
		t.Fatalf("leído: %q", regs)
	}
}

func TestCSVCitaLoQueRompeElFormato(t *testing.T) {
	rows := [][]*string{
		{p("1"), p("con, coma"), p("t"), p(`dice "hola"`)},
		{p("2"), p("dos\nlíneas"), p("f"), p(`\N`)},
		{p("3"), p(" con espacios "), p("t"), p("a;b")},
	}
	got := render(t, CSV, Options{NoHeader: true}, rows)
	want := "1,\"con, coma\",t,\"dice \"\"hola\"\"\"\n" +
		"2,\"dos\nlíneas\",f,\"\\N\"\n" +
		"3, con espacios ,t,a;b\n"
	if got != want {
		t.Fatalf("CSV:\n%s\nquería:\n%s", got, want)
	}
	regs, err := csv.NewReader(strings.NewReader(got)).ReadAll()
	if err != nil {
		t.Fatalf("no se vuelve a leer: %v", err)
	}
	// El `\N` que era un valor de verdad vuelve como valor, no como NULL: está
	// citado, y así lo distingue quien lo lea.
	if regs[1][3] != `\N` || regs[0][3] != `dice "hola"` || regs[1][1] != "dos\nlíneas" {
		t.Fatalf("leído: %q", regs)
	}
}

func TestCSVOpciones(t *testing.T) {
	casos := []struct {
		nombre string
		o      Options
		want   string
	}{
		{"punto y coma", Options{Delimiter: ";"}, "id;nombre;activo;extra\n1;Ana;t;\"{\"\"a\"\": 1}\"\n"},
		{"tabulación", Options{Delimiter: "\t"}, "id\tnombre\tactivo\textra\n1\tAna\tt\t\"{\"\"a\"\": 1}\"\n"},
		{"sin encabezado", Options{NoHeader: true}, "1,Ana,t,\"{\"\"a\"\": 1}\"\n"},
		{"todo citado", Options{QuoteAll: true, NoHeader: true}, "\"1\",\"Ana\",\"t\",\"{\"\"a\"\": 1}\"\n"},
		{"con BOM", Options{BOM: true, NoHeader: true}, "\uFEFF1,Ana,t,\"{\"\"a\"\": 1}\"\n"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := render(t, CSV, c.o, filas[:1])
			if got != c.want {
				t.Fatalf("CSV:\n%q\nquería:\n%q", got, c.want)
			}
		})
	}
}

func TestCSVNuloVacio(t *testing.T) {
	got := render(t, CSV, Options{NullAsEmpty: true, NoHeader: true}, filas[1:])
	// Con NullAsEmpty, NULL es nada y la cadena vacía sigue siendo `""`: es
	// exactamente la convención de `COPY … CSV`, y por eso se distinguen.
	want := "2,\"\",f,\n3,,,no es json\n"
	if got != want {
		t.Fatalf("CSV:\n%q\nquería:\n%q", got, want)
	}
}

func TestCSVQuoteAllNoCitaElNulo(t *testing.T) {
	got := render(t, CSV, Options{QuoteAll: true, NoHeader: true}, filas[2:])
	// Un NULL citado dejaría de ser NULL para quien lo lea.
	if got != "\"3\",\\N,\\N,\"no es json\"\n" {
		t.Fatalf("CSV: %q", got)
	}
}

func TestDelimitadorInvalido(t *testing.T) {
	if _, err := New(CSV, io.Discard, Options{Delimiter: "|"}); err == nil {
		t.Fatal("aceptó un delimitador que no está en la lista")
	}
	if _, err := New(Format("xml"), io.Discard, Options{}); err == nil {
		t.Fatal("aceptó un formato desconocido")
	}
}

func TestJSONRespetaLosTiposSoloCuandoElTextoEsInequivoco(t *testing.T) {
	got := render(t, JSON, Options{}, filas)
	want := "[\n" +
		`{"id":1,"nombre":"Ana","activo":true,"extra":{"a": 1}}` + ",\n" +
		`{"id":2,"nombre":"","activo":false,"extra":null}` + ",\n" +
		`{"id":3,"nombre":null,"activo":null,"extra":"no es json"}` + "\n]\n"
	if got != want {
		t.Fatalf("JSON:\n%s\nquería:\n%s", got, want)
	}
	var back []map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("el JSON no se vuelve a leer: %v", err)
	}
	if back[0]["id"] != float64(1) || back[0]["activo"] != true || back[2]["nombre"] != nil {
		t.Fatalf("leído: %v", back)
	}
}

func TestJSONNoInventaTipos(t *testing.T) {
	cols := []query.Column{
		{Name: "n", Class: query.ClassNumber},
		{Name: "b", Class: query.ClassBool},
		{Name: "t", Class: query.ClassText},
		{Name: "j", Class: query.ClassJSON},
		{Name: "o", Class: query.ClassOther},
	}
	rows := [][]*string{
		// NaN no es un número de JSON; "42" en una columna de texto es texto;
		// un json inválido es una cadena; "1" en bool es true.
		{p("NaN"), p("1"), p("42"), p("{no}"), p("true")},
		{p("1e+20"), p("yes"), p("t"), p("[1, 2]"), p("null")},
		{p("007"), p("0"), nil, p(`"cadena"`), p("1")},
	}
	s, err := Render(JSONL, Options{}, cols, rows, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"n":"NaN","b":true,"t":"42","j":"{no}","o":"true"}` + "\n" +
		`{"n":1e+20,"b":"yes","t":"t","j":[1, 2],"o":"null"}` + "\n" +
		`{"n":"007","b":false,"t":null,"j":"cadena","o":"1"}` + "\n"
	if s != want {
		t.Fatalf("JSONL:\n%s\nquería:\n%s", s, want)
	}
	for _, linea := range strings.Split(strings.TrimSpace(s), "\n") {
		if !json.Valid([]byte(linea)) {
			t.Fatalf("línea inválida: %s", linea)
		}
	}
}

func TestJSONEscapaYNoEscapaDeMas(t *testing.T) {
	cols := []query.Column{{Name: `col "rara" | <x>`, Class: query.ClassText}}
	rows := [][]*string{{p("línea\ncon\ttab, \"comillas\", \\ y <b>&amp;</b> y \x01 y \xff")}}
	s, err := Render(JSONL, Options{}, cols, rows, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"col \"rara\" | <x>":"línea\ncon\ttab, \"comillas\", \\ y <b>&amp;</b> y \u0001 y ` + "\uFFFD" + `"}` + "\n"
	if s != want {
		t.Fatalf("JSONL:\n%s\nquería:\n%s", s, want)
	}
	var back map[string]string
	if err := json.Unmarshal([]byte(s), &back); err != nil {
		t.Fatalf("no se vuelve a leer: %v", err)
	}
	if back[`col "rara" | <x>`] != "línea\ncon\ttab, \"comillas\", \\ y <b>&amp;</b> y \x01 y \uFFFD" {
		t.Fatalf("leído: %q", back)
	}
}

func TestJSONSinFilasEsUnArrayVacio(t *testing.T) {
	if got := render(t, JSON, Options{}, nil); got != "[]\n" {
		t.Fatalf("JSON vacío: %q", got)
	}
	if got := render(t, JSONL, Options{}, nil); got != "" {
		t.Fatalf("JSONL vacío: %q", got)
	}
}

func TestMarkdown(t *testing.T) {
	rows := [][]*string{
		{p("1"), p("con | barra"), p("t"), p("dos\r\nlíneas")},
		{p("2"), p(""), nil, p("y\notra")},
	}
	got := render(t, Markdown, Options{}, rows)
	want := "| id | nombre | activo | extra |\n" +
		"| --: | --- | --- | --- |\n" +
		"| 1 | con \\| barra | t | dos<br>líneas |\n" +
		"| 2 |  | NULL | y<br>otra |\n"
	if got != want {
		t.Fatalf("Markdown:\n%s\nquería:\n%s", got, want)
	}
	// Con NullAsEmpty el NULL desaparece; el encabezado se queda aunque se
	// pida sin él, porque una tabla sin encabezado no es una tabla.
	got = render(t, Markdown, Options{NullAsEmpty: true, NoHeader: true}, rows[1:])
	if !strings.HasPrefix(got, "| id |") || !strings.Contains(got, "| 2 |  |  | y<br>otra |\n") {
		t.Fatalf("Markdown:\n%s", got)
	}
}

func TestRenderCortaEnElLimite(t *testing.T) {
	s, err := Render(CSV, Options{NoHeader: true}, columnas, filas, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(s, "\n") != 2 {
		t.Fatalf("quería 2 filas: %q", s)
	}
	// El gzip se ignora al renderizar: el texto es para leerlo.
	s2, err := Render(CSV, Options{NoHeader: true, Gzip: true}, columnas, filas, 2)
	if err != nil {
		t.Fatal(err)
	}
	if s2 != s {
		t.Fatalf("con gzip cambió el texto: %q", s2)
	}
}

func TestGzipEscribeUnArchivoQueSeDescomprime(t *testing.T) {
	var b bytes.Buffer
	n, err := Write(CSV, &b, Options{Gzip: true}, columnas, filas, 0)
	if err != nil || n != 3 {
		t.Fatalf("Write: %d, %v", n, err)
	}
	r, err := gzip.NewReader(&b)
	if err != nil {
		t.Fatalf("no es gzip: %v", err)
	}
	plano, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("descomprimir: %v", err)
	}
	if string(plano) != render(t, CSV, Options{}, filas) {
		t.Fatalf("descomprimido:\n%s", plano)
	}
}

func TestUnaFilaConOtraCantidadDeColumnasEsUnError(t *testing.T) {
	for _, f := range Formats {
		esc, err := NewInto(f, io.Discard, Options{}, &destinoDePrueba)
		if err != nil {
			t.Fatal(err)
		}
		if err := esc.Begin(columnas); err != nil {
			t.Fatal(err)
		}
		if err := esc.Row([]*string{p("1")}); err == nil {
			t.Fatalf("%s aceptó una fila corta", f)
		}
	}
}

func TestExtension(t *testing.T) {
	want := map[Format]string{CSV: ".csv", JSON: ".json", JSONL: ".jsonl", Markdown: ".md", SQL: ".sql"}
	for _, f := range Formats {
		if f.Extension() != want[f] {
			t.Fatalf("%s: %q", f, f.Extension())
		}
	}
}

func TestNeutralizarFormulasDePlanilla(t *testing.T) {
	cols := []query.Column{{Name: "v", Class: query.ClassText}}
	rows := [][]*string{
		{p("=1+1")},
		{p("=cmd|' /c calc'!A1")},
		{p("+34 600 00 00 00")},
		{p("-5")},
		{p("@SUM(A1)")},
		{p("\t=1+1")},
		{p("texto normal")},
		{p("a=b")},
		{p("")},
		{nil},
	}
	got, err := Render(CSV, Options{NoHeader: true, NeutralizeFormulas: true}, cols, rows, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "\"'=1+1\"\n" +
		"\"'=cmd|' /c calc'!A1\"\n" +
		"\"'+34 600 00 00 00\"\n" +
		"\"'-5\"\n" +
		"\"'@SUM(A1)\"\n" +
		"\"'\t=1+1\"\n" +
		"texto normal\n" +
		"a=b\n" +
		"\"\"\n" +
		`\N` + "\n"
	if got != want {
		t.Fatalf("CSV:\n%q\nquería:\n%q", got, want)
	}
	// Lo que sale sigue siendo CSV válido y el apóstrofo es parte del VALOR:
	// es lo que hace que la planilla lo lea como texto.
	regs, err := csv.NewReader(strings.NewReader(got)).ReadAll()
	if err != nil {
		t.Fatalf("no se vuelve a leer: %v", err)
	}
	if regs[0][0] != "'=1+1" || regs[6][0] != "texto normal" {
		t.Fatalf("leído: %q", regs)
	}

	// Apagado —que es el default— no toca nada: un archivo que se va a volver
	// a importar tiene que decir lo que decía.
	sinTocar, err := Render(CSV, Options{NoHeader: true}, cols, rows[:1], 0)
	if err != nil {
		t.Fatal(err)
	}
	if sinTocar != "=1+1\n" {
		t.Fatalf("con la opción apagada cambió el valor: %q", sinTocar)
	}
}

func TestSoloElCSVNeutralizaFormulas(t *testing.T) {
	// JSON y Markdown no los ejecuta ninguna planilla: agregarles un apóstrofo
	// sería corromper el valor sin ganar nada.
	cols := []query.Column{{Name: "v", Class: query.ClassText}}
	rows := [][]*string{{p("=1+1")}}
	for _, f := range []Format{JSON, JSONL, Markdown} {
		got, err := Render(f, Options{NeutralizeFormulas: true}, cols, rows, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "=1+1") || strings.Contains(got, "'=1+1") {
			t.Errorf("%s cambió el valor: %q", f, got)
		}
	}
}

/* ------------------------------------------------------------ formato SQL */

// destinoDePrueba cita como Postgres.
var destinoDePrueba = SQLTarget{
	Table:        `public."pedidos"`,
	QuoteIdent:   func(s string) string { return `"` + s + `"` },
	QuoteLiteral: func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" },
}

func sql(t *testing.T, o Options, cols []query.Column, rows [][]*string) string {
	t.Helper()
	var b strings.Builder
	e, err := NewInto(SQL, &b, o, &destinoDePrueba)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Begin(cols); err != nil {
		t.Fatal(err)
	}
	for _, f := range rows {
		if err := e.Row(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.End(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestSQLEscribeInsertsQueSePuedenVolverACorrer(t *testing.T) {
	got := sql(t, Options{}, columnas, filas)
	want := `INSERT INTO public."pedidos" ("id", "nombre", "activo", "extra") VALUES` + "\n" +
		`(1, 'Ana', 't', '{"a": 1}'),` + "\n" +
		`(2, '', 'f', NULL),` + "\n" +
		`(3, NULL, NULL, 'no es json');` + "\n"
	if got != want {
		t.Fatalf("SQL:\n%s\nquería:\n%s", got, want)
	}
}

// TestSQLCitaLoQueRompeElArchivo: es el único lugar donde un valor se escribe
// adentro de la SQL, así que la comilla simple tiene que quedar escapada.
func TestSQLCitaLoQueRompeElArchivo(t *testing.T) {
	cols := []query.Column{{Name: "n", Class: query.ClassText}, {Name: "x", Class: query.ClassNumber}}
	rows := [][]*string{
		{p("O'Brien"), p("1")},
		{p("'); drop table pedidos; --"), p("2")},
		{p("dos\nlíneas"), p("NaN")},
	}
	got := sql(t, Options{}, cols, rows)
	want := `INSERT INTO public."pedidos" ("n", "x") VALUES` + "\n" +
		`('O''Brien', 1),` + "\n" +
		`('''); drop table pedidos; --', 2),` + "\n" +
		"('dos\nlíneas', 'NaN');\n"
	if got != want {
		t.Fatalf("SQL:\n%s\nquería:\n%s", got, want)
	}
}

func TestSQLParteEnLotesYCierraElUltimo(t *testing.T) {
	cols := []query.Column{{Name: "id", Class: query.ClassNumber}}
	rows := make([][]*string, filasPorSentencia+3)
	for i := range rows {
		v := fmt.Sprint(i + 1)
		rows[i] = []*string{&v}
	}
	got := sql(t, Options{}, cols, rows)
	// Dos sentencias: una de 500 y otra de 3, las dos terminadas en punto y coma.
	if n := strings.Count(got, "INSERT INTO"); n != 2 {
		t.Errorf("se escribieron %d sentencias y se esperaban 2", n)
	}
	if n := strings.Count(got, ";\n"); n != 2 {
		t.Errorf("hay %d puntos y coma y se esperaban 2: un archivo sin cerrar la última "+
			"sentencia no se puede volver a correr", n)
	}
	if !strings.HasSuffix(got, "(503);\n") {
		t.Errorf("el archivo termina en %q", got[len(got)-20:])
	}
}

func TestSQLSinFilasNoEscribeNada(t *testing.T) {
	if got := sql(t, Options{}, columnas, nil); got != "" {
		t.Fatalf("una tabla vacía escribió %q; un INSERT sin filas no es SQL válida", got)
	}
}

func TestSQLNecesitaSaberLaTabla(t *testing.T) {
	if _, err := New(SQL, io.Discard, Options{}); err == nil {
		t.Fatal("se aceptó el formato SQL sin decir a qué tabla insertar")
	}
}
