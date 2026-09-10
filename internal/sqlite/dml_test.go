package sqlite

import (
	"context"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

func texto(s string) *string { return &s }

// Los cambios de datos con la gramática de SQLite: comillas dobles, marcadores
// ?, sin esquema, y DEFAULT VALUES para una fila vacía. No hace falta una base
// abierta: a diferencia del resto del DDL de SQLite, un UPDATE no reconstruye
// nada y no tiene que leer la tabla.
func TestLosCambiosDeDatosSeEscribenComoSQLite(t *testing.T) {
	casos := []struct {
		nombre  string
		c       change.Change
		legible string
		corre   string
	}{
		{
			"insertar",
			change.Change{Type: change.InsertRow, Schema: "main", Table: "personas",
				Values: []change.Cell{{Column: "nombre", Value: texto(`O'Brien\n`)}}},
			`INSERT INTO "personas" ("nombre") VALUES ('O''Brien\n')`,
			`INSERT INTO "personas" ("nombre") VALUES (?)`,
		},
		{
			"insertar sin valores",
			change.Change{Type: change.InsertRow, Table: "personas"},
			`INSERT INTO "personas" DEFAULT VALUES`,
			`INSERT INTO "personas" DEFAULT VALUES`,
		},
		{
			"actualizar",
			change.Change{Type: change.UpdateRow, Table: "personas",
				Values: []change.Cell{{Column: "apodo", Value: nil}},
				Key:    []change.Cell{{Column: "id", Value: texto("7")}}},
			`UPDATE "personas" SET "apodo" = NULL WHERE "id" = '7'`,
			`UPDATE "personas" SET "apodo" = NULL WHERE "id" = ?`,
		},
		{
			"borrar",
			change.Change{Type: change.DeleteRow, Table: "personas",
				Key: []change.Cell{{Column: "id", Value: texto("7")}}},
			`DELETE FROM "personas" WHERE "id" = '7'`,
			`DELETE FROM "personas" WHERE "id" = ?`,
		},
	}
	for _, tc := range casos {
		st, err := renderDDL(context.Background(), nil, tc.c)
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
