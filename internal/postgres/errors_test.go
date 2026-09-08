package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

const desc = "app_rw@dev-db.internal:5432/shop_dev"

func TestClassifyDeNilEsNil(t *testing.T) {
	if got := Classify(nil, desc); got != nil {
		t.Errorf("Classify(nil) = %v, se esperaba nil", got)
	}
}

// La UI necesita saber qué hay que arreglar: un host mal escrito, una
// contraseña vieja y un firewall se arreglan en lugares distintos.
func TestClassifyDistingueLasCausas(t *testing.T) {
	casos := []struct {
		nombre string
		err    error
		quiere FailureKind
	}{
		{"contraseña rechazada", &pgconn.PgError{Code: sqlStateInvalidPassword}, FailureAuth},
		{"autorización inválida", &pgconn.PgError{Code: sqlStateInvalidAuthorization}, FailureAuth},
		{"base inexistente", &pgconn.PgError{Code: sqlStateUndefinedDatabase}, FailureDatabase},
		{"sin permiso", &pgconn.PgError{Code: sqlStateInsufficientPrivilege}, FailurePermission},
		{"demasiadas conexiones", &pgconn.PgError{Code: sqlStateTooManyConnections}, FailureOther},
		{"servidor arrancando", &pgconn.PgError{Code: sqlStateCannotConnectNow}, FailureOther},
		{"dns", &net.DNSError{Err: "no such host", Name: "dev-db.internal"}, FailureNetwork},
		{"timeout de contexto", context.DeadlineExceeded, FailureTimeout},
		{"cancelado", context.Canceled, FailureOther},
		{"tls", errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority"), FailureTLS},
		{"ssl no habilitado", errors.New("server does not support SSL, but SSL was required"), FailureTLS},
		{"conexión rechazada", errors.New("dial tcp 10.0.0.1:5432: connect: connection refused"), FailureNetwork},
		{"desconocido", errors.New("algo raro pasó"), FailureOther},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			got := Classify(tc.err, desc)
			if got == nil {
				t.Fatal("Classify() devolvió nil")
			}
			if got.Kind != tc.quiere {
				t.Errorf("Kind = %q, se esperaba %q (mensaje: %q)", got.Kind, tc.quiere, got.Message)
			}
			if got.Message == "" {
				t.Error("el mensaje está vacío")
			}
		})
	}
}

// Un error tipado siempre gana sobre la heurística de texto, que es frágil.
func TestUnErrorDeSQLGanaSobreLaHeuristicaDeTexto(t *testing.T) {
	// Un PgError cuyo mensaje contiene la palabra "certificate" no es un fallo
	// de TLS: el servidor contestó, y el SQLSTATE manda.
	err := &pgconn.PgError{Code: sqlStateInvalidPassword, Message: "certificate rejected"}
	if got := Classify(err, desc); got.Kind != FailureAuth {
		t.Errorf("Kind = %q, se esperaba auth: el SQLSTATE debe ganar", got.Kind)
	}
}

func TestClassifyConservaElSQLState(t *testing.T) {
	got := Classify(&pgconn.PgError{Code: sqlStateUndefinedDatabase}, desc)
	if got.SQLState != sqlStateUndefinedDatabase {
		t.Errorf("SQLState = %q, se esperaba %q", got.SQLState, sqlStateUndefinedDatabase)
	}
}

// Los mensajes terminan en toasts, logs y tickets: no pueden llevar el DSN ni la
// contraseña adentro.
func TestNingunMensajeFiltraCredenciales(t *testing.T) {
	const pw = "ContraseñaSecreta123"
	dsn := "postgres://app_rw:" + pw + "@dev-db.internal:5432/shop_dev"

	// Errores que citan el DSN entero, que es lo que haría un driver descuidado.
	errores := []error{
		fmt.Errorf("failed to connect to %s", dsn),
		fmt.Errorf("parse %q: invalid port", dsn),
		&pgconn.PgError{Code: "XX000", Message: "connection string " + dsn + " rejected"},
	}
	for i, err := range errores {
		got := Classify(err, desc)
		if strings.Contains(got.Message, pw) {
			t.Errorf("caso %d: el mensaje filtra la contraseña: %q", i, got.Message)
		}
		if strings.Contains(got.Hint, pw) {
			t.Errorf("caso %d: la sugerencia filtra la contraseña: %q", i, got.Hint)
		}
	}
}

// Un Failure sin sugerencia es aceptable. Uno con una sugerencia inventada, no:
// mandar a alguien a revisar lo que no es cuesta más que no decir nada.
func TestLasSugerenciasSonEspecificas(t *testing.T) {
	casos := map[FailureKind][]string{
		FailureAuth:       {"usuario", "contraseña"},
		FailureDatabase:   {"nombre de la base"},
		FailurePermission: {"CONNECT"},
		FailureTLS:        {"SSL"},
	}
	porTipo := map[FailureKind]error{
		FailureAuth:       &pgconn.PgError{Code: sqlStateInvalidPassword},
		FailureDatabase:   &pgconn.PgError{Code: sqlStateUndefinedDatabase},
		FailurePermission: &pgconn.PgError{Code: sqlStateInsufficientPrivilege},
		FailureTLS:        errors.New("tls: handshake failure"),
	}
	for kind, esperadas := range casos {
		got := Classify(porTipo[kind], desc)
		if got.Hint == "" {
			t.Errorf("%s no trae sugerencia", kind)
			continue
		}
		for _, e := range esperadas {
			if !strings.Contains(got.Hint, e) {
				t.Errorf("la sugerencia de %s no menciona %q: %q", kind, e, got.Hint)
			}
		}
	}
}

func TestFailureImplementaError(t *testing.T) {
	var err error = &Failure{Kind: FailureAuth, Message: "rechazado"}
	if err.Error() != "rechazado" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestRedactEnmascaraContrasenasDeCadenasDeConexion(t *testing.T) {
	casos := []struct {
		nombre  string
		entrada string
		noDebe  string
	}{
		{"dsn simple", "postgres://u:p4ssw0rd@host:5432/db", "p4ssw0rd"},
		{"con esquema largo", "postgresql://user:S3cr3t@host/db", "S3cr3t"},
		{"mysql", "mysql://root:toor@127.0.0.1:3306/x", "toor"},
		{"dentro de una frase", `failed to connect to "postgres://u:hunter2@h/db": timeout`, "hunter2"},
		{"contraseña con símbolos", "postgres://u:a%40b%3Ac@host/db", "a%40b%3Ac"},
		{"dos dsn en el mismo texto", "de postgres://a:uno@h/x a postgres://b:dos@h/y", "uno"},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			got := Redact(tc.entrada)
			if strings.Contains(got, tc.noDebe) {
				t.Errorf("Redact(%q) = %q: todavía contiene %q", tc.entrada, got, tc.noDebe)
			}
			if !strings.Contains(got, "***") {
				t.Errorf("Redact(%q) = %q: no marcó la redacción", tc.entrada, got)
			}
		})
	}
}

func TestRedactConservaLoQueNoEsUnaCredencial(t *testing.T) {
	casos := []string{
		"column \"discount\" does not exist",
		"relation orders does not exist at character 15",
		"https://atlasgo.io/docs",
		"conectando a host:5432/db",
		"",
	}
	for _, entrada := range casos {
		if got := Redact(entrada); got != entrada {
			t.Errorf("Redact(%q) = %q: no debería haber cambiado nada", entrada, got)
		}
	}
}

// El usuario no es secreto y sirve para diagnosticar; la contraseña sí lo es.
func TestRedactConservaElUsuario(t *testing.T) {
	got := Redact("postgres://app_rw:secreta@host/db")
	if !strings.Contains(got, "app_rw") {
		t.Errorf("Redact() borró el usuario, que sirve para diagnosticar: %q", got)
	}
	if strings.Contains(got, "secreta") {
		t.Errorf("Redact() dejó la contraseña: %q", got)
	}
}
