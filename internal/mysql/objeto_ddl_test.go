package mysql_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// TestReemplazarUnProcedimientoMandaLosPasosPorSeparado.
//
// Es el camino que el caso compartido NO toca, y por eso el fallo vivió sin que
// nada se pusiera rojo: la suite reemplaza una VISTA, y una vista en esta
// familia se reemplaza en el lugar. Las rutinas y los triggers no, así que son
// los únicos que pasan por el DROP + CREATE.
//
// Y ese par NO se puede mandar como una sola cadena: el DSN lleva
// `multiStatements=false` a propósito, así que `DROP …; CREATE …` junto es un
// ERROR DE SINTAXIS y no dos sentencias. Sin los pasos separados, reemplazar
// una rutina o un trigger era imposible en MySQL y en MariaDB — todas.
func TestReemplazarUnProcedimientoMandaLosPasosPorSeparado(t *testing.T) {
	for _, m := range motores {
		t.Run(m.nombre, func(t *testing.T) {
			c := abrir(t, m.nombre, m.dsn)
			ctx := context.Background()
			base := c.Server().CurrentDB

			exec := func(sql string) {
				t.Helper()
				if err := c.Exec(ctx, sql); err != nil {
					t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
				}
			}
			_ = c.Exec(ctx, "DROP PROCEDURE IF EXISTS kn_proc")
			exec("CREATE PROCEDURE kn_proc() SELECT 1")
			t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP PROCEDURE IF EXISTS kn_proc") })

			cambio := change.Change{
				Type:       change.ReplaceObject,
				ObjectKind: schema.ObjProcedure,
				Schema:     base,
				Name:       "kn_proc",
				Recreate:   true,
				Definition: "CREATE PROCEDURE kn_proc() SELECT 2",
			}
			st, err := c.RenderDDL(ctx, cambio)
			if err != nil {
				t.Fatalf("RenderDDL(): %v", err)
			}
			if len(st.Steps) != 2 {
				t.Fatalf("los dos pasos tienen que ir por separado y hay %d: %v", len(st.Steps), st.Steps)
			}
			// Mandar la cadena entera es lo que NO funciona, y se comprueba: si
			// algún día el DSN cambiara, este caso deja de tener sentido y hay
			// que saberlo.
			if err := c.Exec(ctx, st.SQL); err == nil {
				t.Error("las dos sentencias juntas corrieron: el DSN dejó de tener multiStatements=false " +
					"y este caso ya no prueba lo que dice")
			}

			for _, paso := range st.Steps {
				if err := c.Exec(ctx, paso); err != nil {
					t.Fatalf("el paso %q no corre: %v", paso, err)
				}
			}

			def, err := c.ObjectDefinition(ctx, schema.Object{
				Kind: schema.ObjProcedure, Schema: base, Name: "kn_proc",
			})
			if err != nil {
				t.Fatalf("ObjectDefinition(): %v", err)
			}
			if !strings.Contains(def.SQL, "2") {
				t.Errorf("el procedimiento no se reemplazó:\n%s", def.SQL)
			}
		})
	}
}
