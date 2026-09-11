package tunnel

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Los dos secretos que el túnel recibe —la contraseña del bastión y la frase de
// paso de la clave— no pueden aparecer en el error cuando son incorrectos. Es
// cuando más probable es que alguien copie el error a un ticket.
//
// La contraseña viaja de verdad: el servidor la rechaza y el error lo escribe
// x/crypto. La frase de paso no llega a la red —la clave se descifra antes de
// discar— y el error lo escribe también x/crypto, que puede citar lo que le
// dieron. Por eso los dos se prueban con un centinela que no está en ningún
// otro lado.
func TestLosErroresDeAutenticacionNoLlevanElSecreto(t *testing.T) {
	cfg, kh := configDePrueba(t)
	aceptar(t, cfg, kh)

	const centinela = "zz-centinela-ssh-4c1f8e"

	t.Run("contraseña incorrecta", func(t *testing.T) {
		_, err := Dial(context.Background(), cfg, kh, Secrets{Password: centinela}, DialOptions{})
		if err == nil {
			t.Fatal("el servidor aceptó una contraseña incorrecta: el test no prueba nada")
		}
		sinSecreto(t, err, centinela)
	})

	t.Run("frase de paso incorrecta", func(t *testing.T) {
		priv, _ := parDeClaves(t)
		conClave := cfg
		conClave.Auth = AuthKeyFile
		conClave.KeyPath = escribirClave(t, priv, "la-frase-buena")

		_, err := Dial(context.Background(), conClave, kh, Secrets{Passphrase: centinela}, DialOptions{})
		if err == nil {
			t.Fatal("una frase de paso incorrecta abrió la clave: el test no prueba nada")
		}
		sinSecreto(t, err, centinela)
	})
}

// sinSecreto mira el error de todas las formas en que un error se convierte en
// texto: la que usa un toast, la que usa un log y la que usa alguien
// depurando.
func sinSecreto(t *testing.T, err error, centinela string) {
	t.Helper()
	for nombre, texto := range map[string]string{
		"Error()": err.Error(),
		"%v":      fmt.Sprintf("%v", err),
		"%+v":     fmt.Sprintf("%+v", err),
		"%#v":     fmt.Sprintf("%#v", err),
	} {
		if strings.Contains(texto, centinela) {
			t.Errorf("%s lleva el secreto: %s", nombre, strings.ReplaceAll(texto, centinela, "«el secreto»"))
		}
	}
}
