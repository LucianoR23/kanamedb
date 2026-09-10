package dml

import (
	"strconv"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// dialectoDePrueba imita a Postgres: `$n` y comillas dobles.
var dialectoDePrueba = Dialect{
	Table:        func(e, t string) string { return e + "." + t },
	QuoteIdent:   func(s string) string { return `"` + s + `"` },
	QuoteLiteral: func(s string) string { return "'" + s + "'" },
	Placeholder:  func(n int) string { return "$" + strconv.Itoa(n) },
	EmptyInsert:  "DEFAULT VALUES",
}

func vals(s ...string) []*string {
	out := make([]*string, len(s))
	for i := range s {
		x := s[i]
		out[i] = &x
	}
	return out
}

func TestWhereEscribeCadaOperador(t *testing.T) {
	casos := []struct {
		nombre string
		cond   query.Condition
		sql    string
		args   []string
	}{
		{"igual", query.Condition{Column: "a", Operator: query.OpEq, Values: vals("1")},
			`"a" = $1`, []string{"1"}},
		{"distinto incluye los nulos", query.Condition{Column: "a", Operator: query.OpNe, Values: vals("1")},
			`("a" <> $1 OR "a" IS NULL)`, []string{"1"}},
		{"menor", query.Condition{Column: "a", Operator: query.OpLt, Values: vals("5")},
			`"a" < $1`, []string{"5"}},
		{"mayor o igual", query.Condition{Column: "a", Operator: query.OpGte, Values: vals("5")},
			`"a" >= $1`, []string{"5"}},
		{"contiene", query.Condition{Column: "a", Operator: query.OpContains, Values: vals("ana")},
			`"a" LIKE $1 ESCAPE '!'`, []string{"%ana%"}},
		{"empieza con", query.Condition{Column: "a", Operator: query.OpStartsWith, Values: vals("ana")},
			`"a" LIKE $1 ESCAPE '!'`, []string{"ana%"}},
		{"termina con", query.Condition{Column: "a", Operator: query.OpEndsWith, Values: vals("ana")},
			`"a" LIKE $1 ESCAPE '!'`, []string{"%ana"}},
		{"es nulo", query.Condition{Column: "a", Operator: query.OpIsNull},
			`"a" IS NULL`, nil},
		{"no es nulo", query.Condition{Column: "a", Operator: query.OpIsNotNull},
			`"a" IS NOT NULL`, nil},
		{"en la lista", query.Condition{Column: "a", Operator: query.OpIn, Values: vals("1", "2", "3")},
			`"a" IN ($1, $2, $3)`, []string{"1", "2", "3"}},
		{"no en la lista", query.Condition{Column: "a", Operator: query.OpNotIn, Values: vals("1", "2")},
			`"a" NOT IN ($1, $2)`, []string{"1", "2"}},
		{"entre", query.Condition{Column: "a", Operator: query.OpBetween, Values: vals("1", "9")},
			`"a" BETWEEN $1 AND $2`, []string{"1", "9"}},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			sql, args, err := Where([]query.Condition{c.cond}, dialectoDePrueba, 0)
			if err != nil {
				t.Fatalf("Where(): %v", err)
			}
			if sql != c.sql {
				t.Errorf("SQL = %q, quería %q", sql, c.sql)
			}
			if len(args) != len(c.args) {
				t.Fatalf("args = %d, quería %d", len(args), len(c.args))
			}
			for i, a := range args {
				p, ok := a.(*string)
				if !ok || p == nil {
					t.Fatalf("el argumento %d no es un *string con valor: %#v", i, a)
				}
				if *p != c.args[i] {
					t.Errorf("el argumento %d es %q y quería %q", i, *p, c.args[i])
				}
			}
		})
	}
}

// TestNingunValorEntraEnLaSQL es la invariante de CLAUDE.md aplicada al filtro:
// lo único que se concatena es el nombre de la columna.
func TestNingunValorEntraEnLaSQL(t *testing.T) {
	conds := []query.Condition{
		{Column: "nombre", Operator: query.OpEq, Values: vals("'; DROP TABLE t; --")},
		{Column: "nota", Operator: query.OpContains, Values: vals("O'Brien")},
		{Column: "id", Operator: query.OpIn, Values: vals("1", "2")},
		{Column: "f", Operator: query.OpBetween, Values: vals("2026-01-01", "2026-12-31")},
	}
	sql, args, err := Where(conds, dialectoDePrueba, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, prohibido := range []string{"DROP TABLE", "O'Brien", "2026-01-01", "'1'"} {
		if strings.Contains(sql, prohibido) {
			t.Errorf("el valor %q está escrito en la SQL:\n%s", prohibido, sql)
		}
	}
	if len(args) != 6 {
		t.Errorf("se esperaban 6 parámetros y hay %d", len(args))
	}
	// Y las condiciones se combinan con AND, no con nada.
	if strings.Count(sql, " AND ") < 3 {
		t.Errorf("las condiciones no se combinaron con AND:\n%s", sql)
	}
}

// TestUnComodinTecleadoAManoNoEsUnComodin: buscar «50%» tiene que buscar «50%»,
// no «todo lo que empieza con 50».
func TestUnComodinTecleadoAManoNoEsUnComodin(t *testing.T) {
	casos := []struct{ entra, sale string }{
		{"50%", "%50!%%"},
		{"a_b", "%a!_b%"},
		{"!", "%!!%"},
		{"100%_!", "%100!%!_!!%"},
		{"sin nada raro", "%sin nada raro%"},
	}
	for _, c := range casos {
		t.Run(c.entra, func(t *testing.T) {
			_, args, err := Where([]query.Condition{
				{Column: "a", Operator: query.OpContains, Values: vals(c.entra)},
			}, dialectoDePrueba, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := *(args[0].(*string)); got != c.sale {
				t.Errorf("el patrón de %q es %q y quería %q", c.entra, got, c.sale)
			}
		})
	}
}

func TestLosParametrosSiguenLaNumeracionQueSeLePide(t *testing.T) {
	// `desde` existe para engancharse después de otros parámetros: el conteo
	// filtrado no tiene ninguno antes, pero una lectura con clave sí.
	sql, args, err := Where([]query.Condition{
		{Column: "a", Operator: query.OpEq, Values: vals("1")},
		{Column: "b", Operator: query.OpEq, Values: vals("2")},
	}, dialectoDePrueba, 3)
	if err != nil {
		t.Fatal(err)
	}
	if sql != `"a" = $4 AND "b" = $5` {
		t.Errorf("SQL = %q", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %d", len(args))
	}
}

func TestUnFiltroInvalidoNoLlegaAlMotor(t *testing.T) {
	casos := []struct {
		nombre string
		cond   query.Condition
	}{
		{"sin columna", query.Condition{Operator: query.OpEq, Values: vals("1")}},
		{"operador que no existe", query.Condition{Column: "a", Operator: "regex", Values: vals("1")}},
		{"faltan valores", query.Condition{Column: "a", Operator: query.OpBetween, Values: vals("1")}},
		{"sobran valores", query.Condition{Column: "a", Operator: query.OpEq, Values: vals("1", "2")}},
		{"lista vacía", query.Condition{Column: "a", Operator: query.OpIn}},
		{"un valor donde no va", query.Condition{Column: "a", Operator: query.OpIsNull, Values: vals("1")}},
		{"NULL como valor", query.Condition{Column: "a", Operator: query.OpEq, Values: []*string{nil}}},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if _, _, err := Where([]query.Condition{c.cond}, dialectoDePrueba, 0); err == nil {
				t.Error("se aceptó un filtro que no se puede aplicar")
			}
		})
	}
}

func TestSinCondicionesNoHayClausula(t *testing.T) {
	sql, args, err := Where(nil, dialectoDePrueba, 0)
	if err != nil || sql != "" || args != nil {
		t.Fatalf("Where(nil) = %q, %v, %v", sql, args, err)
	}
}
