package service

import (
	"context"
	"errors"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// TestElRebuildDeSQLiteSinTransaccionNoDisparaElCascade cierra el hueco que
// dejaba la casilla «Una sola transacción» apagada.
//
// sqlite.Conn.Begin apaga las claves foráneas antes de reconstruir una tabla,
// y sqlite.Conn.Exec advierte que por él una reconstrucción «borraría las
// filas de las tablas hijas» sin dar error. Con la casilla apagada, cada DDL
// iba por Exec —el camino que la advertencia prohíbe—, así que el mismo cambio
// que con la casilla puesta conservaba las filas hijas, sin ella las borraba.
// Es la clase de invariante que se cuida en un camino y se saltea en el otro.
// Hallazgo K-01 de docs/reviews/audit-fable-2026-09-11.md.
func TestElRebuildDeSQLiteSinTransaccionNoDisparaElCascade(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`CREATE TABLE padre (id integer PRIMARY KEY, nombre text)`,
		`CREATE TABLE hija (
			id integer PRIMARY KEY,
			pid integer REFERENCES padre(id) ON DELETE CASCADE)`,
		`INSERT INTO padre VALUES (1,'a'), (2,'b')`,
		`INSERT INTO hija VALUES (10,1), (11,2)`,
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatalf("Schema(refresh): %v", err)
	}

	_, err = sesion.Stage(ctx, change.Change{
		Type: change.SetNotNull, Schema: "main", Table: "padre", Source: "test",
		Column: &change.Column{Name: "nombre", DataType: "text"},
	}, "")
	if err != nil {
		t.Fatalf("Stage(SetNotNull): %v", err)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: false})
	if err != nil {
		t.Fatalf("Apply(): %v", err)
	}
	if !res.OK {
		t.Fatalf("Apply() falló: %+v", res.Failure)
	}

	if n := contar(t, abierta, "hija"); n != 2 {
		t.Fatalf("después de reconstruir «padre» sin transacción única, «hija» quedó "+
			"con %d filas de 2: el DROP TABLE del rebuild corrió con las claves "+
			"foráneas encendidas y disparó el ON DELETE CASCADE", n)
	}
	if n := contar(t, abierta, "padre"); n != 2 {
		t.Errorf("«padre» quedó con %d filas de 2", n)
	}
}

func contar(t *testing.T, s *openSession, tabla string) int {
	t.Helper()
	n, f := s.db.Count(context.Background(), "main", tabla, nil)
	if f != nil {
		t.Fatalf("count(%s): %s", tabla, f.Message)
	}
	return int(n)
}

// TestUnaReconstruccionDeSQLiteVaSolaEnSuTabla.
//
// Encontrado el 2026-09-12 al probar la huella de la vista previa, no estaba
// en la auditoría: `AddColumn a` + `SetNotNull n` sobre la misma tabla dejaba
// la tabla SIN `a`, y `SetNotNull n` + `SetNotNull m` dejaba `n` nullable. Las
// dos con OK. El guion de la reconstrucción se escribe contra el catálogo de
// ANTES de que corra lo anterior. Hasta que haya una sola reconstrucción por
// tabla que acumule sus cambios, la combinación se rechaza al preparar y al
// aplicar.
func TestUnaReconstruccionDeSQLiteVaSolaEnSuTabla(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	abierta.conn.Safety.AllowApplyWithoutPreview = true
	for _, sql := range []string{
		`CREATE TABLE r (id integer PRIMARY KEY, n text, m text)`,
		`CREATE TABLE otra (id integer PRIMARY KEY, n text)`,
		`INSERT INTO r VALUES (1, 'x', 'y')`,
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatal(err)
	}
	notNull := func(tabla, col string) change.Change {
		return change.Change{Type: change.SetNotNull, Schema: "main", Table: tabla, Source: "test",
			Column: &change.Column{Name: col, DataType: "text"}}
	}
	addCol := func(tabla, col string) change.Change {
		return change.Change{Type: change.AddColumn, Schema: "main", Table: tabla, Source: "test",
			Column: &change.Column{Name: col, DataType: "text", Nullable: true}}
	}

	// Una reconstrucción y un ADD COLUMN de la misma tabla: no entran juntos,
	// en ningún orden.
	if _, err := sesion.Stage(ctx, addCol("r", "a"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(ctx, notNull("r", "n"), ""); !errors.Is(err, ErrRebuildNotAlone) {
		t.Fatalf("SetNotNull detrás de un AddColumn de la misma tabla entró: %v", err)
	}
	// Otra tabla no molesta, y los cambios de datos tampoco.
	if _, err := sesion.Stage(ctx, notNull("otra", "n"), ""); err != nil {
		t.Errorf("una reconstrucción de OTRA tabla se rechazó: %v", err)
	}
	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.InsertRow, Schema: "main", Table: "r", Source: "test",
		Values: []change.Cell{{Column: "id", Value: ptr("2")}},
	}, ""); err != nil {
		t.Errorf("un cambio de datos sobre la tabla se rechazó: %v", err)
	}
	abierta.cambios.Clear()

	if _, err := sesion.Stage(ctx, notNull("r", "n"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(ctx, addCol("r", "a"), ""); !errors.Is(err, ErrRebuildNotAlone) {
		t.Fatalf("AddColumn detrás de una reconstrucción de la misma tabla entró: %v", err)
	}
	// Dos reconstrucciones de la misma tabla tampoco.
	if _, err := sesion.Stage(ctx, notNull("r", "m"), ""); !errors.Is(err, ErrRebuildNotAlone) {
		t.Fatalf("dos reconstrucciones de la misma tabla entraron: %v", err)
	}

	// Y si el changeset se armó igual por otro camino, Apply lo para.
	_, _ = abierta.cambios.Add(notNull("r", "m"))
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); !errors.Is(err, ErrRebuildNotAlone) {
		t.Fatalf("Apply() con dos reconstrucciones devolvió %v", err)
	}
	d, err := sesion.TableDetail(ctx, "main", "r")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range d.Columns {
		if c.Name != "id" && !c.Nullable {
			t.Errorf("se aplicó algo: %s quedó NOT NULL", c.Name)
		}
	}

	// De a uno, las dos quedan.
	abierta.cambios.Clear()
	for _, col := range []string{"n", "m"} {
		if _, err := sesion.Stage(ctx, notNull("r", col), ""); err != nil {
			t.Fatal(err)
		}
		if res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); err != nil || !res.OK {
			t.Fatalf("Apply(%s): %v %+v", col, err, res.Failure)
		}
	}
	d, _ = sesion.TableDetail(ctx, "main", "r")
	for _, c := range d.Columns {
		if (c.Name == "n" || c.Name == "m") && c.Nullable {
			t.Errorf("%s quedó nullable después de aplicar las dos de a una", c.Name)
		}
	}
}
