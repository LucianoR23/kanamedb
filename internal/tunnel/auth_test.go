package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// De los tres métodos de autenticación, el de contraseña es el que usan los
// demás tests. Estos cubren los otros dos, que es donde vive la mayor parte del
// riesgo: la clave privada y la frase de paso son secretos que no pueden
// terminar en un log ni en un mensaje de error.

// Autenticación con una clave privada en disco, de punta a punta: se genera el
// par, se instala la pública en el servidor y se conecta con la privada.
func TestDialConClavePrivadaEnDisco(t *testing.T) {
	cfg, kh := configDePrueba(t)
	aceptar(t, cfg, kh)

	priv, pub := parDeClaves(t)
	instalarClaveAutorizada(t, cfg, kh, pub)

	ruta := escribirClave(t, priv, "")

	conClave := cfg
	conClave.Auth = AuthKeyFile
	conClave.KeyPath = ruta

	c, err := Dial(context.Background(), conClave, kh, Secrets{}, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() con clave privada falló: %v", err)
	}
	defer c.Close()
}

// La misma clave, pero como contenido en vez de ruta: es cómo llega en el
// teléfono, donde no hay ~/.ssh y la clave vive en el keychain. KeyPath apunta
// a algo que no existe a propósito, para probar que con contenido no se lee
// nada del disco.
func TestDialConClavePrivadaComoContenido(t *testing.T) {
	cfg, kh := configDePrueba(t)
	aceptar(t, cfg, kh)

	priv, pub := parDeClaves(t)
	instalarClaveAutorizada(t, cfg, kh, pub)
	ruta := escribirClave(t, priv, "")
	contenido, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}

	conClave := cfg
	conClave.Auth = AuthKeyFile
	conClave.KeyPath = filepath.Join(t.TempDir(), "no-existe")

	c, err := Dial(context.Background(), conClave, kh, Secrets{PrivateKey: contenido}, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() con la clave como contenido falló: %v", err)
	}
	defer c.Close()

	// Y sin contenido, esa ruta inexistente tiene que fallar: si no fallara,
	// el test de arriba no probaría que el contenido se usó.
	if _, err := Dial(context.Background(), conClave, kh, Secrets{}, DialOptions{}); err == nil {
		t.Fatal("Dial() con una ruta inexistente y sin contenido no falló")
	}
}

// Una clave cifrada sin la frase de paso tiene que decir eso, y no un error
// críptico de parseo: es la diferencia entre "te falta la frase" y "tu clave
// está rota".
func TestUnaClaveCifradaPideLaFraseDePaso(t *testing.T) {
	cfg, kh := configDePrueba(t)
	priv, _ := parDeClaves(t)
	ruta := escribirClave(t, priv, "la-frase")

	conClave := cfg
	conClave.Auth = AuthKeyFile
	conClave.KeyPath = ruta

	_, err := Dial(context.Background(), conClave, kh, Secrets{}, DialOptions{})
	if err == nil {
		t.Fatal("una clave cifrada sin frase de paso no dio error")
	}
	if !strings.Contains(err.Error(), "frase de paso") {
		t.Errorf("el error no dice que falta la frase de paso: %v", err)
	}
}

// Y con la frase correcta, entra.
func TestUnaClaveCifradaSeAbreConSuFrase(t *testing.T) {
	cfg, kh := configDePrueba(t)
	aceptar(t, cfg, kh)

	priv, pub := parDeClaves(t)
	instalarClaveAutorizada(t, cfg, kh, pub)
	ruta := escribirClave(t, priv, "la-frase")

	conClave := cfg
	conClave.Auth = AuthKeyFile
	conClave.KeyPath = ruta

	c, err := Dial(context.Background(), conClave, kh, Secrets{Passphrase: "la-frase"}, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() con la frase correcta falló: %v", err)
	}
	defer c.Close()
}

// El contenido de la clave privada no puede aparecer en ningún error.
//
// x/crypto puede citar partes del archivo al fallar el parseo, y el archivo ES
// la clave privada. Por eso los errores de esa ruta se envuelven con un mensaje
// propio en vez de devolverse tal cual.
func TestUnErrorDeClaveNoFiltraSuContenido(t *testing.T) {
	cfg, kh := configDePrueba(t)

	// Un archivo que parece una clave y no lo es, con una marca reconocible.
	const marca = "MARCA-QUE-NO-DEBE-APARECER"
	ruta := filepath.Join(t.TempDir(), "rota")
	contenido := "-----BEGIN OPENSSH PRIVATE KEY-----\n" + marca + "\n-----END OPENSSH PRIVATE KEY-----\n"
	if err := os.WriteFile(ruta, []byte(contenido), 0o600); err != nil {
		t.Fatalf("escribir el archivo: %v", err)
	}

	conClave := cfg
	conClave.Auth = AuthKeyFile
	conClave.KeyPath = ruta

	_, err := Dial(context.Background(), conClave, kh, Secrets{}, DialOptions{})
	if err == nil {
		t.Fatal("una clave ilegible no dio error")
	}
	if strings.Contains(err.Error(), marca) {
		t.Errorf("el error filtró contenido del archivo de la clave: %v", err)
	}
	// Y sí dice de qué archivo se trata, que no es secreto y hace falta para
	// saber cuál arreglar.
	if !strings.Contains(err.Error(), ruta) {
		t.Errorf("el error no dice qué archivo no se pudo leer: %v", err)
	}
}

// El agente no se puede levantar desde un test, así que este corre solo donde
// hay uno y se saltea en el resto.
//
// Queda dicho sin vueltas: de los tres métodos, el agente es el que menos
// cobertura automática tiene, y es el default. En CI no corre.
func TestDialConElAgenteDelSistema(t *testing.T) {
	cfg, kh := configDePrueba(t)

	signers, err := agentSigners()
	if err != nil {
		t.Skipf("no hay agente SSH con claves en esta máquina: %v", err)
	}
	if len(signers) == 0 {
		t.Skip("el agente no tiene claves cargadas")
	}

	aceptar(t, cfg, kh)
	instalarClaveAutorizada(t, cfg, kh, signers[0].PublicKey())

	conAgente := cfg
	conAgente.Auth = AuthAgent

	c, err := Dial(context.Background(), conAgente, kh, Secrets{}, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() con el agente falló: %v", err)
	}
	defer c.Close()
}

// parDeClaves genera un par ed25519 para la prueba.
func parDeClaves(t *testing.T) (ed25519.PrivateKey, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generar el par: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("convertir la pública: %v", err)
	}
	return priv, sshPub
}

// escribirClave deja la privada en un archivo, cifrada si se pide una frase.
func escribirClave(t *testing.T, priv ed25519.PrivateKey, frase string) string {
	t.Helper()
	var bloque *pem.Block
	var err error
	if frase == "" {
		bloque, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		bloque, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(frase))
	}
	if err != nil {
		t.Fatalf("serializar la clave: %v", err)
	}
	ruta := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(ruta, pem.EncodeToMemory(bloque), 0o600); err != nil {
		t.Fatalf("escribir la clave: %v", err)
	}
	return ruta
}

// instalarClaveAutorizada agrega la pública al authorized_keys del servidor.
//
// Se hace por SSH con la contraseña, no con `docker exec`: así el test no
// depende de tener el CLI de Docker a mano y funciona igual contra cualquier
// servidor de pruebas al que se lo apunte con las variables de entorno.
func instalarClaveAutorizada(t *testing.T, cfg Config, kh *KnownHosts, pub ssh.PublicKey) {
	t.Helper()

	conPassword := cfg
	conPassword.Auth = AuthPassword
	c, err := Dial(context.Background(), conPassword, kh, Secrets{Password: sshPassword}, DialOptions{})
	if err != nil {
		t.Fatalf("conectar para instalar la clave: %v", err)
	}
	defer c.Close()

	sesion, err := c.ssh.NewSession()
	if err != nil {
		t.Fatalf("abrir sesión: %v", err)
	}
	defer sesion.Close()

	linea := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	cmd := "mkdir -p ~/.ssh && chmod 700 ~/.ssh && " +
		"printf '%s\\n' '" + linea + "' >> ~/.ssh/authorized_keys && " +
		"chmod 600 ~/.ssh/authorized_keys"
	if out, err := sesion.CombinedOutput(cmd); err != nil {
		t.Fatalf("instalar la clave autorizada: %v (%s)", err, out)
	}
}
