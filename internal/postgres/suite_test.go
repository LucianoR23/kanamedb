package postgres_test

import (
	"context"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/engine/enginetest"
	"github.com/LucianoR23/kanamedb/internal/postgres"
)

const dsnPrueba = "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable"

// TestSuiteDeMotor corre contra Postgres la misma batería que corren los otros
// tres. Es lo que hace que «está implementado» signifique lo mismo para todos.
func TestSuiteDeMotor(t *testing.T) {
	enginetest.Correr(t, enginetest.Fixture{
		Abrir:          abrirPG,
		Esquema:        func(engine.Conn) string { return esquemaDePrueba },
		TipoTexto:      "text",
		TipoEntero:     "bigint",
		TiposEsperados: []string{"text", "bigint"},
	})
}

const esquemaDePrueba = "kn_suite"

func abrirPG(t *testing.T) engine.Conn {
	t.Helper()
	c, f := postgres.Open(context.Background(), dsnPrueba, "base de pruebas",
		engine.OpenOptions{MaxConns: 4})
	if f != nil {
		t.Skipf("no hay Postgres escuchando (%s).\n"+
			"Levantalo con: docker compose -f docker-compose.test.yml up -d", f.Message)
	}
	ctx := context.Background()
	if err := c.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+esquemaDePrueba); err != nil {
		c.Close()
		t.Fatalf("crear el esquema de prueba: %v", err)
	}
	return c
}
