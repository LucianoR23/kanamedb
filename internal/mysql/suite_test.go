package mysql_test

import (
	"context"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/engine/enginetest"
	"github.com/LucianoR23/kanamedb/internal/mysql"
)

const (
	dsnMySQL   = "kaname:kaname@tcp(127.0.0.1:53306)/kaname_test?parseTime=true"
	dsnMariaDB = "kaname:kaname@tcp(127.0.0.1:53307)/kaname_test?parseTime=true"
)

// Los dos motores corren la MISMA batería que Postgres. Es lo que hace que
// «está implementado» signifique lo mismo para todos.
func TestSuiteMySQL(t *testing.T)   { correr(t, dsnMySQL) }
func TestSuiteMariaDB(t *testing.T) { correr(t, dsnMariaDB) }

func correr(t *testing.T, dsn string) {
	enginetest.Correr(t, enginetest.Fixture{
		Abrir: func(t *testing.T) engine.Conn { return abrir(t, dsn) },
		// En MySQL «esquema» y «base» son la misma cosa.
		Esquema: func(c engine.Conn) string { return c.Server().CurrentDB },
		// varchar(64) y no text: MySQL rechaza varchar sin largo, y un TEXT no
		// se indexa sin decirle cuántos caracteres — y la suite crea un índice
		// sobre esa columna.
		TipoTexto:      "varchar(64)",
		TipoEntero:     "bigint",
		TiposEsperados: []string{"varchar", "bigint"},
	})
}

func abrir(t *testing.T, dsn string) engine.Conn {
	t.Helper()
	c, f := mysql.Open(context.Background(), dsn, "base de pruebas",
		engine.OpenOptions{MaxConns: 4})
	if f != nil {
		t.Skipf("no hay motor escuchando (%s).\n"+
			"Levantalo con: docker compose -f docker-compose.test.yml up -d", f.Message)
	}
	return c
}
