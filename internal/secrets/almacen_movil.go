package secrets

import "fmt"

// almacenSeguro es el almacenamiento de secretos del teléfono con un solo
// espacio de claves. En Android lo implementa el vault (KanameVault.java por
// JNI); la forma es la de application.MobileManager de Wails más SecureHas,
// declarada acá para que la lógica se pueda probar con un doble.
//
// Contrato: una clave que no existe devuelve ("", false, nil); borrar lo que
// no está no falla; SecureHas no descifra ni pide biometría.
type almacenSeguro interface {
	SecureSet(clave, valor string) error
	SecureGet(clave string) (valor string, hay bool, err error)
	SecureDelete(clave string) error
	SecureHas(clave string) (bool, error)
}

// almacenMovil adapta almacenSeguro al contrato de almacen.
//
// Hay un solo espacio de claves —no hay «servicio» como en el keychain de
// escritorio—, así que el servicio va dentro de la clave. Es lo que mantiene
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

func (a almacenMovil) hay(service, id string) (bool, error) {
	return a.s.SecureHas(a.clave(service, id))
}

// nuevoMovil construye un Keyring sobre un almacenSeguro concreto. Es lo que
// usa Android con el vault real y lo que usan los tests con el doble.
func nuevoMovil(service string, s almacenSeguro) *Keyring {
	return &Keyring{service: service, almacen: almacenMovil{s: s}}
}
