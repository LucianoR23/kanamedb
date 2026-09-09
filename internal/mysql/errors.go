package mysql

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"

	sqldriver "github.com/go-sql-driver/mysql"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Números de error de MySQL y MariaDB que sabemos interpretar.
// https://dev.mysql.com/doc/mysql-errors/9.7/en/server-error-reference.html
const (
	errAccesoDenegado      = 1045 // ER_ACCESS_DENIED_ERROR
	errBaseDesconocida     = 1049 // ER_BAD_DB_ERROR
	errDemasiadasConex     = 1040 // ER_CON_COUNT_ERROR
	errHostBloqueado       = 1129 // ER_HOST_IS_BLOCKED
	errSinPrivilegios      = 1142 // ER_TABLEACCESS_DENIED_ERROR
	errSinPrivilegiosCol   = 1143 // ER_COLUMNACCESS_DENIED_ERROR
	errDuplicado           = 1062 // ER_DUP_ENTRY
	errNoPuedeSerNulo      = 1048 // ER_BAD_NULL_ERROR
	errClaveForanea        = 1452 // ER_NO_REFERENCED_ROW_2
	errClaveForaneaBorrar  = 1451 // ER_ROW_IS_REFERENCED_2
	errNoSePuedeCrearFK    = 1215 // ER_CANNOT_ADD_FOREIGN
	errFKMalDefinida       = 1005 // ER_CANT_CREATE_TABLE (con errno 150)
	errCheckViolado        = 3819 // ER_CHECK_CONSTRAINT_VIOLATED
	errTablaNoExiste       = 1146 // ER_NO_SUCH_TABLE
	errTablaDesconocida    = 1051 // ER_BAD_TABLE_ERROR
	errColumnaNoExiste     = 1054 // ER_BAD_FIELD_ERROR
	errTablaYaExiste       = 1050 // ER_TABLE_EXISTS_ERROR
	errColumnaYaExiste     = 1060 // ER_DUP_FIELDNAME
	errClaveYaExiste       = 1061 // ER_DUP_KEYNAME
	errRestriccionYaExiste = 3822 // ER_CONSTRAINT_NAME_ALREADY_EXISTS
	errSintaxis            = 1064 // ER_PARSE_ERROR
	errBloqueoEspera       = 1205 // ER_LOCK_WAIT_TIMEOUT
	errInterbloqueo        = 1213 // ER_LOCK_DEADLOCK
	errTablaBloqueada      = 1099 // ER_TABLE_NOT_LOCKED_FOR_WRITE
	errSoloLectura         = 1290 // ER_OPTION_PREVENTS_STATEMENT (--read-only)
	// ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION. Es el que da una escritura en
	// una conexión que Kaname abrió en modo solo lectura, así que es el más
	// probable de toda esta lista en una conexión de producción. Estaba sin
	// clasificar y salía como «el motor rechazó la sentencia».
	errTxSoloLectura      = 1792
	errConsultaCancelada  = 1317 // ER_QUERY_INTERRUPTED
	errAlgoritmoNoSoporta = 1845 // ER_ALTER_OPERATION_NOT_SUPPORTED
	errSinDefault         = 1364 // ER_NO_DEFAULT_FOR_FIELD
	errTipoInvalido       = 1366 // ER_TRUNCATED_WRONG_VALUE_FOR_FIELD
	errDependencias       = 3730 // ER_FK_CANNOT_DROP_PARENT
)

// Classify interpreta un error de CONEXIÓN.
//
// Nunca incluye el DSN ni la contraseña en el mensaje: estos textos terminan en
// toasts, logs y tickets.
func Classify(err error, desc string) *engine.Failure {
	if err == nil {
		return nil
	}
	f := classify(err, desc)
	if f.Detail == "" {
		f.Detail = engine.Redact(err.Error())
	}
	return f
}

func classify(err error, desc string) *engine.Failure {
	if errors.Is(err, context.DeadlineExceeded) {
		return &engine.Failure{
			Kind:    engine.FailureTimeout,
			Message: "El servidor no respondió a tiempo.",
			Hint:    "Puede estar caído, saturado o detrás de un firewall que descarta los paquetes en silencio.",
		}
	}
	if errors.Is(err, context.Canceled) {
		return &engine.Failure{Kind: engine.FailureCanceled, Message: "Cancelada."}
	}

	var me *sqldriver.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case errAccesoDenegado:
			return &engine.Failure{
				Kind:    engine.FailureAuth,
				Message: "El servidor rechazó las credenciales.",
				Hint: "Revisá el usuario y la contraseña. En MySQL el usuario incluye desde " +
					"dónde se conecta, así que 'kaname'@'localhost' y 'kaname'@'%' son dos " +
					"cuentas distintas.",
			}
		case errBaseDesconocida:
			return &engine.Failure{
				Kind:    engine.FailureDatabase,
				Message: "La base indicada no existe en ese servidor.",
				Hint:    "Revisá el nombre. En Linux los nombres de base distinguen mayúsculas.",
			}
		case errDemasiadasConex:
			return &engine.Failure{
				Kind:    engine.FailureOther,
				Message: "El servidor llegó a su límite de conexiones.",
				Hint:    "Esperá a que se libere alguna, o pedí que suban max_connections.",
			}
		case errHostBloqueado:
			return &engine.Failure{
				Kind:    engine.FailureNetwork,
				Message: "El servidor bloqueó a esta máquina por demasiados errores de conexión.",
				Hint:    "Se destraba con FLUSH HOSTS en el servidor.",
			}
		case errSinPrivilegios, errSinPrivilegiosCol:
			return &engine.Failure{
				Kind:    engine.FailurePermission,
				Message: "El usuario no tiene permiso sobre esa base.",
			}
		}
		return &engine.Failure{
			Kind:     engine.FailureOther,
			Message:  "El servidor rechazó la conexión con " + desc + ".",
			SQLState: string(me.SQLState[:]),
			Detail:   engine.Redact(me.Message),
		}
	}

	return red(err, desc)
}

// red interpreta los fallos que no llegaron a hablar con el servidor. Es igual
// para los tres motores de servidor, pero cada paquete tiene el suyo porque el
// texto de los errores de TLS depende del driver.
func red(err error, desc string) *engine.Failure {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return &engine.Failure{
			Kind:    engine.FailureNetwork,
			Message: "No se pudo resolver el nombre del servidor.",
			Hint:    "Revisá el host. Si está en una red privada, puede que necesites la VPN o un túnel SSH.",
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &engine.Failure{
			Kind:    engine.FailureTimeout,
			Message: "Se agotó el tiempo de espera al conectar.",
			Hint:    "El host puede estar inaccesible desde esta red, o un firewall puede estar descartando la conexión.",
		}
	}
	texto := strings.ToLower(err.Error())
	switch {
	case strings.Contains(texto, "tls"), strings.Contains(texto, "x509"),
		strings.Contains(texto, "certificate"):
		return &engine.Failure{
			Kind:    engine.FailureTLS,
			Message: "No se pudo establecer la conexión cifrada.",
			Hint: "Revisá el modo SSL. Con verify-full el certificado del servidor tiene que " +
				"ser válido y coincidir con el host.",
		}
	case strings.Contains(texto, "connection refused"):
		return &engine.Failure{
			Kind:    engine.FailureNetwork,
			Message: "El servidor rechazó la conexión.",
			Hint:    "Puede que no haya nada escuchando en ese puerto.",
		}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return &engine.Failure{
			Kind:    engine.FailureNetwork,
			Message: "No se pudo abrir la conexión con el servidor.",
			Hint:    "Verificá el host y el puerto, y que el servidor acepte conexiones desde esta máquina.",
		}
	}
	return &engine.Failure{
		Kind:    engine.FailureOther,
		Message: "No se pudo conectar con " + desc + ".",
	}
}

// ClassifyStatement interpreta el error de una SENTENCIA que se estaba
// ejecutando.
//
// Va aparte de Classify por el mismo motivo que en Postgres: son dos preguntas
// distintas y no se contestan con el mismo vocabulario. «El servidor rechazó la
// conexión» para un 1062 es falso — el servidor contestó perfectamente.
func ClassifyStatement(err error, desc string) *engine.Failure {
	if err == nil {
		return nil
	}
	f := classifyStatement(err, desc)
	if f.Detail == "" {
		f.Detail = engine.Redact(err.Error())
	}
	return f
}

func classifyStatement(err error, desc string) *engine.Failure {
	if errors.Is(err, context.Canceled) {
		return &engine.Failure{Kind: engine.FailureCanceled, Message: "Cancelada."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &engine.Failure{
			Kind:    engine.FailureTimeout,
			Message: "Se acabó el tiempo antes de que la sentencia terminara.",
			Hint: "La sentencia puede haber quedado corriendo en el servidor: comprobá el " +
				"estado del esquema antes de volver a aplicar.",
		}
	}

	var me *sqldriver.MySQLError
	if !errors.As(err, &me) {
		// Sin número de error no llegamos a hablar con el servidor.
		return classify(err, desc)
	}
	f := deNumero(me)
	f.SQLState = string(me.SQLState[:])
	f.Detail = engine.Redact(me.Message)
	return f
}

func deNumero(me *sqldriver.MySQLError) *engine.Failure {
	switch me.Number {

	// ------------------------------------------------ los datos no dan

	case errDuplicado:
		clave, valor := duplicadoDelMensaje(me.Message)
		msg := "Hay valores repetidos, así que no se puede exigir que sean únicos."
		hint := "Buscá los repetidos y decidí cuál se queda."
		if valor != "" {
			msg = "Ya existe una fila con " + valor + ": ese valor tiene que ser único."
		}
		if clave != "" {
			hint = "La restricción que lo impide se llama «" + clave + "»."
		}
		return &engine.Failure{Kind: engine.FailureData, Message: msg, Hint: hint}

	case errNoPuedeSerNulo:
		col := entreComillas(me.Message)
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "La columna " + comillas(col) + " no admite nulos y se intentó dejarla vacía.",
		}

	case errSinDefault:
		col := entreComillas(me.Message)
		return &engine.Failure{
			Kind: engine.FailureData,
			Message: "La columna " + comillas(col) + " no admite nulos y no tiene valor por " +
				"defecto, así que las filas que ya están no tendrían qué poner.",
			Hint: "Dale un valor por defecto, o dejala admitiendo nulos y llenala después.",
		}

	case errClaveForanea, errNoSePuedeCrearFK, errFKMalDefinida:
		return &engine.Failure{
			Kind: engine.FailureData,
			Message: "Hay filas que apuntan a algo que no existe en la tabla referenciada, o " +
				"los tipos de las dos columnas no coinciden.",
			Hint: "MySQL exige que las columnas de los dos lados tengan el MISMO tipo y la " +
				"misma colación, y que la del otro lado esté indexada. Un bigint contra un " +
				"int no se puede.",
		}

	case errClaveForaneaBorrar:
		return &engine.Failure{
			Kind:    engine.FailureDependency,
			Message: "Hay filas de otra tabla que apuntan a esta, así que no se puede borrar.",
			Hint:    "Borrá primero las que dependen, o poné la clave en ON DELETE CASCADE.",
		}

	case errCheckViolado:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Hay filas que no cumplen la restricción " + comillas(entreComillas(me.Message)) + ".",
			Hint:    "Corregí esas filas antes de agregarla.",
		}

	case errTipoInvalido:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Un valor no es válido para el tipo de la columna.",
			Hint: "Si es un valor por defecto, acordate de que el texto va entre comillas " +
				"simples.",
		}

	case errDependencias:
		return &engine.Failure{
			Kind:    engine.FailureDependency,
			Message: "No se puede: hay una clave foránea de otra tabla que depende de esto.",
			Hint:    "Borrá primero la clave que depende, o sumala al mismo changeset.",
		}

	// ------------------------------------------------ ya existe / no existe

	case errTablaYaExiste:
		return &engine.Failure{
			Kind:    engine.FailureConflict,
			Message: "Ya existe una tabla llamada " + comillas(entreComillas(me.Message)) + ".",
			Hint: "Si el esquema cambió desde que abriste la pantalla, refrescá: puede que " +
				"este cambio ya esté aplicado.",
		}

	case errColumnaYaExiste:
		return &engine.Failure{
			Kind:    engine.FailureConflict,
			Message: "La columna " + comillas(entreComillas(me.Message)) + " ya existe en la tabla.",
			Hint: "Si el esquema cambió desde que abriste la pantalla, refrescá: puede que " +
				"este cambio ya esté aplicado.",
		}

	case errClaveYaExiste, errRestriccionYaExiste:
		return &engine.Failure{
			Kind:    engine.FailureConflict,
			Message: "Ya hay un índice o una restricción con el nombre " + comillas(entreComillas(me.Message)) + ".",
			Hint:    "Dejá el nombre vacío y lo elige el motor, sin repetirse.",
		}

	case errTablaNoExiste, errTablaDesconocida:
		return &engine.Failure{
			Kind:    engine.FailureMissing,
			Message: "La tabla " + comillas(entreComillas(me.Message)) + " no existe.",
			Hint: "Puede que la hayan borrado o renombrado desde que se leyó el esquema. " +
				"Refrescá y revisá los cambios pendientes.",
		}

	case errColumnaNoExiste:
		return &engine.Failure{
			Kind:    engine.FailureMissing,
			Message: "La columna " + comillas(entreComillas(me.Message)) + " no existe.",
			Hint: "Puede que la hayan borrado o renombrado desde que se leyó el esquema. " +
				"Refrescá y revisá los cambios pendientes.",
		}

	// ------------------------------------------------ el momento no da

	case errBloqueoEspera, errTablaBloqueada:
		return &engine.Failure{
			Kind:    engine.FailureLock,
			Message: "Otra sesión tiene la tabla tomada y se acabó la espera.",
			Hint: "Un ALTER necesita un lock exclusivo. Mirá quién la tiene con " +
				"SHOW PROCESSLIST.",
		}

	case errInterbloqueo:
		return &engine.Failure{
			Kind:    engine.FailureLock,
			Message: "Hubo un interbloqueo con otra sesión y el motor cortó esta.",
			Hint:    "No quedó nada aplicado de esta sentencia. Volvé a intentar.",
		}

	case errConsultaCancelada:
		return &engine.Failure{
			Kind:    engine.FailureTimeout,
			Message: "La sentencia se canceló por el límite de tiempo.",
		}

	case errSoloLectura:
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "El servidor está en modo solo lectura y no acepta escrituras.",
			Hint:    "Puede ser una réplica, o tener read_only puesto.",
		}

	case errTxSoloLectura:
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "Esta conexión está abierta en modo solo lectura.",
			Hint: "Lo pide Kaname, no el servidor: la conexión tiene la casilla de solo " +
				"lectura puesta. Se saca en la pantalla de conexiones.",
		}

	case errSinPrivilegios, errSinPrivilegiosCol:
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "El usuario no tiene permiso para hacer eso.",
		}

	// ------------------------------------------------ la sentencia no va

	case errSintaxis:
		return &engine.Failure{
			Kind:    engine.FailureSyntax,
			Message: "El motor no entendió la sentencia.",
			Hint: "Si la escribió Kaname entera, es un error nuestro y conviene reportarlo. " +
				"Si tiene una expresión escrita a mano —un CHECK, un valor por defecto—, el " +
				"problema está casi seguro ahí.",
		}

	case errAlgoritmoNoSoporta:
		return &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "El motor no puede hacer ese cambio sin reescribir la tabla.",
			Hint:    "Sacá el ALGORITHM del ALTER y va a funcionar, pero va a tardar.",
		}
	}

	return porClase(me)
}

// porClase contesta con lo que se sabe por el SQLSTATE cuando el número no está
// enumerado. MySQL tiene más de mil números y esta lista tiene treinta; lo que
// no puede pasar es quedarse sin nada que decir.
func porClase(me *sqldriver.MySQLError) *engine.Failure {
	estado := string(me.SQLState[:])
	clase := ""
	if len(estado) >= 2 {
		clase = estado[:2]
	}
	switch clase {
	case "23":
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Los datos que ya están en la tabla no permiten este cambio.",
		}
	case "42":
		return &engine.Failure{Kind: engine.FailureSyntax, Message: "El motor rechazó la sentencia."}
	case "22":
		return &engine.Failure{Kind: engine.FailureData, Message: "Hay un valor que no es válido."}
	case "40":
		return &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "La transacción se canceló y no quedó nada aplicado.",
		}
	case "25":
		// Clase «estado de transacción inválido»: casi siempre, escribir en una
		// transacción de solo lectura.
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "La transacción no admite esta operación.",
			Hint:    "Suele ser una escritura en una conexión abierta en modo solo lectura.",
		}
	case "08":
		return &engine.Failure{Kind: engine.FailureNetwork, Message: "Se perdió la conexión."}
	}
	return &engine.Failure{Kind: engine.FailureOther, Message: "El motor rechazó la sentencia."}
}

/* ------------------------------------------------------------- ayudantes */

var entrecomillado = regexp.MustCompile("'([^']+)'|`([^`]+)`")

// entreComillas saca el primer nombre citado del mensaje. MySQL usa comillas
// simples para casi todo y acentos invertidos en algunos, así que se aceptan
// las dos.
func entreComillas(mensaje string) string {
	m := entrecomillado.FindStringSubmatch(mensaje)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return recortarBase(m[1])
	}
	return recortarBase(m[2])
}

// recortarBase saca el prefijo de base de un nombre calificado. MySQL escribe
// «base.tabla» en algunos mensajes y «tabla» en otros; para mostrar alcanza con
// el último componente.
func recortarBase(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 && i < len(s)-1 {
		return s[i+1:]
	}
	return s
}

// duplicadoDelMensaje lee «Duplicate entry 'x' for key 'tabla.clave'».
//
// Es la única parte del error que dice CUÁL fila choca, y sin ella el mensaje
// obliga a buscar a mano.
var duplicado = regexp.MustCompile(`Duplicate entry '([^']*)' for key '([^']+)'`)

func duplicadoDelMensaje(mensaje string) (clave, valor string) {
	m := duplicado.FindStringSubmatch(mensaje)
	if m == nil {
		return "", ""
	}
	return recortarBase(m[2]), "«" + m[1] + "»"
}

func comillas(s string) string {
	if s == "" {
		return "esa"
	}
	return "«" + s + "»"
}
