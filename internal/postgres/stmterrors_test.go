package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestClassifyStatementConErroresDeVerdad clasifica errores que produjo el
// servidor, no errores fabricados a mano.
//
// La diferencia importa: la mitad de estos mensajes se arman leyendo campos que
// PostgreSQL llena solo en algunos códigos —ColumnName está en el 23502 y no en
// el 42701, ConstraintName está en el 23505 y no en el 42P07—. Un test con
// pgconn.PgError construidos a mano probaría lo que yo creo que manda el
// servidor, que es exactamente la parte que no sé.
func TestClassifyStatementConErroresDeVerdad(t *testing.T) {
	pool, esq := conectar(t)
	ctx := context.Background()

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.con_nulos (id int PRIMARY KEY, apodo text);
		INSERT INTO %[1]s.con_nulos VALUES (1, 'ana'), (2, NULL);

		CREATE TABLE %[1]s.con_repetidos (id int PRIMARY KEY, codigo text NOT NULL);
		INSERT INTO %[1]s.con_repetidos VALUES (1, 'AAA'), (2, 'AAA');

		CREATE TABLE %[1]s.padre (id int PRIMARY KEY, nombre text);
		INSERT INTO %[1]s.padre VALUES (1, 'uno');

		CREATE TABLE %[1]s.huerfanos (id int PRIMARY KEY, padre_id int NOT NULL);
		INSERT INTO %[1]s.huerfanos VALUES (1, 1), (2, 999999);
	`, esq))

	casos := []struct {
		nombre string
		sql    string
		kind   FailureKind
		estado string
		// enMensaje son los pedazos que el mensaje TIENE que decir para que
		// sirva: sin ellos hay que ir a buscar a mano cuál fila falla.
		enMensaje []string
		enHint    []string
	}{
		{
			nombre:    "nulos al exigir not null",
			sql:       fmt.Sprintf("ALTER TABLE %s.con_nulos ALTER COLUMN apodo SET NOT NULL", esq),
			kind:      FailureData,
			estado:    "23502",
			enMensaje: []string{"apodo", "con_nulos", "NULL"},
			enHint:    []string{"IS NULL"},
		},
		{
			nombre:    "repetidos al crear un único",
			sql:       fmt.Sprintf("CREATE UNIQUE INDEX uq_cod ON %s.con_repetidos (codigo)", esq),
			kind:      FailureData,
			estado:    "23505",
			enMensaje: []string{"codigo", "AAA"},
			enHint:    []string{"count(*)", "con_repetidos"},
		},
		{
			nombre: "huérfanos al crear la clave foránea",
			sql: fmt.Sprintf(
				"ALTER TABLE %[1]s.huerfanos ADD CONSTRAINT fk FOREIGN KEY (padre_id) "+
					"REFERENCES %[1]s.padre (id)", esq),
			kind:      FailureData,
			estado:    "23503",
			enMensaje: []string{"huerfanos", "padre_id", "999999"},
			enHint:    []string{"NOT VALID"},
		},
		{
			nombre:    "filas que no cumplen el check",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ADD CONSTRAINT c CHECK (id > 100)", esq),
			kind:      FailureData,
			estado:    "23514",
			enMensaje: []string{"padre", "«c»"},
		},
		{
			nombre:    "la columna ya existe",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ADD COLUMN nombre text", esq),
			kind:      FailureConflict,
			estado:    "42701",
			enMensaje: []string{"nombre", "ya existe"},
		},
		{
			nombre:    "el nombre de la restricción ya existe",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ADD CONSTRAINT padre_pkey CHECK (id > 0)", esq),
			kind:      FailureConflict,
			estado:    "42710",
			enMensaje: []string{"padre_pkey"},
		},
		{
			nombre:    "el objeto ya existe",
			sql:       fmt.Sprintf("CREATE TABLE %s.padre (id int)", esq),
			kind:      FailureConflict,
			estado:    "42P07",
			enMensaje: []string{"padre"},
		},
		{
			nombre:    "la tabla no existe",
			sql:       fmt.Sprintf("ALTER TABLE %s.nohay ADD COLUMN x text", esq),
			kind:      FailureMissing,
			estado:    "42P01",
			enMensaje: []string{"no existe"},
		},
		{
			nombre:    "la columna no existe",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre DROP COLUMN nohay", esq),
			kind:      FailureMissing,
			estado:    "42703",
			enMensaje: []string{"nohay", "no existe"},
		},
		{
			nombre:    "el tipo no existe",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ADD COLUMN x noexiste", esq),
			kind:      FailureMissing,
			estado:    "42704",
			enMensaje: []string{"noexiste"},
			enHint:    []string{"esquema"},
		},
		{
			nombre: "la clave apunta a columnas que no son únicas",
			sql: fmt.Sprintf(
				"ALTER TABLE %[1]s.huerfanos ADD CONSTRAINT fk2 FOREIGN KEY (padre_id) "+
					"REFERENCES %[1]s.padre (nombre)", esq),
			kind:   FailureConflict,
			estado: "42830",
			enHint: []string{"única"},
		},
		{
			nombre:    "hay objetos que dependen",
			sql:       fmt.Sprintf("DROP TABLE %s.con_nulos", esq),
			kind:      FailureDependency,
			estado:    "2BP01",
			enMensaje: []string{"dependen"},
		},
		{
			nombre:    "el default no es del tipo",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ADD COLUMN n integer DEFAULT 'hola'", esq),
			kind:      FailureData,
			estado:    "22P02",
			enMensaje: []string{"no es válido"},
		},
		{
			nombre:    "el default nombra otra columna",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ADD COLUMN d bigint DEFAULT id", esq),
			kind:      FailureSyntax,
			estado:    "0A000",
			enMensaje: []string{"no puede nombrar otra columna"},
			enHint:    []string{"GENERATED ALWAYS AS"},
		},
		{
			nombre:    "el default es una consulta",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ADD COLUMN d2 bigint DEFAULT (SELECT 1)", esq),
			kind:      FailureSyntax,
			estado:    "0A000",
			enMensaje: []string{"no puede ser una consulta"},
		},
		{
			nombre:    "una vista usa la columna",
			sql:       fmt.Sprintf("ALTER TABLE %s.con_nulos ALTER COLUMN apodo TYPE varchar(4)", esq),
			kind:      FailureDependency,
			estado:    "0A000",
			enMensaje: []string{"vista"},
			enHint:    []string{"volver a crearla"},
		},
		{
			nombre:    "no sabe convertir al tipo nuevo",
			sql:       fmt.Sprintf("ALTER TABLE %s.padre ALTER COLUMN nombre TYPE integer", esq),
			kind:      FailureData,
			estado:    "42804",
			enMensaje: []string{"convertir"},
			enHint:    []string{"USING"},
		},
	}

	// El caso de dependencias necesita que algo dependa de con_nulos.
	ejecutar(t, pool, fmt.Sprintf(
		"CREATE VIEW %[1]s.v AS SELECT * FROM %[1]s.con_nulos", esq))

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := pool.Exec(ctx, c.sql)
			if err == nil {
				t.Fatalf("la sentencia no falló, así que el caso no prueba nada: %s", c.sql)
			}

			f := ClassifyStatement(err, "usuario@host:5432/base")
			if f.Kind != c.kind {
				t.Errorf("Kind = %q, se esperaba %q", f.Kind, c.kind)
			}
			if f.SQLState != c.estado {
				t.Errorf("SQLState = %q, se esperaba %q", f.SQLState, c.estado)
			}
			for _, quiere := range c.enMensaje {
				if !strings.Contains(f.Message, quiere) {
					t.Errorf("el mensaje no dice %q: %s", quiere, f.Message)
				}
			}
			for _, quiere := range c.enHint {
				if !strings.Contains(f.Hint, quiere) {
					t.Errorf("el hint no dice %q: %s", quiere, f.Hint)
				}
			}
			// El original siempre viaja: es la frase que otro reconoce.
			if f.Detail == "" {
				t.Error("Detail quedó vacío: se pierde lo que dijo el motor")
			}
			// Y nunca, jamás, el vocabulario de conexión. Es el bug que este
			// archivo existe para que no vuelva.
			for _, prohibido := range []string{"conectar", "conexión", "usuario@host"} {
				if strings.Contains(f.Message, prohibido) {
					t.Errorf("el mensaje habla de la conexión y esto es una sentencia: %s", f.Message)
				}
			}
		})
	}
}

// TestClassifyStatementNoSeQuedaSinNadaQueDecir comprueba que un SQLSTATE que
// nadie enumeró igual sale con un mensaje y sin hablar de conexiones.
//
// Es el caso que más va a pasar con el tiempo: PostgreSQL tiene cientos de
// códigos y esta lista tiene treinta.
func TestClassifyStatementNoSeQuedaSinNadaQueDecir(t *testing.T) {
	casos := []struct {
		codigo string
		kind   FailureKind
	}{
		{"23P01", FailureData},   // exclusion_violation
		{"22003", FailureData},   // numeric_value_out_of_range
		{"42P17", FailureSyntax}, // invalid_object_definition
		{"53100", FailureOther},  // disk_full
		{"55000", FailureLock},   // object_not_in_prerequisite_state
		{"40001", FailureOther},  // serialization_failure
		{"XX000", FailureOther},  // internal_error
		{"ZZ999", FailureOther},  // no existe: el default del default
	}
	for _, c := range casos {
		t.Run(c.codigo, func(t *testing.T) {
			err := error(&pgconn.PgError{Code: c.codigo, Message: "algo pasó"})
			f := ClassifyStatement(fmt.Errorf("al aplicar: %w", err), "usuario@host:5432/base")
			if f.Kind != c.kind {
				t.Errorf("Kind = %q, se esperaba %q", f.Kind, c.kind)
			}
			if f.Message == "" {
				t.Error("se quedó sin mensaje")
			}
			if f.SQLState != c.codigo {
				t.Errorf("SQLState = %q, se esperaba %q", f.SQLState, c.codigo)
			}
			if strings.Contains(f.Message, "conect") {
				t.Errorf("habla de conectarse y esto es una sentencia: %s", f.Message)
			}
		})
	}
}

// TestClassifyStatementSinSQLStateCaeEnConexion comprueba que un error que
// nunca llegó al servidor sigue interpretándose como lo que es.
func TestClassifyStatementSinSQLStateCaeEnConexion(t *testing.T) {
	f := ClassifyStatement(errors.New("dial tcp: connection refused"), "usuario@host:5432/base")
	if f.Kind != FailureNetwork {
		t.Errorf("Kind = %q, se esperaba %q", f.Kind, FailureNetwork)
	}
}
