package postgres_test

import (
	"context"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// TestVerifyAdelantaLasRestriccionesDiferidas.
//
// Es el test que hace que el ensayo de S15 no mienta.
//
// Una restricción DEFERRABLE INITIALLY DEFERRED no se comprueba en la sentencia
// que la viola: se comprueba en el COMMIT. Un ensayo abre una transacción,
// corre todo y la REVIERTE, así que nunca llega al COMMIT — y entonces una
// violación de ese tipo pasa desapercibida, el ensayo dice «va a andar» y el
// apply falla.
//
// `Verify` existe para eso: adelanta la comprobación con
// `SET CONSTRAINTS ALL IMMEDIATE` y deja la transacción abierta para que el
// ROLLBACK la tire igual.
//
// La segunda mitad del test es la que le da valor a la primera: comprueba que
// SIN Verify la misma transacción se revierte sin un solo error. Si eso también
// fallara, `Verify` no estaría haciendo nada y este test estaría pasando por
// otro motivo.
func TestVerifyAdelantaLasRestriccionesDiferidas(t *testing.T) {
	c := abrirPG(t)
	// t.Cleanup y no `defer c.Close()`: los defer de la funcion de test corren
	// ANTES que los t.Cleanup, así que con defer la conexión queda cerrada cuando
	// `limpiar` intenta usarla y los DROP no hacen nada —en silencio, porque el
	// error se descarta—. Los cleanup corren LIFO, así que registrar el cierre
	// PRIMERO lo deja corriendo ÚLTIMO. Comprobado: las tablas quedaban en la base.
	t.Cleanup(c.Close)
	ctx := context.Background()

	padre := esquemaDePrueba + `."kn_ver_padre"`
	hija := esquemaDePrueba + `."kn_ver_hija"`
	limpiar := func() {
		_ = c.Exec(context.Background(), "DROP TABLE IF EXISTS "+hija)
		_ = c.Exec(context.Background(), "DROP TABLE IF EXISTS "+padre)
	}
	limpiar()
	t.Cleanup(limpiar)

	if err := c.Exec(ctx, "CREATE TABLE "+padre+" (id bigint PRIMARY KEY)"); err != nil {
		t.Fatalf("crear el padre: %v", err)
	}
	if err := c.Exec(ctx, "CREATE TABLE "+hija+" (id bigint, padre_id bigint "+
		"REFERENCES "+padre+" (id) DEFERRABLE INITIALLY DEFERRED)"); err != nil {
		t.Fatalf("crear la hija: %v", err)
	}

	// Con Verify: la violación aparece adentro de la transacción.
	tx, err := c.Begin(ctx, engine.TxOptions{})
	if err != nil {
		t.Fatalf("Begin(): %v", err)
	}
	// Apunta a un padre que no existe. Con la clave diferida, esto NO falla.
	if err := tx.Exec(ctx, "INSERT INTO "+hija+" VALUES (1, 999)"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("el INSERT falló en el acto, así que la clave no quedó diferida "+
			"y este test no está probando lo que dice: %v", err)
	}
	errVerify := tx.Verify(ctx)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback(): %v", err)
	}
	if errVerify == nil {
		t.Error("Verify() no vio una clave foránea rota: el ensayo de S15 va a decir " +
			"que el cambio anda y el apply va a fallar en el COMMIT")
	}

	// Sin Verify: exactamente la misma transacción se revierte sin un error, y
	// por eso hace falta Verify. Esta mitad es la que prueba que la de arriba
	// no está pasando de casualidad.
	tx2, err := c.Begin(ctx, engine.TxOptions{})
	if err != nil {
		t.Fatalf("Begin(): %v", err)
	}
	if err := tx2.Exec(ctx, "INSERT INTO "+hija+" VALUES (2, 999)"); err != nil {
		_ = tx2.Rollback(ctx)
		t.Fatalf("el INSERT falló: %v", err)
	}
	if err := tx2.Rollback(ctx); err != nil {
		t.Fatalf("Rollback(): %v", err)
	}
	// Nada explotó. Ese silencio es el bug que Verify tapa.

	// Y Verify no rompe una transacción sana: después de llamarlo se puede
	// seguir usando y commitear. Sin esto, el ensayo funcionaría y el día que
	// alguien reusara Verify en otro lado se llevaría una sorpresa.
	tx3, err := c.Begin(ctx, engine.TxOptions{})
	if err != nil {
		t.Fatalf("Begin(): %v", err)
	}
	if err := tx3.Exec(ctx, "INSERT INTO "+padre+" VALUES (7)"); err != nil {
		_ = tx3.Rollback(ctx)
		t.Fatalf("INSERT en el padre: %v", err)
	}
	if err := tx3.Verify(ctx); err != nil {
		_ = tx3.Rollback(ctx)
		t.Fatalf("Verify() falló con todo en orden: %v", err)
	}
	if err := tx3.Exec(ctx, "INSERT INTO "+hija+" VALUES (3, 7)"); err != nil {
		_ = tx3.Rollback(ctx)
		t.Fatalf("la transacción quedó inutilizable después de Verify(): %v", err)
	}
	if err := tx3.Commit(ctx); err != nil {
		t.Fatalf("Commit() después de Verify(): %v", err)
	}
}
