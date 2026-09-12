package secrets

import "fmt"

// almacenSeguro es lo que Wails expone en el teléfono para guardar secretos:
// en Android, EncryptedSharedPreferences con una clave AES del Keystore. Es el
// subconjunto de application.MobileManager que se usa, declarado acá para que
// la lógica de arriba se pueda probar con un doble sin arrastrar Wails.
//
// Contrato heredado de Wails: un valor vacío guardado es válido (hay=true);
// una clave que no existe devuelve ("", false, nil); borrar lo que no está no
// falla.
type almacenSeguro interface {
	SecureSet(clave, valor string) error
	SecureGet(clave string) (valor string, hay bool, err error)
	SecureDelete(clave string) error
}

// almacenMovil adapta almacenSeguro al contrato de almacen.
//
// Wails tiene un solo espacio de claves —no hay «servicio» como en el keychain
// de escritorio—, así que el servicio va dentro de la clave. Es lo que mantiene
// aislados a los tests del secreto real del usuario también acá.
type almacenMovil struct {
	s almacenSeguro
}

// clave arma la entrada a partir del servicio y el id. Con prefijo de longitud
// y no con un separador: el id no puede traer caracteres de control (validID),
// pero el servicio es texto libre —los tests usan t.Name(), que lleva «/»—, y
// cualquier separador dejaría ambiguo dónde termina uno y empieza el otro.
// Con la longitud adelante no hay separador que adivinar.
func (almacenMovil) clave(service, id string) string {
	return fmt.Sprintf("%d:%s:%s", len(service), service, id)
}

func (a almacenMovil) guardar(service, id, secreto string) error {
	return a.s.SecureSet(a.clave(service, id), secreto)
}

func (a almacenMovil) leer(service, id string) (string, bool, error) {
	return a.s.SecureGet(a.clave(service, id))
}

func (a almacenMovil) borrar(service, id string) error {
	return a.s.SecureDelete(a.clave(service, id))
}

// nuevoMovil construye un Keyring sobre un almacenSeguro concreto. Es lo que
// usa Android con el bridge real y lo que usan los tests con el doble.
func nuevoMovil(service string, s almacenSeguro) *Keyring {
	return &Keyring{service: service, almacen: almacenMovil{s: s}}
}
