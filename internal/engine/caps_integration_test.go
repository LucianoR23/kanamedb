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

// TestDDLAtomicoEsCierto comprueba la otra mitad de la historia.
//
// AtomicDDL y TransactionalDDL son cosas distintas, y confundirlas sería
// injusto con MySQL y MariaDB. Las dos hacen commit implícito antes de cada DDL
// —así que no hay «todo o nada» sobre un conjunto— pero cada sentencia POR
// SEPARADO sí es todo o nada desde MySQL 8.0 y MariaDB 10.6.
//
// Importa porque cambia lo que S15 dice al fallar. «Falló a la mitad» es cierto
// para el conjunto; decir además que la sentencia que falló quedó a medias
// sería falso, y asustaría de más justo cuando alguien necesita pensar claro.
//
// Se comprueba con un ALTER que agrega DOS columnas y falla en la segunda: si
// el DDL es atómico, la primera no puede haber quedado.
func TestDDLAtomicoEsCierto(t *testing.T) {
	casos := []struct {
		kind    engine.Kind
		abrir   func(t *testing.T) *sql.DB
		columna func(db *sql.DB, tabla, col string) (bool, error)
	}{
		{engine.Postgres, abrirPostgres, columnaPostgres},
		{engine.MySQL, abrirDSN("mysql", dsnMySQL), columnaMySQL},
		{engine.MariaDB, abrirDSN("mysql", dsnMariaDB), columnaMySQL},
		{engine.SQLite, abrirSQLite, columnaSQLite},
	}

	for _, c := range casos {
		t.Run(c.kind.String(), func(t *testing.T) {
			db := c.abrir(t)
			tabla := "kn_ddl_atom"
			if _, err := db.Exec("DROP TABLE IF EXISTS " + tabla); err != nil {
				t.Fatalf("limpiar: %v", err)
			}
			t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + tabla) })
			if _, err := db.Exec("CREATE TABLE " + tabla + " (id integer)"); err != nil {
				t.Fatalf("CREATE: %v", err)
			}
			// Con una fila adentro. Sin ella el caso de SQLite no prueba nada:
			// un ADD COLUMN NOT NULL sobre una tabla vacía es perfectamente
			// válido, así que el ALTER no fallaba y el test daba verde sin
			// haber comprobado nada.
			if _, err := db.Exec("INSERT INTO " + tabla + " (id) VALUES (1)"); err != nil {
				t.Fatalf("INSERT: %v", err)
			}

			// La segunda columna choca con la que ya existe, así que el ALTER
			// entero tiene que fallar. SQLite no acepta dos ADD COLUMN en una
			// sentencia; ahí se prueba lo mismo de la única forma que existe,
			// con una sentencia sola que falla por las filas que ya están.
			sql := "ALTER TABLE " + tabla + " ADD COLUMN nueva integer, ADD COLUMN id integer"
			if c.kind == engine.SQLite {
				sql = "ALTER TABLE " + tabla + " ADD COLUMN nueva integer NOT NULL"
			}
			if _, err := db.Exec(sql); err == nil {
				t.Fatalf("el ALTER no falló, así que el caso no prueba nada: %s", sql)
			}

			quedo, err := c.columna(db, tabla, "nueva")
			if err != nil {
				t.Fatalf("comprobar la columna: %v", err)
			}
			if engine.CapsOf(c.kind).AtomicDDL && quedo {
				t.Errorf("Caps dice que %s tiene DDL atómico, pero un ALTER que falló "+
					"dejó puesta la primera columna. S15 estaría diciendo que la "+
					"sentencia fallida no dejó nada, y sí dejó.", c.kind.Label())
			}
			if !engine.CapsOf(c.kind).AtomicDDL && !quedo {
				t.Errorf("Caps dice que %s NO tiene DDL atómico, pero el ALTER "+
					"fallido no dejó nada. El motor mejoró y la tabla quedó vieja.",
					c.kind.Label())
			}
		})
	}
}

// TestElDDLEnElMedioCommiteaLoAnterior es el test más importante de este
// archivo, porque documenta con código la trampa que obliga a partir el apply
// en tramos.
//
// En MySQL y MariaDB un DDL en el medio de una transacción no solo no se
// revierte: hace **commit implícito de todo lo anterior** y deja la conexión
// fuera de la transacción, así que lo que venga después también se commitea
// solo. Un ROLLBACK al final no revierte absolutamente nada.
//
// Es peor que «no hay DDL transaccional», porque la interfaz habría prometido
// «todo o nada» y la base habría aplicado todo. Es exactamente el escenario de
// la Iteración 7, donde el changeset mezcla datos y esquema en un solo apply.
//
// Si algún día un motor arregla esto, este test se pone rojo y hay que revisar
// TramosDe: estaría partiendo de más.
func TestElDDLEnElMedioCommiteaLoAnterior(t *testing.T) {
	casos := []struct {
		kind  engine.Kind
		abrir func(t *testing.T) *sql.DB
	}{
		{engine.Postgres, abrirPostgres},
		{engine.MySQL, abrirDSN("mysql", dsnMySQL)},
		{engine.MariaDB, abrirDSN("mysql", dsnMariaDB)},
		{engine.SQLite, abrirSQLite},
	}

	for _, c := range casos {
		t.Run(c.kind.String(), func(t *testing.T) {
			db := c.abrir(t)
			tabla := "kn_mezcla"
			t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + tabla) })

			for _, q := range []string{
				"DROP TABLE IF EXISTS " + tabla,
				"CREATE TABLE " + tabla + " (id integer primary key, n varchar(20))",
				"INSERT INTO " + tabla + " VALUES (1,'ana'), (2,'beto')",
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatalf("preparar %q: %v", q, err)
				}
			}

			tx, err := db.Begin()
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			// Un dato, después estructura, después otro dato. El orden es el
			// que importa: lo que se prueba es qué le pasa al PRIMERO.
			for _, q := range []string{
				"UPDATE " + tabla + " SET n='PRIMERO' WHERE id=1",
				"ALTER TABLE " + tabla + " ADD COLUMN extra integer",
				"UPDATE " + tabla + " SET n='SEGUNDO' WHERE id=2",
			} {
				if _, err := tx.Exec(q); err != nil {
					t.Fatalf("dentro de la transacción, %q: %v", q, err)
				}
			}
			_ = tx.Rollback()

			var uno string
			if err := db.QueryRow("SELECT n FROM " + tabla + " WHERE id=1").Scan(&uno); err != nil {
				t.Fatalf("leer: %v", err)
			}
			revirtio := uno == "ana"

			if engine.CapsOf(c.kind).TransactionalDDL && !revirtio {
				t.Errorf("%s dice tener DDL transaccional, pero el UPDATE anterior al "+
					"ALTER quedó commiteado (id1=%q). La casilla «Una sola "+
					"transacción» estaría mintiendo.", c.kind.Label(), uno)
			}
			if !engine.CapsOf(c.kind).TransactionalDDL && revirtio {
				t.Errorf("%s dice NO tener DDL transaccional, pero el ROLLBACK sí "+
					"revirtió el UPDATE anterior al ALTER. El motor mejoró: hay que "+
					"revisar TramosDe, que estaría partiendo el apply de más.",
					c.kind.Label())
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

func columnaPostgres(db *sql.DB, tabla, col string) (bool, error) {
	var n int
	err := db.QueryRow(
		"SELECT count(*) FROM information_schema.columns "+
			"WHERE table_name = $1 AND column_name = $2", tabla, col).Scan(&n)
	return n > 0, err
}

func columnaMySQL(db *sql.DB, tabla, col string) (bool, error) {
	var n int
	err := db.QueryRow(
		"SELECT count(*) FROM information_schema.columns "+
			"WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		tabla, col).Scan(&n)
	return n > 0, err
}

func columnaSQLite(db *sql.DB, tabla, col string) (bool, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", tabla)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return false, err
		}
		if n == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

func existeSQLite(db *sql.DB, tabla string) (bool, error) {
	var n int
	err := db.QueryRow(
		"SELECT count(*) FROM sqlite_schema WHERE type='table' AND name = ?", tabla).Scan(&n)
	return n > 0, err
}
