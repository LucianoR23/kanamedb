package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
)

// TestConAutocommitSacadoLaTransaccionEsDeVerdad.
//
// `BEGIN`, `INSERT` y `ROLLBACK` en TRES ejecuciones separadas, como las hace
// una persona: con la pestaña dueña de una conexión, el ROLLBACK revierte el
// INSERT en los cuatro motores. Es lo que K-03 hacía al revés.
func TestConAutocommitSacadoLaTransaccionEsDeVerdad(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_manual", true)
			abierta, _ := sesion.abierta()
			q := NewQueries(sesion)
			nom := califica(c, esq, tabla)

			st, err := q.SetAutocommit(ctx, "tab1", false)
			if err != nil || st.Autocommit {
				t.Fatalf("SetAutocommit(false): %v %+v", err, st)
			}
			for i, sql := range []string{"BEGIN", "INSERT INTO " + nom + " (id, nombre) VALUES (9, 'nueve')"} {
				res := q.RunIn(ctx, "tab1", "r", sql)
				if !res.OK {
					t.Fatalf("%q: %s %s", sql, mensajeDe(res), detalleDe(res))
				}
				if res.Transaction == nil || !res.Transaction.InTransaction || res.Transaction.Statements != i+1 {
					t.Errorf("%q: estado %+v", sql, res.Transaction)
				}
			}
			// Otra pestaña, en autocommit, no ve la fila: la transacción es de la
			// primera.
			if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 2 {
				t.Errorf("la fila sin confirmar se ve desde otra conexión: %d filas", n)
			}
			res := q.RunIn(ctx, "tab1", "r", "ROLLBACK")
			if !res.OK || res.Transaction == nil || res.Transaction.InTransaction {
				t.Fatalf("ROLLBACK: %s %+v", mensajeDe(res), res.Transaction)
			}
			if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 2 {
				t.Errorf("después del ROLLBACK hay %d filas de 2: la transacción no era de verdad", n)
			}

			// Y confirmar por el botón deja la fila.
			for _, sql := range []string{"BEGIN", "INSERT INTO " + nom + " (id, nombre) VALUES (9, 'nueve')"} {
				if res := q.RunIn(ctx, "tab1", "r", sql); !res.OK {
					t.Fatal(mensajeDe(res))
				}
			}
			if res := q.Commit(ctx, "tab1", ""); !res.OK || res.Transaction.InTransaction {
				t.Fatalf("Commit: %s %+v", mensajeDe(res), res.Transaction)
			}
			if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 3 {
				t.Errorf("después del Commit hay %d filas de 3", n)
			}

			// Volver a auto-commit con una transacción abierta se niega; sin
			// ella, se puede.
			if res := q.RunIn(ctx, "tab1", "r", "BEGIN"); !res.OK {
				t.Fatal(mensajeDe(res))
			}
			if _, err := q.SetAutocommit(ctx, "tab1", true); !errors.Is(err, ErrTransactionOpen) {
				t.Errorf("SetAutocommit(true) con transacción abierta devolvió %v", err)
			}
			if res := q.Rollback(ctx, "tab1"); !res.OK {
				t.Fatal(mensajeDe(res))
			}
			if st, err := q.SetAutocommit(ctx, "tab1", true); err != nil || !st.Autocommit {
				t.Errorf("SetAutocommit(true): %v %+v", err, st)
			}
			// En modo normal, BEGIN vuelve a rechazarse.
			if res := q.RunIn(ctx, "tab1", "r", "BEGIN"); res.OK {
				t.Error("con auto-commit puesto se aceptó un BEGIN")
			}
		})
	}
}

// TestCerrarLaPestanaRevierteYNoEnvenenaElPool: cerrar con una transacción
// abierta revierte, y la conexión vuelve limpia: un Apply después funciona
// (en SQLite daba «cannot start a transaction within a transaction» si la
// conexión volvía en transacción, C-01).
func TestCerrarLaPestanaRevierteYNoEnvenenaElPool(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_cierre", true)
			abierta, _ := sesion.abierta()
			q := NewQueries(sesion)
			if _, err := q.SetAutocommit(ctx, "tab2", false); err != nil {
				t.Fatal(err)
			}
			for _, sql := range []string{"BEGIN", "UPDATE " + califica(c, esq, tabla) + " SET nombre = 'x'"} {
				if res := q.RunIn(ctx, "tab2", "r", sql); !res.OK {
					t.Fatal(mensajeDe(res))
				}
			}
			if err := q.CloseTab("tab2"); err != nil {
				t.Fatalf("CloseTab: %v", err)
			}
			if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos" {
				t.Errorf("cerrar la pestaña no revirtió: %q", got)
			}
			// El pool quedó sano: un cambio de esquema aplica.
			if _, err := sesion.Schema(ctx, true); err != nil {
				t.Fatal(err)
			}
			if _, err := sesion.Stage(ctx, change.Change{
				Type: change.AddColumn, Schema: esq, Table: tabla, Source: "test",
				Column: &change.Column{Name: "extra", DataType: tipoTextoDe(c.Engine), Nullable: true},
			}, ""); err != nil {
				t.Fatal(err)
			}
			if res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true}); err != nil || !res.OK {
				t.Fatalf("Apply después de cerrar la pestaña: %v %+v", err, res.Failure)
			}
			_ = abierta

			// Y desconectar también cierra lo que las pestañas tuvieran.
			if _, err := q.SetAutocommit(ctx, "tab3", false); err != nil {
				t.Fatal(err)
			}
			if res := q.RunIn(ctx, "tab3", "r", "BEGIN"); !res.OK {
				t.Fatal(mensajeDe(res))
			}
			sesion.Disconnect()
			if res := sesion.Connect(ctx, c.ID); !res.OK {
				t.Fatalf("reconectar: %+v", res.Failure)
			}
			if st, err := q.TransactionOf("tab3"); err != nil || !st.Autocommit {
				t.Errorf("la pestaña sobrevivió a la desconexión: %v %+v", err, st)
			}
		})
	}
}

// TestContraProduccionElCommitPideElNombre: el botón pide la palabra, y un
// COMMIT tipeado en el editor no la saltea.
func TestContraProduccionElCommitPideElNombre(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_prod_tx", true)
	abierta, _ := sesion.abierta()
	abierta.conn.Environment = connection.Production
	q := NewQueries(sesion)
	if _, err := q.SetAutocommit(ctx, "p1", false); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"BEGIN", "DELETE FROM " + califica(c, esq, tabla)} {
		if res := q.RunIn(ctx, "p1", "r", sql); !res.OK {
			t.Fatal(mensajeDe(res))
		}
	}
	if res := q.RunIn(ctx, "p1", "r", "COMMIT"); res.OK || !strings.Contains(res.Failure.Message, "botón") {
		t.Errorf("un COMMIT tipeado contra producción se aceptó: %s", mensajeDe(res))
	}
	if res := q.Commit(ctx, "p1", ""); res.OK || !strings.Contains(res.Failure.Message, "hay que escribir") {
		t.Errorf("Commit sin la palabra: %s", mensajeDe(res))
	}
	if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 2 {
		t.Fatalf("se confirmó sin la palabra: %d filas", n)
	}
	if res := q.Commit(ctx, "p1", nombreDeLaBase(abierta)); !res.OK {
		t.Fatalf("Commit con la palabra: %s", mensajeDe(res))
	}
	if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 0 {
		t.Errorf("el Commit con la palabra no confirmó: %d filas", n)
	}
}

// TestCancelarDentroDeUnaTransaccionNoDejaLaPestanaAtascada: pgx y el driver
// de MySQL cierran la conexión al cancelar. La pestaña tiene que descartarla,
// decir que la transacción se perdió, y seguir andando en la próxima
// ejecución (review del 2026-09-12).
func TestCancelarDentroDeUnaTransaccionNoDejaLaPestanaAtascada(t *testing.T) {
	lentas := map[string]string{
		"postgres": "SELECT pg_sleep(20)",
		"mysql":    "SELECT SLEEP(20)",
		"mariadb":  "SELECT SLEEP(20)",
	}
	for _, caso := range motoresDeEnsayo {
		lenta, hay := lentas[caso.nombre]
		if !hay {
			continue
		}
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_cancel_tx", true)
			q := NewQueries(sesion)
			if _, err := q.SetAutocommit(ctx, "tc", false); err != nil {
				t.Fatal(err)
			}
			for _, sql := range []string{"BEGIN", "DELETE FROM " + califica(c, esq, tabla)} {
				if res := q.RunIn(ctx, "tc", "r", sql); !res.OK {
					t.Fatal(mensajeDe(res))
				}
			}
			hecho := make(chan RunResult, 1)
			go func() { hecho <- q.RunIn(ctx, "tc", "lenta", lenta) }()
			time.Sleep(600 * time.Millisecond)
			q.Cancel("lenta")
			res := <-hecho
			if res.OK {
				t.Fatal("la sentencia cancelada salió bien")
			}
			if res.Transaction == nil || res.Transaction.InTransaction {
				t.Errorf("después de cancelar, la pestaña sigue diciendo que hay transacción: %+v", res.Transaction)
			}
			if !strings.Contains(res.Failure.Hint, "se perdió") && !strings.Contains(res.Failure.Hint, "ya no lo está") {
				t.Errorf("no se avisó que la transacción se perdió: %q", res.Failure.Hint)
			}
			// El DELETE no quedó, y la pestaña sigue sirviendo.
			if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos" {
				t.Errorf("el DELETE de la transacción perdida se confirmó: %q", got)
			}
			if res := q.RunIn(ctx, "tc", "r", "SELECT 1"); !res.OK {
				t.Errorf("la pestaña quedó atascada: %s %s", mensajeDe(res), detalleDe(res))
			}
			if _, err := q.SetAutocommit(ctx, "tc", true); err != nil {
				t.Errorf("no se pudo volver a auto-commit: %v", err)
			}
		})
	}
}

// TestSQLiteResincronizaCuandoUnaSentenciaFalla: SQLite revierte la
// transacción entera si una escritura falla por ciertas causas, y un ROLLBACK
// «sin transacción activa» falla. El estado se vuelve a preguntar.
func TestSQLiteResincronizaCuandoUnaSentenciaFalla(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_resync", true)
	q := NewQueries(sesion)
	if _, err := q.SetAutocommit(ctx, "rs", false); err != nil {
		t.Fatal(err)
	}
	if res := q.RunIn(ctx, "rs", "r", "BEGIN"); !res.OK {
		t.Fatal(mensajeDe(res))
	}
	// Un ROLLBACK tipeado cierra; un segundo ROLLBACK falla, y el estado tiene
	// que decir «sin transacción», no seguir en la anterior.
	if res := q.RunIn(ctx, "rs", "r", "ROLLBACK"); !res.OK {
		t.Fatal(mensajeDe(res))
	}
	res := q.RunIn(ctx, "rs", "r", "ROLLBACK")
	if res.OK || res.Transaction == nil || res.Transaction.InTransaction {
		t.Errorf("después de un ROLLBACK fallido el estado es %+v", res.Transaction)
	}
	// Y una sentencia que falla adentro de una transacción no la cierra: el
	// estado sigue abierto y el ROLLBACK del botón anda.
	for _, sql := range []string{"BEGIN", "DELETE FROM " + califica(c, esq, tabla)} {
		if res := q.RunIn(ctx, "rs", "r", sql); !res.OK {
			t.Fatal(mensajeDe(res))
		}
	}
	res = q.RunIn(ctx, "rs", "r", "SELECT * FROM no_existe")
	if res.OK || res.Transaction == nil || !res.Transaction.InTransaction {
		t.Errorf("un SELECT fallido cerró la transacción según la pestaña: %+v", res.Transaction)
	}
	if res := q.Rollback(ctx, "rs"); !res.OK {
		t.Errorf("Rollback: %s", mensajeDe(res))
	}
	if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos" {
		t.Errorf("la tabla quedó %q", got)
	}
}

// TestEnModoManualSetAutocommitYCommitEncadenadoSeSiguen: `SET autocommit` se
// rechaza también en manual (haría por debajo lo que la casilla hace, sin
// que se vea), y `ROLLBACK TO`, `COMMIT AND CHAIN` y los comentarios no
// despistan al seguimiento.
func TestEnModoManualSetAutocommitYCommitEncadenadoSeSiguen(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_chain", true)
	q := NewQueries(sesion)
	if _, err := q.SetAutocommit(ctx, "ch", false); err != nil {
		t.Fatal(err)
	}
	if res := q.RunIn(ctx, "ch", "r", "SET autocommit = 0"); res.OK {
		t.Error("SET autocommit se aceptó en modo manual")
	}
	pasos := []struct {
		sql  string
		enTx bool
	}{
		{"BEGIN", true},
		{"SAVEPOINT a", true},
		{"DELETE FROM " + califica(c, esq, tabla), true},
		{"-- deshacer\nROLLBACK TRANSACTION TO SAVEPOINT a", true},
		{"RELEASE SAVEPOINT a", true},
		{"ROLLBACK", false},
	}
	for _, p := range pasos {
		res := q.RunIn(ctx, "ch", "r", p.sql)
		if !res.OK {
			t.Fatalf("%q: %s %s", p.sql, mensajeDe(res), detalleDe(res))
		}
		if res.Transaction.InTransaction != p.enTx {
			t.Errorf("%q: InTransaction=%v, se esperaba %v", p.sql, res.Transaction.InTransaction, p.enTx)
		}
	}
	if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos" {
		t.Errorf("la tabla quedó %q", got)
	}
	// Cerrar la pestaña con el estado bien seguido no deja nada abierto: la
	// conexión vuelve limpia y un BEGIN por otra conexión no choca.
	if err := q.CloseTab("ch"); err != nil {
		t.Fatal(err)
	}
	ab, _ := sesion.abierta()
	if err := ab.db.Exec(ctx, "BEGIN; ROLLBACK;"); err != nil {
		t.Errorf("la conexión volvió al pool en transacción: %v", err)
	}
}

// TestApagarAutocommitMientrasVenceLaInactividadNoSeTraba: los dos tomaban
// los candados en órdenes distintos y se trababan para siempre (review del
// 2026-09-12). Se martillan en paralelo: el test termina, o el timeout de go
// test lo mata. Comprobado que con el orden viejo se traba.
func TestApagarAutocommitMientrasVenceLaInactividadNoSeTraba(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	q := NewQueries(sesion)
	abierta, _ := sesion.abierta()
	ahora := time.Now()
	sesion.usarReloj(func() time.Time { return ahora })
	listo := make(chan struct{})
	go func() {
		defer close(listo)
		for i := 0; i < 2000; i++ {
			_, _ = q.SetAutocommit(ctx, "dl", false)
			_ = q.RunIn(ctx, "dl", "r", "SELECT 1")
			_, _ = q.SetAutocommit(ctx, "dl", true)
		}
	}()
	for {
		select {
		case <-listo:
			return
		default:
		}
		// El plazo no venció: vencerInactividad cuenta las transacciones
		// (p.mu → t.mu) y se rearma. Se martilla hasta que la otra termine.
		sesion.vencerInactividad(abierta)
		_ = abierta.pestanas.enTransaccion()
	}
}
