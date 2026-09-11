package tunnel

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// El verificador es la única puerta entre "el servidor presentó una clave" y
// "se manda una credencial". Acá se lo prueba solo, sin servidor: recibe una
// clave y decide, y las seis combinaciones de known_hosts × aceptación de una
// vez × clave presentada caben en una tabla.
//
// Los tests de integración cubren dos de estas seis contra el servidor real;
// las otras cuatro —sobre todo "la clave cambió entre el diálogo y el
// conectar"— no se pueden provocar contra un servidor que no cambia de clave.
func TestElVerificadorSoloAceptaLaClaveQueSeAcepto(t *testing.T) {
	cfg := Config{Enabled: true, Host: "bastion.example", Port: 22, User: "ops", Auth: AuthAgent}
	aceptada := clavePublicaAlAzar(t)
	otra := clavePublicaAlAzar(t)
	huellaAceptada := ssh.FingerprintSHA256(aceptada)

	casos := []struct {
		nombre     string
		enArchivo  ssh.PublicKey // nil: el host no está en known_hosts
		unaVez     string        // huella pasada como AcceptOnce
		presentada ssh.PublicKey
		acepta     bool
		mensaje    string // qué tiene que decir el error cuando no acepta
	}{
		{"host desconocido, sin aceptación", nil, "", aceptada, false, "no está aceptada"},
		{"host conocido, misma clave", aceptada, "", aceptada, true, ""},
		{"host conocido, otra clave", aceptada, "", otra, false, "cambió"},
		{"una vez, la misma huella", nil, huellaAceptada, aceptada, true, ""},
		// El caso que ningún test con servidor puede provocar: la persona
		// aceptó una huella en el diálogo y al conectar el servidor presenta
		// OTRA. Aceptar "la que venga" convertiría el diálogo en teatro.
		{"una vez, otra huella, host desconocido", nil, huellaAceptada, otra, false, "no está aceptada"},
		{"una vez, otra huella, host conocido con la aceptada", aceptada, huellaAceptada, otra, false, "cambió"},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			kh := NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"))
			if c.enArchivo != nil {
				if err := kh.TrustKey(cfg.Address(), c.enArchivo); err != nil {
					t.Fatalf("TrustKey(): %v", err)
				}
			}

			err := verificador(cfg, kh, c.unaVez)(cfg.Address(), nil, c.presentada)

			if c.acepta && err != nil {
				t.Fatalf("rechazó una clave que tenía que aceptar: %v", err)
			}
			if !c.acepta {
				if err == nil {
					t.Fatal("aceptó una clave que no está aceptada")
				}
				if !strings.Contains(err.Error(), c.mensaje) {
					t.Errorf("el error no explica el motivo (%q): %v", c.mensaje, err)
				}
				// El diálogo necesita la huella presentada para mostrarla.
				if !strings.Contains(err.Error(), ssh.FingerprintSHA256(c.presentada)) {
					t.Errorf("el error no lleva la huella que presentó el servidor: %v", err)
				}
			}
		})
	}
}
