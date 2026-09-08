package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// El servidor SSH de docker-compose.test.yml.
const (
	sshHostDefault = "127.0.0.1"
	sshPortDefault = 52222
	sshUser        = "kaname"
	sshPassword    = "kaname"
)

// configDePrueba arma la configuración contra el servidor de pruebas, o saltea
// el test si no hay ninguno escuchando.
//
// Mismo criterio que internal/postgres: sin build tag, para que el código de
// test siempre compile y pase vet; y con KANAME_REQUIRE_SSH para que en CI un
// servidor ausente sea un fallo y no un verde sin haber probado nada.
func configDePrueba(t *testing.T) (Config, *KnownHosts) {
	t.Helper()

	host := sshHostDefault
	port := sshPortDefault
	if v := os.Getenv("KANAME_TEST_SSH_HOST"); v != "" {
		host = v
	}
	if v := os.Getenv("KANAME_TEST_SSH_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("KANAME_TEST_SSH_PORT=%q no es un número", v)
		}
		port = n
	}

	cfg := Config{
		Enabled: true,
		Host:    host,
		Port:    port,
		User:    sshUser,
		Auth:    AuthPassword,
	}

	// known_hosts vacío en un directorio propio del test: cada test arranca sin
	// nada aceptado, que es la única forma de probar el primer contacto.
	kh := NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Inspect(ctx, cfg, kh); err != nil {
		if os.Getenv("KANAME_REQUIRE_SSH") != "" {
			t.Fatalf("KANAME_REQUIRE_SSH está puesto y no hay servidor SSH: %v", err)
		}
		t.Skipf("no hay servidor SSH de pruebas en %s:%d (%v).\n"+
			"Levantalo con: docker compose -f docker-compose.test.yml up -d", host, port, err)
	}
	return cfg, kh
}

// La primera vez que se ve un host no se confía en él solo.
func TestInspectUnHostNuevoEsDesconocido(t *testing.T) {
	cfg, kh := configDePrueba(t)

	insp, err := Inspect(context.Background(), cfg, kh)
	if err != nil {
		t.Fatalf("Inspect() error: %v", err)
	}
	if insp.Verdict != VerdictUnknown {
		t.Errorf("Verdict = %q, se esperaba %q", insp.Verdict, VerdictUnknown)
	}
	if !strings.HasPrefix(insp.Presented.Fingerprint, "SHA256:") {
		t.Errorf("la huella no tiene el formato de ssh-keygen: %q", insp.Presented.Fingerprint)
	}
	if insp.Presented.Algorithm == "" {
		t.Error("no vino el algoritmo de la clave")
	}
	if insp.Known != nil {
		t.Errorf("un host nuevo no puede traer una clave previa: %+v", insp.Known)
	}
}

// LA prueba de la iteración: inspeccionar no autentica.
//
// El diálogo de S04 dice "no credentials sent yet" y eso tiene que ser cierto.
// Se usa una contraseña deliberadamente equivocada: si Inspect autenticara,
// esto fallaría. Que devuelva la clave del host con una contraseña incorrecta
// demuestra que el protocolo se detuvo antes de la autenticación.
func TestInspectNoManeraCredenciales(t *testing.T) {
	cfg, kh := configDePrueba(t)
	cfg.User = "usuario-que-no-existe"

	insp, err := Inspect(context.Background(), cfg, kh)
	if err != nil {
		t.Fatalf("Inspect() con credenciales inválidas falló, así que autenticó: %v", err)
	}
	if insp.Presented.Fingerprint == "" {
		t.Fatal("no se obtuvo la clave del host")
	}

	// Y para que no queden dudas: con esas mismas credenciales, conectar de
	// verdad falla. O sea que el éxito de arriba no fue porque las credenciales
	// sirvieran.
	if err := kh.Trust(cfg.Address(), insp.AuthorizedKey); err != nil {
		t.Fatalf("Trust() error: %v", err)
	}
	_, err = Dial(context.Background(), cfg, kh, Secrets{Password: "tampoco"}, DialOptions{})
	if err == nil {
		t.Fatal("Dial() con usuario y contraseña inválidos no falló")
	}
}

// Aceptar una clave y volver a mirar tiene que dar "ya conocida". Si diera
// desconocida de nuevo, el diálogo aparecería en cada conexión y eso entrena a
// apretar "confiar" sin leer.
func TestTrustHaceQueLaSiguienteInspeccionSeaConocida(t *testing.T) {
	cfg, kh := configDePrueba(t)

	insp, err := Inspect(context.Background(), cfg, kh)
	if err != nil {
		t.Fatalf("Inspect() error: %v", err)
	}
	if err := kh.Trust(cfg.Address(), insp.AuthorizedKey); err != nil {
		t.Fatalf("Trust() error: %v", err)
	}

	segunda, err := Inspect(context.Background(), cfg, kh)
	if err != nil {
		t.Fatalf("Inspect() error: %v", err)
	}
	if segunda.Verdict != VerdictTrusted {
		t.Errorf("Verdict = %q, se esperaba %q", segunda.Verdict, VerdictTrusted)
	}
	if segunda.Presented.Fingerprint != insp.Presented.Fingerprint {
		t.Error("la huella cambió entre dos inspecciones seguidas")
	}
}

// Una clave distinta a la aceptada es la señal de que algo pasó, y no se puede
// tratar como un host nuevo.
func TestUnaClaveDistintaDaCambiadaYNoDesconocida(t *testing.T) {
	cfg, kh := configDePrueba(t)

	// Se acepta una clave que NO es la del servidor: es el estado en el que
	// queda alguien cuando el host se reinstala, o cuando hay un impostor.
	otra := clavePublicaAlAzar(t)
	if err := kh.TrustKey(cfg.Address(), otra); err != nil {
		t.Fatalf("Trust() error: %v", err)
	}

	insp, err := Inspect(context.Background(), cfg, kh)
	if err != nil {
		t.Fatalf("Inspect() error: %v", err)
	}
	if insp.Verdict != VerdictChanged {
		t.Fatalf("Verdict = %q, se esperaba %q", insp.Verdict, VerdictChanged)
	}
	// El diálogo muestra las dos huellas, así que las dos tienen que llegar.
	if insp.Known == nil {
		t.Fatal("no vino la clave que teníamos guardada")
	}
	if insp.Known.Fingerprint != ssh.FingerprintSHA256(otra) {
		t.Errorf("la clave previa no es la que se había aceptado")
	}
	if insp.Known.Fingerprint == insp.Presented.Fingerprint {
		t.Error("las dos huellas son iguales: no habría nada que decidir")
	}
	if insp.Known.AddedAt.IsZero() {
		t.Error("no se guardó cuándo se aceptó la clave, y el diálogo lo muestra")
	}
}

// No existe ninguna ruta que conecte sin verificar. Es el requisito duro del
// proyecto: nunca InsecureIgnoreHostKey.
func TestDialSeNiegaSiLaClaveNoEstaAceptada(t *testing.T) {
	cfg, kh := configDePrueba(t)

	_, err := Dial(context.Background(), cfg, kh, Secrets{Password: sshPassword}, DialOptions{})
	if err == nil {
		t.Fatal("Dial() conectó a un host cuya clave no está aceptada")
	}
	if !strings.Contains(err.Error(), "no está aceptada") {
		t.Errorf("el error no explica que falta aceptar la clave: %v", err)
	}
}

// Y con la clave aceptada sí conecta.
func TestDialConectaConLaClaveAceptada(t *testing.T) {
	cfg, kh := configDePrueba(t)
	aceptar(t, cfg, kh)

	c, err := Dial(context.Background(), cfg, kh, Secrets{Password: sshPassword}, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer c.Close()

	if c.Describe() != cfg.Describe() {
		t.Errorf("Describe() = %q", c.Describe())
	}
}

// "Conectar una vez" conecta y NO deja rastro. Si escribiera en known_hosts
// sería lo mismo que "confiar", y el botón estaría mintiendo.
func TestConectarUnaVezNoGuardaNada(t *testing.T) {
	cfg, kh := configDePrueba(t)

	insp, err := Inspect(context.Background(), cfg, kh)
	if err != nil {
		t.Fatalf("Inspect() error: %v", err)
	}

	c, err := Dial(context.Background(), cfg, kh, Secrets{Password: sshPassword},
		DialOptions{AcceptOnce: insp.Presented.Fingerprint})
	if err != nil {
		t.Fatalf("Dial() con aceptación de una vez falló: %v", err)
	}
	c.Close()

	guardada, err := kh.Lookup(cfg.Address())
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if guardada != nil {
		t.Errorf("«conectar una vez» guardó la clave: %+v", guardada)
	}
	if _, err := os.Stat(kh.Path()); !os.IsNotExist(err) {
		t.Error("«conectar una vez» creó el archivo known_hosts")
	}
}

// Aceptar algo que no es una clave no puede dejar el archivo a medias.
func TestTrustRechazaUnaClaveIlegible(t *testing.T) {
	cfg, kh := configDePrueba(t)

	if err := kh.Trust(cfg.Address(), "esto no es una clave"); err == nil {
		t.Fatal("Trust() aceptó algo que no es una clave")
	}
	if guardada, _ := kh.Lookup(cfg.Address()); guardada != nil {
		t.Error("guardó algo pese a que no se pudo interpretar")
	}
}

// aceptar confía en la clave que el servidor presenta ahora.
func aceptar(t *testing.T, cfg Config, kh *KnownHosts) {
	t.Helper()
	insp, err := Inspect(context.Background(), cfg, kh)
	if err != nil {
		t.Fatalf("Inspect() error: %v", err)
	}
	if err := kh.Trust(cfg.Address(), insp.AuthorizedKey); err != nil {
		t.Fatalf("Trust() error: %v", err)
	}
}

// clavePublicaAlAzar genera una clave que no es la de nadie.
func clavePublicaAlAzar(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generar la clave: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("convertir la clave: %v", err)
	}
	return sshPub
}

// Un túnel que se cae tiene que poder decirlo.
//
// Sin esto, un túnel muerto se manifiesta como un error de red de la base
// —"connection refused"— y la interfaz solo puede ofrecer una lista de posibles
// causas. Saber que el camino se cortó convierte esa lista en una respuesta.
func TestElClienteSabeCuandoElTunelSeCayo(t *testing.T) {
	cfg, kh := configDePrueba(t)
	aceptar(t, cfg, kh)

	c, err := Dial(context.Background(), cfg, kh, Secrets{Password: sshPassword}, DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}

	if c.Closed() {
		t.Fatal("un túnel recién abierto dice que está cerrado")
	}

	// Cerrarlo simula que el servidor cortó: para el cliente es lo mismo, la
	// conexión se termina y Wait vuelve.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// La goroutine que vigila corre en paralelo, así que se espera a que se
	// entere. Con un plazo generoso: lo que se prueba es que se entera, no en
	// cuántos milisegundos.
	limite := time.Now().Add(3 * time.Second)
	for !c.Closed() && time.Now().Before(limite) {
		time.Sleep(10 * time.Millisecond)
	}
	if !c.Closed() {
		t.Error("el túnel se cerró y el cliente no se enteró")
	}
}

// Un cliente nulo cuenta como cerrado: quien pregunta quiere saber si puede
// contar con el túnel, y no tenerlo es la forma más clara de no poder.
func TestUnClienteNuloCuentaComoCerrado(t *testing.T) {
	var c *Client
	if !c.Closed() {
		t.Error("un cliente nulo dijo que estaba abierto")
	}
}
