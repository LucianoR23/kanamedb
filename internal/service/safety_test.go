package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/csvimport"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Los tests de este archivo prueban que cada casilla de Safety tiene código
// detrás. La auditoría del 2026-09-11 (K-02 y K-07) encontró tres que se
// guardaban, se mostraban encendidas en el gestor y no las leía nadie: un
// cartel, que es lo que CLAUDE.md prohíbe. Cada test inyecta la violación
// —apaga la comprobación— y confirma que la tabla se va.

// TestBloquearDropRechazaElApplyYDejaLaTabla: con la casilla puesta, un
// changeset con un DROP no ejecuta NADA, ni siquiera lo que no es DROP.
func TestBloquearDropRechazaElApplyYDejaLaTabla(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE TABLE kn_bloq (id integer PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatal(err)
	}
	abierta.conn.Safety.BlockDropTruncate = true

	cambios := []change.Change{
		{Type: change.CreateTable, Schema: "main", Table: "kn_bloq_nueva", Source: "test",
			Columns: []change.Column{{Name: "id", DataType: "integer"}}, Names: []string{"id"}},
		{Type: change.DropTable, Schema: "main", Table: "kn_bloq", Source: "test"},
	}
	if _, err := sesion.StageMany(ctx, cambios, ""); err != nil {
		t.Fatalf("StageMany(): %v", err)
	}

	_, err = sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if !errors.Is(err, ErrBlockedByPolicy) {
		t.Fatalf("Apply() con «Bloquear DROP y TRUNCATE» tenía que rechazar el changeset "+
			"y devolvió %v", err)
	}
	if !strings.Contains(err.Error(), "DROP") {
		t.Errorf("el error no nombra qué se bloqueó: %v", err)
	}
	if _, f := abierta.db.Count(ctx, "main", "kn_bloq", nil); f != nil {
		t.Fatalf("la tabla se borró con la casilla puesta: %s", f.Message)
	}
	if _, f := abierta.db.Count(ctx, "main", "kn_bloq_nueva", nil); f == nil {
		t.Fatalf("el CREATE del mismo changeset se ejecutó aunque el DROP se rechazó: " +
			"un changeset se aplica entero o no se aplica")
	}
	// El ensayo pasa por el mismo preparar, así que también se niega.
	if _, err := sesion.DryRun(ctx, ""); !errors.Is(err, ErrBlockedByPolicy) {
		t.Errorf("DryRun() tenía que rechazar el changeset y devolvió %v", err)
	}
}

// TestBloquearDropRechazaElDropDelEditor: la misma casilla vale para lo que se
// escribe a mano. Y vale ANTES de correr nada: si el DROP es la tercera
// sentencia, las dos primeras tampoco corren.
func TestBloquearDropRechazaElDropDelEditor(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE TABLE kn_bloq (id integer PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	abierta.conn.Safety.BlockDropTruncate = true
	q := NewQueries(sesion)

	res := q.Run(ctx, "r1", "INSERT INTO kn_bloq VALUES (1);\n/* drop */ drop table kn_bloq;")
	if res.OK || res.Failure == nil {
		t.Fatal("Run() ejecutó un DROP con «Bloquear DROP y TRUNCATE» puesta")
	}
	if res.Failure.Statement != 2 {
		t.Errorf("el fallo tenía que señalar la sentencia 2 y señala la %d", res.Failure.Statement)
	}
	if !strings.Contains(res.Failure.Message, "DROP") {
		t.Errorf("el mensaje no dice qué se bloqueó: %s", res.Failure.Message)
	}
	n, f := abierta.db.Count(ctx, "main", "kn_bloq", nil)
	if f != nil {
		t.Fatalf("la tabla se borró: %s", f.Message)
	}
	if n != 0 {
		t.Errorf("el INSERT anterior al DROP corrió (%d filas): el lote se rechaza entero", n)
	}

	// TRUNCATE también, aunque SQLite no lo tenga: la comprobación es sobre el
	// texto y no depende del motor.
	res = q.Run(ctx, "r2", "TRUNCATE TABLE kn_bloq")
	if res.OK || res.Failure == nil || !strings.Contains(res.Failure.Message, "TRUNCATE") {
		t.Errorf("TRUNCATE no se bloqueó: %s", mensajeDe(res))
	}
	if res.Failure != nil && res.Failure.Kind != engine.FailurePermission {
		t.Errorf("el fallo tenía que ser de tipo permission y es %q", res.Failure.Kind)
	}

	// Un ALTER que tira algo es un DROP: la puerta del editor tiene que decir
	// lo mismo que la del changeset, que bloquea DropColumn.
	res = q.Run(ctx, "r2b", "ALTER TABLE kn_bloq DROP COLUMN id")
	if res.OK || res.Failure == nil || !strings.Contains(res.Failure.Message, "DROP") {
		t.Errorf("ALTER … DROP COLUMN no se bloqueó: %s", mensajeDe(res))
	}
	// Y un ALTER que no tira nada pasa.
	if res := q.Run(ctx, "r2c", "ALTER TABLE kn_bloq ADD COLUMN dropped_at text"); !res.OK {
		t.Errorf("un ALTER sin DROP se bloqueó por la palabra en un nombre: %s", mensajeDe(res))
	}

	// Sin la casilla, el mismo DROP corre: la protección es la casilla, no el
	// comando.
	abierta.conn.Safety.BlockDropTruncate = false
	if res := q.Run(ctx, "r3", "DROP TABLE kn_bloq"); !res.OK {
		t.Errorf("sin la casilla el DROP tenía que correr: %s", mensajeDe(res))
	}
}

// TestLaConfirmacionPorNombreValeFueraDeProduccion: «Tipear el nombre de la
// base para confirmar escrituras» se mostraba activa en toda conexión que no
// tuviera puesta «Escribir sin confirmar», y Go solo la exigía en producción.
// Ahora la regla es Connection.RequiresWriteConfirmation, en los tres caminos
// que escriben: Apply, DryRun e Import.
func TestLaConfirmacionPorNombreValeFueraDeProduccion(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE TABLE kn_conf (id integer PRIMARY KEY, n text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatal(err)
	}
	base := nombreDeLaBase(abierta)
	if base == "" {
		t.Fatal("la sesión no informó el nombre de la base")
	}

	// Staging, con la casilla en cero: el default del modelo, que documenta
	// «hay que tipear el nombre».
	abierta.conn.Environment = connection.Staging
	abierta.conn.Safety.AllowWriteWithoutConfirmation = false

	v, err := sesion.Changeset(ctx)
	if err == nil && !v.NeedsConfirmation {
		t.Error("el changeset no avisa que hace falta confirmar")
	}
	if err == nil && v.Production {
		t.Error("staging no es producción: la pantalla lo pintaría en rojo")
	}
	if err == nil && v.ConfirmWord != base {
		t.Errorf("ConfirmWord = %q, se esperaba %q", v.ConfirmWord, base)
	}

	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.AddColumn, Schema: "main", Table: "kn_conf", Source: "test",
		Column: &change.Column{Name: "c", DataType: "text", Nullable: true},
	}, ""); err != nil {
		t.Fatalf("Stage(): %v — preparar no es escribir y no pide la palabra", err)
	}

	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("Apply() sin palabra contra staging devolvió %v, se esperaba ErrNeedsConfirmation", err)
	}
	if _, err := sesion.Apply(ctx, ApplyOptions{}); strings.Contains(err.Error(), "producción") {
		t.Error("el mensaje dice «producción» y la conexión es de staging")
	}
	if _, err := sesion.DryRun(ctx, ""); !errors.Is(err, ErrNeedsConfirmation) {
		t.Errorf("DryRun() sin palabra devolvió %v", err)
	}

	imp := NewImports(NewQueries(sesion))
	target, err := imp.Target()
	if err != nil {
		t.Fatal(err)
	}
	if !target.NeedsConfirmation || target.Production || target.ConfirmWord != base {
		t.Errorf("ImportTarget = %+v: tenía que pedir la palabra sin ser producción", target)
	}
	plan := ImportPlan{
		RunID: "c1", Path: csvDePrueba(t, "id,n\n1,a\n"), Schema: "main", Table: "kn_conf",
		Options: csvimport.Options{HasHeader: true}, Mapping: []string{"id", "n"},
	}
	if res := imp.Run(ctx, plan); res.OK || res.Failure == nil ||
		!strings.Contains(res.Failure.Message, "hay que escribir") {
		t.Errorf("Import sin palabra: %+v", res)
	}
	if n := contar(t, abierta, "kn_conf"); n != 0 {
		t.Fatalf("la importación escribió %d filas sin la palabra", n)
	}

	// Con la palabra, los tres caminos escriben.
	plan.Confirm = base
	if res := imp.Run(ctx, plan); !res.OK {
		t.Errorf("Import con la palabra falló: %+v", res.Failure)
	}
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true, Confirm: base}); err != nil {
		t.Errorf("Apply() con la palabra falló: %v", err)
	}

	// Con la casilla puesta, staging no pide nada.
	abierta.conn.Safety.AllowWriteWithoutConfirmation = true
	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.AddColumn, Schema: "main", Table: "kn_conf", Source: "test",
		Column: &change.Column{Name: "d", DataType: "text", Nullable: true},
	}, ""); err != nil {
		t.Fatal(err)
	}
	if v, err := sesion.Changeset(ctx); err != nil || v.NeedsConfirmation {
		t.Errorf("con «Escribir sin confirmar» el changeset sigue pidiendo la palabra (%v)", err)
	}
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); err != nil {
		t.Errorf("Apply() con la casilla puesta pidió la palabra: %v", err)
	}

	// Y en producción la casilla se ignora: es la protección que no se apaga.
	abierta.conn.Environment = connection.Production
	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.AddColumn, Schema: "main", Table: "kn_conf", Source: "test",
		Column: &change.Column{Name: "e", DataType: "text", Nullable: true},
	}, ""); err != nil {
		t.Fatal(err)
	}
	v, err = sesion.Changeset(ctx)
	if err != nil || !v.NeedsConfirmation || !v.Production {
		t.Errorf("producción con la casilla puesta: NeedsConfirmation=%v Production=%v (%v)",
			v.NeedsConfirmation, v.Production, err)
	}
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); !errors.Is(err, ErrNeedsConfirmation) {
		t.Errorf("Apply() en producción con la casilla puesta devolvió %v", err)
	}
}
