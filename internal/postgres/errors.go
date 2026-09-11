package postgres

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// El vocabulario de fallos vive en `internal/engine`: es el mismo para los
// cuatro motores. Acá quedan alias para que el paquete —y todo lo que ya lo
// llama— siga leyéndose igual. Un alias no es un tipo nuevo, así que
// `postgres.Failure` y `engine.Failure` son literalmente el mismo tipo.
type (
	FailureKind = engine.FailureKind
	Failure     = engine.Failure
)

const (
	FailureNetwork    = engine.FailureNetwork
	FailureTimeout    = engine.FailureTimeout
	FailureAuth       = engine.FailureAuth
	FailureDatabase   = engine.FailureDatabase
	FailurePermission = engine.FailurePermission
	FailureTLS        = engine.FailureTLS
	FailureTunnel     = engine.FailureTunnel
	FailureCanceled   = engine.FailureCanceled
	FailureOther      = engine.FailureOther

	FailureData       = engine.FailureData
	FailureConflict   = engine.FailureConflict
	FailureMissing    = engine.FailureMissing
	FailureDependency = engine.FailureDependency
	FailureLock       = engine.FailureLock
	FailureSyntax     = engine.FailureSyntax
)

// Redact enmascara contraseñas en cualquier texto. Ver engine.Redact.
func Redact(s string) string { return engine.Redact(s) }

// Códigos SQLSTATE de PostgreSQL que sabemos interpretar.
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	sqlStateInvalidPassword             = "28P01"
	sqlStateInvalidAuthorization        = "28000"
	sqlStateUndefinedDatabase           = "3D000"
	sqlStateInsufficientPrivilege       = "42501"
	sqlStateTooManyConnections          = "53300"
	sqlStateCannotConnectNow            = "57P03"
	sqlStateDatabaseDroppedOrRestricted = "55006"
)

// Classify interpreta un error de conexión.
//
// Nunca incluye el DSN ni la contraseña en el mensaje: estos textos terminan en
// toasts, logs y tickets. El `desc` que recibe es la descripción segura de la
// conexión.
func Classify(err error, desc string) *Failure {
	if err == nil {
		return nil
	}
	f := classify(err, desc)
	if f.Detail == "" {
		f.Detail = Redact(err.Error())
	}
	return f
}

func classify(err error, desc string) *Failure {

	// El contexto vencido gana sobre cualquier otra interpretación: si el
	// usuario canceló o se acabó el tiempo, lo demás es ruido.
	if errors.Is(err, context.DeadlineExceeded) {
		return &Failure{
			Kind:    FailureTimeout,
			Message: "El servidor no respondió a tiempo.",
			Hint:    "Puede estar caído, saturado o detrás de un firewall que descarta los paquetes en silencio.",
		}
	}
	if errors.Is(err, context.Canceled) {
		return &Failure{Kind: FailureCanceled, Message: "Cancelada."}
	}

	// El servidor contestó y dijo que no: acá hay un SQLSTATE que interpretar.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return fromSQLState(pgErr, desc)
	}

	// No se llegó al servidor.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return &Failure{
			Kind:    FailureNetwork,
			Message: "No se pudo resolver el nombre del servidor.",
			Hint:    "Revisá el host. Si está en una red privada, puede que necesites la VPN o un túnel SSH.",
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &Failure{
			Kind:    FailureTimeout,
			Message: "Se agotó el tiempo de espera al conectar.",
			Hint:    "El host puede estar inaccesible desde esta red, o un firewall puede estar descartando la conexión.",
		}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return &Failure{
			Kind:    FailureNetwork,
			Message: "No se pudo abrir la conexión con el servidor.",
			Hint:    "Verificá el host y el puerto, y que el servidor acepte conexiones desde esta máquina.",
		}
	}

	// pgx no expone un tipo para los fallos de TLS, así que hay que mirar el
	// texto. Es frágil, y por eso va último: cualquier error tipado gana.
	if texto := strings.ToLower(err.Error()); esTLS(texto) {
		return &Failure{
			Kind:    FailureTLS,
			Message: "No se pudo establecer la conexión cifrada.",
			Hint: "Revisá el modo SSL. Con verify-full el certificado del servidor tiene que ser válido " +
				"y coincidir con el host; con verify-ca, estar firmado por la raíz cargada.",
			// El texto del error dice para qué nombre vale el certificado o
			// quién lo firmó, que es lo que hace falta para arreglarlo.
			Detail: engine.Redact(err.Error()),
		}
	} else if strings.Contains(texto, "connection refused") {
		return &Failure{
			Kind:    FailureNetwork,
			Message: "El servidor rechazó la conexión.",
			Hint:    "Puede que no haya nada escuchando en ese puerto, o que el servidor no acepte conexiones desde esta máquina.",
		}
	}

	// Un fallo sin clasificar igual tiene que decir algo. El texto original va
	// en Detail, que Classify completa para todos los casos.
	return &Failure{
		Kind:    FailureOther,
		Message: "No se pudo conectar con " + desc + ".",
	}
}

func fromSQLState(pgErr *pgconn.PgError, desc string) *Failure {
	f := &Failure{SQLState: pgErr.Code}
	switch pgErr.Code {
	case sqlStateInvalidPassword, sqlStateInvalidAuthorization:
		f.Kind = FailureAuth
		f.Message = "El servidor rechazó las credenciales."
		f.Hint = "Revisá el usuario y la contraseña. Si la contraseña cambió, actualizala en el editor de la conexión."
	case sqlStateUndefinedDatabase:
		f.Kind = FailureDatabase
		f.Message = "La base indicada no existe en ese servidor."
		f.Hint = "Revisá el nombre de la base. Es sensible a mayúsculas."
	case sqlStateInsufficientPrivilege, sqlStateDatabaseDroppedOrRestricted:
		f.Kind = FailurePermission
		f.Message = "El usuario no tiene permiso para entrar a esa base."
		f.Hint = "Las credenciales son válidas, pero le falta el permiso CONNECT sobre la base."
	case sqlStateTooManyConnections:
		f.Kind = FailureOther
		f.Message = "El servidor llegó a su límite de conexiones."
		f.Hint = "Esperá a que se libere alguna, o pedile a quien administre el servidor que suba max_connections."
	case sqlStateCannotConnectNow:
		f.Kind = FailureOther
		f.Message = "El servidor está arrancando y todavía no acepta conexiones."
		f.Hint = "Probá de nuevo en unos segundos."
	default:
		f.Kind = FailureOther
		f.Message = "El servidor rechazó la conexión con " + desc + "."
	}
	// El mensaje del motor tiene información de diagnóstico que vale la pena
	// conservar, pero no se puede confiar en que no cite una cadena de conexión:
	// pasa por Redact antes de mostrarse.
	f.Detail = Redact(pgErr.Message)
	return f
}

func esTLS(texto string) bool {
	for _, s := range []string{
		"tls", "x509", "certificate", "ssl is not enabled", "server does not support ssl",
	} {
		if strings.Contains(texto, s) {
			return true
		}
	}
	return false
}
