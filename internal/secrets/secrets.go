// Package secrets guarda las contraseñas de las conexiones en el keychain del
// sistema operativo: Credential Manager en Windows, Keychain en macOS y el
// Secret Service en Linux.
//
// Es el único lugar de la aplicación donde vive una credencial. No se escriben
// en el archivo de conexiones, no se guardan en el estado local, no salen en
// logs y no cruzan hacia el frontend. Ver CLAUDE.md.
//
// Consecuencia deliberada: el keychain no se sincroniza entre máquinas. El
// archivo de conexiones sí, y la contraseña se carga una vez por máquina.
package secrets

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// ErrNotFound lo devuelve Get cuando esa conexión no tiene contraseña guardada.
//
// No es una falla: una conexión puede usar autenticación por otro medio, o el
// usuario puede no haber cargado todavía la contraseña en esta máquina. Quien
// llama decide si pedirla.
var ErrNotFound = errors.New("no hay contraseña guardada para esta conexión")

// DefaultService es el nombre bajo el que aparecen las credenciales en el
// gestor del sistema. Es lo que el usuario ve al auditar qué guardó la app.
const DefaultService = "Kaname"

// maxPasswordLen es un tope defensivo. El Credential Manager de Windows corta
// el blob en 2560 bytes; cortar en 1024 deja margen y descarta temprano lo que
// evidentemente no es una contraseña.
const maxPasswordLen = 1024

// Keyring guarda y recupera contraseñas por ID de conexión.
//
// La clave es el ID de la conexión y no su nombre: el nombre cambia y el ID no,
// así que renombrar una conexión no deja una credencial huérfana.
type Keyring struct {
	service string
}

// New construye un Keyring sobre el servicio por defecto.
func New() *Keyring { return &Keyring{service: DefaultService} }

// NewWithService construye un Keyring sobre otro nombre de servicio. Existe
// para que los tests no ensucien las credenciales reales del usuario.
func NewWithService(service string) *Keyring { return &Keyring{service: service} }

// Service devuelve el nombre de servicio bajo el que guarda.
func (k *Keyring) Service() string { return k.service }

// Set guarda la contraseña de una conexión, reemplazando la anterior si había.
//
// Guardar una contraseña vacía no tiene sentido y probablemente sea un bug de
// quien llama, así que se rechaza en vez de dejar una credencial inútil.
func (k *Keyring) Set(connectionID, password string) error {
	if err := validID(connectionID); err != nil {
		return err
	}
	if password == "" {
		return errors.New("la contraseña está vacía: usá Delete para quitarla")
	}
	if len(password) > maxPasswordLen {
		return fmt.Errorf("la contraseña supera los %d bytes", maxPasswordLen)
	}
	if err := keyring.Set(k.service, connectionID, password); err != nil {
		// El error del sistema no lleva la contraseña, pero se envuelve con el
		// ID y no con el valor, por las dudas.
		return fmt.Errorf("guardar la contraseña de %s en el keychain: %w", connectionID, err)
	}
	return nil
}

// Get devuelve la contraseña de una conexión.
//
// El valor devuelto ES UN SECRETO: se usa para armar el DSN y se descarta. No se
// loguea, no se cachea y no cruza hacia el frontend.
func (k *Keyring) Get(connectionID string) (string, error) {
	if err := validID(connectionID); err != nil {
		return "", err
	}
	pw, err := keyring.Get(k.service, connectionID)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, connectionID)
	}
	if err != nil {
		return "", fmt.Errorf("leer la contraseña de %s del keychain: %w", connectionID, err)
	}
	return pw, nil
}

// Has dice si hay una contraseña guardada, sin traerla a memoria.
//
// La UI lo usa para mostrar si una conexión ya tiene credencial en esta máquina
// sin necesidad de leer el secreto.
func (k *Keyring) Has(connectionID string) (bool, error) {
	_, err := k.Get(connectionID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Delete borra la contraseña de una conexión.
//
// Borrar algo que no está no es un error: el resultado buscado —que no haya
// credencial— ya se cumple.
func (k *Keyring) Delete(connectionID string) error {
	if err := validID(connectionID); err != nil {
		return err
	}
	err := keyring.Delete(k.service, connectionID)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("borrar la contraseña de %s del keychain: %w", connectionID, err)
}

// validID rechaza identificadores que no sirven como clave.
//
// Un ID vacío haría que todas las conexiones sin ID compartan la misma entrada
// del keychain, y una con espacios o saltos de línea puede confundir a los
// backends del sistema.
func validID(connectionID string) error {
	if connectionID == "" {
		return errors.New("el id de conexión está vacío")
	}
	if strings.TrimSpace(connectionID) != connectionID {
		return fmt.Errorf("el id de conexión %q tiene espacios al principio o al final", connectionID)
	}
	if strings.ContainsAny(connectionID, "\x00\n\r") {
		return errors.New("el id de conexión tiene caracteres de control")
	}
	return nil
}
