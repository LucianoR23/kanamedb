package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
)

// sesionConEsquema deja una sesión conectada y un esquema vacío para el test.
func sesionConEsquema(t *testing.T) (*Session, string) {
	t.Helper()
	sesion, _, id := sesionDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta() error: %v", err)
	}
	esq := "kn_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	for _, sql := range []string{
		"DROP SCHEMA IF EXISTS " + esq + " CASCADE",
		"CREATE SCHEMA " + esq,
	} {
		if err := abierta.db.Exec(context.Background(), sql); err != nil {
			t.Fatalf("preparar %q: %v", sql, err)
		}
	}
	t.Cleanup(func() {
		_ = abierta.db.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	})
	return sesion, esq
}

func crearTabla(esq, tabla string, cols ...change.Column) change.Change {
	return change.Change{
		Type: change.CreateTable, Schema: esq, Table: tabla,
		Source: "test", Columns: cols, Names: []string{cols[0].Name},
	}
}

func columna(nombre, tipo string, nullable bool) change.Column {
	return change.Column{Name: nombre, DataType: tipo, Nullable: nullable}
}

func TestApplyDejaElEsquemaComoSePidioYVaciaElChangeset(t *testing.T) {
	sesion, esq := sesionConEsquema(t)
	ctx := context.Background()

	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "clientes", columna("id", "bigint", false)), ""); err != nil {
		t.Fatalf("Stage() falló: %v", err)
	}
	if _, err := sesion.Stage(context.Background(), change.Change{
		Type: change.AddColumn, Schema: esq, Table: "clientes", Source: "test",
		Column: &change.Column{Name: "email", DataType: "text", Nullable: true},
	}, ""); err != nil {
		t.Fatalf("Stage() falló: %v", err)
	}

	// La sentencia viaja ya escrita: la interfaz nunca arma SQL.
	vista, err := sesion.Changeset(context.Background())
	if err != nil {
		t.Fatalf("Changeset() falló: %v", err)
	}
	if len(vista.Changes) != 2 || vista.Summary.Included != 2 {
		t.Fatalf("changeset = %+v", vista.Summary)
	}
	if !strings.Contains(vista.Changes[0].Statement.SQL, "CREATE TABLE") {
		t.Errorf("la primera sentencia = %q", vista.Changes[0].Statement.SQL)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil {
		t.Fatalf("Apply() falló: %v", err)
	}
	if !res.OK || len(res.Results) != 2 {
		t.Fatalf("Apply() = %+v", res)
	}

	d, err := sesion.TableDetail(ctx, esq, "clientes")
	if err != nil {
		t.Fatalf("la tabla no quedó creada: %v", err)
	}
	if len(d.Columns) != 2 {
		t.Errorf("la tabla quedó con %d columnas: %+v", len(d.Columns), d.Columns)
	}

	// Lo aplicado deja de estar pendiente.
	if v, _ := sesion.Changeset(context.Background()); v.Summary.Total != 0 {
		t.Errorf("el changeset quedó con %d cambios después de aplicar", v.Summary.Total)
	}
}

// Con transacción única, una sentencia que falla tiene que dejar la base COMO
// ESTABA. No alcanza con que Apply devuelva error: hay que mirar la base.
func TestApplyEnUnaTransaccionRevierteTodoSiAlgoFalla(t *testing.T) {
	sesion, esq := sesionConEsquema(t)
	ctx := context.Background()

	// Primera: válida. Segunda: una columna sobre una tabla que no existe.
	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "buena", columna("id", "bigint", false)), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(context.Background(), change.Change{
		Type: change.AddColumn, Schema: esq, Table: "no_existe", Source: "test",
		Column: &change.Column{Name: "c", DataType: "text", Nullable: true},
	}, ""); err != nil {
		t.Fatal(err)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil {
		t.Fatalf("Apply() devolvió error de programa: %v", err)
	}
	if res.OK {
		t.Fatal("Apply() dijo que salió bien con una sentencia inválida")
	}
	if !res.RolledBack {
		t.Error("RolledBack = false: con transacción única tiene que revertirse todo")
	}
	// NINGUNA queda aplicada, ni siquiera la primera, que corrió sin error:
	// Applied dice si la sentencia quedó EN LA BASE, no si el motor la aceptó.
	// La diferencia importa porque `olvidarAplicados` saca del changeset lo que
	// figure aplicado, y sacar una edición que se revirtió la pierde para
	// siempre.
	if len(res.Results) != 2 || res.Results[0].Applied || res.Results[1].Applied {
		t.Errorf("resultados = %+v", res.Results)
	}
	if res.Failure == nil {
		t.Error("Apply() no clasificó el fallo")
	}

	// Lo que importa: la tabla de la PRIMERA sentencia no puede existir.
	if _, err := sesion.TableDetail(ctx, esq, "buena"); err == nil {
		t.Error("la tabla de la primera sentencia quedó creada: no se revirtió nada")
	}

	// Y el changeset se conserva ENTERO: no se aplicó nada, así que no hay nada
	// que sacar. Es lo contrario del caso sin transacción.
	if v, _ := sesion.Changeset(context.Background()); v.Summary.Total != 2 {
		t.Errorf("el changeset quedó con %d cambios después de fallar", v.Summary.Total)
	}
}

// El fallo se explica en el vocabulario de la sentencia, no en el de la
// conexión.
//
// Este test existe por un bug concreto: Apply reconstruía el error con
// errors.New(texto) antes de clasificarlo, lo que tiraba el *pgconn.PgError y
// con él el SQLSTATE. El clasificador de conexiones se quedaba sin nada que
// mirar y contestaba «el servidor rechazó la conexión con usuario@host/base»
// para cualquier fallo, incluida una columna con nulos.
func TestApplyExplicaElFalloDeLaSentenciaYNoElDeLaConexion(t *testing.T) {
	sesion, esq := sesionConEsquema(t)
	ctx := context.Background()

	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"CREATE TABLE " + esq + ".con_nulos (id int PRIMARY KEY, apodo text)",
		"INSERT INTO " + esq + ".con_nulos VALUES (1, 'ana'), (2, NULL)",
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatalf("preparar %q: %v", sql, err)
		}
	}

	if _, err := sesion.Stage(context.Background(), change.Change{
		Type: change.SetNotNull, Schema: esq, Table: "con_nulos", Source: "test",
		Column: &change.Column{Name: "apodo"},
	}, ""); err != nil {
		t.Fatal(err)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil {
		t.Fatalf("Apply() error de programa: %v", err)
	}
	if res.OK {
		t.Fatal("Apply() dijo que salió bien sobre una columna con nulos")
	}
	if res.Failure == nil {
		t.Fatal("Apply() no clasificó el fallo")
	}
	if res.Failure.SQLState != "23502" {
		t.Errorf("SQLState = %q, se esperaba 23502", res.Failure.SQLState)
	}
	for _, quiere := range []string{"apodo", "NULL"} {
		if !strings.Contains(res.Failure.Message, quiere) {
			t.Errorf("el mensaje no dice %q: %s", quiere, res.Failure.Message)
		}
	}
	if strings.Contains(res.Failure.Message, "conexión") {
		t.Errorf("el mensaje habla de la conexión: %s", res.Failure.Message)
	}
	if res.Results[0].SQLState != "23502" {
		t.Errorf("el resultado de la sentencia perdió el SQLSTATE: %+v", res.Results[0])
	}
}

// Sin transacción, lo anterior queda aplicado. Es la diferencia que el
// interruptor de la pantalla promete, y tiene que ser cierta.
func TestApplySinTransaccionDejaLoAnteriorAplicado(t *testing.T) {
	sesion, esq := sesionConEsquema(t)
	ctx := context.Background()

	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "buena", columna("id", "bigint", false)), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(context.Background(), change.Change{
		Type: change.AddColumn, Schema: esq, Table: "no_existe", Source: "test",
		Column: &change.Column{Name: "c", DataType: "text", Nullable: true},
	}, ""); err != nil {
		t.Fatal(err)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: false})
	if err != nil {
		t.Fatalf("Apply() error de programa: %v", err)
	}
	if res.OK || res.RolledBack {
		t.Errorf("Apply() = OK %v, RolledBack %v", res.OK, res.RolledBack)
	}
	if _, err := sesion.TableDetail(ctx, esq, "buena"); err != nil {
		t.Errorf("sin transacción, la primera sentencia tenía que quedar aplicada: %v", err)
	}

	// Y lo que quedó aplicado tiene que SALIR de la lista de pendientes.
	//
	// No es cosmético: si se queda, el próximo apply la vuelve a correr, y un
	// CREATE TABLE que ya corrió hace fallar el conjunto entero por algo que ya
	// estaba hecho. Antes se vaciaba el changeset solo cuando el apply completo
	// salía bien, así que este caso —el único donde la lista y la base pueden
	// discrepar— era justo el que quedaba mal.
	v, err := sesion.Changeset(context.Background())
	if err != nil {
		t.Fatalf("Changeset() error: %v", err)
	}
	if v.Summary.Total != 1 {
		t.Fatalf("quedaron %d cambios pendientes, se esperaba 1 (el que falló)", v.Summary.Total)
	}
	if v.Changes[0].Change.Table != "no_existe" {
		t.Errorf("quedó pendiente %q y tenía que quedar el que falló", v.Changes[0].Change.Table)
	}
}

// Ninguna sentencia se ejecuta si alguna del conjunto no se puede escribir.
// Aplicar la mitad porque la otra mitad no compila es la peor combinación.
func TestApplyNoEjecutaNadaSiUnCambioNoSePuedeEscribir(t *testing.T) {
	sesion, esq := sesionConEsquema(t)

	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "buena", columna("id", "bigint", false)), ""); err != nil {
		t.Fatal(err)
	}
	// Se mete un cambio inválido POR DEBAJO de Stage, que lo habría rechazado.
	// Es la única forma de probar la red que hay en Apply.
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	roto := change.Change{
		Type: change.AddColumn, Schema: esq, Table: "buena", Source: "test",
		Column: &change.Column{Name: "c", DataType: "text; DROP TABLE x", Nullable: true},
	}
	if _, err := abierta.cambios.Add(roto); err != nil {
		t.Fatalf("la fixture no pudo agregar el cambio: %v", err)
	}

	if _, err := sesion.Apply(context.Background(), ApplyOptions{SingleTransaction: true}); err == nil {
		t.Fatal("Apply() aceptó un changeset con un cambio que no se puede escribir")
	}
	if _, err := sesion.TableDetail(context.Background(), esq, "buena"); err == nil {
		t.Error("se ejecutó la sentencia válida igual: tenía que no correr nada")
	}
}

// La confirmación de producción se verifica del lado de Go. Una comprobación
// que vive solo en la interfaz no es una protección, es un cartel.
func TestApplyContraProduccionExigeElNombreEscrito(t *testing.T) {
	sesion, esq := sesionConEsquema(t)

	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	sesion.mu.Lock()
	sesion.current.conn.Environment = connection.Production
	sesion.mu.Unlock()

	// Lo que hay que escribir es el nombre de LA BASE. El de la conexión lo
	// eligió quien la configuró y puede llamarse igual en dos máquinas.
	base := abierta.server.CurrentDB
	if base == "" {
		t.Fatal("la sesión de prueba no informó el nombre de la base")
	}

	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "t", columna("id", "bigint", false)), ""); err != nil {
		t.Fatal(err)
	}

	v, err := sesion.Changeset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !v.NeedsConfirmation {
		t.Error("el changeset no avisa que hace falta confirmar")
	}
	if v.ConfirmWord != base {
		t.Errorf("ConfirmWord = %q, se esperaba el nombre de la base %q", v.ConfirmWord, base)
	}
	// El nombre de la conexión NO sirve: es la confusión que este campo evita.
	if v.ConfirmWord == sesion.Current().Name && base != sesion.Current().Name {
		t.Error("ConfirmWord quedó apuntando al nombre de la conexión")
	}

	for _, malo := range []string{"", "cualquier cosa", base + "x", strings.ToUpper(base) + "x"} {
		_, err := sesion.Apply(context.Background(), ApplyOptions{
			SingleTransaction: true, Confirm: malo,
		})
		if !errors.Is(err, ErrNeedsConfirmation) {
			t.Errorf("Apply(Confirm=%q) error = %v, se esperaba ErrNeedsConfirmation", malo, err)
		}
	}
	if _, err := sesion.TableDetail(context.Background(), esq, "t"); err == nil {
		t.Fatal("se aplicó sin confirmar")
	}

	// Con el nombre exacto, sí.
	if _, err := sesion.Apply(context.Background(), ApplyOptions{
		SingleTransaction: true, Confirm: base,
	}); err != nil {
		t.Fatalf("Apply() con el nombre correcto falló: %v", err)
	}
	if _, err := sesion.TableDetail(context.Background(), esq, "t"); err != nil {
		t.Errorf("no se aplicó con la confirmación correcta: %v", err)
	}
}

func TestApplyEnUnaConexionDeSoloLecturaNoEjecutaNada(t *testing.T) {
	sesion, esq := sesionConEsquema(t)

	sesion.mu.Lock()
	sesion.current.conn.Safety.ReadOnly = true
	sesion.mu.Unlock()

	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "t", columna("id", "bigint", false)), ""); err != nil {
		t.Fatal(err)
	}
	_, err := sesion.Apply(context.Background(), ApplyOptions{SingleTransaction: true})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Apply() error = %v, se esperaba ErrReadOnly", err)
	}
	if _, err := sesion.TableDetail(context.Background(), esq, "t"); err == nil {
		t.Error("se aplicó igual en una conexión de solo lectura")
	}
}

// El orden de ejecución no es el de edición. Se editan al revés a propósito: si
// se ejecutaran en ese orden, la clave foránea fallaría porque su tabla todavía
// no existe.
func TestApplyEjecutaEnOrdenDeDependencias(t *testing.T) {
	sesion, esq := sesionConEsquema(t)
	ctx := context.Background()

	fk := change.Change{
		Type: change.AddForeignKey, Schema: esq, Table: "pedidos", Source: "test",
		Name: "pedidos_cliente_fk", Names: []string{"cliente_id"},
		RefTable: "clientes", RefNames: []string{"id"}, OnDelete: "cascade",
	}
	col := change.Change{
		Type: change.AddColumn, Schema: esq, Table: "pedidos", Source: "test",
		Column: &change.Column{Name: "cliente_id", DataType: "bigint", Nullable: true},
	}
	for _, c := range []change.Change{
		fk,
		col,
		crearTabla(esq, "clientes", columna("id", "bigint", false)),
		crearTabla(esq, "pedidos", columna("id", "bigint", false)),
	} {
		if _, err := sesion.Stage(context.Background(), c, ""); err != nil {
			t.Fatalf("Stage(%s) falló: %v", c.Type, err)
		}
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil {
		t.Fatalf("Apply() falló: %v", err)
	}
	if !res.OK {
		t.Fatalf("Apply() falló en el orden de edición: %+v", res.Results)
	}

	d, err := sesion.TableDetail(ctx, esq, "pedidos")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ForeignKeys) != 1 || d.ForeignKeys[0].RefTable != "clientes" {
		t.Errorf("claves foráneas = %+v", d.ForeignKeys)
	}
}

func TestApplySinCambiosNoHaceNada(t *testing.T) {
	sesion, _ := sesionConEsquema(t)
	if _, err := sesion.Apply(context.Background(), ApplyOptions{}); err == nil {
		t.Error("Apply() sin cambios no devolvió error")
	}
}

func TestStageRechazaLoQueNoSePuedeEscribir(t *testing.T) {
	sesion, esq := sesionConEsquema(t)

	_, err := sesion.Stage(context.Background(), change.Change{
		Type: change.AddColumn, Schema: esq, Table: "t", Source: "test",
		Column: &change.Column{Name: "c", DataType: "text; DROP TABLE x", Nullable: true},
	}, "")
	if err == nil {
		t.Fatal("Stage() aceptó un cambio que no se puede escribir")
	}
	if v, _ := sesion.Changeset(context.Background()); v.Summary.Total != 0 {
		t.Error("el cambio inválido quedó en el changeset")
	}
}

// Un aviso que no aparece no protege. Se comprueba que los tres se emitan.
func TestElChangesetAvisaDeLoQueVaACostar(t *testing.T) {
	sesion, esq := sesionConEsquema(t)

	// Una tabla real, para poder pedir cosas caras sobre ella.
	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "t",
		columna("id", "bigint", false), columna("v", "text", true)), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Apply(context.Background(), ApplyOptions{SingleTransaction: true}); err != nil {
		t.Fatal(err)
	}

	// Destructiva y con bloqueo total.
	if _, err := sesion.Stage(context.Background(), change.Change{
		Type: change.DropColumn, Schema: esq, Table: "t", Source: "test",
		Column: &change.Column{Name: "v"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	v, err := sesion.Changeset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	junto := strings.Join(v.Warnings, " | ")
	if !strings.Contains(junto, "bloquean") {
		t.Errorf("falta el aviso de bloqueo: %q", junto)
	}
	if !strings.Contains(junto, "irreversible") {
		t.Errorf("falta el aviso de pérdida de datos: %q", junto)
	}
	if v.Summary.Destructive != 1 {
		t.Errorf("Destructive = %d", v.Summary.Destructive)
	}
}

// El guion es lo que se copia y se guarda: termina en un ticket o en un
// repositorio, donde nadie va a tener la pantalla al lado. Tiene que llevar
// consigo el orden, la transacción y qué cuesta cada sentencia.
func TestElGuionSeExplicaSolo(t *testing.T) {
	sesion, esq := sesionConEsquema(t)

	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "t",
		columna("id", "bigint", false), columna("v", "text", true)), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Apply(context.Background(), ApplyOptions{SingleTransaction: true}); err != nil {
		t.Fatal(err)
	}

	// Una destructiva y una que lee la tabla entera.
	if _, err := sesion.Stage(context.Background(), change.Change{
		Type: change.DropColumn, Schema: esq, Table: "t", Source: "test",
		Column: &change.Column{Name: "v"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(context.Background(), change.Change{
		Type: change.AddCheck, Schema: esq, Table: "t", Source: "test",
		Name: "t_id_positivo", Expression: "id > 0",
	}, ""); err != nil {
		t.Fatal(err)
	}

	v, err := sesion.Changeset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	g := v.Script

	for _, debe := range []string{
		"BEGIN;", "COMMIT;", // la transacción se ve en el guion
		"-- 1 ", "-- 2 ", // numerado
		"DESTRUCTIVA",         // qué cuesta
		"lee la tabla entera", // y qué va a tardar
		"DROP COLUMN", "ADD CONSTRAINT",
	} {
		if !strings.Contains(g, debe) {
			t.Errorf("el guion no contiene %q:\n%s", debe, g)
		}
	}

	// El orden del guion es el de EJECUCIÓN: el check se agrega antes de que la
	// columna desaparezca, aunque se editó después.
	if strings.Index(g, "ADD CONSTRAINT") > strings.Index(g, "DROP COLUMN") {
		t.Errorf("el guion no está en orden de ejecución:\n%s", g)
	}
}

// Contra producción, un cambio destructivo no entra al changeset sin confirmar.
//
// Se pide al PREPARAR y no solo al aplicar porque son dos preguntas distintas:
// «¿de verdad querés borrar esta columna?» se contesta mirando la columna, y
// «¿de verdad querés correr estas ocho sentencias?» mirando la lista.
func TestStageDestructivoContraProduccionExigeConfirmacion(t *testing.T) {
	sesion, esq := sesionConEsquema(t)

	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	base := abierta.server.CurrentDB

	// Una tabla real sobre la que pedir el borrado.
	if _, err := sesion.Stage(context.Background(), crearTabla(esq, "t", columna("id", "bigint", false)), ""); err != nil {
		t.Fatal(err)
	}

	sesion.mu.Lock()
	sesion.current.conn.Environment = connection.Production
	sesion.mu.Unlock()

	destructivo := change.Change{
		Type: change.DropTable, Schema: esq, Table: "t", Source: "test",
	}
	if _, err := sesion.Stage(context.Background(), destructivo, ""); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("Stage() sin confirmar: err = %v, se esperaba ErrNeedsConfirmation", err)
	}
	if _, err := sesion.Stage(context.Background(), destructivo, "otra cosa"); !errors.Is(err, ErrNeedsConfirmation) {
		t.Error("Stage() aceptó una confirmación equivocada")
	}
	if _, err := sesion.Stage(context.Background(), destructivo, base); err != nil {
		t.Fatalf("Stage() con la confirmación correcta falló: %v", err)
	}

	// Y uno NO destructivo no molesta con la pregunta: preguntar por todo enseña
	// a decir que sí sin leer.
	inocente := change.Change{
		Type: change.AddColumn, Schema: esq, Table: "t", Source: "test",
		Column: &change.Column{Name: "c", DataType: "text", Nullable: true},
	}
	if _, err := sesion.Stage(context.Background(), inocente, ""); err != nil {
		t.Errorf("Stage() de un cambio inocuo pidió confirmación: %v", err)
	}
}
