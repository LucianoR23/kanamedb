package tunnel

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestLaNegociacionPideElTipoDeClaveQueYaSeConoce.
//
// known_hosts guarda UNA clave por dirección y un servidor suele tener varias.
// Sin fijar HostKeyAlgorithms, agregar un tipo en el servidor o cambiar el
// orden hacía que presentara otra clave legítima y el veredicto fuera «la
// clave cambió»: cada falso positivo entrena a apretar «Reemplazar la clave»,
// el botón que un intermediario necesita (K-13).
func TestLaNegociacionPideElTipoDeClaveQueYaSeConoce(t *testing.T) {
	kh := NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"))

	if got := algoritmosPreferidos(kh, "bastion:22"); got != nil {
		t.Errorf("sin clave conocida no hay preferencia y devolvió %v", got)
	}
	if got := algoritmosPreferidos(nil, "bastion:22"); got != nil {
		t.Errorf("sin known_hosts devolvió %v", got)
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ed, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := kh.TrustKey("bastion:22", ed); err != nil {
		t.Fatal(err)
	}
	got := algoritmosPreferidos(kh, "bastion:22")
	if len(got) < 2 || got[0] != ssh.KeyAlgoED25519 {
		t.Errorf("con una ed25519 guardada tenía que pedir ssh-ed25519 PRIMERO y pidió %v", got)
	}
	// Y los demás después: un servidor que cambió de tipo de clave tiene que
	// poder presentar la nueva para que se vea el diálogo de «cambió».
	tieneRSA := false
	for _, a := range got {
		if a == ssh.KeyAlgoRSASHA256 {
			tieneRSA = true
		}
	}
	if !tieneRSA {
		t.Errorf("la lista no deja lugar a otros tipos: %v", got)
	}
	// Otra dirección no hereda nada.
	if got := algoritmosPreferidos(kh, "otro:22"); got != nil {
		t.Errorf("otra dirección devolvió %v", got)
	}

	// Con RSA se piden las firmas rsa-sha2, que son las que un servidor
	// moderno acepta para ese tipo de clave.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPub, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := kh.TrustKey("viejo:22", rsaPub); err != nil {
		t.Fatal(err)
	}
	got = algoritmosPreferidos(kh, "viejo:22")
	if len(got) < 3 || got[0] != ssh.KeyAlgoRSASHA512 || got[1] != ssh.KeyAlgoRSASHA256 || got[2] != ssh.KeyAlgoRSA {
		t.Errorf("con una RSA guardada pidió %v", got)
	}
}
