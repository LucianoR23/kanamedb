// Package connection define qué es una conexión a una base de datos en Kaname:
// cómo se valida, cómo se arma su DSN y cómo se la nombra sin filtrar secretos.
//
// El paquete es puro: no toca disco, red ni keychain. La contraseña nunca es un
// campo de Connection — vive en el keychain del sistema y se pasa como argumento
// solo en el momento de conectar. Ver CLAUDE.md.
package connection

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Engine es el motor de base de datos.
type Engine string

const (
	Postgres Engine = "postgres"
	MySQL    Engine = "mysql"
	MariaDB  Engine = "mariadb"
	SQLite   Engine = "sqlite"
)

// Supported son los motores que la app puede usar hoy. Los demás están
// definidos porque el modelo los contempla, pero todavía no se conectan.
var Supported = []Engine{Postgres}

// Supports dice si el motor está implementado.
func (e Engine) Supports() bool {
	for _, s := range Supported {
		if s == e {
			return true
		}
	}
	return false
}

// Known dice si el motor existe en el modelo, esté implementado o no.
func (e Engine) Known() bool {
	switch e {
	case Postgres, MySQL, MariaDB, SQLite:
		return true
	}
	return false
}

// DefaultPort es el puerto habitual del motor. Cero para SQLite, que es un
// archivo y no tiene puerto.
func (e Engine) DefaultPort() int {
	switch e {
	case Postgres:
		return 5432
	case MySQL, MariaDB:
		return 3306
	}
	return 0
}

// Environment clasifica contra qué está apuntando la conexión. Decide el color
// del acento, si hace falta confirmación extra para escribir y cuánto ruido
// hace la app antes de tocar datos.
type Environment string

const (
	Local      Environment = "local"
	Dev        Environment = "dev"
	Staging    Environment = "staging"
	Production Environment = "production"
)

// Known dice si el entorno es uno de los cuatro definidos.
func (env Environment) Known() bool {
	switch env {
	case Local, Dev, Staging, Production:
		return true
	}
	return false
}

// NeedsWriteConfirmation dice si escribir contra este entorno exige que el
// usuario tipee el nombre de la base para confirmar.
func (env Environment) NeedsWriteConfirmation() bool {
	return env == Production
}

// SSLMode es el modo TLS de la conexión, con la semántica de libpq.
type SSLMode string

const (
	SSLDisable    SSLMode = "disable"
	SSLAllow      SSLMode = "allow"
	SSLPrefer     SSLMode = "prefer"
	SSLRequire    SSLMode = "require"
	SSLVerifyCA   SSLMode = "verify-ca"
	SSLVerifyFull SSLMode = "verify-full"
)

// Known dice si el modo SSL es uno de los definidos por libpq.
func (s SSLMode) Known() bool {
	switch s {
	case SSLDisable, SSLAllow, SSLPrefer, SSLRequire, SSLVerifyCA, SSLVerifyFull:
		return true
	}
	return false
}

// EffectiveSSLMode es el modo que se va a usar de verdad. Un campo vacío no
// significa "sin TLS" sino `prefer`, que es el default de libpq: cifra si el
// servidor ofrece, y si no, sigue en claro. Sin este método, cualquier chequeo
// sobre SSLMode trata el caso vacío como si fuera seguro.
func (c Connection) EffectiveSSLMode() SSLMode {
	if c.SSLMode == "" {
		return SSLPrefer
	}
	return c.SSLMode
}

// Verifies dice si el modo valida realmente el certificado del servidor.
// `require` cifra pero no verifica nada: no protege contra un intermediario.
func (s SSLMode) Verifies() bool {
	return s == SSLVerifyCA || s == SSLVerifyFull
}

// Connection es la configuración de una conexión, tal como se guarda en disco.
//
// No tiene campo de contraseña, y no debe tenerlo nunca: el archivo de
// conexiones está pensado para sincronizarse entre máquinas.
type Connection struct {
	ID          string      `toml:"id" json:"id"`
	Name        string      `toml:"name" json:"name"`
	Engine      Engine      `toml:"engine" json:"engine"`
	Host        string      `toml:"host" json:"host"`
	Port        int         `toml:"port" json:"port"`
	Database    string      `toml:"database" json:"database"`
	User        string      `toml:"user" json:"user"`
	Environment Environment `toml:"environment" json:"environment"`

	// SSLMode aplica a Postgres, MySQL y MariaDB. SQLite lo ignora.
	SSLMode SSLMode `toml:"ssl_mode" json:"sslMode"`

	Safety Safety `toml:"safety" json:"safety"`
}

// Safety son las protecciones por conexión.
//
// Los nombres están elegidos para que el VALOR CERO SEA EL LADO SEGURO, y no
// para que suenen bien. El archivo se edita a mano: si alguien borra una línea,
// o si una versión futura lee un archivo escrito por una anterior, la clave
// ausente decodifica como cero. Con `RequirePreview` el cero sería `false` y la
// protección quedaría apagada sin que nadie lo pidiera; con
// `AllowApplyWithoutPreview` el cero es `false` y la protección queda puesta.
//
// La conexión más vieja es la que tiene más chances de que le falte una clave
// nueva, y suele ser la más importante.
type Safety struct {
	// ReadOnly bloquea toda escritura desde la app, sin importar los permisos
	// que tenga el usuario en el motor. Cero: la conexión permite escribir, que
	// es lo que el usuario espera al crear una conexión común.
	ReadOnly bool `toml:"read_only" json:"readOnly"`

	// AllowApplyWithoutPreview deja aplicar cambios sin abrir el preview de SQL.
	// Cero: el changeset siempre se abre para revisar.
	AllowApplyWithoutPreview bool `toml:"allow_apply_without_preview" json:"allowApplyWithoutPreview"`

	// AllowWriteWithoutConfirmation saltea la confirmación por nombre de base.
	// Cero: hay que tipear el nombre. En producción se ignora: ver
	// RequiresWriteConfirmation.
	AllowWriteWithoutConfirmation bool `toml:"allow_write_without_confirmation" json:"allowWriteWithoutConfirmation"`

	// BlockDropTruncate hace que aplicar se niegue a ejecutar DROP y TRUNCATE.
	// Las sentencias igual se generan y se muestran; lo que no se hace es
	// correrlas. Cero: no se bloquean, que es el default del diseño.
	BlockDropTruncate bool `toml:"block_drop_truncate" json:"blockDropTruncate"`

	// StatementTimeoutSeconds corta una consulta que se cuelga.
	// Cero significa "usar el default", no "sin límite": un archivo al que le
	// falta la clave tiene que quedar protegido, no desprotegido. Para sacar el
	// límite hay que pedirlo con -1.
	StatementTimeoutSeconds int `toml:"statement_timeout_seconds" json:"statementTimeoutSeconds"`

	// RowLimit es cuántas filas trae una consulta antes de "cargar más".
	// Cero es el default; -1 es sin límite.
	RowLimit int `toml:"row_limit" json:"rowLimit"`

	// IdleDisconnectMinutes cierra la conexión tras ese tiempo sin actividad.
	// Cero es el default; -1 es nunca desconectar.
	IdleDisconnectMinutes int `toml:"idle_disconnect_minutes" json:"idleDisconnectMinutes"`
}

// Defaults de las protecciones, tomados de S03.
const (
	DefaultStatementTimeoutSeconds = 30
	DefaultRowLimit                = 1000
	DefaultIdleDisconnectMinutes   = 15
)

// Unlimited es el valor que hay que poner explícitamente para sacar un límite.
// No es cero justamente para que una clave ausente no lo saque sin querer.
const Unlimited = -1

// RequiresPreview dice si el changeset tiene que abrirse antes de aplicar.
func (s Safety) RequiresPreview() bool { return !s.AllowApplyWithoutPreview }

// RequiresWriteConfirmation dice si hay que tipear el nombre de la base antes
// de escribir.
//
// En producción es true siempre, sin importar la configuración: es la
// protección que no se puede apagar.
func (c Connection) RequiresWriteConfirmation() bool {
	if c.Environment.NeedsWriteConfirmation() {
		return true
	}
	return !c.Safety.AllowWriteWithoutConfirmation
}

// StatementTimeout devuelve el timeout efectivo. Cero significa sin límite.
func (s Safety) StatementTimeout() time.Duration {
	switch {
	case s.StatementTimeoutSeconds == Unlimited:
		return 0
	case s.StatementTimeoutSeconds <= 0:
		return DefaultStatementTimeoutSeconds * time.Second
	default:
		return time.Duration(s.StatementTimeoutSeconds) * time.Second
	}
}

// EffectiveRowLimit devuelve el límite efectivo. Cero significa sin límite.
func (s Safety) EffectiveRowLimit() int {
	switch {
	case s.RowLimit == Unlimited:
		return 0
	case s.RowLimit <= 0:
		return DefaultRowLimit
	default:
		return s.RowLimit
	}
}

// IdleDisconnect devuelve el tiempo de inactividad efectivo. Cero es nunca.
func (s Safety) IdleDisconnect() time.Duration {
	switch {
	case s.IdleDisconnectMinutes == Unlimited:
		return 0
	case s.IdleDisconnectMinutes <= 0:
		return DefaultIdleDisconnectMinutes * time.Minute
	default:
		return time.Duration(s.IdleDisconnectMinutes) * time.Minute
	}
}

// NewID genera un identificador aleatorio para una conexión nueva.
//
// Es aleatorio y no derivado del nombre a propósito: el nombre cambia, y el ID
// es la clave con la que la contraseña quedó guardada en el keychain.
func NewID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generar id de conexión: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Describe devuelve una descripción legible y segura para logs y mensajes de
// error: usuario, host, puerto y base, sin nada secreto.
func (c Connection) Describe() string {
	if c.Engine == SQLite {
		return c.Database
	}
	host := c.Host
	if c.Port != 0 {
		host = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	}
	if c.User == "" {
		return host + "/" + c.Database
	}
	return c.User + "@" + host + "/" + c.Database
}

// String hace que una Connection sea segura de imprimir con %v o %s. Sin esto,
// cualquier log accidental de la estructura completa sería legible; con esto,
// lo peor que puede pasar es que se filtre un host.
func (c Connection) String() string {
	return c.Describe()
}

// DSN arma la cadena de conexión para el motor, con la contraseña incrustada.
//
// El resultado ES UN SECRETO: no se loguea, no se muestra en la UI y no se
// guarda. Se construye en el momento de conectar y se descarta.
func (c Connection) DSN(password string) (string, error) {
	if c.Engine != Postgres {
		return "", fmt.Errorf("motor %q todavía no está implementado", c.Engine)
	}
	// El DSN es la última puerta antes de la red: se normaliza y se valida acá
	// aunque quien llama ya debería haberlo hecho. El archivo de conexiones se
	// edita a mano y llega hasta acá sin garantías.
	//
	// Importa especialmente que Database no esté vacío: sin ese parámetro
	// PostgreSQL usa el nombre del usuario como base, y la app se conectaría en
	// silencio a una base distinta de la que el usuario cree.
	c = c.Normalize()
	if err := c.ValidateForConnect(); err != nil {
		return "", fmt.Errorf("no se puede armar la conexión: %w", err)
	}

	port := c.Port
	if port == 0 {
		port = c.Engine.DefaultPort()
	}
	sslMode := c.EffectiveSSLMode()

	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(port)),
		Path:   "/" + c.Database,
	}
	// url.UserPassword escapa usuario y contraseña. Sin eso, una contraseña con
	// `@` o `/` rompería el DSN o, peor, lo redirigiría a otro host.
	u.User = url.UserPassword(c.User, password)
	u.RawQuery = url.Values{
		"sslmode": {string(sslMode)},
		// Identifica la app en pg_stat_activity, para que un DBA sepa de dónde
		// vino una query. No lleva ningún dato del usuario.
		"application_name": {"kaname"},
	}.Encode()

	return u.String(), nil
}

// Normalize completa lo que se pueda deducir y limpia espacios. Se llama antes
// de validar y antes de guardar, para que el archivo en disco quede prolijo.
func (c Connection) Normalize() Connection {
	c.Name = strings.TrimSpace(c.Name)
	c.Host = strings.TrimSpace(c.Host)
	c.Database = strings.TrimSpace(c.Database)
	c.User = strings.TrimSpace(c.User)

	// Los enums también se limpian porque el archivo se edita a mano. Sin esto,
	// `engine = " Postgres"` da "Motor desconocido" y el motivo —un espacio de
	// más, o una mayúscula— queda invisible en el mensaje.
	c.Engine = Engine(strings.ToLower(strings.TrimSpace(string(c.Engine))))
	c.Environment = Environment(strings.ToLower(strings.TrimSpace(string(c.Environment))))
	c.SSLMode = SSLMode(strings.ToLower(strings.TrimSpace(string(c.SSLMode))))

	if c.Port == 0 {
		c.Port = c.Engine.DefaultPort()
	}
	if c.Environment == "" {
		// El default más seguro: si no se dijo nada, no es producción.
		c.Environment = Local
	}
	if c.SSLMode == "" && c.Engine != SQLite {
		c.SSLMode = SSLPrefer
	}
	return c
}
