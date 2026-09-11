package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// La SQL de sesión corre en CADA conexión del pool, no en la primera: es la
// trampa de database/sql y de pgxpool por igual, y la razón de que vaya en
// AfterConnect.
func TestLaSQLDeSesionCorreEnCadaConexionDelPool(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()

	pool, _, f := Connect(ctx, dsn, "pruebas", ConnectOptions{
		MaxConns:   3,
		SessionSQL: "-- lo que corre al abrir\nSET lock_timeout = '3s';\nSET search_path = kn_sesion, public;",
	})
	if f != nil {
		t.Fatalf("Connect() con SQL de sesión falló: %s — %s", f.Message, f.Detail)
	}
	t.Cleanup(pool.Close)

	// Tres conexiones tomadas a la vez son tres conexiones distintas.
	for i := 0; i < 3; i++ {
		c, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Release)
		var lock, path string
		if err := c.QueryRow(ctx, "SHOW lock_timeout").Scan(&lock); err != nil {
			t.Fatal(err)
		}
		if err := c.QueryRow(ctx, "SHOW search_path").Scan(&path); err != nil {
			t.Fatal(err)
		}
		if lock != "3s" || !strings.HasPrefix(path, "kn_sesion") {
			t.Errorf("conexión %d: lock_timeout=%q search_path=%q; la SQL de sesión no llegó", i, lock, path)
		}
	}
}

// Una sentencia que el servidor rechaza no es «no se pudo conectar»: dice
// que es la SQL de sesión y en qué línea, con el código del motor.
func TestUnaSQLDeSesionRotaSeDiceConSuLinea(t *testing.T) {
	dsn := testDSN(t)
	_, _, f := Connect(context.Background(), dsn, "pruebas", ConnectOptions{
		MaxConns:   2,
		SessionSQL: "SET lock_timeout = '3s';\nSELCT 1;",
	})
	if f == nil {
		t.Fatal("una SQL de sesión con un error de sintaxis abrió la conexión igual")
	}
	if !strings.Contains(f.Message, "SQL de sesión") || !strings.Contains(f.Message, "línea 2") {
		t.Errorf("el fallo no dice que es la SQL de sesión ni la línea: %+v", f)
	}
	if f.SQLState != "42601" {
		t.Errorf("SQLState = %q, se esperaba 42601 (syntax_error)", f.SQLState)
	}
}

// Las protecciones se aplican DESPUÉS de la SQL de sesión: una que las apague
// no las gana. Es lo que hace que «solo lectura» siga siendo una promesa
// aunque la SQL de sesión la contradiga.
func TestLaSQLDeSesionNoApagaLasProtecciones(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()

	normal, _, f := Connect(ctx, dsn, "pruebas", ConnectOptions{MaxConns: 2})
	if f != nil {
		t.Fatalf("Connect() falló: %s", f.Message)
	}
	t.Cleanup(normal.Close)
	ejecutar(t, normal, `drop table if exists kn_sesion_ro`)
	ejecutar(t, normal, `create table kn_sesion_ro (id int)`)
	t.Cleanup(func() { ejecutar(t, normal, `drop table if exists kn_sesion_ro`) })

	ro, _, f := Connect(ctx, dsn, "pruebas", ConnectOptions{
		MaxConns:         2,
		ReadOnly:         true,
		StatementTimeout: 300 * time.Millisecond,
		SessionSQL:       "SET default_transaction_read_only = off;\nSET statement_timeout = 0;",
	})
	if f != nil {
		t.Fatalf("Connect(ReadOnly) falló: %s — %s", f.Message, f.Detail)
	}
	t.Cleanup(ro.Close)

	_, f = Run(ctx, ro, `insert into kn_sesion_ro values (1)`, RunOptions{})
	if f == nil {
		t.Fatal("la SQL de sesión apagó el modo solo lectura y el INSERT se ejecutó")
	}

	// Y la SQL de sesión misma corre protegida: una escritura ahí no abre la
	// conexión, porque el modo solo lectura ya viajó en el arranque.
	_, _, f = Connect(ctx, dsn, "pruebas", ConnectOptions{
		MaxConns: 2, ReadOnly: true, SessionSQL: "insert into kn_sesion_ro values (2);",
	})
	if f == nil {
		t.Fatal("una escritura en la SQL de sesión de una conexión de solo lectura abrió la conexión")
	}
	if f.SQLState != "25006" {
		t.Errorf("SQLState = %q, se esperaba 25006: %+v", f.SQLState, f)
	}
	if f.SQLState != "25006" {
		t.Errorf("SQLState = %q, se esperaba 25006", f.SQLState)
	}

	arranque := time.Now()
	if _, f = Run(ctx, ro, `select pg_sleep(10)`, RunOptions{}); f == nil {
		t.Fatal("la SQL de sesión sacó el límite de tiempo y pg_sleep(10) terminó")
	}
	if time.Since(arranque) > 5*time.Second {
		t.Errorf("la consulta tardó %v: el límite de tiempo no se repuso", time.Since(arranque))
	}
}
