package postgres

import (
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

func texto(s string) *string { return &s }

// Los cambios de datos salen por el mismo RenderDDL que el esquema, con la
// gramática de Postgres: comillas dobles, marcadores $n, y una fila sin
// valores es DEFAULT VALUES.
func TestLosCambiosDeDatosSeEscribenComoPostgres(t *testing.T) {
	casos := []struct {
		nombre  string
		c       change.Change
		legible string
		corre   string
		args    int
	}{
		{
			"insertar",
			change.Change{Type: change.InsertRow, Schema: "demo", Table: "personas",
				Values: []change.Cell{{Column: "nombre", Value: texto("O'Brien")}, {Column: "apodo", Value: nil}}},
			`INSERT INTO "demo"."personas" ("nombre", "apodo") VALUES ('O''Brien', NULL)`,
			`INSERT INTO "demo"."personas" ("nombre", "apodo") VALUES ($1, NULL)`,
			1,
		},
		{
			"insertar sin valores",
			change.Change{Type: change.InsertRow, Schema: "demo", Table: "personas"},
			`INSERT INTO "demo"."personas" DEFAULT VALUES`,
			`INSERT INTO "demo"."personas" DEFAULT VALUES`,
			0,
		},
		{
			"actualizar",
			change.Change{Type: change.UpdateRow, Schema: "demo", Table: "personas",
				Values: []change.Cell{{Column: "apodo", Value: texto(`C:\ruta`)}},
				Key:    []change.Cell{{Column: "id", Value: texto("7")}}},
			`UPDATE "demo"."personas" SET "apodo" = 'C:\ruta' WHERE "id" = '7'`,
			`UPDATE "demo"."personas" SET "apodo" = $1 WHERE "id" = $2`,
			2,
		},
		{
			"borrar con clave compuesta",
			change.Change{Type: change.DeleteRow, Schema: "demo", Table: "prestamos",
				Key: []change.Cell{{Column: "libro", Value: texto("1")}, {Column: "socio", Value: texto("2")}}},
			`DELETE FROM "demo"."prestamos" WHERE "libro" = '1' AND "socio" = '2'`,
			`DELETE FROM "demo"."prestamos" WHERE "libro" = $1 AND "socio" = $2`,
			2,
		},
	}
	for _, tc := range casos {
		st, err := RenderDDL(tc.c)
		if err != nil {
			t.Fatalf("%s: %v", tc.nombre, err)
		}
		if st.SQL != tc.legible {
			t.Errorf("%s, SQL legible:\n  %s\nse esperaba\n  %s", tc.nombre, st.SQL, tc.legible)
		}
		if st.Bound == nil {
			t.Fatalf("%s: sin Bound", tc.nombre)
		}
		if st.Bound.SQL != tc.corre {
			t.Errorf("%s, SQL ejecutable:\n  %s\nse esperaba\n  %s", tc.nombre, st.Bound.SQL, tc.corre)
		}
		if len(st.Bound.Args) != tc.args {
			t.Errorf("%s: %d args, se esperaban %d", tc.nombre, len(st.Bound.Args), tc.args)
		}
		if st.Impact != change.ImpactData {
			t.Errorf("%s: Impact = %q", tc.nombre, st.Impact)
		}
	}
}

// Los nombres de columna de los valores y de la clave pasan por la misma
// validación que cualquier otro identificador.
func TestLasColumnasDeUnCambioDeDatosSeValidan(t *testing.T) {
	largo := ""
	for len(largo) <= maxIdent {
		largo += "x"
	}
	_, err := RenderDDL(change.Change{Type: change.DeleteRow, Table: "t",
		Key: []change.Cell{{Column: largo, Value: texto("1")}}})
	if err == nil {
		t.Fatal("una columna de clave más larga que el límite tendría que rechazarse")
	}
}
