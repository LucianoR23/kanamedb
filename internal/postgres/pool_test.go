package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestRunNoPideUnaSegundaConexionMientrasRetieneLaPrimera.
//
// Run toma una conexión del pool y la retiene hasta el final. Resolver los
// tipos que pgx no conoce —un enum, acá— pedía OTRA al pool en vez de usar la
// que ya tenía: con PoolSize 1 se colgaba hasta cancelar, y con dos pestañas
// sobre un pool de 2 las dos esperaban a la otra. Hallazgo C-15 de la
// auditoría del 2026-09-11. Con un pool de UNA conexión, la consulta tiene que
// terminar sola.
func TestRunNoPideUnaSegundaConexionMientrasRetieneLaPrimera(t *testing.T) {
	dsn := testDSN(t)
	pool, _, f := Connect(context.Background(), dsn, "base de pruebas", ConnectOptions{MaxConns: 1})
	if f != nil {
		t.Fatalf("Connect(): %s", f.Message)
	}
	t.Cleanup(pool.Close)

	esq := "kn_" + sanear(t.Name())
	for _, sql := range []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", esq),
		fmt.Sprintf("CREATE SCHEMA %s", esq),
		fmt.Sprintf("CREATE TYPE %s.animo AS ENUM ('bien', 'mal')", esq),
	} {
		ejecutar(t, pool, sql)
	}
	t.Cleanup(func() { ejecutar(t, pool, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", esq)) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lote, f := Run(ctx, pool, fmt.Sprintf("SELECT 'bien'::%s.animo AS a", esq), RunOptions{})
	if f != nil {
		if ctx.Err() != nil {
			t.Fatal("Run se colgó con un pool de una conexión: pidió una segunda para resolver el enum")
		}
		t.Fatalf("Run(): %s", f.Message)
	}
	if len(lote.Results) != 1 || len(lote.Results[0].Columns) != 1 {
		t.Fatalf("resultado inesperado: %+v", lote)
	}
	if got := lote.Results[0].Columns[0].DataType; got != "animo" && got != esq+".animo" {
		t.Errorf("el tipo del enum no se resolvió: %q", got)
	}
}
