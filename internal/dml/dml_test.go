package dml

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// prueba es un dialecto inventado con marcas reconocibles, para que un test
// pueda ver de dónde salió cada pedazo: los identificadores van entre «» y los
// literales entre ‹›.
var prueba = Dialect{
	Table: func(esq, tabla string) string {
		if esq == "" {
			return "«" + tabla + "»"
		}
		return "«" + esq + "».«" + tabla + "»"
	},
	QuoteIdent:   func(s string) string { return "«" + s + "»" },
	QuoteLiteral: func(s string) string { return "‹" + s + "›" },
	Placeholder:  func(n int) string { return fmt.Sprintf("$%d", n) },
	EmptyInsert:  "DEFAULT VALUES",
}

func v(s string) *string { return &s }

func render(t *testing.T, c change.Change) change.Statement {
	t.Helper()
	st, err := Render(c, prueba)
	if err != nil {
		t.Fatalf("Render(%s) falló: %v", c.Type, err)
	}
	return st
}

func TestLasDosFormasDicenLoMismoYLosValoresVanAparte(t *testing.T) {
	st := render(t, change.Change{
		Type: change.UpdateRow, Schema: "p", Table: "t",
		Values: []change.Cell{{Column: "a", Value: v("uno")}, {Column: "b", Value: nil}},
		Key:    []change.Cell{{Column: "id", Value: v("7")}},
	})
	if quiero := `UPDATE «p».«t» SET «a» = ‹uno›, «b» = NULL WHERE «id» = ‹7›`; st.SQL != quiero {
		t.Errorf("SQL legible:\n  %s\nse esperaba\n  %s", st.SQL, quiero)
	}
	if st.Bound == nil {
		t.Fatal("un cambio de datos tiene que traer Bound")
	}
	if quiero := `UPDATE «p».«t» SET «a» = $1, «b» = NULL WHERE «id» = $2`; st.Bound.SQL != quiero {
		t.Errorf("SQL ejecutable:\n  %s\nse esperaba\n  %s", st.Bound.SQL, quiero)
	}
	// El NULL no lleva parámetro: va escrito. Así que hay dos args y no tres,
	// y el orden es el de los marcadores.
	if len(st.Bound.Args) != 2 || *st.Bound.Args[0].(*string) != "uno" || *st.Bound.Args[1].(*string) != "7" {
		t.Errorf("Args = %v, se esperaba [uno 7]", st.Bound.Args)
	}
	if st.Bound.Rows != 1 {
		t.Errorf("Rows = %d: un UPDATE por clave tiene que tocar exactamente una fila", st.Bound.Rows)
	}
	if st.Impact != change.ImpactData || st.Destructive {
		t.Errorf("Impact = %q, Destructive = %v", st.Impact, st.Destructive)
	}
}

// El motivo por el que existe Bound: un valor hostil aparece citado en la SQL
// que se lee y NO aparece en la que se ejecuta.
func TestUnValorHostilNuncaEstaEnLaSQLQueCorre(t *testing.T) {
	hostil := `'; DROP TABLE t; --`
	st := render(t, change.Change{
		Type: change.InsertRow, Table: "t",
		Values: []change.Cell{{Column: "a", Value: v(hostil)}},
	})
	if !strings.Contains(st.SQL, "‹"+hostil+"›") {
		t.Errorf("la SQL legible tiene que mostrar el valor citado: %s", st.SQL)
	}
	if strings.Contains(st.Bound.SQL, "DROP") {
		t.Errorf("el valor se metió en la SQL ejecutable: %s", st.Bound.SQL)
	}
	if quiero := `INSERT INTO «t» («a») VALUES ($1)`; st.Bound.SQL != quiero {
		t.Errorf("SQL ejecutable = %s, se esperaba %s", st.Bound.SQL, quiero)
	}
}

func TestInsertarSinValoresUsaLosDefaults(t *testing.T) {
	st := render(t, change.Change{Type: change.InsertRow, Schema: "p", Table: "t"})
	if quiero := `INSERT INTO «p».«t» DEFAULT VALUES`; st.SQL != quiero || st.Bound.SQL != quiero {
		t.Errorf("SQL = %q / %q, se esperaba %q", st.SQL, st.Bound.SQL, quiero)
	}
	if len(st.Bound.Args) != 0 {
		t.Errorf("sin valores no hay parámetros, hay %d", len(st.Bound.Args))
	}
}

func TestBorrarEsDestructivoYUnaClaveNulaVaComoIsNull(t *testing.T) {
	st := render(t, change.Change{
		Type: change.DeleteRow, Table: "t",
		Key: []change.Cell{{Column: "a", Value: v("1")}, {Column: "b", Value: nil}},
	})
	if quiero := `DELETE FROM «t» WHERE «a» = ‹1› AND «b» IS NULL`; st.SQL != quiero {
		t.Errorf("SQL = %s, se esperaba %s", st.SQL, quiero)
	}
	if quiero := `DELETE FROM «t» WHERE «a» = $1 AND «b» IS NULL`; st.Bound.SQL != quiero {
		t.Errorf("SQL ejecutable = %s, se esperaba %s", st.Bound.SQL, quiero)
	}
	if !st.Destructive || st.Note == "" {
		t.Errorf("borrar una fila es destructivo y lo dice: Destructive = %v, Note = %q", st.Destructive, st.Note)
	}
}

func TestLoQueNoEsDatosNoSeEscribeAca(t *testing.T) {
	_, err := Render(change.Change{Type: change.DropTable, Table: "t"}, prueba)
	if err == nil {
		t.Fatal("un DDL no tiene que renderizarse como DML")
	}
}

func TestLoQueNoValidaNoSeEscribe(t *testing.T) {
	casos := []change.Change{
		{Type: change.UpdateRow, Table: "t", Values: []change.Cell{{Column: "a", Value: v("1")}}},
		{Type: change.DeleteRow, Table: "t"},
		{Type: change.UpdateRow, Table: "t", Key: []change.Cell{{Column: "id", Value: v("1")}}},
		{Type: change.InsertRow, Table: "t",
			Values: []change.Cell{{Column: "a", Value: v("1")}, {Column: "a", Value: v("2")}}},
		{Type: change.InsertRow, Table: "t", Values: []change.Cell{{Column: "", Value: v("1")}}},
	}
	for _, c := range casos {
		if _, err := Render(c, prueba); err == nil {
			t.Errorf("%s con %+v tendría que fallar la validación", c.Type, c)
		}
	}
}
