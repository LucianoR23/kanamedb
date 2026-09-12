package engine

import "regexp"

// FailureKind clasifica por qué falló algo.
//
// Vive acá y no en `internal/postgres` porque es el vocabulario de los cuatro
// motores. Que un error de MySQL se tipara como `postgres.Failure` sería
// absurdo, y la interfaz —que recibe esto por los bindings— no tiene por qué
// enterarse de qué motor hay del otro lado para saber leerlo.
//
// Existe para que la UI pueda decir qué hay que arreglar. «no se pudo conectar»
// obliga a adivinar entre un host mal escrito, una contraseña vieja y un
// firewall; cada una se arregla en un lugar distinto.
type FailureKind string

// Fallos de conexión: no se llegó a hablar con el servidor, o el servidor
// rechazó la sesión.
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
	// rojo y su código, porque eso hace dudar de si además pasó algo malo.
	FailureCanceled FailureKind = "canceled"
	// FailureOther es todo lo demás.
	FailureOther FailureKind = "other"
)

// Fallos de sentencia: el servidor contestó perfectamente y dijo que no.
const (
	// FailureData es que los datos que ya están en la tabla no permiten el
	// cambio: hay nulos, hay repetidos, hay huérfanos. La sentencia está bien
	// escrita; lo que no da es la tabla.
	FailureData FailureKind = "data"
	// FailureConflict es que el objeto ya existe.
	FailureConflict FailureKind = "conflict"
	// FailureMissing es que el objeto que se nombra no existe.
	FailureMissing FailureKind = "missing"
	// FailureDependency es que hay otros objetos colgando del que se quiere
	// tocar.
	FailureDependency FailureKind = "dependency"
	// FailureLock es que otra sesión tiene el objeto tomado.
	FailureLock FailureKind = "lock"
	// FailureSyntax es que el motor no entendió la sentencia. Casi siempre es
	// un error de Kaname escribiéndola, o de una expresión escrita a mano.
	FailureSyntax FailureKind = "syntax"
)

// Failure es un fallo ya interpretado, listo para mostrar.
type Failure struct {
	Kind FailureKind `json:"kind"`
	// Message es qué pasó, en una línea.
	Message string `json:"message"`
	// Hint es qué hacer al respecto. Vacío si no hay nada útil que decir:
	// inventar una sugerencia es peor que no darla.
	Hint string `json:"hint"`
	// SQLState es el código del motor, cuando lo hubo. Sirve para buscar y para
	// pegar en un ticket.
	//
	// El nombre viene de Postgres pero el campo no es suyo: MySQL da SQLSTATE
	// además de su número propio, y SQLite da un código extendido. Los tres van
	// acá, como texto, porque lo único que se hace con esto es mostrarlo y
	// copiarlo.
	SQLState string `json:"sqlState,omitempty"`

	// Detail es el mensaje original, redactado.
	//
	// Message dice qué pasó en castellano; esto dice qué dijo el motor. Los dos
	// hacen falta: el primero para entender, el segundo para buscar en Google o
	// pegar en un ticket. Interpretar y descartar el original deja al usuario
	// sin la única frase que otro va a reconocer.
	Detail string `json:"detail,omitempty"`

	// Statement, Line y TotalStatements ubican el fallo cuando el texto tenía
	// varias sentencias.
	//
	// Sin esto, correr diez sentencias y ver «error de sintaxis» obliga a
	// buscar a mano cuál fue. Line es la que sirve de verdad: quien escribió el
	// texto está mirando números de línea, no contando sentencias.
	//
	// Cero significa que no viene al caso —un fallo de conexión no pertenece a
	// ninguna sentencia—.
	Statement       int `json:"statement,omitempty"`
	Line            int `json:"line,omitempty"`
	TotalStatements int `json:"totalStatements,omitempty"`
}

func (f *Failure) Error() string { return f.Message }

// dsnCredentials captura las credenciales de cualquier URI con la forma
// esquema://usuario:contraseña@host. Es el DSN de Postgres.
var dsnCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^:/?#\[\]@\s]*):([^@\s]*)@`)

// dsnMySQL captura el DSN de MySQL, que NO es un URI: tiene la forma
// usuario:contraseña@tcp(host:puerto)/base.
//
// Va aparte porque la expresión de arriba exige el `esquema://` y por lo tanto
// no lo reconocía. Mientras el DSN se armó solo para Postgres eso alcanzaba;
// desde la Iteración 6 hay cuatro motores y dos formatos más.
//
// La contraseña es `\S*` y no `[^@]*`, a propósito: el DSN de MySQL se arma
// sin escaparla —el driver parte por el ÚLTIMO `@`— así que una contraseña con
// `@` adentro tiene que llegar codiciosa hasta el `@` que precede a `tcp(`.
// Con `[^@]*` no había coincidencia y el DSN quedaba entero (K-11).
var dsnMySQL = regexp.MustCompile(`([^\s:/@]+):(\S*)@(tcp|unix|kaname-tunnel-\d+)\(`)

// Redact enmascara las contraseñas de cualquier cadena de conexión que aparezca
// en un texto.
//
// Se aplica a todo texto de origen ajeno —mensajes del servidor, errores de
// librerías— antes de que llegue a un Failure. Los mensajes terminan en toasts,
// logs y tickets, y no se puede asumir que quien los escribió tuvo cuidado.
func Redact(s string) string {
	s = dsnCredentials.ReplaceAllString(s, "${1}${2}:***@")
	return dsnMySQL.ReplaceAllString(s, "${1}:***@${3}(")
}
