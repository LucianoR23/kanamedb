package postgres

import (
	"reflect"
	"testing"
)

// Los valores de tgtype salen de src/include/catalog/pg_trigger.h. Se prueban
// combinaciones reales y no bits sueltos: un trigger siempre tiene al menos el
// bit de nivel y uno de evento, así que un caso con un solo bit no se parece a
// nada que el catálogo devuelva.
func TestDescomponerTgtype(t *testing.T) {
	casos := []struct {
		nombre  string
		tipo    int32
		timing  string
		nivel   string
		eventos []string
	}{
		{
			nombre: "after insert or update, por fila",
			// ROW(1) + INSERT(4) + UPDATE(16)
			tipo: 1 | 4 | 16, timing: "after", nivel: "row",
			eventos: []string{"insert", "update"},
		},
		{
			nombre: "before insert, por fila",
			// ROW(1) + BEFORE(2) + INSERT(4)
			tipo: 1 | 2 | 4, timing: "before", nivel: "row",
			eventos: []string{"insert"},
		},
		{
			nombre: "after truncate, por sentencia",
			// TRUNCATE(32), sin ROW: truncate solo existe a nivel sentencia
			tipo: 32, timing: "after", nivel: "statement",
			eventos: []string{"truncate"},
		},
		{
			nombre: "instead of delete, por fila (una vista)",
			// ROW(1) + DELETE(8) + INSTEAD(64)
			tipo: 1 | 8 | 64, timing: "instead of", nivel: "row",
			eventos: []string{"delete"},
		},
		{
			nombre: "after insert or update or delete",
			tipo:   1 | 4 | 8 | 16, timing: "after", nivel: "row",
			eventos: []string{"insert", "update", "delete"},
		},
		{
			nombre: "BEFORE gana sobre el default, pero INSTEAD gana sobre BEFORE",
			// INSTEAD(64) + BEFORE(2): Postgres no los combina, pero si algún
			// día lo hiciera, "instead of" es el que describe el momento real.
			tipo: 1 | 2 | 4 | 64, timing: "instead of", nivel: "row",
			eventos: []string{"insert"},
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			timing, nivel, eventos := descomponerTgtype(c.tipo)
			if timing != c.timing {
				t.Errorf("timing = %q, se esperaba %q", timing, c.timing)
			}
			if nivel != c.nivel {
				t.Errorf("nivel = %q, se esperaba %q", nivel, c.nivel)
			}
			if !reflect.DeepEqual(eventos, c.eventos) {
				t.Errorf("eventos = %v, se esperaba %v", eventos, c.eventos)
			}
		})
	}
}

// Las entradas son salidas literales de pg_get_constraintdef, copiadas de una
// base real. Inventarlas a mano haría un test sobre una gramática imaginaria.
func TestExpresionDeCheck(t *testing.T) {
	casos := []struct{ def, quiero string }{
		{"CHECK ((total >= (0)::numeric))", "total >= (0)::numeric"},
		{"CHECK ((currency ~ '^[A-Z]{3}$'::text))", "currency ~ '^[A-Z]{3}$'::text"},
		{
			"CHECK (((shipped_at IS NULL) OR (shipped_at >= placed_at))) NOT VALID",
			"(shipped_at IS NULL) OR (shipped_at >= placed_at)",
		},
		// Ya sin envoltorio: no se le puede sacar nada más.
		{"CHECK (activo)", "activo"},
		// Los paréntesis del medio no son los externos: sacarlos partiría la
		// expresión en dos mitades inválidas.
		{"CHECK ((a) OR (b))", "(a) OR (b)"},
		// Un paréntesis dentro de un literal desbalancea la cuenta. Ante la
		// duda no se toca nada: dejar de más es prolijo, romper la expresión no.
		{`CHECK ((x ~ '($'::text))`, `(x ~ '($'::text)`},
	}

	for _, c := range casos {
		if got := expresionDeCheck(c.def); got != c.quiero {
			t.Errorf("expresionDeCheck(%q)\n  = %q\n  se esperaba %q", c.def, got, c.quiero)
		}
	}
}

func TestSinParentesisExternosNoTocaLoQueNoEnvuelve(t *testing.T) {
	casos := []struct{ in, quiero string }{
		{"(a)", "a"},
		{"((a))", "(a)"},
		{"(a) OR (b)", "(a) OR (b)"},
		{"a", "a"},
		{"", ""},
		{"(", "("},
		{")", ")"},
		{"()", ""},
		// Desbalanceado: no se toca.
		{"((a)", "((a)"},
	}
	for _, c := range casos {
		if got := sinParentesisExternos(c.in); got != c.quiero {
			t.Errorf("sinParentesisExternos(%q) = %q, se esperaba %q", c.in, got, c.quiero)
		}
	}
}
