package connection

import (
	"fmt"
	"strings"
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

// Validate revisa la conexión y devuelve todos los problemas encontrados.
// Devuelve nil si está bien.
//
// Se espera que la conexión ya haya pasado por Normalize.
func (c Connection) Validate() error {
	var errs []FieldError
	add := func(field, msg string) {
		errs = append(errs, FieldError{Field: field, Message: msg})
	}

	switch {
	case c.Name == "":
		add("name", "El nombre es obligatorio.")
	case len(c.Name) > maxNameLength:
		add("name", fmt.Sprintf("El nombre no puede pasar de %d caracteres.", maxNameLength))
	}

	switch {
	case c.Engine == "":
		add("engine", "Elegí un motor.")
	case !c.Engine.Known():
		add("engine", fmt.Sprintf("Motor desconocido: %q.", c.Engine))
	case !c.Engine.Supports():
		add("engine", fmt.Sprintf("%s todavía no está implementado.", c.Engine))
	}

	if !c.Environment.Known() {
		add("environment", fmt.Sprintf("Entorno desconocido: %q.", c.Environment))
	}

	if c.Database == "" {
		add("database", "El nombre de la base es obligatorio.")
	}

	// SQLite es un archivo: no tiene host, puerto ni usuario.
	if c.Engine == SQLite {
		return wrap(errs)
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

	return wrap(errs)
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

	if c.Engine != SQLite && c.SSLMode != "" && !c.SSLMode.Verifies() {
		msg := "La conexión no verifica el certificado del servidor."
		if c.SSLMode == SSLDisable {
			msg = "La conexión viaja sin cifrar."
		}
		if c.Environment == Production {
			msg += " Contra producción, conviene verify-full."
		}
		w = append(w, Warning{Field: "sslMode", Message: msg})
	}

	if c.Environment == Production && !c.ReadOnly {
		w = append(w, Warning{
			Field:   "readOnly",
			Message: "Conexión de producción con escritura habilitada. Cada cambio va a pedir confirmación con el nombre de la base.",
		})
	}

	return w
}
