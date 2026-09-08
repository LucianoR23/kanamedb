package connection

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FieldError es un problema en un campo concreto. La UI lo pinta debajo del
// input correspondiente, así que Field tiene que coincidir con el nombre del
// campo en el formulario.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError junta todos los problemas de una conexión. Se devuelven todos
// juntos y no de a uno: corregir un formulario campo por campo, con un viaje al
// backend por vez, es una mala experiencia.
type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

func (v *ValidationError) Error() string {
	parts := make([]string, 0, len(v.Errors))
	for _, e := range v.Errors {
		parts = append(parts, e.Field+": "+e.Message)
	}
	return "conexión inválida — " + strings.Join(parts, "; ")
}

// Has dice si hay un error para ese campo.
func (v *ValidationError) Has(field string) bool {
	for _, e := range v.Errors {
		if e.Field == field {
			return true
		}
	}
	return false
}

const maxNameLength = 120

// Validate revisa la conexión entera y devuelve todos los problemas
// encontrados. Devuelve nil si está bien.
//
// Se espera que la conexión ya haya pasado por Normalize.
func (c Connection) Validate() error {
	errs := c.identityErrors()
	errs = append(errs, c.connectErrors()...)
	errs = append(errs, c.safetyErrors()...)
	return wrap(errs)
}

// ValidateForConnect revisa solo lo que hace falta para abrir la conexión.
//
// Existe porque el DSN no necesita ni nombre ni identificador: esos son datos
// de la libreta de conexiones, no del protocolo. Sin esta separación, armar un
// DSN exigiría cosas que no tienen nada que ver con conectar.
func (c Connection) ValidateForConnect() error {
	return wrap(c.connectErrors())
}

// identityErrors valida lo que identifica la conexión dentro de la aplicación.
func (c Connection) identityErrors() []FieldError {
	var errs []FieldError
	add := func(field, msg string) {
		errs = append(errs, FieldError{Field: field, Message: msg})
	}

	// El ID es la clave con la que la contraseña quedó guardada en el keychain
	// y la que usan Get, Update y Delete. Sin ID, todas las conexiones sin ID
	// comparten la misma entrada del keychain y las operaciones resuelven
	// siempre la primera: se edita o se borra la conexión equivocada.
	if c.ID == "" {
		add("id", "La conexión no tiene identificador.")
	}

	switch {
	case c.Name == "":
		add("name", "El nombre es obligatorio.")
	case utf8.RuneCountInString(c.Name) > maxNameLength:
		add("name", fmt.Sprintf("El nombre no puede pasar de %d caracteres.", maxNameLength))
	}

	if !c.Environment.Known() {
		add("environment", fmt.Sprintf("Entorno desconocido: %q.", c.Environment))
	}
	return errs
}

// connectErrors valida lo que hace falta para llegar al servidor.
func (c Connection) connectErrors() []FieldError {
	var errs []FieldError
	add := func(field, msg string) {
		errs = append(errs, FieldError{Field: field, Message: msg})
	}

	switch {
	case c.Engine == "":
		add("engine", "Elegí un motor.")
	case !c.Engine.Known():
		add("engine", fmt.Sprintf("Motor desconocido: %q.", c.Engine))
	case !c.Engine.Supports():
		add("engine", fmt.Sprintf("%s todavía no está implementado.", c.Engine))
	}

	// Sin base, Postgres usa el NOMBRE DEL USUARIO como base por defecto: la
	// app se conectaría en silencio a una base distinta de la que el usuario
	// cree. Es peor que fallar.
	if c.Database == "" {
		add("database", "El nombre de la base es obligatorio.")
	}

	// SQLite es un archivo: no tiene host, puerto ni usuario.
	if c.Engine == SQLite {
		return errs
	}

	if c.Host == "" {
		add("host", "El host es obligatorio.")
	}
	if c.Port < 1 || c.Port > 65535 {
		add("port", "El puerto tiene que estar entre 1 y 65535.")
	}
	if c.User == "" {
		add("user", "El usuario es obligatorio.")
	}
	if c.SSLMode != "" && !c.SSLMode.Known() {
		add("sslMode", fmt.Sprintf("Modo SSL desconocido: %q.", c.SSLMode))
	}
	return errs
}

// safetyErrors valida los números de las protecciones.
//
// Cero es "usar el default" y -1 es "sin límite"; cualquier otro negativo es un
// error de tipeo en el archivo, y dejarlo pasar significaría que el usuario
// cree haber puesto un límite que no está.
func (c Connection) safetyErrors() []FieldError {
	var errs []FieldError
	numeros := []struct {
		field string
		valor int
		label string
	}{
		{"statementTimeoutSeconds", c.Safety.StatementTimeoutSeconds, "El timeout de sentencia"},
		{"rowLimit", c.Safety.RowLimit, "El límite de filas"},
		{"idleDisconnectMinutes", c.Safety.IdleDisconnectMinutes, "La desconexión por inactividad"},
	}
	for _, n := range numeros {
		if n.valor < 0 && n.valor != Unlimited {
			errs = append(errs, FieldError{
				Field:   n.field,
				Message: fmt.Sprintf("%s no puede ser negativo. Usá 0 para el valor por defecto o %d para sin límite.", n.label, Unlimited),
			})
		}
	}
	return errs
}

func wrap(errs []FieldError) error {
	if len(errs) == 0 {
		return nil
	}
	return &ValidationError{Errors: errs}
}

// Warning es un aviso que no impide guardar, pero que la UI muestra.
type Warning struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Warnings devuelve avisos sobre configuraciones que funcionan pero son
// riesgosas. Deliberadamente no son errores: la app no está para decidir por el
// usuario, pero sí para que no se entere tarde.
func (c Connection) Warnings() []Warning {
	var w []Warning

	// Se mira el modo EFECTIVO: un SSLMode vacío no es "sin configurar", es
	// `prefer`, que ni verifica el certificado ni garantiza cifrado. Mirar el
	// campo crudo apagaba el aviso justo en el caso inseguro.
	if mode := c.EffectiveSSLMode(); c.Engine != SQLite && !mode.Verifies() {
		msg := "La conexión no verifica el certificado del servidor."
		switch mode {
		case SSLDisable:
			msg = "La conexión viaja sin cifrar."
		case SSLPrefer, SSLAllow:
			msg = "Con " + string(mode) + " la conexión no verifica el certificado y acepta seguir en claro si el servidor no ofrece TLS."
		}
		if c.Environment == Production {
			msg += " Contra producción, conviene verify-full."
		}
		w = append(w, Warning{Field: "sslMode", Message: msg})
	}

	if c.Environment == Production && !c.Safety.ReadOnly {
		w = append(w, Warning{
			Field:   "readOnly",
			Message: "Conexión de producción con escritura habilitada. Cada cambio va a pedir confirmación con el nombre de la base.",
		})
	}

	// Aplicar sin preview contra algo que no es local es cómo se borra una
	// columna sin haber leído el DDL.
	if c.Safety.AllowApplyWithoutPreview && c.Environment != Local {
		w = append(w, Warning{
			Field:   "allowApplyWithoutPreview",
			Message: "Los cambios se van a aplicar sin mostrar el SQL primero.",
		})
	}

	if c.Safety.AllowWriteWithoutConfirmation && c.Environment == Staging {
		w = append(w, Warning{
			Field:   "allowWriteWithoutConfirmation",
			Message: "Las escrituras contra staging no van a pedir confirmación.",
		})
	}

	if c.Safety.StatementTimeoutSeconds == Unlimited {
		w = append(w, Warning{
			Field:   "statementTimeoutSeconds",
			Message: "Sin timeout de sentencia: una consulta pesada puede quedar corriendo y tomando bloqueos indefinidamente.",
		})
	}

	return w
}
