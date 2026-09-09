// Package connection define qué es una conexión a una base de datos en Kaname:
// cómo se valida, cómo se arma su DSN y cómo se la nombra sin filtrar secretos.
//
// El paquete no toca disco, red ni keychain: se puede probar entero sin montar
// nada. La contraseña nunca es un campo de Connection — vive en el keychain del
// sistema y se pasa como argumento solo en el momento de conectar. Ver CLAUDE.md.
//
// Importa internal/tunnel por su tipo de configuración, que es igual de puro.
// Se prefiere el tipo compartido antes que una copia de sus campos: dos
// definiciones del mismo dato se separan, y separarse acá significaría que la
// conexión guarda un campo que el túnel no lee.
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

	"github.com/LucianoR23/kanamedb/internal/tunnel"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Engine es el motor de base de datos.
//
// Es un alias de engine.Kind y no un tipo propio: eran dos enumeraciones con
// exactamente los mismos cuatro valores, y dos enumeraciones iguales terminan
// con una función de conversión en el medio que algún día se olvida un caso.
type Engine = engine.Kind

const (
	Postgres = engine.Postgres
	MySQL    = engine.MySQL
	MariaDB  = engine.MariaDB
	SQLite   = engine.SQLite
)

// Supported son los motores que la app puede usar hoy.
var Supported = []Engine{Postgres, MySQL, MariaDB, SQLite}

// Supports dice si el motor está implementado.
//
// Es función y no método porque Engine es un alias de engine.Kind, y Go no deja
// colgarle métodos a un tipo de otro paquete. La alternativa era tener dos
// enumeraciones iguales, que es peor.
func Supports(e Engine) bool {
	for _, s := range Supported {
		if s == e {
			return true
		}
	}
	return false
}

// Known dice si el motor existe en el modelo, esté implementado o no.
func Known(e Engine) bool {
	switch e {
	case Postgres, MySQL, MariaDB, SQLite:
		return true
	}
	return false
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

	// SSH es el salto por un bastión. Apagado, el resto de sus campos queda
	// como configuración muerta pero legible.
	//
	// Se usa el tipo de internal/tunnel y no una copia de sus campos para que
	// no haya dos definiciones que mantener en sincronía. El paquete de
	// configuración de tunnel no importa nada de criptografía —eso vive en los
	// otros archivos— así que el modelo no arrastra peso por esto.
	SSH tunnel.Config `toml:"ssh,omitempty" json:"ssh"`
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

	switch c.Engine {
	case Postgres:
		return c.dsnPostgres(password), nil
	case MySQL, MariaDB:
		return c.dsnMySQL(password), nil
	case SQLite:
		return c.dsnSQLite(), nil
	}
	return "", fmt.Errorf("motor %q desconocido", c.Engine)
}

func (c Connection) dsnPostgres(password string) string {
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(c.puerto())),
		Path:   "/" + c.Database,
	}
	// url.UserPassword escapa usuario y contraseña. Sin eso, una contraseña con
	// `@` o `/` rompería el DSN o, peor, lo redirigiría a otro host.
	u.User = url.UserPassword(c.User, password)
	u.RawQuery = url.Values{
		"sslmode": {string(c.EffectiveSSLMode())},
		// Identifica la app en pg_stat_activity, para que un DBA sepa de dónde
		// vino una query. No lleva ningún dato del usuario.
		"application_name": {"kaname"},
	}.Encode()
	return u.String()
}

// dsnMySQL arma el DSN con el formato propio de go-sql-driver/mysql, que no es
// una URI: `usuario:contraseña@tcp(host:puerto)/base?params`.
//
// Usuario y contraseña van SIN escapar de URL porque este formato no lo usa;
// escaparlos rompería cualquier contraseña con un `%`. Lo que sí importa es que
// una contraseña con `@` funcione, y funciona: el driver parte por el ÚLTIMO
// `@`, que es el que separa las credenciales del host.
func (c Connection) dsnMySQL(password string) string {
	q := url.Values{
		// Sin esto, DATE y DATETIME llegan como []byte y la grilla mostraría
		// bytes crudos en vez de fechas.
		"parseTime": {"true"},
		"loc":       {"UTC"},
		// Una sentencia por viaje. Con varias, el driver no puede asociar cada
		// error a su sentencia, que es justo lo que la pantalla de apply
		// necesita para decir cuál falló.
		"multiStatements": {"false"},
		"tls":             {tlsDeMySQL(c.EffectiveSSLMode())},
	}
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?%s",
		c.User, password,
		net.JoinHostPort(c.Host, strconv.Itoa(c.puerto())),
		c.Database, q.Encode())
}

// tlsDeMySQL traduce el modo SSL al vocabulario del driver de MySQL.
//
// No son los mismos nombres que en Postgres y la diferencia no es cosmética:
// `require` en Postgres significa «cifrado sí, certificado no me importa», y en
// el driver de MySQL eso se llama `skip-verify`. Traducirlo mal a `true` haría
// fallar conexiones que el usuario pidió explícitamente que no verificaran.
func tlsDeMySQL(m SSLMode) string {
	switch m {
	case SSLDisable:
		return "false"
	case SSLAllow, SSLPrefer:
		return "preferred"
	case SSLRequire:
		return "skip-verify"
	case SSLVerifyCA, SSLVerifyFull:
		return "true"
	}
	return "preferred"
}

// dsnSQLite arma el DSN de un archivo. No hay host, puerto ni usuario: el
// permiso lo da el sistema de archivos.
func (c Connection) dsnSQLite() string {
	q := url.Values{
		// Las claves foráneas están APAGADAS por defecto en SQLite, y hay que
		// encenderlas por conexión. Sin esto, el diagrama dibujaría relaciones
		// que la base no hace cumplir: se podría borrar un padre con hijos y
		// nadie diría nada.
		"_pragma": {
			"foreign_keys(1)",
			// Espera en vez de fallar al toque si otro proceso está
			// escribiendo. Un gestor de escritorio abre archivos que otras
			// aplicaciones están usando.
			"busy_timeout(5000)",
		},
	}
	return "file:" + rutaParaURI(c.Database) + "?" + q.Encode()
}

// rutaParaURI escapa lo que el analizador de URI de SQLite trata como especial.
//
// Hace falta porque el DSN es un URI de verdad —`file:` con parámetros— y
// SQLite lo parsea como tal: la ruta TERMINA en el primer `?` o `#`, y las
// secuencias `%HH` se decodifican. Una ruta escrita tal cual no es una ruta:
// es un URI que casualmente se le parece.
//
// Lo comprobado, y por qué esto no es cosmética: con una base en
// `…/notas#1/app.db`, SQLite corta en el `#`, se queda con `…/notas`, y como
// el driver abre con SQLITE_OPEN_CREATE **crea un archivo vacío ahí y lo
// abre**. No hay error: Kaname muestra una base vacía mientras la del usuario
// sigue intacta en otro lado. Con `%20` en el nombre, directamente no abre.
//
// Se escapan exactamente tres caracteres y no se usa url.PathEscape, que
// escaparía también las barras y los dos puntos de `C:/` y rompería toda ruta
// de Windows. El `%` va primero: al revés, se escaparían los `%` recién
// puestos.
func rutaParaURI(ruta string) string {
	r := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")
	return r.Replace(ruta)
}

func (c Connection) puerto() int {
	if c.Port != 0 {
		return c.Port
	}
	return c.Engine.DefaultPort()
}

// Normalize completa lo que se pueda deducir y limpia espacios. Se llama antes
// de validar y antes de guardar, para que el archivo en disco quede prolijo.
func (c Connection) Normalize() Connection {
	c.Name = strings.TrimSpace(c.Name)
	c.Host = strings.TrimSpace(c.Host)
	c.Database = strings.TrimSpace(c.Database)
	c.User = strings.TrimSpace(c.User)

	c.SSH = c.SSH.Normalize()

	// Los enums también se limpian porque el archivo se edita a mano. Sin esto,
	// `engine = " Postgres"` da "Motor desconocido" y el motivo —un espacio de
	// más, o una mayúscula— queda invisible en el mensaje.
	c.Engine = Engine(strings.ToLower(strings.TrimSpace(string(c.Engine))))
	// SQLite no tiene host ni usuario: dejarlos escritos sería configuración
	// que no hace nada y que confunde a quien lea el archivo.
	if c.Engine == SQLite {
		c.Host, c.User, c.Port, c.SSLMode = "", "", 0, ""
	}
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

// Los métodos de autenticación SSH, re-exportados.
//
// Existen para que quien ya usa este paquete no tenga que importar además el
// del túnel solo para nombrar una constante. Son los mismos valores.
const (
	SSHAuthAgent    = tunnel.AuthAgent
	SSHAuthKeyFile  = tunnel.AuthKeyFile
	SSHAuthPassword = tunnel.AuthPassword
)
