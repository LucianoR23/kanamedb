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
	"strings"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/secrets"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// Keyring es lo que este paquete necesita del almacén de credenciales.
//
// Es una interfaz y no el tipo concreto por una razón que no es de estilo: la
// integración con el keychain del sistema vive en internal/secrets y se prueba
// ahí, contra el almacén real. Los tests de este paquete son de orquestación y
// no tienen por qué tocar el Credential Manager — cuando lo hacían, dos
// paquetes de test corriendo en procesos paralelos se pisaban y el resultado
// era intermitente. También permite que estos tests corran en un CI sin
// keychain, como el job de Linux.
type Keyring interface {
	Set(connectionID, password string) error
	Get(connectionID string) (string, error)
	Has(connectionID string) (bool, error)
	Delete(connectionID string) error
	Service() string
}

// Connections es el servicio de la libreta de conexiones: S01, S02 y S03.
type Connections struct {
	store   *store.Store
	keyring Keyring
	// known hace falta para probar una conexión con túnel: la prueba abre el
	// salto, y abrirlo exige que la clave del bastión esté aceptada.
	known *tunnel.KnownHosts
}

// NewConnections arma el servicio.
func NewConnections(st *store.Store, kr Keyring, known *tunnel.KnownHosts) *Connections {
	return &Connections{store: st, keyring: kr, known: known}
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

	// HasSSHSecret dice si el bastión tiene su secreto guardado. Es
	// independiente del de la base: se puede tener una y no el otro.
	HasSSHSecret bool `json:"hasSSHSecret"`

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

// DraftSQLite arma la conexión de un archivo que el usuario acaba de elegir.
//
// Es el atajo «Abrir archivo SQLite…» de S01 y S02: el selector de archivo ya
// existe en S03, así que lo único que falta es no obligar a pasar por el
// formulario entero —elegir el motor, tipear la ruta, inventar un nombre— para
// abrir una base que es un archivo y nada más.
//
// Devuelve un borrador y NO lo guarda. El atajo termina en el editor, con todo
// completo y un clic para conectar, en vez de escribir una conexión en la
// libreta sin que el usuario la haya visto. Abrir un archivo para mirarlo no
// debería dejar rastro en la lista sin avisar.
//
// El nombre sale del archivo porque el nombre es obligatorio y porque
// «northwind» es lo que la persona iba a escribir igual. Puede repetirse con
// otra conexión y no importa: lo que identifica una conexión es el ID.
func (s *Connections) DraftSQLite(path string) (ConnectionView, error) {
	v, err := s.Draft()
	if err != nil {
		return ConnectionView{}, err
	}
	c := v.Connection
	c.Engine = connection.SQLite
	c.Database = path
	c.Name = nombreDeArchivo(path)
	return s.Check(c), nil
}

// nombreDeArchivo saca un nombre de conexión de una ruta.
//
// `C:\datos\northwind.db` da «northwind».
//
// Corta en los DOS separadores a mano en vez de usar `filepath`, y no es
// pedantería: `filepath` usa el separador del sistema donde corre el programa,
// así que en Linux `filepath.Base` de una ruta de Windows devuelve la ruta
// ENTERA. La libreta de conexiones es un archivo que se sincroniza entre
// máquinas, y CI corre estos tests en Linux: una ruta de Windows tiene que
// leerse igual en los dos lados.
//
// Los dos casos raros son reales. Muchas bases de SQLite no tienen extensión, y
// un archivo puede llamarse `.db` —todo extensión y nada de nombre—; sacarle la
// extensión ahí dejaría la cadena vacía y el editor abriría con el campo del
// nombre en rojo, que es justo lo que este atajo viene a evitar.
// No recorta los espacios: `Normalize` ya lo hace con el nombre, y una línea
// que ningún caso puede volver roja es una línea que sobra.
func nombreDeArchivo(ruta string) string {
	base := ruta
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

// Check valida una conexión sin guardarla. Es lo que usa el formulario para
// mostrar errores mientras se escribe.
func (s *Connections) Check(c connection.Connection) ConnectionView {
	return s.view(c.Normalize())
}

// Save guarda la conexión y aplica la acción pedida sobre la contraseña.
func (s *Connections) Save(c connection.Connection, action PasswordAction, password string) (ConnectionView, error) {
	return s.SaveWithSSH(c, action, password, PasswordKeep, "")
}

// SaveWithSSH guarda la conexión, su contraseña y el secreto del bastión.
//
// Son dos secretos con su propia acción cada uno porque son independientes: se
// puede cambiar la contraseña de la base sin tocar la del salto, y al revés.
// Una sola acción para los dos obligaría a reescribir uno para cambiar el otro.
func (s *Connections) SaveWithSSH(
	c connection.Connection,
	action PasswordAction, password string,
	sshAction PasswordAction, sshSecret string,
) (ConnectionView, error) {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return ConnectionView{}, err
	}

	// Los secretos se guardan ANTES que la conexión: si el keychain falla, no
	// queda una conexión a medias apuntando a una credencial que no existe.
	if err := s.aplicarSecreto(c.ID, action, password); err != nil {
		return ConnectionView{}, err
	}
	if err := s.aplicarSecreto(SSHSecretID(c.ID), sshAction, sshSecret); err != nil {
		return ConnectionView{}, err
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

// aplicarSecreto guarda, borra o deja como está un secreto del keychain.
func (s *Connections) aplicarSecreto(id string, action PasswordAction, secreto string) error {
	switch action {
	case PasswordSet:
		if secreto == "" {
			return errors.New("no se puede guardar un secreto vacío; usá quitar")
		}
		return s.keyring.Set(id, secreto)
	case PasswordRemove:
		return s.keyring.Delete(id)
	case PasswordKeep, "":
		return nil
	default:
		return fmt.Errorf("acción de contraseña desconocida: %q", action)
	}
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
	// Los dos secretos: el de la base y el del bastión. Dejar uno huérfano
	// sería un secreto en el sistema que ya nadie sabe a qué corresponde.
	if err := s.keyring.Delete(id); err != nil {
		return err
	}
	return s.keyring.Delete(SSHSecretID(id))
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

// RevealSSHSecret devuelve el secreto del bastión: contraseña o frase de paso.
//
// Va aparte de RevealPassword y no con un parámetro que elija, para que en el
// código de la interfaz se lea cuál de los dos secretos se está pidiendo.
func (s *Connections) RevealSSHSecret(id string) (string, error) {
	return s.keyring.Get(SSHSecretID(id))
}

// ParseURI interpreta una cadena de conexión pegada.
func (s *Connections) ParseURI(raw string) (connection.Parsed, error) {
	return connection.ParseURI(raw)
}

// TestResult es el resultado de probar una conexión.
type TestResult struct {
	OK bool `json:"ok"`
	// Server viene cuando OK es true.
	Server *engine.ServerInfo `json:"server,omitempty"`
	// Failure viene cuando OK es false, ya interpretado.
	Failure *engine.Failure `json:"failure,omitempty"`
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
			return TestResult{Failure: &engine.Failure{
				Kind:    engine.FailureOther,
				Message: "No se pudo leer la contraseña del keychain.",
			}}
		}
	}

	dsn, err := c.DSN(password)
	if err != nil {
		return TestResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: err.Error(),
		}}
	}

	// Con túnel, la prueba pasa por el túnel. Probar directo daría un error de
	// red que no menciona el bastión — o peor, funcionaría desde una red donde
	// la base es alcanzable y daría por buena una configuración que en otra
	// máquina no va a andar.
	var dial engine.DialFunc
	if c.SSH.Enabled {
		cli, f := s.abrirTunelParaProbar(ctx, c)
		if f != nil {
			return TestResult{Failure: f}
		}
		defer cli.Close()
		dial = cli.DialContext
	}

	info, failure := probarMotor(ctx, c, dsn, dial)
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

		// Solo se pregunta por el secreto del bastión si la conexión usa túnel:
		// sin túnel no hay nada que guardar, y consultar el keychain de más
		// pide una interacción del sistema por cada conexión de la lista.
		if c.SSH.Enabled {
			hasSSH, err := s.keyring.Has(SSHSecretID(c.ID))
			v.HasSSHSecret = err == nil && hasSSH
		}
	}

	if v.Warnings == nil {
		v.Warnings = []connection.Warning{}
	}
	return v
}

// abrirTunelParaProbar levanta el salto SSH para una prueba de conexión.
//
// Si la clave del bastión todavía no está aceptada, no se conecta ni se
// pregunta acá: probar es una acción del editor y la verificación de la clave es
// un diálogo bloqueante que vive en otro lugar. Se devuelve un fallo que dice
// exactamente qué hacer, en vez de un error de SSH que no lo diría.
func (s *Connections) abrirTunelParaProbar(
	ctx context.Context, c connection.Connection,
) (*tunnel.Client, *engine.Failure) {
	secreto, err := s.keyring.Get(SSHSecretID(c.ID))
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return nil, &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "No se pudo leer el secreto del bastión del keychain.",
		}
	}
	sec := tunnel.Secrets{}
	switch c.SSH.Auth {
	case connection.SSHAuthPassword:
		sec.Password = secreto
	case connection.SSHAuthKeyFile:
		sec.Passphrase = secreto
	}

	insp, err := tunnel.Inspect(ctx, c.SSH, s.known)
	if err != nil {
		return nil, &engine.Failure{
			Kind:    engine.FailureTunnel,
			Message: "No se pudo contactar al bastión SSH.",
			Detail:  engine.Redact(err.Error()),
			Hint:    "Revisá el host y el puerto de la pestaña Túnel SSH, y que el servidor esté escuchando.",
		}
	}
	if insp.Verdict != tunnel.VerdictTrusted {
		return nil, &engine.Failure{
			Kind:    engine.FailureTunnel,
			Message: "La clave del bastión todavía no está verificada.",
			Detail:  insp.Presented.Fingerprint,
			Hint:    "Guardá la conexión y conectá: ahí se muestra la huella para que la verifiques antes de enviar ninguna credencial.",
		}
	}

	cli, err := tunnel.Dial(ctx, c.SSH, s.known, sec, tunnel.DialOptions{})
	if err != nil {
		return nil, &engine.Failure{
			Kind:    engine.FailureTunnel,
			Message: "No se pudo abrir el túnel SSH.",
			Detail:  engine.Redact(err.Error()),
			Hint:    "Revisá el usuario y el método de autenticación en la pestaña Túnel SSH.",
		}
	}
	return cli, nil
}
