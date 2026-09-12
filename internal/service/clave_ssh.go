package service

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/secrets"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// La clave privada SSH como contenido.
//
// En escritorio la clave es una ruta (`KeyPath`) y se lee del disco al
// conectar. En el teléfono no hay ~/.ssh ni forma razonable de apuntar a un
// archivo: la clave se guarda en el keychain —cifrada por el vault, con la
// misma biometría que una contraseña— y al conectar se usa ese contenido en
// lugar de leer la ruta. Es el punto 4 de «Seguridad» en kaname-android.md.
//
// No es exclusivo de Android: el backend es el mismo en todas partes, y
// en escritorio el keychain simplemente tiene un tope más bajo (una clave
// ed25519 entra; una RSA de 4096 no, y el error lo dice).

// SetSSHKey guarda la clave privada de una conexión como contenido.
//
// Se acepta lo que x/crypto/ssh reconoce como clave privada, cifrada o no: si
// está cifrada, la frase de paso es el secreto del bastión de siempre
// (SSHSecretID). Lo que no parsea se rechaza antes de tocar el keychain, para
// no guardar como «clave» un archivo cualquiera que después falla al conectar
// con un error de SSH que no explica nada.
func (s *Connections) SetSSHKey(id, pem string) error {
	c, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if !c.SSH.Enabled || c.SSH.Auth != connection.SSHAuthKeyFile {
		return errors.New("esta conexión no usa una clave privada para el túnel SSH")
	}
	pem = strings.TrimSpace(pem)
	if pem == "" {
		return errors.New("la clave privada está vacía; usá ClearSSHKey para quitarla")
	}
	if _, err := ssh.ParseRawPrivateKey([]byte(pem)); err != nil {
		var falta *ssh.PassphraseMissingError
		if !errors.As(err, &falta) {
			// El error de x/crypto puede citar el contenido: no se envuelve.
			return errors.New("eso no es una clave privada SSH que Kaname sepa leer (OpenSSH, PEM de RSA/EC/ed25519, PKCS#8)")
		}
	}
	if err := s.keyring.Set(SSHKeySecretID(id), pem+"\n"); err != nil {
		return fmt.Errorf("guardar la clave privada SSH: %w", err)
	}
	return nil
}

// ClearSSHKey quita la clave privada guardada como contenido. Volver a
// conectar lee KeyPath del disco, como en escritorio.
func (s *Connections) ClearSSHKey(id string) error {
	if _, err := s.store.Get(id); err != nil {
		return err
	}
	if err := s.keyring.Delete(SSHKeySecretID(id)); err != nil {
		return fmt.Errorf("quitar la clave privada SSH: %w", err)
	}
	return nil
}

// secretosDelTunel arma lo que el túnel necesita para autenticarse a partir del
// secreto del bastión ya leído y, con clave privada, de la clave como
// contenido si está guardada. Es lo que comparten Connect y Test.
func secretosDelTunel(kr Keyring, c connection.Connection, secreto string) (tunnel.Secrets, *engine.Failure) {
	sec := tunnel.Secrets{}
	switch c.SSH.Auth {
	case connection.SSHAuthPassword:
		sec.Password = secreto
	case connection.SSHAuthKeyFile:
		sec.Passphrase = secreto
		clave, err := kr.Get(SSHKeySecretID(c.ID))
		switch {
		case err == nil:
			sec.PrivateKey = []byte(clave)
		case errors.Is(err, secrets.ErrNotFound):
			// Sin contenido se lee KeyPath del disco, como siempre.
		default:
			return tunnel.Secrets{}, &engine.Failure{
				Kind:    engine.FailureOther,
				Message: "No se pudo leer la clave privada SSH del keychain.",
			}
		}
	}
	return sec, nil
}
