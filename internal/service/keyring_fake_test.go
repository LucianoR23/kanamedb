package service

import (
	"fmt"
	"strings"
	"sync"

	"github.com/LucianoR23/kanamedb/internal/secrets"
)

// fakeKeyring es un almacén en memoria con las mismas reglas que el real.
//
// Los tests de este paquete son de orquestación: qué se guarda antes que qué,
// qué se borra con qué. Que el keychain del sistema funcione se prueba en
// internal/secrets, contra el almacén de verdad.
type fakeKeyring struct {
	mu    sync.Mutex
	datos map[string]string
	// failSet fuerza un error, para probar que la conexión no se guarda si la
	// contraseña no se pudo guardar.
	failSet error
	// failGet fuerza un error en Get: un keychain bloqueado o roto, que no es
	// lo mismo que «no está».
	failGet error
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{datos: map[string]string{}}
}

func (f *fakeKeyring) Service() string { return "Kaname-fake" }

// validID replica el rechazo de ids que hace el keychain real. Sin esto, el
// fake sería más permisivo que la implementación y los tests pasarían por
// motivos que no valen.
func (f *fakeKeyring) validID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("el id de conexión está vacío")
	case strings.TrimSpace(id) != id:
		return fmt.Errorf("el id de conexión %q tiene espacios al principio o al final", id)
	case strings.ContainsAny(id, "\x00\n\r"):
		return fmt.Errorf("el id de conexión tiene caracteres de control")
	}
	return nil
}

func (f *fakeKeyring) Set(id, password string) error {
	if err := f.validID(id); err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("la contraseña está vacía")
	}
	if f.failSet != nil {
		return f.failSet
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.datos[id] = password
	return nil
}

func (f *fakeKeyring) Get(id string) (string, error) {
	if err := f.validID(id); err != nil {
		return "", err
	}
	if f.failGet != nil {
		return "", f.failGet
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pw, ok := f.datos[id]
	if !ok {
		return "", fmt.Errorf("%w: %s", secrets.ErrNotFound, id)
	}
	return pw, nil
}

func (f *fakeKeyring) Has(id string) (bool, error) {
	if _, err := f.Get(id); err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Delete es idempotente, igual que el real.
func (f *fakeKeyring) Delete(id string) error {
	if err := f.validID(id); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.datos, id)
	return nil
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), secrets.ErrNotFound.Error())
}

// El keychain real tiene que seguir satisfaciendo la interfaz: si alguien le
// cambia una firma, esto no compila en vez de romper recién en main.
var _ Keyring = (*secrets.Keyring)(nil)

// Y el fake también, para que no se separe de lo que el servicio espera.
var _ Keyring = (*fakeKeyring)(nil)
