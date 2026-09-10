package mysql_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/engine/enginetest"
	"github.com/LucianoR23/kanamedb/internal/mysql"
)

const (
	dsnMySQL   = "kaname:kaname@tcp(127.0.0.1:53306)/kaname_test?parseTime=true"
	dsnMariaDB = "kaname:kaname@tcp(127.0.0.1:53307)/kaname_test?parseTime=true"
	// Las LTS más viejas que la aplicación declara soportar. Corren la batería
	// entera igual que las otras, y no por prolijidad: probando solo la 12.3,
	// la 10.11 de MariaDB no conectaba en absoluto y nadie se enteraba.
	// Declarar una versión soportada y no correr un test contra ella es
	// prometer sin comprobar.
	//
	// La 8.4 de MySQL es la que está instalada en más lugares que la 9.7, así
	// que es la que más importa que ande: es la LTS anterior y le quedan años
	// de soporte.
	dsnMariaDBLTS = "kaname:kaname@tcp(127.0.0.1:53308)/kaname_test?parseTime=true"
	dsnMySQLLTS   = "kaname:kaname@tcp(127.0.0.1:53309)/kaname_test?parseTime=true"
)

// motores son los cuatro servidores contra los que corren los tests que
// dependen de la versión del motor.
//
// El nombre va aparte del DSN porque el nombre del subtest se IMPRIME, y un DSN
// lleva credenciales adentro. Acá son de juguete, pero el hábito de mandar un
// DSN a la salida es el que después manda uno de verdad. Ver CLAUDE.md.
var motores = []struct {
	nombre string
	dsn    string
}{
	{"mysql", dsnMySQL},
	{"mysql-lts", dsnMySQLLTS},
	{"mariadb", dsnMariaDB},
	{"mariadb-lts", dsnMariaDBLTS},
}

// Los dos motores corren la MISMA batería que Postgres. Es lo que hace que
// «está implementado» signifique lo mismo para todos.
func TestSuiteMySQL(t *testing.T)      { correr(t, "mysql", dsnMySQL) }
func TestSuiteMySQLLTS(t *testing.T)   { correr(t, "mysql-lts", dsnMySQLLTS) }
func TestSuiteMariaDB(t *testing.T)    { correr(t, "mariadb", dsnMariaDB) }
func TestSuiteMariaDBLTS(t *testing.T) { correr(t, "mariadb-lts", dsnMariaDBLTS) }

func correr(t *testing.T, nombre, dsn string) {
	enginetest.Correr(t, enginetest.Fixture{
		Abrir: func(t *testing.T) engine.Conn { return abrir(t, nombre, dsn) },
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

// El nombre viaja aparte del DSN porque es lo que se IMPRIME cuando falla. Un
// DSN lleva credenciales adentro; acá son de juguete, pero el hábito de mandar
// uno a la salida es el que después manda uno de verdad. Ver CLAUDE.md.
func abrir(t *testing.T, nombre, dsn string) engine.Conn {
	t.Helper()
	c, f := mysql.Open(context.Background(), dsn, "base de pruebas",
		engine.OpenOptions{MaxConns: 4})
	if f != nil {
		saltear(t, nombre, f)
	}
	return c
}

// saltear corta el subtest cuando el motor no está, y lo convierte en un FALLO
// cuando CI dice que tenía que estar.
//
// Sin lo segundo, un contenedor que no arranca deja el job en verde sin haber
// probado nada — que es exactamente cómo MariaDB estuvo salteándose sin que
// nadie lo notara.
func saltear(t *testing.T, nombre string, f *engine.Failure) {
	t.Helper()
	if os.Getenv("KANAME_REQUIRE_ENGINES") != "" {
		t.Fatalf("KANAME_REQUIRE_ENGINES está puesto y no se pudo abrir %s: %s — %s",
			nombre, f.Message, f.Detail)
	}
	t.Skipf("no hay %s escuchando (%s).\n"+
		"Si el motor está levantado, esto NO es un salteo: es un fallo.\n"+
		"Detalle: %s\n"+
		"Levantalo con: docker compose -f docker-compose.test.yml up -d",
		nombre, f.Message, f.Detail)
}

// TestElModoSoloLecturaValeParaTodasLasConexiones.
//
// `SET SESSION TRANSACTION READ ONLY` vale para UNA conexión. Ejecutado con
// db.Exec después de abrir, se aplica a la que el pool haya entregado en ese
// momento, y la escritura siguiente puede salir por otra que nunca lo recibió.
//
// El escenario real: una conexión marcada como producción y abierta en solo
// lectura. El usuario abre una tabla —conexión 1— y después corre un DELETE en
// el editor —conexión 2—, y se ejecuta. El cartel decía «solo lectura».
//
// Por eso se prueban VARIAS escrituras seguidas, con el pool bien abierto: con
// una sola, la conexión configurada podría ser justo la que toca.
func TestElModoSoloLecturaValeParaTodasLasConexiones(t *testing.T) {
	for _, m := range motores {
		t.Run(m.nombre, func(t *testing.T) {
			dsn := m.dsn
			ctx := context.Background()
			// Primero, con una conexión normal, se crea la tabla.
			w, f := mysql.Open(ctx, dsn, "pruebas", engine.OpenOptions{MaxConns: 2})
			if f != nil {
				saltear(t, m.nombre, f)
			}
			defer w.Close()
			_ = w.Exec(ctx, "DROP TABLE IF EXISTS kn_solo_lectura")
			if err := w.Exec(ctx, "CREATE TABLE kn_solo_lectura (id int)"); err != nil {
				t.Fatalf("crear la tabla: %v", err)
			}
			t.Cleanup(func() { _ = w.Exec(context.Background(), "DROP TABLE IF EXISTS kn_solo_lectura") })

			ro, f := mysql.Open(ctx, dsn, "pruebas",
				engine.OpenOptions{MaxConns: 4, ReadOnly: true})
			if f != nil {
				t.Fatalf("abrir en solo lectura: %s", f.Message)
			}
			defer ro.Close()

			// Leer tiene que andar.
			if _, fail := ro.Run(ctx, "SELECT 1", engine.RunOptions{}); fail != nil {
				t.Fatalf("una lectura falló en modo solo lectura: %s", fail.Message)
			}
			// Y escribir NO, por ninguna de las conexiones del pool.
			for i := 0; i < 12; i++ {
				err := ro.Exec(ctx, "INSERT INTO kn_solo_lectura VALUES (1)")
				if err == nil {
					t.Fatalf("la escritura número %d se ejecutó en una conexión abierta "+
						"en modo solo lectura", i)
				}
			}
		})
	}
}

// TestElLimiteDeTiempoCortaLaSentencia.
//
// El límite se pide al SERVIDOR y no se implementa cancelando desde el cliente:
// cancelar depende de que el cliente siga vivo, y esto no. Una consulta pesada
// tiene que morir aunque se cierre la aplicación.
//
// Estuvo escrito en un comentario y sin implementar: el campo se asignaba y no
// se leía en ninguna parte, así que quien ponía un límite en la interfaz no
// tenía ninguno.
func TestElLimiteDeTiempoCortaLaSentencia(t *testing.T) {
	for _, m := range motores {
		t.Run(m.nombre, func(t *testing.T) {
			dsn := m.dsn
			ctx := context.Background()
			c, f := mysql.Open(ctx, dsn, "pruebas", engine.OpenOptions{
				MaxConns: 2, StatementTimeout: 400 * time.Millisecond,
			})
			if f != nil {
				saltear(t, m.nombre, f)
			}
			defer c.Close()

			// El contexto NO lleva plazo a propósito: lo que se comprueba es
			// que corta el SERVIDOR, no el cliente. Cancelar desde el cliente
			// depende de que el cliente siga vivo; esto no.
			//
			// Y lo que se mide es el TIEMPO, no el error, porque los dos
			// motores difieren en eso y el error no es lo que se prometió: con
			// max_execution_time, MySQL interrumpe el SLEEP y la sentencia
			// termina BIEN —SLEEP() devuelve 1— mientras que MariaDB devuelve
			// un error. Comprobado contra los dos. Lo que la interfaz promete
			// es que no se queda corriendo, y eso es el reloj.
			inicio := time.Now()
			_, _ = c.Run(ctx, "SELECT SLEEP(5)", engine.RunOptions{})
			if pasado := time.Since(inicio); pasado > 3*time.Second {
				t.Errorf("un SELECT SLEEP(5) tardó %v con un límite de 400ms: el "+
					"servidor no lo cortó", pasado)
			}
		})
	}
}
