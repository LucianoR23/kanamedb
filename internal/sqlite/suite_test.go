package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/engine/enginetest"
	"github.com/LucianoR23/kanamedb/internal/sqlite"
)

// SQLite corre la MISMA batería que los otros tres motores. Es lo que hace que
// «está implementado» signifique lo mismo para todos.
//
// A diferencia de ellos, no necesita un contenedor: cada corrida usa un archivo
// nuevo en el directorio temporal del test. Por eso este no se saltea nunca —y
// eso también es una diferencia que vale la pena tener presente cuando algo
// falla acá y no allá.
func TestSuiteSQLite(t *testing.T) {
	enginetest.Correr(t, enginetest.Fixture{
		Abrir: func(t *testing.T) engine.Conn { return abrir(t) },
		// SQLite no tiene esquemas. La suite ya sabe que con "" no hay que
		// calificar los nombres.
		Esquema:        func(engine.Conn) string { return "" },
		TipoTexto:      "text",
		TipoEntero:     "integer",
		TiposEsperados: []string{"text", "integer"},
	})
}

func abrir(t *testing.T) engine.Conn {
	t.Helper()
	f := filepath.Join(t.TempDir(), "kaname.db")
	// El DSN es el que arma connection.dsnSQLite: las claves foráneas
	// ENCENDIDAS. Probar con ellas apagadas sería probar otra cosa —justamente
	// la que no tiene el problema que el rebuild tiene que resolver—.
	dsn := "file:" + filepath.ToSlash(f) + "?_pragma=foreign_keys%281%29&_pragma=busy_timeout%285000%29"
	c, fail := sqlite.Open(context.Background(), dsn, f, engine.OpenOptions{MaxConns: 4})
	if fail != nil {
		t.Fatalf("no se pudo abrir el archivo de prueba: %s — %s", fail.Message, fail.Detail)
	}
	// El cierre se registra acá y no solo en quien llama porque en Windows un
	// archivo abierto no se puede borrar: sin esto, la limpieza de t.TempDir()
	// falla y el test da rojo aunque haya pasado. Close es idempotente, así
	// que registrarlo dos veces no molesta.
	t.Cleanup(c.Close)
	return c
}

// TestElDSNQueArmaConnectionEnciendeLasClavesForaneas prueba la unión entre los
// dos paquetes, que ninguno de los dos prueba solo.
//
// connection.dsnSQLite arma el DSN con url.Values.Encode(), que escapa los
// paréntesis: `foreign_keys(1)` viaja como `foreign_keys%281%29`. Que el driver
// lo desescape antes de aplicarlo es una suposición, y de ella depende que las
// claves foráneas estén encendidas — que a su vez es de lo que depende que la
// reconstrucción de tabla haga falta apagarlas.
//
// El test de connection comprueba el TEXTO del DSN, no su efecto. Este
// comprueba el efecto.
func TestElDSNQueArmaConnectionEnciendeLasClavesForaneas(t *testing.T) {
	archivo := filepath.Join(t.TempDir(), "real.db")
	cn := connection.Connection{
		Name:     "prueba",
		Engine:   engine.SQLite,
		Database: filepath.ToSlash(archivo),
	}
	dsn, err := cn.DSN("")
	if err != nil {
		t.Fatalf("DSN(): %v", err)
	}
	c, fail := sqlite.Open(context.Background(), dsn, archivo, engine.OpenOptions{MaxConns: 2})
	if fail != nil {
		t.Fatalf("abrir con el DSN de connection: %s — %s", fail.Message, fail.Detail)
	}
	t.Cleanup(c.Close)

	ctx := context.Background()
	for _, s := range []string{
		`CREATE TABLE padre (id integer PRIMARY KEY)`,
		`CREATE TABLE hija (id integer PRIMARY KEY, pid integer REFERENCES padre(id))`,
	} {
		if err := c.Exec(ctx, s); err != nil {
			t.Fatalf("preparar: %v", err)
		}
	}
	if err := c.Exec(ctx, `INSERT INTO hija VALUES (1, 999)`); err == nil {
		t.Fatal("se pudo insertar una fila huérfana: el DSN de connection NO encendió las " +
			"claves foráneas, así que el diagrama dibujaría relaciones que la base no " +
			"hace cumplir")
	}
}

// TestUnaRutaConCaracteresDeURIAbreElArchivoQueSePidio.
//
// El DSN de SQLite es un URI de verdad, y SQLite lo parsea como tal: la ruta
// termina en el primer `?` o `#`, y las secuencias `%HH` se decodifican. Con la
// ruta metida sin escapar, una base en `…/notas#1/app.db` se cortaba en el `#`
// y —como el driver abre con SQLITE_OPEN_CREATE— SQLite CREABA un archivo
// vacío llamado `notas` y lo abría. Sin error: Kaname mostraba una base vacía
// mientras la del usuario seguía intacta en otro lado.
//
// El test comprueba el efecto y no el texto del DSN: lo que importa es qué
// archivo termina abierto.
func TestUnaRutaConCaracteresDeURIAbreElArchivoQueSePidio(t *testing.T) {
	base := t.TempDir()
	for _, carpeta := range []string{"notas#1", "cien%20por%20ciento", "con espacio", "que?"} {
		t.Run(carpeta, func(t *testing.T) {
			dir := filepath.Join(base, carpeta)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Skipf("el sistema de archivos no acepta el nombre %q: %v", carpeta, err)
			}
			archivo := filepath.ToSlash(filepath.Join(dir, "app.db"))

			cn := connection.Connection{Name: "p", Engine: engine.SQLite, Database: archivo}
			dsn, err := cn.DSN("")
			if err != nil {
				t.Fatalf("DSN(): %v", err)
			}
			c, fail := sqlite.Open(context.Background(), dsn, archivo,
				engine.OpenOptions{MaxConns: 1})
			if fail != nil {
				t.Fatalf("no abrió %s: %s — %s", archivo, fail.Message, fail.Detail)
			}
			if err := c.Exec(context.Background(), "CREATE TABLE marca (x integer)"); err != nil {
				t.Fatalf("escribir: %v", err)
			}
			c.Close()

			if _, err := os.Stat(filepath.FromSlash(archivo)); err != nil {
				t.Fatalf("se abrió y se escribió, pero el archivo pedido no existe: %v.\n"+
					"SQLite cortó la ruta en un carácter de URI y creó otro archivo.", err)
			}
		})
	}
}
