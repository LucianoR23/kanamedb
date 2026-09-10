package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
)

// motoresDeEnsayo son los cuatro, con lo que el ensayo de S15 puede prometer en
// cada uno. Es la misma lista que usa el test de tramos, y por el mismo motivo:
// el límite no es «Postgres sí y los otros no», es el DDL transaccional.
var motoresDeEnsayo = []struct {
	nombre string
	uri    string
	// puede es si el motor soporta ensayar, que es tener DDL transaccional.
	puede bool
}{
	{"postgres", "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable", true},
	{"mysql", "mysql://kaname:kaname@127.0.0.1:53306/kaname_test", false},
	{"mariadb", "mariadb://kaname:kaname@127.0.0.1:53307/kaname_test", false},
	{"sqlite", "", true},
}

// TestElEnsayoCorreTodoYNoDejaNada.
//
// Es la promesa entera del botón, y las dos mitades importan igual: si no
// corriera todo no comprobaría nada, y si dejara algo sería un apply con otro
// nombre.
//
// Contra MySQL y MariaDB no existe, y eso también se comprueba acá. Ahí «correr
// todo y revertir» no revierte nada —cada DDL se confirma solo— así que un
// botón de ensayo aplicaría de verdad. Que se niegue no es una limitación
// escondida: es la única respuesta honesta, y tiene que ser imposible que se
// active por descuido.
func TestElEnsayoCorreTodoYNoDejaNada(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)
			tipo := tipoEnteroDe(c.Engine)

			limpiar := func() {
				abierta, err := sesion.abierta()
				if err != nil {
					return
				}
				_ = abierta.db.Exec(context.Background(),
					"DROP TABLE IF EXISTS "+califica(c, esq, "kn_dry_una"))
			}
			limpiar()
			t.Cleanup(limpiar)

			if _, err := sesion.Stage(ctx, change.Change{
				Type: change.CreateTable, Schema: esq, Table: "kn_dry_una", Source: "test",
				Columns: []change.Column{{Name: "id", DataType: tipo}}, Names: []string{"id"},
			}, ""); err != nil {
				t.Fatalf("Stage(): %v", err)
			}

			res, err := sesion.DryRun(ctx, "")

			if !caso.puede {
				if err == nil {
					t.Fatalf("%s ensayó un cambio de esquema, y no puede: cada DDL se "+
						"confirma solo, así que eso fue un apply", caso.nombre)
				}
				if !strings.Contains(err.Error(), c.Engine.Label()) {
					t.Errorf("el error no nombra al motor, así que la pantalla no puede "+
						"explicar por qué: %v", err)
				}
				// Y no corrió nada: negarse tiene que ser antes de tocar la base.
				if _, err := sesion.TableDetail(ctx, esq, "kn_dry_una"); err == nil {
					t.Error("la tabla existe: el ensayo que se negó igual ejecutó la sentencia")
				}
				return
			}

			if err != nil {
				t.Fatalf("DryRun() devolvió error de programa: %v", err)
			}
			if !res.OK {
				t.Fatalf("el ensayo falló con un CREATE TABLE válido: %+v", res.Failure)
			}

			// Corrió: hay un resultado por sentencia.
			if len(res.Results) != 1 {
				t.Fatalf("el ensayo devolvió %d resultados y tendría que haber corrido 1 "+
					"sentencia", len(res.Results))
			}
			// Y no dejó nada.
			if !res.RolledBack {
				t.Error("RolledBack = false: el ensayo dice que algo quedó")
			}
			for _, r := range res.Results {
				if r.Applied {
					t.Errorf("la sentencia %q quedó marcada como aplicada; "+
						"olvidarAplicados la sacaría del changeset y el usuario perdería "+
						"la edición sin que el cambio exista", r.SQL)
				}
			}
			if _, err := sesion.TableDetail(ctx, esq, "kn_dry_una"); err == nil {
				t.Error("la tabla existe después del ensayo: eso no fue un ensayo")
			}

			// El changeset sigue entero: ensayar no es aplicar, así que no se
			// consume nada. Sin esto el botón sería de un solo uso.
			vista, err := sesion.Changeset(ctx)
			if err != nil {
				t.Fatalf("Changeset(): %v", err)
			}
			if len(vista.Changes) != 1 {
				t.Errorf("el changeset quedó con %d cambios y tenía 1: el ensayo se "+
					"comió la edición", len(vista.Changes))
			}
			if !vista.CanDryRun {
				t.Error("CanDryRun = false en un motor que acaba de ensayar")
			}

			// Y después del ensayo el apply de verdad anda: la transacción
			// revertida no dejó la conexión envenenada.
			ap, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
			if err != nil {
				t.Fatalf("Apply() después del ensayo: %v", err)
			}
			if !ap.OK {
				t.Fatalf("el apply falló después de un ensayo que salió bien: %+v", ap.Failure)
			}
			if _, err := sesion.TableDetail(ctx, esq, "kn_dry_una"); err != nil {
				t.Errorf("la tabla no quedó creada después del apply: %v", err)
			}
		})
	}
}

// TestElEnsayoEncuentraLoQueLaVistaPreviaNoPuede.
//
// Es el motivo por el que el botón existe. La vista previa dice qué SQL se va a
// mandar, y esa SQL puede ser impecable y fallar igual: `SET NOT NULL` sobre
// una columna que tiene nulos es correcta como texto y la rechaza el motor,
// porque el que decide no es la sintaxis sino los DATOS que hay adentro.
//
// Leyendo la vista previa eso no se ve. Ensayando, sí.
func TestElEnsayoEncuentraLoQueLaVistaPreviaNoPuede(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		if !caso.puede {
			continue
		}
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)
			tipo := tipoEnteroDe(c.Engine)
			abierta, err := sesion.abierta()
			if err != nil {
				t.Fatalf("abierta(): %v", err)
			}
			tabla := califica(c, esq, "kn_dry_nulos")

			limpiar := func() {
				_ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+tabla)
			}
			limpiar()
			t.Cleanup(limpiar)

			// La tabla se crea FUERA del changeset y con una fila en null: es el
			// estado que ya está en la base cuando alguien decide exigir que la
			// columna no admita nulos.
			if err := abierta.db.Exec(ctx,
				"CREATE TABLE "+tabla+" (id "+tipo+", nota "+tipo+")"); err != nil {
				t.Fatalf("crear la tabla: %v", err)
			}
			if err := abierta.db.Exec(ctx,
				"INSERT INTO "+tabla+" (id, nota) VALUES (1, NULL)"); err != nil {
				t.Fatalf("insertar la fila con null: %v", err)
			}

			cambio := change.Change{
				Type: change.SetNotNull, Schema: esq, Table: "kn_dry_nulos", Source: "test",
				Column: &change.Column{Name: "nota", DataType: tipo},
			}
			vista, err := sesion.Stage(ctx, cambio, "")
			if err != nil {
				t.Fatalf("Stage(): %v", err)
			}
			// La SQL se escribió sin problemas: la vista previa no tiene nada
			// que objetar, que es exactamente el punto.
			if vista.Statement.SQL == "" {
				t.Fatal("la vista previa quedó vacía")
			}

			res, err := sesion.DryRun(ctx, "")
			if err != nil {
				t.Fatalf("DryRun() devolvió error de programa: %v", err)
			}
			if res.OK {
				t.Fatal("el ensayo dijo que iba a andar, y la columna tiene un null: " +
					"el apply va a fallar y el ensayo no sirvió para nada")
			}
			if res.Failure == nil {
				t.Error("sin Failure: la pantalla no tiene qué mostrar")
			}
			if !res.RolledBack {
				t.Error("RolledBack = false: un ensayo que falla tampoco deja nada")
			}

			// Y la fila sigue ahí. Un ensayo que arregla los datos para poder
			// pasar sería mucho peor que uno que falla.
			n, fail := abierta.db.Count(ctx, esq, "kn_dry_nulos")
			if fail != nil {
				t.Fatalf("Count(): %s", fail.Message)
			}
			if n != 1 {
				t.Errorf("la tabla quedó con %d filas y tenía 1", n)
			}
		})
	}
}

// TestNoSeEnsayaEnSoloLectura.
//
// Una conexión abierta en solo lectura rechaza toda escritura del lado del
// SERVIDOR, así que el ensayo fallaría en la primera sentencia con un error de
// permisos que no tiene nada que ver con el changeset. Peor: la pantalla lo
// mostraría como «el cambio no va a andar», que es una respuesta falsa a la
// pregunta que se hizo.
func TestNoSeEnsayaEnSoloLectura(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq := esquemaDeApply(t, sesion, c)

	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.CreateTable, Schema: esq, Table: "kn_dry_ro", Source: "test",
		Columns: []change.Column{{Name: "id", DataType: "integer"}}, Names: []string{"id"},
	}, ""); err != nil {
		t.Fatalf("Stage(): %v", err)
	}

	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta(): %v", err)
	}
	abierta.conn.Safety.ReadOnly = true

	if _, err := sesion.DryRun(ctx, ""); err == nil {
		t.Fatal("ensayó en una conexión de solo lectura")
	} else if !strings.Contains(err.Error(), ErrReadOnly.Error()) {
		t.Errorf("el error no es el de solo lectura: %v", err)
	}

	vista, err := sesion.Changeset(ctx)
	if err != nil {
		t.Fatalf("Changeset(): %v", err)
	}
	if vista.CanDryRun {
		t.Error("CanDryRun = true en una conexión de solo lectura: el botón se " +
			"ofrece para que falle")
	}
}

// TestSinCambiosNoHayNadaQueEnsayar: ensayar un changeset vacío abriría una
// transacción y la cerraría sin haber comprobado nada, y devolvería «salió
// bien» — que es la respuesta más engañosa posible.
func TestSinCambiosNoHayNadaQueEnsayar(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	if _, err := sesion.DryRun(context.Background(), ""); err == nil {
		t.Fatal("DryRun() con el changeset vacío dijo que salió bien")
	}
}

// TestElEnsayoEnSQLiteVeLoQueSoloSeSabeAlCerrar.
//
// Es la contraparte de TestVerifyAdelantaLasRestriccionesDiferidas, del lado de
// SQLite, y prueba que `Verify` no es decorativo en este motor.
//
// Agregarle una clave foránea a una tabla es una RECONSTRUCCIÓN: tabla nueva,
// copiar, tirar la vieja, renombrar. Y mientras la reconstrucción corre las
// claves foráneas están APAGADAS —tienen que estarlo, o el DROP borraría en
// cascada las filas de las hijas—, así que ninguna sentencia se queja de que
// los datos no cumplan la clave nueva. Eso se sabe recién en el
// `foreign_key_check` del final.
//
// Sin `Verify`, el ensayo abriría, copiaría, revertiría y diría que todo bien.
func TestElEnsayoEnSQLiteVeLoQueSoloSeSabeAlCerrar(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq := esquemaDeApply(t, sesion, c)
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta(): %v", err)
	}

	// Una hija que apunta a un padre que no existe. Es un estado que SQLite
	// acepta tener: las claves se comprueban al escribir, y esta fila se
	// escribe antes de que exista la clave.
	for _, sql := range []string{
		`DROP TABLE IF EXISTS "kn_dry_hija"`,
		`DROP TABLE IF EXISTS "kn_dry_padre"`,
		`CREATE TABLE "kn_dry_padre" (id integer PRIMARY KEY)`,
		`CREATE TABLE "kn_dry_hija" (id integer PRIMARY KEY, padre_id integer)`,
		`INSERT INTO "kn_dry_padre" (id) VALUES (1)`,
		`INSERT INTO "kn_dry_hija" (id, padre_id) VALUES (1, 999)`,
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatalf("preparar (%s): %v", sql, err)
		}
	}

	vista, err := sesion.Stage(ctx, change.Change{
		Type: change.AddForeignKey, Schema: esq, Table: "kn_dry_hija", Source: "test",
		Names: []string{"padre_id"}, RefTable: "kn_dry_padre", RefNames: []string{"id"},
	}, "")
	if err != nil {
		t.Fatalf("Stage(): %v", err)
	}
	if !vista.Statement.RebuildsTable {
		t.Fatal("agregar una clave foránea en SQLite tiene que ser una reconstrucción; " +
			"si dejó de serlo, este test ya no prueba lo que dice")
	}

	res, err := sesion.DryRun(ctx, "")
	if err != nil {
		t.Fatalf("DryRun(): %v", err)
	}
	if res.OK {
		t.Fatal("el ensayo dijo que la clave foránea iba a entrar, y hay una fila que " +
			"apunta a un padre que no existe: el apply va a fallar en el foreign_key_check")
	}

	// Y las filas siguen las dos ahí: la reconstrucción se revirtió entera.
	for _, tb := range []string{"kn_dry_padre", "kn_dry_hija"} {
		n, fail := abierta.db.Count(ctx, esq, tb)
		if fail != nil {
			t.Fatalf("Count(%s): %s", tb, fail.Message)
		}
		if n != 1 {
			t.Errorf("%s quedó con %d filas y tenía 1", tb, n)
		}
	}
}

// TestEnsayarEnProduccionPideLaMismaConfirmacionQueAplicar.
//
// Al principio el ensayo NO la pedía, con el argumento de que escribir el
// nombre de la base es la puerta de «esto queda» y esto no queda. El argumento
// miraba la consecuencia equivocada: un ensayo hace el MISMO trabajo que el
// apply y toma los MISMOS candados, así que contra una tabla grande de
// producción el corte de servicio es idéntico. Lo único que cambia es lo que
// queda escrito después.
//
// Y la comprobación vive en Go, no en el botón: una que vive solo del lado de la
// interfaz no es una protección, es un cartel.
func TestEnsayarEnProduccionPideLaMismaConfirmacionQueAplicar(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq := esquemaDeApply(t, sesion, c)

	if _, err := sesion.Stage(ctx, change.Change{
		Type: change.CreateTable, Schema: esq, Table: "kn_dry_prod", Source: "test",
		Columns: []change.Column{{Name: "id", DataType: "integer"}}, Names: []string{"id"},
	}, ""); err != nil {
		t.Fatalf("Stage(): %v", err)
	}

	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta(): %v", err)
	}
	abierta.conn.Environment = connection.Production
	t.Cleanup(func() {
		_ = abierta.db.Exec(context.Background(), `DROP TABLE IF EXISTS "kn_dry_prod"`)
	})

	if _, err := sesion.DryRun(ctx, ""); err == nil {
		t.Fatal("ensayó contra producción sin confirmación")
	} else if !errors.Is(err, ErrNeedsConfirmation) {
		t.Errorf("el error no es el de confirmación: %v", err)
	}
	if _, err := sesion.DryRun(ctx, "no es el nombre"); err == nil {
		t.Fatal("aceptó una confirmación que no coincide")
	}

	// Con la palabra correcta sí. Una puerta que no se puede abrir tampoco
	// sirve.
	res, err := sesion.DryRun(ctx, nombreDeLaBase(abierta))
	if err != nil {
		t.Fatalf("DryRun() con la confirmación correcta: %v", err)
	}
	if !res.OK {
		t.Fatalf("el ensayo falló: %+v", res.Failure)
	}
	if _, err := sesion.TableDetail(ctx, esq, "kn_dry_prod"); err == nil {
		t.Error("la tabla existe: el ensayo contra producción aplicó")
	}
}

// txQueNoRevierte es una transacción cuyo ROLLBACK falla.
//
// Existe porque ese caso no se puede provocar contra un motor de verdad sin
// romper la conexión a mano en el medio, y la afirmación que depende de él
// —«no quedó nada»— es la única que el botón hace.
type txQueNoRevierte struct{ err error }

func (t txQueNoRevierte) Exec(context.Context, string) error                   { return nil }
func (t txQueNoRevierte) Modify(context.Context, string, []any) (int64, error) { return 0, nil }
func (t txQueNoRevierte) Commit(context.Context) error                         { return nil }
func (t txQueNoRevierte) Verify(context.Context) error                         { return nil }
func (t txQueNoRevierte) Rollback(context.Context) error                       { return t.err }

// TestUnEnsayoQueNoPudoRevertirNoDiceQueRevirtio.
//
// `RolledBack` estuvo puesto en true al armar el resultado, antes de revertir,
// mientras el ROLLBACK de verdad era el diferido y su error se descartaba. Un
// ROLLBACK que falla casi siempre significa que la conexión se murió —y
// entonces el motor aborta la transacción igual— así que no es que los cambios
// hayan quedado: es que ya no se puede AFIRMAR que no quedaron.
//
// Es la diferencia entre «no quedó nada» y «no sé si quedó algo», y la pantalla
// dice la primera.
func TestUnEnsayoQueNoPudoRevertirNoDiceQueRevirtio(t *testing.T) {
	ctx := context.Background()

	var res ApplyResult
	if revertirEnsayo(ctx, txQueNoRevierte{err: errors.New("la conexión se cayó")}, &res) {
		t.Error("dijo que revirtió con un ROLLBACK que falló")
	}
	if res.RolledBack {
		t.Error("RolledBack quedó en true con un ROLLBACK que falló: la pantalla va a " +
			"decir «no quedó nada» sin saberlo")
	}

	var ok ApplyResult
	if !revertirEnsayo(ctx, txQueNoRevierte{}, &ok) || !ok.RolledBack {
		t.Error("con un ROLLBACK que anda tiene que decir que revirtió; si no, el " +
			"ensayo normal reportaría un fallo que no existe")
	}
}
