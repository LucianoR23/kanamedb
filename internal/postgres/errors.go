package postgres

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// FailureKind clasifica por qué falló una conexión.
//
// Existe para que la UI pueda decir qué hay que arreglar. "no se pudo conectar"
// obliga al usuario a adivinar entre un host mal escrito, una contraseña vieja y
// un firewall; cada una se arregla en un lugar distinto.
type FailureKind string

const (
	// FailureNetwork es que no se llegó al servidor: host inexistente, puerto
	// cerrado, ruta bloqueada.
	FailureNetwork FailureKind = "network"
	// FailureTimeout es que se llegó pero no contestó a tiempo.
	FailureTimeout FailureKind = "timeout"
	// FailureAuth es que el servidor contestó y rechazó las credenciales.
	FailureAuth FailureKind = "auth"
	// FailureDatabase es que la base indicada no existe.
	FailureDatabase FailureKind = "database"
	// FailurePermission es que el usuario existe pero no puede entrar ahí.
	FailurePermission FailureKind = "permission"
	// FailureTLS es que el canal cifrado no se pudo establecer o verificar.
	FailureTLS FailureKind = "tls"
	// FailureTunnel es que el túnel SSH se cayó. La base puede estar
	// perfectamente: lo que se rompió es el camino hasta ella.
	FailureTunnel FailureKind = "tunnel"
	// FailureCanceled es que el usuario canceló. No es un error: es lo que
	// pidió. La interfaz no tiene que dibujarlo como un fallo, con su cartel
	// rojo y su SQLSTATE, porque eso hace dudar de si además pasó algo malo.
	FailureCanceled FailureKind = "canceled"
	// FailureOther es todo lo demás.
	FailureOther FailureKind = "other"
)

// Failure es un fallo de conexión ya interpretado, listo para mostrar.
type Failure struct {
	Kind FailureKind `json:"kind"`
	// Message es qué pasó, en una línea.
	Message string `json:"message"`
	// Hint es qué hacer al respecto. Vacío si no hay nada útil que decir:
	// inventar una sugerencia es peor que no darla.
	Hint string `json:"hint"`
	// SQLState es el código del motor, cuando lo hubo. Sirve para buscar y para
	// pegar en un ticket.
	SQLState string `json:"sqlState,omitempty"`

	// Detail es el mensaje original, redactado.
	//
	// Message dice qué pasó en castellano; esto dice qué dijo el motor. Los dos
	// hacen falta: el primero para entender, el segundo para buscar en Google o
	// pegar en un ticket. Interpretar y descartar el original deja al usuario
	// sin la única frase que otro va a reconocer.
	Detail string `json:"detail,omitempty"`
}

func (f *Failure) Error() string { return f.Message }

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
			Hint:    "Revisá el modo SSL. Con verify-full el certificado del servidor tiene que ser válido y coincidir con el host.",
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

// dsnCredentials captura las credenciales de cualquier URI con la forma
// esquema://usuario:contraseña@host.
var dsnCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^:/?#\[\]@\s]*):([^@\s]*)@`)

// Redact enmascara las contraseñas de cualquier cadena de conexión que aparezca
// en un texto.
//
// Se aplica a todo texto de origen ajeno —mensajes del servidor, errores de
// librerías— antes de que llegue a un Failure. Los mensajes terminan en toasts,
// logs y tickets, y no se puede asumir que quien los escribió tuvo cuidado.
func Redact(s string) string {
	return dsnCredentials.ReplaceAllString(s, "${1}${2}:***@")
}
