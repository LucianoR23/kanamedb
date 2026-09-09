package change

import (
	"strings"
	"testing"
)

func agregar(t *testing.T, s *Set, c Change) string {
	t.Helper()
	id, err := s.Add(c)
	if err != nil {
		t.Fatalf("Add(%s) falló: %v", c.Type, err)
	}
	return id
}

func tipos(cs []Change) []Type {
	out := make([]Type, len(cs))
	for i, c := range cs {
		out[i] = c.Type
	}
	return out
}

// El orden en que se editó no sirve para ejecutar: nadie crea una tabla antes de
// acordarse de que la necesitaba.
//
// La fixture está escrita AL REVÉS del orden correcto a propósito. Si Ordered
// devolviera el orden de edición, cada sentencia fallaría por el objeto que
// todavía no existe o por el que ya se borró.
func TestOrderedPoneCadaCosaDondePuedeEjecutarse(t *testing.T) {
	s := &Set{}

	agregar(t, s, Change{Type: DropTable, Schema: "p", Table: "vieja"})
	agregar(t, s, Change{Type: DropColumn, Schema: "p", Table: "pedidos",
		Column: &Column{Name: "obsoleta"}})
	agregar(t, s, Change{Type: DropConstraint, Schema: "p", Table: "pedidos", Name: "pedidos_fk"})
	agregar(t, s, Change{Type: AddForeignKey, Schema: "p", Table: "pedidos",
		Names: []string{"cliente_id"}, RefTable: "clientes", RefNames: []string{"id"}})
	agregar(t, s, Change{Type: AddColumn, Schema: "p", Table: "pedidos",
		Column: &Column{Name: "cliente_id", DataType: "bigint", Nullable: true}})
	agregar(t, s, Change{Type: CreateTable, Schema: "p", Table: "clientes",
		Columns: []Column{{Name: "id", DataType: "bigint"}}})

	quiero := []Type{
		CreateTable,    // 1. la tabla que las demás van a referenciar
		AddColumn,      // 2. la columna que la clave necesita
		AddForeignKey,  // 3. la clave, ya con su columna
		DropConstraint, // 5. sacar la restricción antes que su columna
		DropColumn,     // 6.
		DropTable,      // 7. lo último
	}
	if got := tipos(s.Ordered()); !igual(got, quiero) {
		t.Errorf("Ordered() = %v\n  se esperaba %v", got, quiero)
	}
}

// Dentro de una misma fase manda el orden de edición: dos columnas agregadas a
// la misma tabla tienen que salir como se escribieron, o la SQL cambia sola
// entre dos previews idénticos.
func TestOrderedConservaElOrdenDeEdicionDentroDeCadaFase(t *testing.T) {
	s := &Set{}
	for _, n := range []string{"a", "b", "c", "d"} {
		agregar(t, s, Change{Type: AddColumn, Schema: "p", Table: "t",
			Column: &Column{Name: n, DataType: "text", Nullable: true}})
	}
	for i, c := range s.Ordered() {
		if quiero := string(rune('a' + i)); c.Column.Name != quiero {
			t.Errorf("posición %d = %q, se esperaba %q", i, c.Column.Name, quiero)
		}
	}
}

func TestLoExcluidoQuedaEnLaListaYFueraDelApply(t *testing.T) {
	s := &Set{}
	uno := agregar(t, s, Change{Type: DropTable, Schema: "p", Table: "a"})
	agregar(t, s, Change{Type: DropTable, Schema: "p", Table: "b"})

	if !s.SetExcluded(uno, true) {
		t.Fatal("SetExcluded no encontró el cambio")
	}
	if n := len(s.List()); n != 2 {
		t.Errorf("List() devolvió %d: lo excluido tiene que seguir en la lista", n)
	}
	if n := len(s.Ordered()); n != 1 {
		t.Errorf("Ordered() devolvió %d: lo excluido no tiene que ejecutarse", n)
	}
	if r := s.Summarize(); r.Total != 2 || r.Included != 1 {
		t.Errorf("Summarize() = %+v", r)
	}

	if !s.SetExcluded(uno, false) {
		t.Fatal("SetExcluded no encontró el cambio para volver a incluirlo")
	}
	if n := len(s.Ordered()); n != 2 {
		t.Errorf("Ordered() devolvió %d después de volver a incluir", n)
	}
}

// Lo que la interfaz pinta en rojo y lo que exige confirmación extra contra
// producción. «Destructivo» no es «riesgoso»: un SET NOT NULL puede fallar y
// bloquear la tabla, pero no pierde nada.
func TestQueSeConsideraDestructivo(t *testing.T) {
	casos := []struct {
		c      Change
		quiero bool
	}{
		{Change{Type: DropTable}, true},
		{Change{Type: DropColumn}, true},
		{Change{Type: SetColumnType}, true},
		{Change{Type: DropConstraint}, false},
		{Change{Type: DropConstraint, Cascade: true}, true},
		{Change{Type: DropIndex}, false},
		{Change{Type: DropIndex, Cascade: true}, true},
		{Change{Type: SetNotNull}, false},
		{Change{Type: AddColumn}, false},
		{Change{Type: CreateTable}, false},
		{Change{Type: RenameTable}, false},
	}
	for _, c := range casos {
		if got := c.c.Destructive(); got != c.quiero {
			t.Errorf("%s.Destructive() = %v, se esperaba %v", c.c.Type, got, c.quiero)
		}
	}
}

// Un cambio que no valida no llega al renderizado. El error tiene que decir qué
// falta, no «invalid input».
func TestValidateExplicaQueFalta(t *testing.T) {
	casos := []struct {
		nombre string
		c      Change
		dice   string
	}{
		{"sin tabla", Change{Type: DropTable}, "tabla"},
		{"crear sin columnas", Change{Type: CreateTable, Table: "t"}, "columna"},
		{"renombrar sin destino", Change{Type: RenameTable, Table: "t"}, "nombre nuevo"},
		{"clave con columnas desparejas", Change{
			Type: AddForeignKey, Table: "t", Names: []string{"a", "b"},
			RefTable: "o", RefNames: []string{"x"},
		}, "posición"},
		{"check sin expresión", Change{Type: AddCheck, Table: "t"}, "expresión"},
		{"operación inventada", Change{Type: "volarLaBase", Table: "t"}, "desconocida"},
		// Postgres rechaza una columna NOT NULL nueva sin default en una tabla
		// con filas, y su error no explica qué hacer.
		{"columna not null sin default", Change{
			Type: AddColumn, Table: "t",
			Column: &Column{Name: "c", DataType: "text", Nullable: false},
		}, "valor por defecto"},
		// Vaciar el default es otra operación, no un SetDefault con "".
		{"default vacío", Change{
			Type: SetDefault, Table: "t", Column: &Column{Name: "c"},
		}, string(DropDefault)},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			err := c.c.Validate()
			if err == nil {
				t.Fatal("Validate() no devolvió error")
			}
			if !strings.Contains(err.Error(), c.dice) {
				t.Errorf("el error no menciona %q: %v", c.dice, err)
			}
		})
	}
}

func TestAddRechazaLoQueNoValida(t *testing.T) {
	s := &Set{}
	if _, err := s.Add(Change{Type: DropTable}); err == nil {
		t.Fatal("Add() aceptó un cambio inválido")
	}
	if s.Len() != 0 {
		t.Error("el cambio inválido quedó en el changeset")
	}
}

// Describe va a logs y mensajes, así que no puede filtrar nada de los datos.
func TestDescribeNoFiltraDatos(t *testing.T) {
	s := &Set{}
	if got := s.Describe(); got != "sin cambios pendientes" {
		t.Errorf("Describe() vacío = %q", got)
	}
	agregar(t, s, Change{Type: DropTable, Schema: "p", Table: "secreta"})
	agregar(t, s, Change{Type: AddColumn, Schema: "p", Table: "t",
		Column: &Column{Name: "c", DataType: "text", Nullable: true}})
	got := s.Describe()
	if !strings.Contains(got, "2 cambios") || !strings.Contains(got, "1 destructivos") {
		t.Errorf("Describe() = %q", got)
	}
}

func igual(a, b []Type) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
