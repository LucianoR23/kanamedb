package sqlite

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	sq "modernc.org/sqlite"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Códigos primarios de SQLite.
// https://www.sqlite.org/rescode.html
const (
	codeError       = 1  // SQLITE_ERROR: el cajón de sastre. Ver abajo.
	codePermiso     = 3  // SQLITE_PERM
	codeAbortada    = 4  // SQLITE_ABORT
	codeOcupada     = 5  // SQLITE_BUSY
	codeBloqueada   = 6  // SQLITE_LOCKED
	codeSinMemoria  = 7  // SQLITE_NOMEM
	codeSoloLect    = 8  // SQLITE_READONLY
	codeInterrump   = 9  // SQLITE_INTERRUPT
	codeES          = 10 // SQLITE_IOERR
	codeCorrupta    = 11 // SQLITE_CORRUPT
	codeLlena       = 13 // SQLITE_FULL
	codeNoAbre      = 14 // SQLITE_CANTOPEN
	codeRestriccion = 19 // SQLITE_CONSTRAINT
	codeNoEsBase    = 26 // SQLITE_NOTADB
)

// Códigos extendidos de restricción. Estos SÍ dicen exactamente qué pasó, al
// revés que SQLITE_ERROR, y son los que permiten dar un mensaje útil.
const (
	codeCheck      = 275  // SQLITE_CONSTRAINT_CHECK
	codeForanea    = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	codeNoNulo     = 1299 // SQLITE_CONSTRAINT_NOTNULL
	codePrimaria   = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	codeUnica      = 2067 // SQLITE_CONSTRAINT_UNIQUE
	codeRowID      = 2579 // SQLITE_CONSTRAINT_ROWID
	codeDisparador = 1811 // SQLITE_CONSTRAINT_TRIGGER
	codeTipoDato   = 3091 // SQLITE_CONSTRAINT_DATATYPE (columnas STRICT)
)

// Classify interpreta un error de APERTURA del archivo.
//
// El equivalente de «conectar» en SQLite es abrir un archivo, así que este
// vocabulario no habla de red ni de credenciales: habla de rutas, permisos del
// sistema de archivos y archivos que no son bases de datos. Un mensaje que
// dijera «el servidor rechazó las credenciales» mandaría a buscar el problema
// donde no está.
//
// `desc` es la ruta del archivo. No es un secreto —no lleva contraseña— pero
// igual pasa por Redact, porque una ruta puede venir de un DSN escrito a mano.
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
	if errors.Is(err, context.Canceled) {
		return &engine.Failure{Kind: engine.FailureCanceled, Message: "Cancelada."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &engine.Failure{
			Kind:    engine.FailureTimeout,
			Message: "Se acabó el tiempo al abrir el archivo.",
			Hint:    "Puede estar en una unidad de red que no responde.",
		}
	}
	// El driver abre el archivo con el sistema operativo antes de mirarlo, así
	// que estos errores llegan crudos y son los más frecuentes de todos.
	if errors.Is(err, fs.ErrNotExist) {
		return &engine.Failure{
			Kind:    engine.FailureDatabase,
			Message: "No hay ningún archivo en esa ruta.",
			Hint: "Revisá la ruta. SQLite CREA el archivo si no existe, así que si " +
				"esperabas una base con datos, casi seguro la ruta está mal.",
		}
	}
	if errors.Is(err, fs.ErrPermission) {
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "El sistema no deja abrir ese archivo.",
			Hint: "En SQLite el permiso lo da el sistema de archivos, no la base: " +
				"revisá los permisos del archivo y de la carpeta que lo contiene.",
		}
	}

	var se *sq.Error
	if errors.As(err, &se) {
		return deCodigoApertura(se, desc)
	}
	return &engine.Failure{
		Kind:    engine.FailureOther,
		Message: "No se pudo abrir " + desc + ".",
	}
}

func deCodigoApertura(se *sq.Error, desc string) *engine.Failure {
	switch se.Code() {
	case codeNoAbre:
		return &engine.Failure{
			Kind:    engine.FailureDatabase,
			Message: "No se pudo abrir el archivo.",
			Hint: "Puede que la carpeta no exista, que la ruta esté mal escrita, o que " +
				"el archivo esté en una unidad de red desconectada.",
		}
	case codeNoEsBase:
		return &engine.Failure{
			Kind:    engine.FailureDatabase,
			Message: "Ese archivo no es una base de SQLite.",
			Hint: "Los primeros 16 bytes de un archivo de SQLite dicen «SQLite format 3». " +
				"Este no. Puede ser otro formato, o estar cifrado.",
		}
	case codeCorrupta:
		return &engine.Failure{
			Kind:    engine.FailureDatabase,
			Message: "El archivo está dañado.",
			Hint: "Probá con .recover del shell de SQLite sobre una COPIA del archivo. " +
				"No trabajes sobre el original.",
		}
	case codePermiso, codeSoloLect:
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "El archivo es de solo lectura o no hay permiso para abrirlo.",
			Hint:    "El permiso lo da el sistema de archivos: revisalo ahí.",
		}
	case codeOcupada, codeBloqueada:
		return &engine.Failure{
			Kind:    engine.FailureLock,
			Message: "Otro proceso tiene la base tomada.",
			Hint:    "SQLite deja un solo escritor a la vez. Cerrá lo que la esté usando.",
		}
	}
	return &engine.Failure{
		Kind:    engine.FailureOther,
		Message: "No se pudo abrir " + desc + ".",
	}
}

// ClassifyStatement interpreta el error de una SENTENCIA.
//
// Acá está la diferencia grande con los otros tres motores, y no es de estilo:
// **SQLite no tiene un código de error por problema**. SQLITE_ERROR es 1 y ahí
// caen, con el mismo número, la tabla que no existe, la columna repetida, el
// índice que ya está y el error de sintaxis. Comprobado contra 3.53.4: los
// cuatro devuelven 1.
//
// Así que para ese caso —y solo para ese— hay que leer el mensaje. Es
// exactamente lo que en Postgres y MySQL se evita a propósito, porque el texto
// cambia entre versiones y con el idioma. Se hace igual porque la alternativa
// es que la pantalla de apply diga «el motor rechazó la sentencia» y nada más
// contra el único motor que no necesita servidor, que va a ser el más usado
// para probar.
//
// Las restricciones son la mitad buena de la historia: SQLITE_CONSTRAINT sí
// tiene códigos extendidos, y son precisos.
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
		}
	}

	var se *sq.Error
	if !errors.As(err, &se) {
		return classify(err, desc)
	}
	f := deCodigo(se)
	// SQLite no tiene SQLSTATE. Se pone el código extendido, que es lo que
	// alguien busca en Google y lo que se pega en un ticket.
	f.SQLState = strconv.Itoa(se.Code())
	f.Detail = engine.Redact(se.Error())
	return f
}

func deCodigo(se *sq.Error) *engine.Failure {
	switch se.Code() {

	// -------------------------------------------- restricciones: sí hay código

	case codePrimaria, codeRowID:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Ya hay una fila con esa clave primaria.",
			Hint:    hintDeColumna(se.Error()),
		}

	case codeUnica:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Hay valores repetidos, así que no se puede exigir que sean únicos.",
			Hint:    hintDeColumna(se.Error()),
		}

	case codeNoNulo:
		col := columnaDelMensaje(se.Error())
		msg := "Hay filas con NULL en una columna que no admite nulos."
		if col != "" {
			msg = "Hay filas con NULL en «" + col + "», que no admite nulos."
		}
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: msg,
			Hint:    "Llenalas antes, o dale un valor por defecto a la columna.",
		}

	case codeCheck:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Hay filas que no cumplen una restricción CHECK.",
			Hint:    "Corregí esas filas antes de agregarla.",
		}

	case codeForanea:
		return &engine.Failure{
			Kind: engine.FailureData,
			Message: "Hay filas que apuntan a algo que no existe en la tabla " +
				"referenciada, o filas de otra tabla que apuntan a esta.",
			Hint: "SQLite no dice cuál fila. PRAGMA foreign_key_check las lista todas.",
		}

	case codeTipoDato:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Un valor no es del tipo que la columna exige.",
			Hint: "Esta tabla es STRICT: a diferencia del SQLite de siempre, no acepta " +
				"cualquier valor en cualquier columna.",
		}

	case codeDisparador:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Un trigger de la tabla abortó la operación.",
		}

	case codeRestriccion:
		return &engine.Failure{
			Kind:    engine.FailureData,
			Message: "Los datos que ya están en la tabla no permiten este cambio.",
		}

	// -------------------------------------------- el momento no da

	case codeOcupada, codeBloqueada:
		return &engine.Failure{
			Kind:    engine.FailureLock,
			Message: "Otro proceso tiene la base tomada y se acabó la espera.",
			Hint: "SQLite deja un solo escritor a la vez en todo el archivo, no por " +
				"tabla. Cerrá lo que la esté usando.",
		}

	case codeSoloLect:
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "La base está abierta en modo solo lectura.",
			Hint:    "Puede ser el permiso del archivo, o la carpeta que lo contiene.",
		}

	case codePermiso:
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "El sistema de archivos no da permiso para eso.",
		}

	case codeInterrump, codeAbortada:
		return &engine.Failure{Kind: engine.FailureCanceled, Message: "Cancelada."}

	case codeLlena:
		return &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "No queda espacio en disco.",
			Hint: "Una reconstrucción de tabla necesita lugar para las DOS copias " +
				"mientras corre.",
		}

	case codeSinMemoria:
		return &engine.Failure{Kind: engine.FailureOther, Message: "El motor se quedó sin memoria."}

	case codeES:
		return &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "Falló una lectura o escritura del archivo.",
			Hint:    "Si está en una unidad de red o en un disco externo, revisá que siga conectado.",
		}

	case codeCorrupta, codeNoEsBase:
		return deCodigoApertura(se, "el archivo")
	}

	// SQLITE_ERROR, o cualquier otro: solo queda el mensaje.
	return delMensaje(se.Error())
}

// delMensaje clasifica lo que cae en SQLITE_ERROR, que es casi todo lo que no
// es una restricción.
//
// El orden importa: «index i1 already exists» contiene «already exists» y
// también «index», así que lo específico va primero.
func delMensaje(mensaje string) *engine.Failure {
	m := strings.ToLower(mensaje)
	switch {
	case strings.Contains(m, "no such table"):
		return &engine.Failure{
			Kind:    engine.FailureMissing,
			Message: "La tabla " + comillas(trasDosPuntos(mensaje)) + " no existe.",
			Hint: "Puede que la hayan borrado o renombrado desde que se leyó el esquema. " +
				"Refrescá y revisá los cambios pendientes.",
		}
	case strings.Contains(m, "no such column"):
		return &engine.Failure{
			Kind:    engine.FailureMissing,
			Message: "La columna " + comillas(trasDosPuntos(mensaje)) + " no existe.",
			Hint:    "Refrescá el esquema y revisá los cambios pendientes.",
		}
	case strings.Contains(m, "no such index"), strings.Contains(m, "no such trigger"),
		strings.Contains(m, "no such view"), strings.Contains(m, "no such collation"):
		return &engine.Failure{
			Kind:    engine.FailureMissing,
			Message: "No existe " + comillas(trasDosPuntos(mensaje)) + ".",
		}
	case strings.Contains(m, "duplicate column name"):
		return &engine.Failure{
			Kind:    engine.FailureConflict,
			Message: "La columna " + comillas(trasDosPuntos(mensaje)) + " ya existe en la tabla.",
			Hint: "Si el esquema cambió desde que abriste la pantalla, refrescá: puede que " +
				"este cambio ya esté aplicado.",
		}
	case strings.Contains(m, "already exists"):
		return &engine.Failure{
			Kind:    engine.FailureConflict,
			Message: "Ya existe " + comillas(antesDeYaExiste(mensaje)) + ".",
			Hint: "Si el esquema cambió desde que abriste la pantalla, refrescá: puede que " +
				"este cambio ya esté aplicado.",
		}
	case strings.Contains(m, "cannot add a not null column with default value null"):
		return &engine.Failure{
			Kind: engine.FailureData,
			Message: "Una columna nueva que no admite nulos necesita un valor por defecto, " +
				"o las filas que ya están no tendrían qué poner.",
			Hint: "Dale un valor por defecto, o dejala admitiendo nulos y llenala después.",
		}
	case strings.Contains(m, "non-constant default"):
		return &engine.Failure{
			Kind: engine.FailureOther,
			Message: "SQLite no deja agregar una columna con un valor por defecto que se " +
				"calcula, como datetime('now').",
			Hint: "Agregala con un valor fijo o sin default, y llenala con un UPDATE después.",
		}
	case strings.Contains(m, "cannot drop"):
		// «cannot drop PRIMARY KEY column», «cannot drop column ... indexed».
		return &engine.Failure{
			Kind:    engine.FailureDependency,
			Message: "SQLite no deja borrar esa columna directamente.",
			Hint: "Pasa cuando es parte de la clave primaria, de un índice o de una " +
				"restricción. Sacá primero lo que depende de ella.",
		}
	case strings.Contains(m, "syntax error"), strings.Contains(m, `near "`):
		return &engine.Failure{
			Kind:    engine.FailureSyntax,
			Message: "El motor no entendió la sentencia.",
			Hint: "Si la escribió Kaname entera, es un error nuestro y conviene reportarlo. " +
				"Si tiene una expresión escrita a mano —un CHECK, un valor por defecto—, " +
				"el problema está casi seguro ahí.",
		}
	case strings.Contains(m, "readonly"), strings.Contains(m, "read-only"):
		return &engine.Failure{
			Kind:    engine.FailurePermission,
			Message: "La base está abierta en modo solo lectura.",
		}
	}
	return &engine.Failure{Kind: engine.FailureOther, Message: "El motor rechazó la sentencia."}
}

/* ------------------------------------------------------------- ayudantes */

// trasDosPuntos saca el nombre de mensajes con la forma «no such table: x».
func trasDosPuntos(mensaje string) string {
	i := strings.LastIndex(mensaje, ": ")
	if i < 0 {
		return ""
	}
	// El mensaje del driver termina con « (1)»: el código entre paréntesis.
	nombre := strings.TrimSpace(mensaje[i+2:])
	if j := strings.LastIndex(nombre, " ("); j > 0 {
		nombre = nombre[:j]
	}
	return strings.Trim(nombre, `"`)
}

// antesDeYaExiste lee «table padre already exists» y «index i1 already exists».
var yaExiste = regexp.MustCompile(`(?i)(table|index|trigger|view)\s+(\S+)\s+already exists`)

func antesDeYaExiste(mensaje string) string {
	m := yaExiste.FindStringSubmatch(mensaje)
	if m == nil {
		return ""
	}
	return strings.Trim(m[2], `"`)
}

// columnaDelMensaje lee «NOT NULL constraint failed: tabla.columna».
func columnaDelMensaje(mensaje string) string {
	n := trasDosPuntos(mensaje)
	if i := strings.LastIndex(n, "."); i >= 0 && i < len(n)-1 {
		return n[i+1:]
	}
	return n
}

// hintDeColumna dice cuál columna choca, cuando el mensaje lo trae.
func hintDeColumna(mensaje string) string {
	c := trasDosPuntos(mensaje)
	if c == "" {
		return "Buscá los repetidos y decidí cuál se queda."
	}
	return "La columna que lo impide es «" + c + "»."
}

func comillas(s string) string {
	if s == "" {
		return "eso"
	}
	return "«" + s + "»"
}
