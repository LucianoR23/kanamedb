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
	// lo mismo que la del changeset, que bloquea DropColumn. Con y sin la
	// palabra COLUMN —Postgres y MySQL aceptan el atajo— y con IF EXISTS.
	for _, sql := range []string{
		"ALTER TABLE kn_bloq DROP COLUMN id",
		"alter table kn_bloq drop id",
		"ALTER TABLE kn_bloq DROP IF EXISTS id",
		"-- drop\nALTER TABLE kn_bloq DROP CONSTRAINT x",
	} {
		res = q.Run(ctx, "r2b", sql)
		if res.OK || res.Failure == nil || !strings.Contains(res.Failure.Message, "DROP") {
			t.Errorf("%q no se bloqueó: %s", sql, mensajeDe(res))
		}
	}
	// Y un ALTER que no tira nada pasa: una columna con «drop» en el nombre, o
	// quitar una propiedad (DROP NOT NULL, DROP DEFAULT no borran nada).
	if res := q.Run(ctx, "r2c", "ALTER TABLE kn_bloq ADD COLUMN dropped_at text"); !res.OK {
		t.Errorf("un ALTER sin DROP se bloqueó por la palabra en un nombre: %s", mensajeDe(res))
	}
	if f := bloqueadaPorPolitica("ALTER TABLE t ALTER COLUMN c DROP NOT NULL", abierta.db.Dialect()); f != nil {
		t.Errorf("DROP NOT NULL no borra nada y se bloqueó: %s", f.Message)
	}
	if f := bloqueadaPorPolitica("ALTER TABLE t ALTER COLUMN c DROP DEFAULT", abierta.db.Dialect()); f != nil {
		t.Errorf("DROP DEFAULT no borra nada y se bloqueó: %s", f.Message)
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

// TestDosEscriturasALaVezNoSePisan: Apply, DryRun e importar se excluyen
// entre sí. Un doble clic o dos ventanas podían leer el mismo changeset dos
// veces y ejecutarlo dos veces (K-05). El test toma el candado como lo haría
// una escritura en curso y comprueba que las tres puertas lo respetan sin
// tocar la base; el reintento después de soltarlo pasa.
func TestDosEscriturasALaVezNoSePisan(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE TABLE kn_dos (id integer PRIMARY KEY, n text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.InsertRow, Schema: "main", Table: "kn_dos", Source: "test",
		Values: []change.Cell{{Column: "id", Value: ptr("1")}, {Column: "n", Value: ptr("a")}},
	}, ""); err != nil {
		t.Fatalf("Stage(): %v", err)
	}

	// Otra escritura tiene el candado.
	abierta.escritura.Lock()
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); !errors.Is(err, ErrBusy) {
		t.Errorf("Apply() con otra escritura en curso devolvió %v", err)
	}
	if _, err := sesion.DryRun(ctx, ""); !errors.Is(err, ErrBusy) {
		t.Errorf("DryRun() con otra escritura en curso devolvió %v", err)
	}
	imp := NewImports(NewQueries(sesion))
	res := imp.Run(ctx, ImportPlan{
		RunID: "d1", Path: csvDePrueba(t, "id,n\n2,b\n"), Schema: "main", Table: "kn_dos",
		Options: csvimport.Options{HasHeader: true}, Mapping: []string{"id", "n"},
	})
	if res.OK || res.Failure == nil || res.Failure.Kind != engine.FailureLock {
		t.Errorf("Import con otra escritura en curso: %+v", res)
	}
	if n := contar(t, abierta, "kn_dos"); n != 0 {
		t.Fatalf("algo escribió %d filas con el candado tomado", n)
	}
	if abierta.cambios.Summarize().Total != 1 {
		t.Error("el changeset se consumió sin aplicarse")
	}
	abierta.escritura.Unlock()

	// Suelto, todo pasa, y el candado queda suelto después.
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); err != nil {
		t.Fatalf("Apply() con el candado suelto: %v", err)
	}
	if res := imp.Run(ctx, ImportPlan{
		RunID: "d2", Path: csvDePrueba(t, "id,n\n2,b\n"), Schema: "main", Table: "kn_dos",
		Options: csvimport.Options{HasHeader: true}, Mapping: []string{"id", "n"},
	}); !res.OK {
		t.Fatalf("Import con el candado suelto: %+v", res.Failure)
	}
	if n := contar(t, abierta, "kn_dos"); n != 2 {
		t.Errorf("quedaron %d filas de 2", n)
	}
}

func ptr(s string) *string { return &s }

// TestSoloLecturaNoSeApagaDesdeElEditor: «Abrir en solo lectura» es un
// parámetro de sesión, y `SET default_transaction_read_only = off; DELETE …`
// en el editor lo revertía y borraba (K-06, comprobado contra Postgres: 2 → 0
// filas con OK=true). Lo que apaga el modo se rechaza antes de correr nada.
func TestSoloLecturaNoSeApagaDesdeElEditor(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_ro", true)

			// Reconectar en solo lectura, por el mismo camino que el gestor.
			guardada, err := sesion.store.Get(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			guardada.Safety.ReadOnly = true
			if err := sesion.store.Update(guardada); err != nil {
				t.Fatal(err)
			}
			if res := sesion.Connect(ctx, c.ID); !res.OK {
				t.Fatalf("no reconectó en solo lectura: %+v", res.Failure)
			}
			abierta, _ := sesion.abierta()
			q := NewQueries(sesion)

			apagan := map[string][]string{
				"postgres": {
					"SET default_transaction_read_only = off",
					"set session default_transaction_read_only to off",
					"SET SESSION CHARACTERISTICS AS TRANSACTION READ WRITE",
					// Un comentario adelante con la palabra adentro, y un salto de
					// línea en el medio: los dos pasaban (review del 2026-09-12).
					"-- settings\nSET default_transaction_read_only = off",
					"/* set */ SET default_transaction_read_only = off",
					"SET SESSION CHARACTERISTICS AS TRANSACTION\nREAD WRITE",
					"RESET default_transaction_read_only",
					"RESET ALL",
				},
				"mysql":   {"SET SESSION TRANSACTION READ WRITE", "SET @@session.transaction_read_only = 0", "SET transaction_read_only = OFF"},
				"mariadb": {"SET SESSION TRANSACTION READ WRITE", "SET @@session.tx_read_only = 0"},
				"sqlite":  {"PRAGMA query_only = 0", "pragma query_only=off"},
			}
			for _, sql := range apagan[caso.nombre] {
				res := q.Run(ctx, "ro1", sql+";\nDELETE FROM "+califica(c, esq, tabla)+";")
				if res.OK || res.Failure == nil || res.Failure.Kind != engine.FailurePermission {
					t.Errorf("%q se aceptó en una conexión de solo lectura: %s", sql, mensajeDe(res))
				}
			}
			n, f := abierta.db.Count(ctx, esq, tabla, nil)
			if f != nil {
				t.Fatal(f.Message)
			}
			if n != 2 {
				t.Fatalf("se borraron filas en una conexión de solo lectura: quedan %d de 2", n)
			}
			// Un SET inocente sigue pasando: search_path, nombres, zona horaria.
			inocente := map[string]string{
				"postgres": "SET search_path TO public", "mysql": "SET SESSION sql_mode = 'ANSI_QUOTES'",
				"mariadb": "SET SESSION sql_mode = 'ANSI_QUOTES'", "sqlite": "PRAGMA case_sensitive_like = 1",
			}
			if res := q.Run(ctx, "ro2", inocente[caso.nombre]); !res.OK {
				t.Errorf("un SET que no toca el modo se rechazó: %s", mensajeDe(res))
			}
		})
	}
}

// TestApplyExigeLaHuellaDeLaVistaPrevia: lo que se aplica es lo que se vio.
// Apply vuelve a escribir la SQL, y nada ataba esa SQL a la de la vista previa
// (K-08). Ahora ChangesetView lleva una huella y Apply la exige: sin huella no
// aplica (salvo «Aplicar sin abrir la vista previa»), y con una huella vieja
// —entró otro cambio, o la tabla cambió por fuera— tampoco.
func TestApplyExigeLaHuellaDeLaVistaPrevia(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE TABLE kn_huella (id integer PRIMARY KEY, n text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatal(err)
	}
	stage := func(col string) {
		t.Helper()
		if _, err := sesion.Stage(ctx, change.Change{
			Type: change.AddColumn, Schema: "main", Table: "kn_huella", Source: "test",
			Column: &change.Column{Name: col, DataType: "text", Nullable: true},
		}, ""); err != nil {
			t.Fatal(err)
		}
	}
	columnas := func() int {
		t.Helper()
		d, err := sesion.TableDetail(ctx, "main", "kn_huella")
		if err != nil {
			t.Fatal(err)
		}
		return len(d.Columns)
	}

	// El helper marca «Aplicar sin abrir la vista previa» para los tests que
	// no son sobre esto; acá se prueba el default.
	abierta.conn.Safety.AllowApplyWithoutPreview = false

	stage("a")
	vista, err := sesion.Changeset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(vista.Fingerprint) != 64 {
		t.Fatalf("la vista previa no trae huella: %q", vista.Fingerprint)
	}

	// Sin huella, con la conexión exigiendo vista previa (el default): no.
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); !errors.Is(err, ErrStalePreview) {
		t.Errorf("Apply() sin huella devolvió %v", err)
	}
	// Con una huella que no es la de lo que se va a correr: tampoco.
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true, Fingerprint: "0000"}); !errors.Is(err, ErrStalePreview) {
		t.Errorf("Apply() con huella ajena devolvió %v", err)
	}
	// Entró otro cambio después de la vista previa: la huella vieja no sirve.
	stage("b")
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true, Fingerprint: vista.Fingerprint}); !errors.Is(err, ErrStalePreview) {
		t.Errorf("Apply() con la huella de antes del segundo cambio devolvió %v", err)
	}
	if n := columnas(); n != 2 {
		t.Fatalf("algo se aplicó sin huella válida: %d columnas", n)
	}
	// Con la huella de lo que se muestra AHORA, aplica.
	vista, err = sesion.Changeset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true, Fingerprint: vista.Fingerprint})
	if err != nil || !res.OK {
		t.Fatalf("Apply() con la huella actual falló: %v %+v", err, res.Failure)
	}
	if n := columnas(); n != 4 {
		t.Fatalf("quedaron %d columnas de 4", n)
	}

	// La tabla cambió por fuera: en SQLite la reconstrucción se escribe contra
	// el catálogo de ahora, así que la SQL ya no es la que se mostró.
	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.SetNotNull, Schema: "main", Table: "kn_huella", Source: "test",
		Column: &change.Column{Name: "n", DataType: "text"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	vista, err = sesion.Changeset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `ALTER TABLE kn_huella ADD COLUMN externa text`); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true, Fingerprint: vista.Fingerprint}); !errors.Is(err, ErrStalePreview) {
		t.Errorf("la tabla cambió por fuera y Apply() devolvió %v", err)
	}
	vista, err = sesion.Changeset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true, Fingerprint: vista.Fingerprint}); err != nil || !res.OK {
		t.Fatalf("Apply() con la huella nueva falló: %v %+v", err, res.Failure)
	}
	if n := columnas(); n != 5 {
		t.Errorf("quedaron %d columnas de 5: la reconstrucción perdió la que se agregó por fuera", n)
	}

	// Con «Aplicar sin abrir la vista previa», la huella es opcional, pero si
	// viene tiene que coincidir igual.
	abierta.conn.Safety.AllowApplyWithoutPreview = true
	stage("d")
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true, Fingerprint: "0000"}); !errors.Is(err, ErrStalePreview) {
		t.Errorf("con la casilla puesta, una huella que no coincide se aceptó: %v", err)
	}
	if _, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); err != nil {
		t.Errorf("con la casilla puesta, Apply() sin huella falló: %v", err)
	}
}
