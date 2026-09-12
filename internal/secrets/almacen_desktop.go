//go:build !android

package secrets

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// almacenDelSistema en escritorio es el keychain del sistema operativo:
// Credential Manager en Windows, Keychain en macOS, Secret Service en Linux.
func almacenDelSistema() almacen { return almacenKeyring{} }

// almacenKeyring es go-keyring tal cual, con el "no está" traducido a hay=false
// para que Keyring no tenga que conocer el error del paquete.
type almacenKeyring struct{}

func (almacenKeyring) guardar(service, id, secreto string) error {
	return keyring.Set(service, id, secreto)
}

func (almacenKeyring) leer(service, id string) (string, bool, error) {
	pw, err := keyring.Get(service, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return pw, true, nil
}

// hay lee: go-keyring no tiene forma de preguntar sin traer el valor, y en
// escritorio eso no le cuesta nada a nadie.
func (a almacenKeyring) hay(service, id string) (bool, error) {
	_, hay, err := a.leer(service, id)
	return hay, err
}

func (almacenKeyring) borrar(service, id string) error {
	err := keyring.Delete(service, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
