package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// TestValidaLosIdentificadores comprueba la única defensa que hay entre un
// nombre escrito por el usuario y la SQL que se manda al motor.
//
// El bucle que valida y el mapa que lo alimenta estaban dados vuelta: el mapa
// guardaba descripción→nombre y el bucle lo leía como nombre→descripción, así
// que lo que se medía era el largo de la DESCRIPCIÓN. Un nombre de tabla de 300
// caracteres pasaba, y uno con un salto de línea adentro también.
func TestValidaLosIdentificadores(t *testing.T) {
	largo := strings.Repeat("x", maxIdent+1)

	malos := []struct {
		nombre string
		c      change.Change
	}{
		{"tabla demasiado larga", change.Change{Type: change.DropTable, Table: largo}},
		{"tabla con salto de línea", change.Change{Type: change.DropTable, Table: "con\nsalto"}},
		{"tabla con byte nulo", change.Change{Type: change.DropTable, Table: "con\x00nulo"}},
		{"columna demasiado larga", change.Change{
			Type: change.DropColumn, Table: "t", Column: &change.Column{Name: largo}}},
		{"nombre nuevo demasiado largo", change.Change{
			Type: change.RenameTable, Table: "t", NewName: largo}},
		{"índice con nombre demasiado largo", change.Change{
			Type: change.DropIndex, Table: "t", Name: largo}},
		{"columna de un índice demasiado larga", change.Change{
			Type: change.AddIndex, Table: "t", Names: []string{largo}}},
		{"tabla referenciada demasiado larga", change.Change{
			Type: change.AddForeignKey, Table: "t", Names: []string{"a"},
			RefTable: largo, RefNames: []string{"b"}}},
	}
	for _, m := range malos {
		t.Run(m.nombre, func(t *testing.T) {
			// db nil: tiene que rechazarlo ANTES de tocar la base. Si llegara a
			// usarla, el test explota en vez de dar un falso verde.
			_, err := renderDDL(context.Background(), nil, m.c)
			if err == nil {
				t.Fatal("renderDDL lo aceptó")
			}
		})
	}

	// Y un nombre justo en el límite tiene que pasar: una validación que
	// rechaza de más es tan mala como una que no rechaza nada.
	enElLimite := change.Change{Type: change.DropTable, Table: strings.Repeat("y", maxIdent)}
	if _, err := renderDDL(context.Background(), nil, enElLimite); err != nil {
		t.Errorf("renderDDL rechazó un nombre de exactamente %d caracteres: %v", maxIdent, err)
	}
}
