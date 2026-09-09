package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Kinds que solo aparecen cuando falla una SENTENCIA, no una conexión.
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
	// FailureSyntax es que PostgreSQL no entendió la sentencia. Casi siempre es
	// un error de Kaname escribiéndola, o de una expresión escrita a mano.
	FailureSyntax FailureKind = "syntax"
)

// Códigos SQLSTATE que aparecen ejecutando DDL.
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	sqlStateNotNullViolation      = "23502"
	sqlStateForeignKeyViolation   = "23503"
	sqlStateUniqueViolation       = "23505"
	sqlStateCheckViolation        = "23514"
	sqlStateInvalidTextRepr       = "22P02"
	sqlStateSyntaxError           = "42601"
	sqlStateUndefinedColumn       = "42703"
	sqlStateUndefinedObject       = "42704"
	sqlStateDuplicateColumn       = "42701"
	sqlStateDuplicateObject       = "42710"
	sqlStateDuplicateTable        = "42P07"
	sqlStateUndefinedTable        = "42P01"
	sqlStateUndefinedFunction     = "42883"
	sqlStateDatatypeMismatch      = "42804"
	sqlStateInvalidForeignKey     = "42830"
	sqlStateDependentObjects      = "2BP01"
	sqlStateObjectInUse           = "55006"
	sqlStateLockNotAvailable      = "55P03"
	sqlStateDeadlock              = "40P01"
	sqlStateQueryCanceled         = "57014"
	sqlStateFeatureNotSupported   = "0A000"
	sqlStateReadOnlyTransaction   = "25006"
	sqlStateInvalidTableDefiniton = "42P16"
)

// ClassifyStatement interpreta el error de una sentencia que se estaba
// ejecutando: un ALTER, un CREATE INDEX, un DROP.
//
// Existe aparte de [Classify] porque son dos preguntas distintas y no se pueden
// contestar con el mismo vocabulario. Classify contesta «¿por qué no llegué al
// servidor?» y sus salidas hablan de hosts, puertos y contraseñas. Acá el
// servidor contestó perfectamente: dijo que no. Pasar un 23505 por Classify
// devolvía «el servidor rechazó la conexión con usuario@host/base», que además
// de inútil es falso — y era exactamente lo que se mostraba al fallar un apply.
//
// La diferencia que importa para quien lo lee es entre «la sentencia está mal»
// y «la sentencia está bien pero los datos que ya están no la aguantan». La
// segunda es la que pasa de verdad, y la que necesita decir dónde mirar.
func ClassifyStatement(err error, desc string) *Failure {
	if err == nil {
		return nil
	}
	f := classifyStatement(err, desc)
	if f.Detail == "" {
		f.Detail = Redact(err.Error())
	}
	return f
}

func classifyStatement(err error, desc string) *Failure {
	if errors.Is(err, context.Canceled) {
		return &Failure{Kind: FailureCanceled, Message: "Cancelada."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Failure{
			Kind:    FailureTimeout,
			Message: "Se acabó el tiempo antes de que la sentencia terminara.",
			Hint: "La sentencia puede haber quedado corriendo en el servidor: comprobá el " +
				"estado del esquema antes de volver a aplicar.",
		}
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		// Sin SQLSTATE no llegamos a hablar con el servidor: esto es la
		// conexión, y de eso sabe el otro clasificador.
		return classify(err, desc)
	}
	f := deSQLState(pgErr)
	f.SQLState = pgErr.Code
	f.Detail = Redact(detalleDelServidor(pgErr))
	return f
}

// detalleDelServidor junta lo que dijo el motor, en sus palabras.
//
// Message y Hint van en castellano y explican qué hacer; esto es lo que se pega
// en un ticket o se busca en Google, y por eso se conserva textual.
func detalleDelServidor(pgErr *pgconn.PgError) string {
	partes := []string{pgErr.Message}
	if pgErr.Detail != "" {
		partes = append(partes, pgErr.Detail)
	}
	if pgErr.Hint != "" {
		partes = append(partes, pgErr.Hint)
	}
	return strings.Join(partes, " · ")
}

func deSQLState(pgErr *pgconn.PgError) *Failure {
	switch pgErr.Code {

	// ---------------------------------------------- los datos no dan

	case sqlStateNotNullViolation:
		col := primero(pgErr.ColumnName, entreComillas(pgErr.Message))
		return &Failure{
			Kind: FailureData,
			Message: fmt.Sprintf(
				"La columna %s de %s tiene filas con NULL, así que no puede pasar a NOT NULL.",
				comillas(col), relacion(pgErr)),
			Hint: fmt.Sprintf(
				"Para verlas: SELECT * FROM %s WHERE %s IS NULL. Poneles un valor —o un valor "+
					"por defecto y después el NOT NULL— antes de exigirlo.",
				relacionSQL(pgErr), ident(col)),
		}

	case sqlStateUniqueViolation:
		cols, valor := claveDelDetalle(pgErr.Detail)
		msg := "Hay valores repetidos, así que no se puede exigir que sean únicos."
		if cols != "" {
			msg = fmt.Sprintf("Ya hay filas con %s = %s repetido: no se puede exigir que sea único.",
				cols, valor)
		}
		hint := "Buscá los repetidos y decidí cuál se queda antes de crear el índice."
		if cols != "" {
			hint = fmt.Sprintf(
				"Para ver todos los repetidos: SELECT %s, count(*) FROM %s GROUP BY %s "+
					"HAVING count(*) > 1.", cols, relacionSQL(pgErr), cols)
		}
		return &Failure{Kind: FailureData, Message: msg, Hint: hint}

	case sqlStateForeignKeyViolation:
		cols, valor := claveDelDetalle(pgErr.Detail)
		msg := "Hay filas que apuntan a algo que no existe en la tabla referenciada."
		if cols != "" {
			msg = fmt.Sprintf(
				"Hay filas de %s con %s = %s, y ese valor no existe en la tabla referenciada.",
				relacion(pgErr), cols, valor)
		}
		return &Failure{
			Kind:    FailureData,
			Message: msg,
			Hint: "Arreglá o borrá esas filas primero. Si querés que la clave valga solo de acá " +
				"en adelante, se puede crear NOT VALID y validarla después.",
		}

	case sqlStateCheckViolation:
		return &Failure{
			Kind: FailureData,
			Message: fmt.Sprintf(
				"Hay filas de %s que no cumplen la restricción %s.",
				relacion(pgErr), comillas(pgErr.ConstraintName)),
			Hint: "Corregí esas filas, o agregá la restricción NOT VALID para que solo se le " +
				"exija a las filas nuevas.",
		}

	case sqlStateInvalidTextRepr:
		return &Failure{
			Kind:    FailureData,
			Message: "Un valor no es válido para el tipo.",
			Hint: "Si es un valor por defecto, acordate de que es una EXPRESIÓN: el texto va " +
				"entre comillas simples.",
		}

	case sqlStateDatatypeMismatch:
		return &Failure{
			Kind:    FailureData,
			Message: "PostgreSQL no sabe convertir sola los valores que ya están al tipo nuevo.",
			Hint: "Hace falta decirle cómo convertir cada valor, con USING. Kaname todavía no " +
				"lo ofrece: por ahora ese cambio se hace desde el editor SQL.",
		}

	// ---------------------------------------------- ya existe / no existe

	case sqlStateDuplicateColumn:
		return &Failure{
			Kind:    FailureConflict,
			Message: fmt.Sprintf("La columna %s ya existe en la tabla.", comillas(entreComillas(pgErr.Message))),
			Hint: "Si el esquema cambió desde que abriste la pantalla, refrescá: puede que este " +
				"cambio ya esté aplicado.",
		}

	case sqlStateDuplicateTable:
		return &Failure{
			Kind:    FailureConflict,
			Message: fmt.Sprintf("Ya existe un objeto llamado %s.", comillas(entreComillas(pgErr.Message))),
			Hint: "Las tablas, las vistas y los índices comparten el mismo espacio de nombres: " +
				"el nombre puede estar tomado por algo de otro tipo.",
		}

	case sqlStateDuplicateObject:
		return &Failure{
			Kind:    FailureConflict,
			Message: fmt.Sprintf("Ya hay una restricción llamada %s en la tabla.", comillas(entreComillas(pgErr.Message))),
			Hint:    "Dejá el nombre vacío y lo elige PostgreSQL, sin repetirse.",
		}

	case sqlStateUndefinedTable:
		return &Failure{
			Kind:    FailureMissing,
			Message: fmt.Sprintf("La tabla %s no existe.", comillas(entreComillas(pgErr.Message))),
			Hint: "Puede que la hayan borrado o renombrado desde que se leyó el esquema. " +
				"Refrescá y revisá los cambios pendientes.",
		}

	case sqlStateUndefinedColumn:
		return &Failure{
			Kind:    FailureMissing,
			Message: fmt.Sprintf("La columna %s no existe en la tabla.", comillas(entreComillas(pgErr.Message))),
			Hint: "Puede que la hayan borrado o renombrado desde que se leyó el esquema. " +
				"Refrescá y revisá los cambios pendientes.",
		}

	case sqlStateUndefinedObject:
		f := &Failure{
			Kind:    FailureMissing,
			Message: fmt.Sprintf("PostgreSQL no conoce %s.", comillas(entreComillas(pgErr.Message))),
		}
		if strings.HasPrefix(pgErr.Message, "type ") {
			f.Message = fmt.Sprintf("El tipo %s no existe en esta base.", comillas(entreComillas(pgErr.Message)))
			f.Hint = "Los tipos propios llevan su esquema adelante: demo.estado, no estado."
		}
		return f

	case sqlStateUndefinedFunction:
		return &Failure{
			Kind:    FailureMissing,
			Message: "La función que usa la expresión no existe con esos argumentos.",
			Hint:    "Si es de un esquema propio, escribila con el esquema adelante.",
		}

	case sqlStateInvalidForeignKey:
		return &Failure{
			Kind: FailureConflict,
			Message: "La tabla a la que apunta la clave no tiene una clave primaria ni un índice " +
				"único sobre esas columnas.",
			Hint: "Una clave foránea solo puede apuntar a columnas únicas. Apuntá a la clave " +
				"primaria, o creá primero el índice único del otro lado.",
		}

	case sqlStateDependentObjects:
		return &Failure{
			Kind:    FailureDependency,
			Message: "No se puede borrar: hay otros objetos que dependen de esto.",
			Hint: "Están listados abajo. Borralos primero —se pueden sumar al mismo changeset— " +
				"o dejá el objeto donde está.",
		}

	// ---------------------------------------------- el momento no da

	case sqlStateObjectInUse, sqlStateLockNotAvailable:
		return &Failure{
			Kind:    FailureLock,
			Message: "Otra sesión tiene la tabla tomada.",
			Hint: "Un ALTER necesita un lock exclusivo y espera a que no quede nadie usándola. " +
				"Mirá quién la tiene con SELECT * FROM pg_stat_activity.",
		}

	case sqlStateDeadlock:
		return &Failure{
			Kind:    FailureLock,
			Message: "Hubo un interbloqueo con otra sesión y PostgreSQL cortó esta.",
			Hint:    "No quedó nada aplicado de esta sentencia. Volvé a intentar.",
		}

	case sqlStateQueryCanceled:
		return &Failure{
			Kind:    FailureTimeout,
			Message: "La sentencia se canceló por el límite de tiempo de la conexión.",
			Hint: "Subí el statement_timeout de esta conexión en su editor, o aplicá el cambio " +
				"en una ventana donde la tabla esté tranquila.",
		}

	case sqlStateReadOnlyTransaction:
		return &Failure{
			Kind:    FailurePermission,
			Message: "La sesión está en modo solo lectura y no acepta escrituras.",
		}

	case sqlStateInsufficientPrivilege:
		return &Failure{
			Kind:    FailurePermission,
			Message: "El usuario no tiene permiso para modificar este objeto.",
			Hint:    "Para un ALTER hace falta ser dueño de la tabla, o miembro del rol que la posee.",
		}

	// ---------------------------------------------- la sentencia no va

	case sqlStateSyntaxError:
		// PostgreSQL usa el 42601 también para «esa columna es generada», que
		// no tiene nada de error de sintaxis y sí una salida concreta.
		if strings.Contains(pgErr.Message, "is a generated column") {
			return &Failure{
				Kind: FailureConflict,
				Message: "La columna es generada: su valor sale de una expresión, así que no " +
					"puede tener un valor por defecto.",
				Hint: "Lo que se le cambia a una columna generada es la expresión, y Kaname " +
					"todavía no lo ofrece: por ahora ese cambio va por el editor SQL.",
			}
		}
		return &Failure{
			Kind:    FailureSyntax,
			Message: "PostgreSQL no entendió la sentencia.",
			Hint: "Si la sentencia la escribió Kaname entera, es un error nuestro y conviene " +
				"reportarlo. Si tiene una expresión escrita a mano —un CHECK, un valor por " +
				"defecto, un WHERE de índice—, el problema está casi seguro ahí.",
		}

	case sqlStateFeatureNotSupported:
		return sinSoporte(pgErr)

	case sqlStateInvalidTableDefiniton:
		return &Failure{
			Kind:    FailureOther,
			Message: "La tabla no puede quedar definida así.",
		}
	}

	return porClase(pgErr)
}

// sinSoporte desarma el 0A000, que PostgreSQL usa para cosas muy distintas.
//
// El código solo dice «esto no se puede»; CUÁL de todas las cosas que no se
// pueden lo dice únicamente el texto. Mirar el texto es frágil —está en inglés y
// puede cambiar entre versiones— y por eso se hace acá, sin apostar nada: si no
// coincide con ninguno queda el mensaje general, y el original viaja igual en
// Detail. Lo que no se puede es dejar los tres casos con la misma frase, porque
// se arreglan de tres maneras distintas.
func sinSoporte(pgErr *pgconn.PgError) *Failure {
	switch m := pgErr.Message; {
	case strings.Contains(m, "column reference in DEFAULT"):
		return &Failure{
			Kind:    FailureSyntax,
			Message: "Un valor por defecto no puede nombrar otra columna.",
			Hint: "El default se evalúa sin ninguna fila a la vista, así que no hay de dónde " +
				"sacar el otro valor. Si el valor tiene que salir de otras columnas, lo que " +
				"hace falta es una columna generada (GENERATED ALWAYS AS), no un default.",
		}

	case strings.Contains(m, "subquery in DEFAULT"):
		return &Failure{
			Kind:    FailureSyntax,
			Message: "Un valor por defecto no puede ser una consulta.",
			Hint: "Puede ser una constante o una llamada a función —now(), gen_random_uuid()—, " +
				"pero no un SELECT.",
		}

	case strings.Contains(m, "used by a view or rule"):
		return &Failure{
			Kind: FailureDependency,
			Message: "Hay una vista o una regla que usa esta columna, y PostgreSQL no le cambia " +
				"el tipo mientras exista.",
			Hint: "Hay que borrar la vista, cambiar el tipo y volver a crearla. El detalle de " +
				"abajo dice cuál es.",
		}
	}

	return &Failure{
		Kind:    FailureOther,
		Message: "PostgreSQL no permite esa operación sobre este objeto.",
	}
}

// porClase contesta con lo que se sabe por los dos primeros dígitos del
// SQLSTATE, que es la familia del error.
//
// Es el último recurso, y aun así dice algo cierto: PostgreSQL tiene cientos de
// códigos y enumerarlos todos sería copiar el apéndice de la documentación para
// que envejezca. Lo que NO puede pasar es caer en «no se pudo conectar»: el
// servidor contestó.
func porClase(pgErr *pgconn.PgError) *Failure {
	clase := ""
	if len(pgErr.Code) >= 2 {
		clase = pgErr.Code[:2]
	}
	switch clase {
	case "23":
		return &Failure{
			Kind:    FailureData,
			Message: "Los datos que ya están en la tabla no permiten este cambio.",
		}
	case "22":
		return &Failure{Kind: FailureData, Message: "Hay un valor que no es válido."}
	case "42":
		return &Failure{
			Kind:    FailureSyntax,
			Message: "PostgreSQL rechazó la sentencia.",
		}
	case "53":
		return &Failure{
			Kind:    FailureOther,
			Message: "Al servidor se le acabaron los recursos para esta sentencia.",
			Hint:    "Puede ser disco, memoria o conexiones. El detalle lo dice.",
		}
	case "55":
		return &Failure{
			Kind:    FailureLock,
			Message: "El objeto no está en condiciones de recibir este cambio ahora.",
		}
	case "40":
		return &Failure{
			Kind:    FailureOther,
			Message: "La transacción se canceló y no quedó nada aplicado.",
		}
	case "58", "XX":
		return &Failure{
			Kind:    FailureOther,
			Message: "El servidor tuvo un error interno ejecutando la sentencia.",
		}
	}
	return &Failure{Kind: FailureOther, Message: "PostgreSQL rechazó la sentencia."}
}

/* ------------------------------------------------------------- ayudantes */

// entrecomillado saca el primer nombre entre comillas dobles de un mensaje del
// servidor. PostgreSQL siempre cita así los identificadores.
var entrecomillado = regexp.MustCompile(`"([^"]+)"`)

func entreComillas(mensaje string) string {
	m := entrecomillado.FindStringSubmatch(mensaje)
	if m == nil {
		return ""
	}
	return m[1]
}

// claveDetalle lee el DETAIL de una violación de clave: PostgreSQL lo escribe
// siempre como `Key (cols)=(valores) ...`.
var claveDetalle = regexp.MustCompile(`Key \((.+?)\)=\((.*?)\)`)

// claveDelDetalle devuelve las columnas y los valores que chocaron.
//
// Es la única parte del error que dice CUÁL fila tiene el problema, y sin ella
// el mensaje obliga a buscar a mano en una tabla de doce mil filas.
func claveDelDetalle(detalle string) (columnas, valor string) {
	m := claveDetalle.FindStringSubmatch(detalle)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// relacion es el nombre completo de la tabla que el servidor señaló.
func relacion(pgErr *pgconn.PgError) string {
	if pgErr.TableName == "" {
		return "la tabla"
	}
	if pgErr.SchemaName == "" {
		return pgErr.TableName
	}
	return pgErr.SchemaName + "." + pgErr.TableName
}

// relacionSQL es la tabla lista para pegar en una consulta, citada.
//
// Va aparte de relacion porque los Hint sugieren SELECTs que alguien copia y
// corre: un nombre con mayúsculas o con espacio sin citar daría una consulta
// que no anda, y un ejemplo que no corre es peor que ninguno.
func relacionSQL(pgErr *pgconn.PgError) string {
	if pgErr.TableName == "" {
		return "<la tabla>"
	}
	return QualifiedName(pgErr.SchemaName, pgErr.TableName)
}

func comillas(s string) string {
	if s == "" {
		return "esa"
	}
	return "«" + s + "»"
}

// ident cita un identificador para poder pegarlo en la consulta que sugiere el
// Hint. Sin esto, una columna con mayúsculas daría una consulta que no corre.
func ident(s string) string {
	if s == "" {
		return `"?"`
	}
	return QuoteIdent(s)
}

func primero(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
