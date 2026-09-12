package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/secrets"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// conClaveSSH es una conexión con túnel por clave privada, apuntando a una
// ruta que no existe: lo que llega al teléfono importado desde la PC.
func conClaveSSH(id string) connection.Connection {
	c := base(id, "con túnel")
	c.SSH = tunnel.Config{Enabled: true, Host: "bastion.local", Port: 22, User: "ops",
		Auth: connection.SSHAuthKeyFile, KeyPath: "~/.ssh/id_ed25519"}
	return c
}

// clavePEM genera una clave ed25519 en el formato de OpenSSH, cifrada con la
// frase si no está vacía.
func clavePEM(t *testing.T, frase string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var bloque *pem.Block
	if frase == "" {
		bloque, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		bloque, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(frase))
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(bloque))
}

// La clave como contenido entra al keychain bajo su propio id, la vista lo
// dice, y quitarla la saca.
func TestSetSSHKeyGuardaLaClaveYLaVistaLoDice(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(conClaveSSH("k1"), PasswordKeep, ""); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	v, err := s.Get("k1")
	if err != nil {
		t.Fatal(err)
	}
	if v.HasSSHKey {
		t.Fatal("HasSSHKey ya era true sin haber guardado nada")
	}

	if err := s.SetSSHKey("k1", clavePEM(t, "")); err != nil {
		t.Fatalf("SetSSHKey() error: %v", err)
	}
	v, _ = s.Get("k1")
	if !v.HasSSHKey {
		t.Error("HasSSHKey sigue en false después de guardar la clave")
	}
	if v.HasSSHSecret {
		t.Error("guardar la clave marcó HasSSHSecret: la frase de paso es otro secreto")
	}
	if _, err := s.keyring.Get(SSHKeySecretID("k1")); err != nil {
		t.Errorf("la clave no está en el keychain bajo SSHKeySecretID: %v", err)
	}

	if err := s.ClearSSHKey("k1"); err != nil {
		t.Fatalf("ClearSSHKey() error: %v", err)
	}
	v, _ = s.Get("k1")
	if v.HasSSHKey {
		t.Error("HasSSHKey sigue en true después de quitar la clave")
	}
}

// Lo que no es una clave privada no llega al keychain: fallaría al conectar
// con un error de SSH que no explica nada. Una clave cifrada sí entra: la
// frase de paso va aparte.
func TestSetSSHKeyRechazaLoQueNoEsUnaClave(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(conClaveSSH("k1"), PasswordKeep, ""); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	for nombre, contenido := range map[string]string{
		"vacío":            "   \n",
		"texto cualquiera": "esto no es una clave",
		"clave pública":    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGq0 nadie@ningun-lado",
	} {
		if err := s.SetSSHKey("k1", contenido); err == nil {
			t.Errorf("%s: SetSSHKey() no falló", nombre)
		}
		if hay, _ := s.keyring.Has(SSHKeySecretID("k1")); hay {
			t.Fatalf("%s: quedó algo en el keychain", nombre)
		}
	}
	if err := s.SetSSHKey("k1", clavePEM(t, "la-frase")); err != nil {
		t.Errorf("una clave cifrada tiene que aceptarse (la frase va aparte): %v", err)
	}
}

// Una conexión sin túnel por clave no tiene dónde usar el contenido.
func TestSetSSHKeySoloConTunelPorClave(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("p1", "sin túnel"), PasswordKeep, ""); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := s.SetSSHKey("p1", clavePEM(t, "")); err == nil {
		t.Error("SetSSHKey() en una conexión sin túnel no falló")
	}
	if err := s.SetSSHKey("no-existe", clavePEM(t, "")); err == nil {
		t.Error("SetSSHKey() en una conexión inexistente no falló")
	}
}

// Cambiar el túnel a contraseña, o apagarlo, borra la clave guardada: si no,
// quedaría en el keychain sin que la vista la muestre ni haya cómo quitarla,
// y al volver a «clave» se usaría sola.
func TestCambiarElAuthDelTunelBorraLaClaveGuardada(t *testing.T) {
	for nombre, cambiar := range map[string]func(*connection.Connection){
		"a contraseña":  func(c *connection.Connection) { c.SSH.Auth = connection.SSHAuthPassword },
		"túnel apagado": func(c *connection.Connection) { c.SSH.Enabled = false },
	} {
		s := nuevo(t)
		c := conClaveSSH("k1")
		if _, err := s.Save(c, PasswordKeep, ""); err != nil {
			t.Fatalf("%s: Save() error: %v", nombre, err)
		}
		if err := s.SetSSHKey("k1", clavePEM(t, "")); err != nil {
			t.Fatalf("%s: SetSSHKey() error: %v", nombre, err)
		}
		cambiar(&c)
		if _, err := s.Save(c, PasswordKeep, ""); err != nil {
			t.Fatalf("%s: Save() del cambio error: %v", nombre, err)
		}
		if hay, _ := s.keyring.Has(SSHKeySecretID("k1")); hay {
			t.Errorf("%s: la clave privada quedó huérfana en el keychain", nombre)
		}
	}
}

// Borrar la conexión borra también la clave: es el tercer secreto que puede
// quedar huérfano (K-17).
func TestDeleteBorraTambienLaClaveSSH(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(conClaveSSH("k1"), PasswordKeep, ""); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := s.SetSSHKey("k1", clavePEM(t, "")); err != nil {
		t.Fatalf("SetSSHKey() error: %v", err)
	}
	if err := s.Delete("k1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := s.keyring.Get(SSHKeySecretID("k1")); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("quedó la clave privada en el keychain: %v", err)
	}
}

// Al armar los secretos del túnel, la clave guardada viaja como contenido y la
// frase de paso como Passphrase; sin clave guardada el contenido queda nil y
// se lee KeyPath. Un keychain roto es un fallo, no «no está».
func TestSecretosDelTunelUsanLaClaveGuardada(t *testing.T) {
	kr := newFakeKeyring()
	c := conClaveSSH("k1")

	sec, f := secretosDelTunel(kr, c, "la-frase")
	if f != nil {
		t.Fatalf("sin clave guardada dio fallo: %v", f)
	}
	if sec.PrivateKey != nil || sec.Passphrase != "la-frase" {
		t.Errorf("sin clave guardada: PrivateKey=%v Passphrase=%q", sec.PrivateKey != nil, sec.Passphrase)
	}

	clave := clavePEM(t, "")
	if err := kr.Set(SSHKeySecretID("k1"), clave); err != nil {
		t.Fatal(err)
	}
	sec, f = secretosDelTunel(kr, c, "la-frase")
	if f != nil {
		t.Fatalf("con clave guardada dio fallo: %v", f)
	}
	if string(sec.PrivateKey) != clave {
		t.Error("PrivateKey no es la clave guardada")
	}
	if sec.Passphrase != "la-frase" {
		t.Errorf("Passphrase = %q", sec.Passphrase)
	}

	// Con contraseña no hay clave que buscar, aunque haya una guardada.
	c.SSH.Auth = connection.SSHAuthPassword
	sec, _ = secretosDelTunel(kr, c, "pw")
	if sec.PrivateKey != nil || sec.Password != "pw" {
		t.Error("con auth por contraseña se buscó la clave privada")
	}

	// Keychain que falla: fallo explícito, no conectar sin clave.
	c.SSH.Auth = connection.SSHAuthKeyFile
	kr.failGet = errors.New("keychain bloqueado")
	if _, f := secretosDelTunel(kr, c, ""); f == nil || !strings.Contains(f.Message, "clave privada") {
		t.Errorf("con el keychain roto no hubo fallo claro: %+v", f)
	}
}
