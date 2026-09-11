package mysql_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/mysql"
)

// La SQL de sesión corre en CADA conexión del pool —por el conector, como
// las protecciones—, dice en qué línea falló si falla, y no puede apagar el
// modo solo lectura: las protecciones se aplican después. Contra los cuatro
// servidores.
func TestLaSQLDeSesionCorreEnCadaConexionYNoApagaLasProtecciones(t *testing.T) {
	for _, m := range motores {
		t.Run(m.nombre, func(t *testing.T) {
			ctx := context.Background()

			c, f := mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{
				MaxConns:   3,
				SessionSQL: "# lo que corre al abrir\nSET @@session.lock_wait_timeout = 7;\nSET SESSION sql_mode = 'ANSI_QUOTES';",
			})
			if f != nil {
				saltear(t, m.nombre, f)
			}
			defer c.Close()
			// Doce consultas por un pool de tres: alguna sale por cada conexión.
			for i := 0; i < 12; i++ {
				lote, fail := c.Run(ctx, "SELECT @@session.lock_wait_timeout", engine.RunOptions{})
				if fail != nil {
					t.Fatalf("SELECT: %s", fail.Message)
				}
				if v := lote.Results[0].Rows[0][0]; v == nil || *v != "7" {
					t.Fatalf("consulta %d: lock_wait_timeout = %v, la SQL de sesión no llegó a esa conexión", i, v)
				}
			}

			// Una sentencia rota se dice con su línea.
			_, f = mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{
				MaxConns:   2,
				SessionSQL: "SET @@session.lock_wait_timeout = 7;\nSELCT 1;",
			})
			if f == nil {
				t.Fatal("una SQL de sesión con un error de sintaxis abrió la conexión igual")
			}
			if !strings.Contains(f.Message, "SQL de sesión") || !strings.Contains(f.Message, "línea 2") {
				t.Errorf("el fallo no dice que es la SQL de sesión ni la línea: %+v", f)
			}

			// Y las protecciones ganan.
			w, f := mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{MaxConns: 2})
			if f != nil {
				t.Fatalf("abrir: %s", f.Message)
			}
			defer w.Close()
			_ = w.Exec(ctx, "DROP TABLE IF EXISTS kn_sesion_ro")
			if err := w.Exec(ctx, "CREATE TABLE kn_sesion_ro (id int)"); err != nil {
				t.Fatalf("crear la tabla: %v", err)
			}
			defer func() { _ = w.Exec(context.Background(), "DROP TABLE IF EXISTS kn_sesion_ro") }()

			// Cada motor conoce una sola de las dos variables del límite; la
			// del otro es un error, y acá lo que se prueba es que apagarla no
			// gane, no que exista.
			sinLimite := "SET SESSION max_execution_time = 0;"
			if strings.HasPrefix(m.nombre, "mariadb") {
				sinLimite = "SET SESSION max_statement_time = 0;"
			}
			ro, f := mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{
				MaxConns:         2,
				ReadOnly:         true,
				StatementTimeout: 400 * time.Millisecond,
				SessionSQL:       "SET SESSION TRANSACTION READ WRITE;\n" + sinLimite,
			})
			if f != nil {
				t.Fatalf("abrir en solo lectura con SQL de sesión: %s — %s", f.Message, f.Detail)
			}
			defer ro.Close()
			for i := 0; i < 6; i++ {
				if err := ro.Exec(ctx, "INSERT INTO kn_sesion_ro VALUES (1)"); err == nil {
					t.Fatalf("la SQL de sesión apagó el modo solo lectura y la escritura %d se ejecutó", i)
				}
			}
			// Y la SQL de sesión misma corre protegida: una escritura ahí, en
			// una conexión de solo lectura, no abre la conexión. En MySQL el
			// modo es un SET y hay que mandarlo ANTES; sin eso el DELETE de una
			// entrada importada corría con escritura en cada conexión del pool.
			_, f = mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{
				MaxConns:   2,
				ReadOnly:   true,
				SessionSQL: "INSERT INTO kn_sesion_ro VALUES (2);",
			})
			if f == nil {
				t.Fatal("una escritura en la SQL de sesión de una conexión de solo lectura abrió la conexión: corrió con escritura")
			}
			if !strings.Contains(f.Message, "SQL de sesión") {
				t.Errorf("el fallo tiene que ser el de la SQL de sesión: %+v", f)
			}

			// Con max_execution_time, MySQL interrumpe el SLEEP y la sentencia
			// termina BIEN; MariaDB devuelve error. Lo que se mide es el tiempo.
			arranque := time.Now()
			_, _ = ro.Run(ctx, "SELECT SLEEP(10)", engine.RunOptions{})
			if time.Since(arranque) > 5*time.Second {
				t.Errorf("la consulta tardó %v: el límite de tiempo no se repuso", time.Since(arranque))
			}
		})
	}
}
