package engine_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// DSN de cada motor de docker-compose.test.yml. Los puertos están corridos a
// propósito para no chocar con una instalación local.
const (
	dsnPostgres = "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable"
	dsnMySQL    = "kaname:kaname@tcp(127.0.0.1:53306)/kaname_test"
	dsnMariaDB  = "kaname:kaname@tcp(127.0.0.1:53307)/kaname_test"
)

// TestDDLTransaccionalEsCierto comprueba contra los motores DE VERDAD la única
// capacidad que puede volver mentirosa una promesa que ya está en pantalla.
//
// La casilla «Una sola transacción» de S15 dice «todo o nada». Contra Postgres
// y SQLite eso es cierto; contra MySQL y MariaDB es falso, porque las dos hacen
// un commit implícito antes de cada DDL y no hay forma de apagarlo. Si alguien
// invierte un booleano de la tabla de capacidades, la interfaz promete algo que
// la base no cumple y nadie se entera hasta que un apply a medias deja el
// esquema partido.
//
// Por eso no se comprueba leyendo la tabla —eso sería un test que no puede
// fallar— sino ejecutando un CREATE TABLE dentro de una transacción, haciendo
// ROLLBACK, y mirando si la tabla sigue ahí.
func TestDDLTransaccionalEsCierto(t *testing.T) {
	casos := []struct {
		kind   engine.Kind
		abrir  func(t *testing.T) *sql.DB
		existe func(db *sql.DB, tabla string) (bool, error)
	}{
		{engine.Postgres, abrirPostgres, existePostgres},
		{engine.MySQL, abrirDSN("mysql", dsnMySQL), existeMySQL},
		{engine.MariaDB, abrirDSN("mysql", dsnMariaDB), existeMySQL},
		{engine.SQLite, abrirSQLite, existeSQLite},
	}

	for _, c := range casos {
		t.Run(c.kind.String(), func(t *testing.T) {
			db := c.abrir(t)
			ctx := context.Background()
			tabla := "kn_ddl_tx"

			if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+tabla); err != nil {
				t.Fatalf("limpiar: %v", err)
			}
			t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + tabla) })

			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			if _, err := tx.ExecContext(ctx, "CREATE TABLE "+tabla+" (id integer)"); err != nil {
				t.Fatalf("CREATE dentro de la transacción: %v", err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatalf("Rollback: %v", err)
			}

			sobrevivio, err := c.existe(db, tabla)
			if err != nil {
				t.Fatalf("comprobar si existe: %v", err)
			}

			// Si el DDL es transaccional, el rollback tiene que haberla
			// borrado. Si no lo es, tiene que seguir ahí.
			quiere := engine.CapsOf(c.kind).TransactionalDDL
			if quiere && sobrevivio {
				t.Errorf("Caps dice que %s tiene DDL transaccional, pero la tabla "+
					"sobrevivió al ROLLBACK. La casilla «Una sola transacción» "+
					"estaría prometiendo algo que la base no cumple.", c.kind.Label())
			}
			if !quiere && !sobrevivio {
				t.Errorf("Caps dice que %s NO tiene DDL transaccional, pero el "+
					"ROLLBACK sí revirtió el CREATE. Si el motor mejoró, hay que "+
					"actualizar la tabla: la interfaz está escondiendo una "+
					"garantía que ahora existe.", c.kind.Label())
			}
		})
	}
}

// TestLasCapacidadesEstanCompletas comprueba que ningún motor quedó sin
// contestar las preguntas.
//
// No comprueba los valores —eso lo hacen los tests de integración— sino que la
// entrada exista. Un motor que se agrega y se olvida de la tabla recibe el
// conjunto conservador de CapsOf, que es correcto pero apaga funciones que
// quizás sí soporta, y eso es un bug silencioso.
func TestLasCapacidadesEstanCompletas(t *testing.T) {
	for _, k := range engine.Kinds() {
		c := engine.CapsOf(k)
		if c.MaxIdentifier <= 0 {
			t.Errorf("%s no declara MaxIdentifier: sin eso no se puede validar un "+
				"nombre antes de mandarlo", k)
		}
		if k.Label() == string(k) {
			t.Errorf("%s no tiene nombre para mostrar", k)
		}
	}
}

/* ------------------------------------------------------------- ayudantes */

func saltearSinMotor(t *testing.T, kind engine.Kind, db *sql.DB, err error) *sql.DB {
	t.Helper()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		err = db.PingContext(ctx)
	}
	if err != nil {
		// En CI saltearse no es aceptable: si el mapeo de puertos o las
		// credenciales se rompen, esto daría verde sin haber probado nada.
		if os.Getenv("KANAME_REQUIRE_ENGINES") != "" {
			t.Fatalf("KANAME_REQUIRE_ENGINES está puesto y no hay %s: %v", kind, err)
		}
		t.Skipf("no hay %s escuchando (%v).\n"+
			"Levantalo con: docker compose -f docker-compose.test.yml up -d", kind, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func abrirDSN(driver, dsn string) func(*testing.T) *sql.DB {
	return func(t *testing.T) *sql.DB {
		t.Helper()
		db, err := sql.Open(driver, dsn)
		return saltearSinMotor(t, engine.Kind(driver), db, err)
	}
}

// Postgres va por pgx/stdlib porque el resto del programa usa pgx: probar con
// otro driver sería probar otra cosa.
func abrirPostgres(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsnPostgres)
	return saltearSinMotor(t, engine.Postgres, db, err)
}

func abrirSQLite(t *testing.T) *sql.DB {
	t.Helper()
	f := filepath.Join(t.TempDir(), "kn.db")
	db, err := sql.Open("sqlite", "file:"+f)
	return saltearSinMotor(t, engine.SQLite, db, err)
}

func existePostgres(db *sql.DB, tabla string) (bool, error) {
	var n int
	err := db.QueryRow(
		"SELECT count(*) FROM pg_class WHERE relname = $1 AND relkind = 'r'", tabla).Scan(&n)
	return n > 0, err
}

func existeMySQL(db *sql.DB, tabla string) (bool, error) {
	var n int
	err := db.QueryRow(
		"SELECT count(*) FROM information_schema.tables "+
			"WHERE table_schema = DATABASE() AND table_name = ?", tabla).Scan(&n)
	return n > 0, err
}

func existeSQLite(db *sql.DB, tabla string) (bool, error) {
	var n int
	err := db.QueryRow(
		"SELECT count(*) FROM sqlite_schema WHERE type='table' AND name = ?", tabla).Scan(&n)
	return n > 0, err
}
