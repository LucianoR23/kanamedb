// Package service orquesta los paquetes internos y es la única superficie que
// ve el frontend.
//
// Nada de acá adentro devuelve una contraseña salvo RevealPassword, que existe
// justamente para eso y se llama solo cuando el usuario lo pide. Ver CLAUDE.md.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/postgres"
	"github.com/LucianoR23/kanamedb/internal/secrets"
	"github.com/LucianoR23/kanamedb/internal/store"
)

// Connections es el servicio de la libreta de conexiones: S01, S02 y S03.
type Connections struct {
	store   *store.Store
	keyring *secrets.Keyring
}

// NewConnections arma el servicio.
func NewConnections(st *store.Store, kr *secrets.Keyring) *Connections {
	return &Connections{store: st, keyring: kr}
}

// ConnectionView es una conexión tal como la ve la interfaz: la configuración,
// más lo que la UI necesita saber y no está en el modelo.
type ConnectionView struct {
	Connection connection.Connection `json:"connection"`

	// URI es la cadena sin contraseña, para mostrar y copiar.
	URI string `json:"uri"`

	// HasPassword dice si hay credencial guardada en ESTA máquina. El archivo
	// de conexiones se sincroniza y el keychain no, así que una conexión puede
	// existir sin su contraseña.
	HasPassword bool `json:"hasPassword"`

	// KeychainRef es dónde buscarla en el gestor del sistema. Se muestra tal
	// cual y no como un slug bonito: la etiqueta no puede mentir sobre lo que
	// hay guardado.
	KeychainRef string `json:"keychainRef"`

	// Problems son los errores de validación. Una conexión editada a mano puede
	// estar rota, y el gestor tiene que poder mostrarla para que se arregle.
	Problems []connection.FieldError `json:"problems"`

	// Warnings son configuraciones válidas pero riesgosas.
	Warnings []connection.Warning `json:"warnings"`
}

// Valid dice si la conexión se puede usar.
func (v ConnectionView) Valid() bool { return len(v.Problems) == 0 }

// PasswordAction dice qué hacer con la contraseña al guardar.
//
// Es un valor con nombre y no un booleano porque el caso peligroso es el
// silencioso: abrir el editor para cambiar un nombre, dejar el campo de
// contraseña vacío y que guardar borre la credencial.
type PasswordAction string

const (
	// PasswordKeep deja la contraseña como está. Es el default del formulario.
	PasswordKeep PasswordAction = "keep"
	// PasswordSet la reemplaza por la que viene.
	PasswordSet PasswordAction = "set"
	// PasswordRemove la borra del keychain.
	PasswordRemove PasswordAction = "remove"
)

// List devuelve todas las conexiones, incluidas las que están mal configuradas.
//
// Una entrada rota se muestra con sus problemas en vez de esconderse: el
// archivo se edita a mano y el usuario necesita verla para arreglarla.
func (s *Connections) List() ([]ConnectionView, error) {
	conns, err := s.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionView, 0, len(conns))
	for _, c := range conns {
		out = append(out, s.view(c))
	}
	return out, nil
}

// Get devuelve una conexión por ID.
func (s *Connections) Get(id string) (ConnectionView, error) {
	c, err := s.store.Get(id)
	if err != nil {
		return ConnectionView{}, err
	}
	return s.view(c), nil
}

// Draft arma una conexión nueva, con ID y los valores por defecto.
//
// El ID se genera acá y no al guardar porque el editor lo necesita antes: es la
// clave con la que se va a guardar la contraseña.
func (s *Connections) Draft() (ConnectionView, error) {
	id, err := connection.NewID()
	if err != nil {
		return ConnectionView{}, err
	}
	c := connection.Connection{
		ID:     id,
		Engine: connection.Postgres,
		// Local es el default seguro: nunca asumir producción.
		Environment: connection.Local,
	}.Normalize()
	return s.view(c), nil
}

// Check valida una conexión sin guardarla. Es lo que usa el formulario para
// mostrar errores mientras se escribe.
func (s *Connections) Check(c connection.Connection) ConnectionView {
	return s.view(c.Normalize())
}

// Save guarda la conexión y aplica la acción pedida sobre la contraseña.
func (s *Connections) Save(c connection.Connection, action PasswordAction, password string) (ConnectionView, error) {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return ConnectionView{}, err
	}

	// La contraseña se guarda ANTES que la conexión: si el keychain falla, no
	// queda una conexión a medias apuntando a una credencial que no existe.
	switch action {
	case PasswordSet:
		if password == "" {
			return ConnectionView{}, errors.New("no se puede guardar una contraseña vacía; usá quitar")
		}
		if err := s.keyring.Set(c.ID, password); err != nil {
			return ConnectionView{}, err
		}
	case PasswordRemove:
		if err := s.keyring.Delete(c.ID); err != nil {
			return ConnectionView{}, err
		}
	case PasswordKeep, "":
		// No se toca.
	default:
		return ConnectionView{}, fmt.Errorf("acción de contraseña desconocida: %q", action)
	}

	// Se intenta actualizar y, si no existía, se agrega. Preguntar primero con
	// Get no serviría: Get valida, así que una conexión existente pero rota
	// —editada a mano— parecería no existir y Add fallaría por id duplicado.
	err := s.store.Update(c)
	if errors.Is(err, store.ErrNotFound) {
		err = s.store.Add(c)
	}
	if err != nil {
		return ConnectionView{}, err
	}
	return s.view(c), nil
}

// Delete borra la conexión Y su contraseña del keychain.
//
// Borrar la credencial es irreversible, así que el diálogo que lleva acá tiene
// que decirlo. Dejarla huérfana sería peor: quedaría un secreto en el sistema
// que ya nadie sabe a qué corresponde.
func (s *Connections) Delete(id string) error {
	if err := s.store.Delete(id); err != nil {
		return err
	}
	return s.keyring.Delete(id)
}

// Duplicate copia una conexión con un ID nuevo.
//
// NO copia la contraseña: el ID es la clave del keychain, y una copia con
// credencial heredada haría que borrar el original deje a la copia sin nada, o
// peor, que dos conexiones compartan un secreto sin que se note.
func (s *Connections) Duplicate(id string) (ConnectionView, error) {
	original, err := s.store.Get(id)
	if err != nil {
		return ConnectionView{}, err
	}
	nuevo, err := connection.NewID()
	if err != nil {
		return ConnectionView{}, err
	}
	copia := original
	copia.ID = nuevo
	copia.Name = original.Name + " (copia)"
	if err := s.store.Add(copia); err != nil {
		return ConnectionView{}, err
	}
	return s.view(copia), nil
}

// RevealPassword devuelve la contraseña guardada.
//
// Es la única función del paquete que devuelve un secreto, y existe porque la
// credencial se indexa por ID: en el gestor del sistema operativo el usuario
// vería `Kaname/a3f9c21b04e7d558` y no sabría cuál es.
//
// El frontend la pide SOLO cuando el usuario aprieta Reveal, nunca al abrir el
// editor: si no, abrir la conexión de producción metería esa contraseña en la
// memoria del webview sin que nadie lo pidiera.
func (s *Connections) RevealPassword(id string) (string, error) {
	return s.keyring.Get(id)
}

// ParseURI interpreta una cadena de conexión pegada.
func (s *Connections) ParseURI(raw string) (connection.Parsed, error) {
	return connection.ParseURI(raw)
}

// TestResult es el resultado de probar una conexión.
type TestResult struct {
	OK bool `json:"ok"`
	// Server viene cuando OK es true.
	Server *postgres.ServerInfo `json:"server,omitempty"`
	// Failure viene cuando OK es false, ya interpretado.
	Failure *postgres.Failure `json:"failure,omitempty"`
}

// Test prueba la conexión sin guardarla y sin dejar nada abierto.
//
// Si `action` es PasswordKeep usa la contraseña guardada; si es PasswordSet usa
// la que viene, para poder probar una contraseña nueva antes de reemplazar la
// que anda.
func (s *Connections) Test(ctx context.Context, c connection.Connection, action PasswordAction, password string) TestResult {
	c = c.Normalize()

	if action != PasswordSet {
		guardada, err := s.keyring.Get(c.ID)
		switch {
		case err == nil:
			password = guardada
		case errors.Is(err, secrets.ErrNotFound):
			// Sin contraseña guardada se prueba igual: el servidor puede estar
			// configurado con `trust` o con autenticación por otro medio, y si
			// no, el error que devuelva va a decirlo mejor que nosotros.
			password = ""
		default:
			return TestResult{Failure: &postgres.Failure{
				Kind:    postgres.FailureOther,
				Message: "No se pudo leer la contraseña del keychain.",
			}}
		}
	}

	dsn, err := c.DSN(password)
	if err != nil {
		return TestResult{Failure: &postgres.Failure{
			Kind:    postgres.FailureOther,
			Message: err.Error(),
		}}
	}

	info, failure := postgres.Probe(ctx, dsn, c.Describe())
	if failure != nil {
		return TestResult{Failure: failure}
	}
	return TestResult{OK: true, Server: info}
}

// view completa lo que la interfaz necesita además de la configuración.
func (s *Connections) view(c connection.Connection) ConnectionView {
	v := ConnectionView{
		Connection:  c,
		URI:         c.URI(),
		KeychainRef: s.keyring.Service() + " / " + c.ID,
		Warnings:    c.Warnings(),
		Problems:    []connection.FieldError{},
	}

	var ve *connection.ValidationError
	if err := c.Validate(); errors.As(err, &ve) {
		v.Problems = ve.Errors
	}

	// Un fallo del keychain no puede impedir listar las conexiones: se informa
	// como "no hay contraseña", que es lo que el usuario va a ver en la
	// práctica cuando intente conectar.
	if c.ID != "" {
		has, err := s.keyring.Has(c.ID)
		v.HasPassword = err == nil && has
	}

	if v.Warnings == nil {
		v.Warnings = []connection.Warning{}
	}
	return v
}
