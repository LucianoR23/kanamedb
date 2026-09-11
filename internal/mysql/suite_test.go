package mysql_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/engine/enginetest"
	"github.com/LucianoR23/kanamedb/internal/mysql"
)

const (
	dsnMySQL   = "kaname:kaname@tcp(127.0.0.1:53306)/kaname_test"
	dsnMariaDB = "kaname:kaname@tcp(127.0.0.1:53307)/kaname_test"
	// Las LTS más viejas que la aplicación declara soportar. Corren la batería
	// entera igual que las otras, y no por prolijidad: probando solo la 12.3,
	// la 10.11 de MariaDB no conectaba en absoluto y nadie se enteraba.
	// Declarar una versión soportada y no correr un test contra ella es
	// prometer sin comprobar.
	//
	// La 8.4 de MySQL es la que está instalada en más lugares que la 9.7, así
	// que es la que más importa que ande: es la LTS anterior y le quedan años
	// de soporte.
	dsnMariaDBLTS = "kaname:kaname@tcp(127.0.0.1:53308)/kaname_test"
	dsnMySQLLTS   = "kaname:kaname@tcp(127.0.0.1:53309)/kaname_test"
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
		ConOtraContrasena: func(t *testing.T, password string) *engine.Failure {
			t.Helper()
			c, f := mysql.Open(context.Background(), strings.Replace(dsn, "kaname:kaname@", "kaname:"+password+"@", 1),
				"base de pruebas", engine.OpenOptions{MaxConns: 1})
			if c != nil {
				c.Close()
			}
			return f
		},
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
			// t.Cleanup y no defer: los defer de la función de test corren ANTES
			// que los t.Cleanup, así que con defer el pool queda cerrado cuando
			// el cleanup de abajo intenta borrar la tabla — y no borra nada, en
			// silencio, porque el error se descarta. Los cleanup son LIFO, así
			// que registrar el cierre primero lo deja último. Comprobado: la
			// tabla quedaba en los cuatro servidores.
			t.Cleanup(w.Close)
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

// TestElValorPorDefectoVuelveAEscribirseComoSQL.
//
// MySQL no tiene COMMENT ON: comentar una columna la reescribe entera con
// MODIFY, así que hay que repetirle el tipo, la nulabilidad y el DEFAULT o los
// pierde. Y el DEFAULT sale del catálogo… que en MySQL lo devuelve SIN comillas.
//
// Resultado: `DEFAULT nuevo`, y el motor contesta 42000. Encontrado desde la
// aplicación, no por un test: la costura estaba bien y el viaje completo no.
//
// Los dos motores difieren y por eso corre contra los cuatro servidores:
// MariaDB entrega el default ya escrito como expresión —un literal con comillas,
// una función sin ellas— y MySQL entrega el valor pelado marcando las
// expresiones en `extra`.
func TestElValorPorDefectoVuelveAEscribirseComoSQL(t *testing.T) {
	for _, m := range motores {
		t.Run(m.nombre, func(t *testing.T) {
			ctx := context.Background()
			c, f := mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{MaxConns: 2})
			if f != nil {
				saltear(t, m.nombre, f)
			}
			t.Cleanup(c.Close)

			const tabla = "kn_def_sql"
			_ = c.Exec(ctx, "DROP TABLE IF EXISTS "+tabla)
			if err := c.Exec(ctx, "CREATE TABLE "+tabla+" ("+
				"id bigint PRIMARY KEY,"+
				"texto varchar(20) NOT NULL DEFAULT 'nuevo',"+
				"numero int DEFAULT 5,"+
				"cuando timestamp DEFAULT CURRENT_TIMESTAMP)"); err != nil {
				t.Fatalf("crear la tabla: %v", err)
			}
			t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP TABLE IF EXISTS "+tabla) })

			d, err := c.Detail(ctx, "", tabla)
			if err != nil {
				t.Fatalf("Detail(): %v", err)
			}
			porNombre := map[string]string{}
			for _, col := range d.Columns {
				porNombre[col.Name] = col.Default
			}
			if got := porNombre["texto"]; got != "'nuevo'" {
				t.Errorf("el default de texto quedó %q y tiene que ser %q: sin comillas, "+
					"un MODIFY que lo repita da error de sintaxis", got, "'nuevo'")
			}
			if got := porNombre["numero"]; got != "5" {
				t.Errorf("el default de numero quedó %q y tiene que ser 5 pelado", got)
			}
			// La expresión NO se cita: citarla guardaría el texto en vez de
			// llamar a la función.
			if got := porNombre["cuando"]; strings.HasPrefix(got, "'") {
				t.Errorf("el default de cuando quedó citado (%q): es una expresión", got)
			}

			// Y lo que de verdad importa: que el DDL que sale de ahí lo acepte
			// el motor. Es el viaje entero, que es donde estaba el bug.
			st, err := c.RenderDDL(ctx, change.Change{
				Type: change.SetColumnComment, Table: tabla,
				Column: &change.Column{
					Name: "texto", DataType: "varchar(20)",
					Nullable: false, Default: porNombre["texto"],
				},
				Comment: `C:\ruta\del\backup`,
			})
			if err != nil {
				t.Fatalf("RenderDDL(): %v", err)
			}
			if err := c.Exec(ctx, st.SQL); err != nil {
				t.Fatalf("el motor rechazó la sentencia:\n%s\n%v", st.SQL, err)
			}

			// Releer: el default sigue, y el comentario quedó con UNA barra.
			d2, err := c.Detail(ctx, "", tabla)
			if err != nil {
				t.Fatalf("Detail() después: %v", err)
			}
			for _, col := range d2.Columns {
				if col.Name != "texto" {
					continue
				}
				if col.Default != "'nuevo'" {
					t.Errorf("comentar la columna le cambió el default a %q", col.Default)
				}
				if col.Comment != `C:\ruta\del\backup` {
					t.Errorf("el comentario quedó %q y se escribió %q",
						col.Comment, `C:\ruta\del\backup`)
				}
			}
		})
	}
}

// TestUnaFechaVuelveComoLaEscribeElServidorYSePuedeDevolver.
//
// Con parseTime en el DSN, el driver convertía DATETIME a time.Time y
// database/sql lo volvía a escribir en RFC 3339 —`2026-09-10T11:49:41Z`—, que
// no es lo que dijo el servidor y que MySQL rechaza si se le manda de vuelta
// (22007). La grilla mostraba una fecha que no se podía editar. Esto comprueba
// el ciclo entero: lo que se lee es el texto del servidor, y ese mismo texto
// entra como parámetro sin error.
func TestUnaFechaVuelveComoLaEscribeElServidorYSePuedeDevolver(t *testing.T) {
	for _, m := range motores {
		t.Run(m.nombre, func(t *testing.T) {
			ctx := context.Background()
			c, f := mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{MaxConns: 2})
			if f != nil {
				saltear(t, m.nombre, f)
			}
			t.Cleanup(c.Close)

			const tabla = "kn_fecha_texto"
			_ = c.Exec(ctx, "DROP TABLE IF EXISTS "+tabla)
			if err := c.Exec(ctx, "CREATE TABLE "+tabla+" (id bigint PRIMARY KEY, cuando datetime, dia date, hora time)"); err != nil {
				t.Fatalf("crear la tabla: %v", err)
			}
			t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP TABLE IF EXISTS "+tabla) })
			if err := c.Exec(ctx, "INSERT INTO "+tabla+" VALUES (1, '2026-09-10 11:49:41', '2026-09-10', '11:49:41')"); err != nil {
				t.Fatalf("cargar la fila: %v", err)
			}

			b, fail := c.Run(ctx, "SELECT cuando, dia, hora FROM "+tabla+" WHERE id = 1", engine.RunOptions{})
			if fail != nil {
				t.Fatalf("Run(): %s", fail.Message)
			}
			fila := b.Results[0].Rows[0]
			quiero := []string{"2026-09-10 11:49:41", "2026-09-10", "11:49:41"}
			for i, q := range quiero {
				if fila[i] == nil || *fila[i] != q {
					got := "NULL"
					if fila[i] != nil {
						got = *fila[i]
					}
					t.Errorf("columna %d: se leyó %q y el servidor escribe %q", i, got, q)
				}
			}

			// Y de vuelta, tal cual, como parámetro: es lo que hace la grilla al
			// editar cualquier otra celda de la fila… o esta misma.
			n, err := c.Modify(ctx, "UPDATE "+tabla+" SET cuando = ?, dia = ?, hora = ? WHERE id = ?",
				[]any{fila[0], fila[1], fila[2], texto("1")})
			if err != nil || n != 1 {
				t.Fatalf("el texto leído no volvió a entrar: n=%d err=%v", n, err)
			}
		})
	}
}

func texto(s string) *string { return &s }
