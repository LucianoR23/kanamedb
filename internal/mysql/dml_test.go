package mysql

import (
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

func texto(s string) *string { return &s }

// Los cambios de datos con la gramática de MySQL: acentos invertidos,
// marcadores ?, y una fila sin valores es `() VALUES ()` porque DEFAULT VALUES
// no existe.
func TestLosCambiosDeDatosSeEscribenComoMySQL(t *testing.T) {
	casos := []struct {
		nombre  string
		c       change.Change
		legible string
		corre   string
	}{
		{
			"insertar",
			change.Change{Type: change.InsertRow, Table: "personas",
				Values: []change.Cell{{Column: "nombre", Value: texto("O'Brien")}, {Column: "apodo", Value: nil}}},
			"INSERT INTO `personas` (`nombre`, `apodo`) VALUES ('O''Brien', NULL)",
			"INSERT INTO `personas` (`nombre`, `apodo`) VALUES (?, NULL)",
		},
		{
			"insertar sin valores",
			change.Change{Type: change.InsertRow, Schema: "demo", Table: "personas"},
			"INSERT INTO `demo`.`personas` () VALUES ()",
			"INSERT INTO `demo`.`personas` () VALUES ()",
		},
		{
			"actualizar",
			change.Change{Type: change.UpdateRow, Table: "personas",
				Values: []change.Cell{{Column: "apodo", Value: texto("x")}, {Column: "edad", Value: texto("30")}},
				Key:    []change.Cell{{Column: "id", Value: texto("7")}}},
			"UPDATE `personas` SET `apodo` = 'x', `edad` = '30' WHERE `id` = '7'",
			"UPDATE `personas` SET `apodo` = ?, `edad` = ? WHERE `id` = ?",
		},
		{
			"borrar",
			change.Change{Type: change.DeleteRow, Table: "personas",
				Key: []change.Cell{{Column: "id", Value: texto("7")}}},
			"DELETE FROM `personas` WHERE `id` = '7'",
			"DELETE FROM `personas` WHERE `id` = ?",
		},
	}
	for _, tc := range casos {
		st, err := RenderDDL(tc.c, engine.MySQL)
		if err != nil {
			t.Fatalf("%s: %v", tc.nombre, err)
		}
		if st.SQL != tc.legible {
			t.Errorf("%s, SQL legible:\n  %s\nse esperaba\n  %s", tc.nombre, st.SQL, tc.legible)
		}
		if st.Bound == nil || st.Bound.SQL != tc.corre {
			t.Errorf("%s, SQL ejecutable:\n  %v\nse esperaba\n  %s", tc.nombre, st.Bound, tc.corre)
		}
	}
}

// El literal de la SQL legible sigue el modo del servidor, igual que en el
// DDL: con la barra invertida como escape se duplica, y con
// NO_BACKSLASH_ESCAPES no se toca. La SQL que corre no cambia en ninguno de
// los dos, porque el valor va como parámetro.
func TestElLiteralDeUnValorSigueElModoDelServidor(t *testing.T) {
	c := change.Change{Type: change.UpdateRow, Table: "t",
		Values: []change.Cell{{Column: "ruta", Value: texto(`C:\datos`)}},
		Key:    []change.Cell{{Column: "id", Value: texto("1")}}}

	normal, _ := renderDDL(c, engine.MySQL, false)
	if quiero := "UPDATE `t` SET `ruta` = 'C:" + `\\` + "datos' WHERE `id` = '1'"; normal.SQL != quiero {
		t.Errorf("modo normal:\n  %s\nse esperaba\n  %s", normal.SQL, quiero)
	}
	sinEscapes, _ := renderDDL(c, engine.MySQL, true)
	if quiero := "UPDATE `t` SET `ruta` = 'C:" + `\` + "datos' WHERE `id` = '1'"; sinEscapes.SQL != quiero {
		t.Errorf("NO_BACKSLASH_ESCAPES:\n  %s\nse esperaba\n  %s", sinEscapes.SQL, quiero)
	}
	if normal.Bound.SQL != sinEscapes.Bound.SQL {
		t.Errorf("la SQL ejecutable no depende del modo: %q contra %q", normal.Bound.SQL, sinEscapes.Bound.SQL)
	}
	if v := *normal.Bound.Args[0].(*string); v != `C:\datos` {
		t.Errorf("el parámetro va sin escapar, tal cual: %q", v)
	}
}
