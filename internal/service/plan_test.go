package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/history"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// La única decisión de seguridad del plan: pedir el plan de un DELETE no borra
// nada, en ninguno de los motores. Es lo que distingue EXPLAIN de EXPLAIN
// ANALYZE, y la razón de que el segundo no exista acá.
func TestElPlanNoEjecutaLaSentencia(t *testing.T) {
	for _, caso := range motoresDePrueba {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c, _ := abrirMotorDePrueba(t, caso)
			q := NewQueries(sesion)
			ctx := context.Background()
			tabla := "kn_plan_" + strings.ReplaceAll(caso.nombre, "-", "_")

			correr := func(sql string) *query.Batch {
				t.Helper()
				res := q.Run(ctx, "r", sql)
				if !res.OK {
					t.Fatalf("Run(%q) falló: %+v", sql, res.Failure)
				}
				return res.Batch
			}
			correr("DROP TABLE IF EXISTS " + tabla)
			correr("CREATE TABLE " + tabla + " (id " + tipoEnteroDe(c.Engine) + " PRIMARY KEY, nombre " + tipoTextoDe(c.Engine) + ")")
			t.Cleanup(func() { q.Run(ctx, "r", "DROP TABLE IF EXISTS "+tabla) })
			correr("INSERT INTO " + tabla + " (id, nombre) VALUES (1, 'uno')")
			correr("INSERT INTO " + tabla + " (id, nombre) VALUES (2, 'dos')")

			res := q.Explain(ctx, "p", "DELETE FROM "+tabla+" WHERE id > 0", 1)
			if !res.OK {
				t.Fatalf("Explain() falló: %+v", res.Failure)
			}
			if res.Batch == nil || len(res.Batch.Results) != 1 {
				t.Fatalf("el plan tendría que ser un resultado: %+v", res.Batch)
			}
			plan := res.Batch.Results[0]
			if !plan.ReturnsRows || len(plan.Rows) == 0 {
				t.Errorf("el plan vino sin filas: %+v", plan)
			}
			if plan.Line != 1 {
				t.Errorf("el plan no dice de qué línea es: %d", plan.Line)
			}

			cuenta := correr("SELECT count(*) FROM " + tabla)
			if got := *cuenta.Results[0].Rows[0][0]; got != "2" {
				t.Errorf("después de pedir el plan del DELETE quedan %s filas; tenían que quedar 2", got)
			}

			// Y lo que el review encontró: una sentencia que empieza con las
			// OPCIONES de EXPLAIN, pegada detrás del prefijo, es un EXPLAIN
			// ANALYZE, que ejecuta. Postgres las acepta con y sin paréntesis;
			// MySQL también tiene EXPLAIN ANALYZE. Ninguna puede llegar al
			// motor, y las filas tienen que seguir ahí.
			for _, trampa := range []string{
				"ANALYZE DELETE FROM " + tabla + " WHERE id > 0",
				"(ANALYZE) DELETE FROM " + tabla + " WHERE id > 0",
				"ANALYSE DELETE FROM " + tabla + " WHERE id > 0",
				"FORMAT=TREE DELETE FROM " + tabla + " WHERE id > 0",
				"QUERY PLAN DELETE FROM " + tabla + " WHERE id > 0",
				"DROP TABLE " + tabla,
			} {
				res := q.Explain(ctx, "p", trampa, 1)
				if res.OK {
					t.Errorf("Explain(%q) aceptó una sentencia que no es una consulta", trampa)
				} else if res.Failure == nil || !strings.Contains(res.Failure.Hint, "SELECT, INSERT, UPDATE, DELETE") {
					t.Errorf("Explain(%q) no explica qué se puede planificar: %+v", trampa, res.Failure)
				}
			}
			cuenta = correr("SELECT count(*) FROM " + tabla)
			if got := *cuenta.Results[0].Rows[0][0]; got != "2" {
				t.Errorf("después de las trampas quedan %s filas; tenían que quedar 2", got)
			}
		})
	}
}

// Con varias sentencias, se explica la que está bajo el cursor; sin cursor
// sobre ninguna, se dice.
func TestElPlanEsDeLaSentenciaBajoElCursor(t *testing.T) {
	sesion, _, _ := abrirMotorDePrueba(t, motoresDePrueba[len(motoresDePrueba)-1]) // sqlite
	q := NewQueries(sesion)
	ctx := context.Background()
	texto := "-- dos consultas\nSELECT 1 AS primera;\n\nSELECT 2 AS segunda\n  WHERE 1 = 1;\n"

	casos := []struct {
		linea  int
		quiere string
	}{
		{2, "primera"},
		{3, "primera"}, // la línea en blanco después: sigue siendo la anterior
		{4, "segunda"},
		{5, "segunda"},
	}
	for _, caso := range casos {
		res := q.Explain(ctx, "p", texto, caso.linea)
		if !res.OK {
			t.Fatalf("Explain(línea %d) falló: %+v", caso.linea, res.Failure)
		}
		plan := res.Batch.Results[0]
		var detalle strings.Builder
		for _, fila := range plan.Rows {
			for _, celda := range fila {
				if celda != nil {
					detalle.WriteString(*celda + " ")
				}
			}
		}
		// SQLite escribe en el plan qué hace; un SELECT constante es «SCAN
		// CONSTANT ROW». No se puede leer el alias, así que se verifica la
		// línea que el resultado declara.
		quiereLinea := map[string]int{"primera": 2, "segunda": 4}[caso.quiere]
		if plan.Line != quiereLinea {
			t.Errorf("cursor en la línea %d: el plan es de la línea %d, se esperaba la %d (%s)", caso.linea, plan.Line, quiereLinea, detalle.String())
		}
	}

	// Cursor sobre el comentario, antes de la primera.
	res := q.Explain(ctx, "p", texto, 1)
	if res.OK || res.Failure == nil || !strings.Contains(res.Failure.Message, "el cursor no está sobre ninguna") {
		t.Errorf("con el cursor antes de la primera se esperaba que lo dijera; dio %+v", res)
	}

	// Sin sentencias.
	res = q.Explain(ctx, "p", "-- nada\n", 1)
	if res.OK || res.Failure == nil || !strings.Contains(res.Failure.Message, "ninguna sentencia") {
		t.Errorf("sin sentencias se esperaba un fallo claro; dio %+v", res)
	}
}

// Un EXPLAIN escrito a mano no se envuelve: `EXPLAIN EXPLAIN` es un error de
// sintaxis, y un `EXPLAIN ANALYZE` a mano correría desde un botón que promete
// no correr nada.
func TestElPlanRechazaUnExplainEscritoAMano(t *testing.T) {
	sesion, _, _ := abrirMotorDePrueba(t, motoresDePrueba[len(motoresDePrueba)-1]) // sqlite
	q := NewQueries(sesion)
	ctx := context.Background()

	for _, sql := range []string{"EXPLAIN SELECT 1", "-- con comentario\n  explain query plan SELECT 1"} {
		res := q.Explain(ctx, "p", sql, 1)
		if res.OK || res.Failure == nil || !strings.Contains(res.Failure.Message, "ya es un EXPLAIN") {
			t.Errorf("Explain(%q) tendría que rechazarse; dio %+v", sql, res)
		}
	}
}

// El plan no es una ejecución: no va al historial.
func TestElPlanNoSeAnotaEnElHistorial(t *testing.T) {
	sesion, c, _ := abrirMotorDePrueba(t, motoresDePrueba[len(motoresDePrueba)-1]) // sqlite
	q := NewQueries(sesion)
	dir := t.TempDir()
	h := history.New(filepath.Join(dir, "historial.json"), filepath.Join(dir, "consultas.json"))
	UsarHistorial(q, h)
	ctx := context.Background()
	entradas := func() int {
		t.Helper()
		lista, err := h.List(c.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		return len(lista)
	}

	if res := q.Explain(ctx, "p", "SELECT 1", 1); !res.OK {
		t.Fatalf("Explain(): %+v", res.Failure)
	}
	if n := entradas(); n != 0 {
		t.Errorf("el plan quedó en el historial (%d entradas)", n)
	}
	if res := q.Run(ctx, "r", "SELECT 1"); !res.OK {
		t.Fatalf("Run(): %+v", res.Failure)
	}
	if n := entradas(); n != 1 {
		t.Errorf("la ejecución no quedó en el historial (%d entradas)", n)
	}
}
