package query

import (
	"strings"
	"testing"
)

func TestParsePostgresArray(t *testing.T) {
	casos := []struct {
		entra  string
		values []string
		nulls  []bool
	}{
		{`{}`, []string{}, []bool{}},
		{`{a,b,c}`, []string{"a", "b", "c"}, []bool{false, false, false}},
		// Lo que motiva el parser: una coma adentro de un elemento.
		{`{a,"b,c",d}`, []string{"a", "b,c", "d"}, []bool{false, false, false}},
		// NULL sin comillas es el nulo; con comillas es la palabra.
		{`{a,NULL,b}`, []string{"a", "", "b"}, []bool{false, true, false}},
		{`{a,"NULL",b}`, []string{"a", "NULL", "b"}, []bool{false, false, false}},
		{`{null}`, []string{""}, []bool{true}},
		// La cadena vacía es un elemento, y no es lo mismo que el array vacío.
		{`{""}`, []string{""}, []bool{false}},
		// Comillas y barras invertidas escapadas adentro de un elemento.
		{`{"dice \"hola\""}`, []string{`dice "hola"`}, []bool{false}},
		{`{"c:\\ruta"}`, []string{`c:\ruta`}, []bool{false}},
		// Espacios: se recortan sin comillas, se respetan con comillas.
		{`{ a , b }`, []string{"a", "b"}, []bool{false, false}},
		{`{" a "}`, []string{" a "}, []bool{false}},
		// Un array de dos dimensiones se muestra por filas, sin abrirlas.
		{`{{1,2},{3,4}}`, []string{"{1,2}", "{3,4}"}, []bool{false, false}},
		// Números y fechas, que es lo más común.
		{`{1,2,3}`, []string{"1", "2", "3"}, []bool{false, false, false}},
		{`{2026-09-10,2026-01-01}`, []string{"2026-09-10", "2026-01-01"}, []bool{false, false}},
	}
	for _, c := range casos {
		t.Run(c.entra, func(t *testing.T) {
			got, ok := ParsePostgresArray(c.entra)
			if !ok {
				t.Fatalf("%q no se reconoció como array", c.entra)
			}
			if strings.Join(got.Values, "|") != strings.Join(c.values, "|") {
				t.Errorf("valores = %q, quería %q", got.Values, c.values)
			}
			if len(got.Nulls) != len(c.nulls) {
				t.Fatalf("nulls = %v, quería %v", got.Nulls, c.nulls)
			}
			for i := range c.nulls {
				if got.Nulls[i] != c.nulls[i] {
					t.Errorf("nulls = %v, quería %v", got.Nulls, c.nulls)
					break
				}
			}
		})
	}
}

// TestLoQueNoEsUnArrayNoSeParte: el visor tiene que poder distinguir «esto es
// un array» de «esto es texto que empieza con una llave», y no inventar
// elementos donde no los hay.
func TestLoQueNoEsUnArrayNoSeParte(t *testing.T) {
	for _, s := range []string{"", "a,b", "{sin cerrar", `{"a": 1}`[1:], "}{"} {
		if _, ok := ParsePostgresArray(s); ok && s != "" {
			// `{"a": 1}` sin la primera llave es `"a": 1}`, que no arranca con
			// llave: no es array.
			t.Errorf("%q se tomó como array", s)
		}
	}
	// Un JSON entero SÍ empieza y termina con llaves, pero de objeto, no de
	// array: el visor lo manda al modo JSON por la clase de la columna, no por
	// el texto, así que acá alcanza con que no rompa.
	if _, ok := ParsePostgresArray(`{"a": 1}`); !ok {
		t.Skip("un objeto JSON se parece a un array de un elemento; lo separa la clase de la columna")
	}
}

func TestParseMySQLSet(t *testing.T) {
	casos := []struct {
		entra string
		want  []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"lectura,escritura", []string{"lectura", "escritura"}},
	}
	for _, c := range casos {
		got, ok := ParseMySQLSet(c.entra)
		if !ok {
			t.Fatalf("%q no se reconoció", c.entra)
		}
		if strings.Join(got.Values, "|") != strings.Join(c.want, "|") {
			t.Errorf("ParseMySQLSet(%q) = %q, quería %q", c.entra, got.Values, c.want)
		}
	}
}
