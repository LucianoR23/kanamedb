package mysql

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Estos tests no necesitan un MySQL: lo que se prueba es la traducción de los
// seis modos de libpq a un *tls.Config, y eso se verifica contra un servidor
// TLS de la biblioteca estándar que presenta un certificado conocido. Es la
// misma negociación que hace el driver con `cfg.TLS`, sin el protocolo de
// MySQL arriba.

// pki es una autoridad de juguete generada por el test: una raíz, un
// certificado de servidor para `db.interna` y uno de cliente.
type pki struct {
	dir      string
	raiz     string // ruta al PEM de la raíz
	otraRaiz string // otra raíz, que no firmó nada de lo de arriba
	servidor tls.Certificate
	cliCert  string
	cliKey   string
	pool     *x509.CertPool
}

func generarPKI(t *testing.T) pki {
	t.Helper()
	dir := t.TempDir()

	raizKey, raizCert := autoridad(t, "CA de prueba")
	otraKey, otraCert := autoridad(t, "Otra CA")
	_ = otraKey

	p := pki{
		dir:      dir,
		raiz:     escribirPEM(t, dir, "raiz.pem", "CERTIFICATE", raizCert.Raw),
		otraRaiz: escribirPEM(t, dir, "otra.pem", "CERTIFICATE", otraCert.Raw),
		pool:     x509.NewCertPool(),
	}
	p.pool.AddCert(raizCert)

	// Servidor: vale para db.interna y NO para 127.0.0.1, que es donde va a
	// escuchar. Es la diferencia entre verify-ca y verify-full.
	srvKey, srvCert := firmado(t, raizKey, raizCert, "db.interna", []string{"db.interna"}, false)
	p.servidor = tls.Certificate{Certificate: [][]byte{srvCert.Raw}, PrivateKey: srvKey}

	cliKey, cliCert := firmado(t, raizKey, raizCert, "cliente", nil, true)
	p.cliCert = escribirPEM(t, dir, "cliente.crt", "CERTIFICATE", cliCert.Raw)
	der, err := x509.MarshalECPrivateKey(cliKey)
	if err != nil {
		t.Fatal(err)
	}
	p.cliKey = escribirPEM(t, dir, "cliente.key", "EC PRIVATE KEY", der)
	return p
}

func autoridad(t *testing.T, nombre string) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plantilla := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: nombre},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

func firmado(
	t *testing.T, caKey *ecdsa.PrivateKey, ca *x509.Certificate,
	cn string, dns []string, cliente bool,
) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uso := x509.ExtKeyUsageServerAuth
	if cliente {
		uso = x509.ExtKeyUsageClientAuth
	}
	plantilla := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dns,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{uso},
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

func escribirPEM(t *testing.T, dir, nombre, tipo string, der []byte) string {
	t.Helper()
	ruta := filepath.Join(dir, nombre)
	if err := os.WriteFile(ruta, pem.EncodeToMemory(&pem.Block{Type: tipo, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return ruta
}

// servidorTLS escucha en 127.0.0.1 con el certificado de la PKI y completa la
// negociación de cada conexión que llega. Devuelve host:puerto.
func servidorTLS(t *testing.T, p pki, exigirCliente bool) string {
	t.Helper()
	cfg := &tls.Config{Certificates: []tls.Certificate{p.servidor}, MinVersion: tls.VersionTLS12}
	if exigirCliente {
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ClientCAs = p.pool
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.(*tls.Conn).Handshake()
			}()
		}
	}()
	return ln.Addr().String()
}

// negociar arma la configuración con configTLS y hace la negociación completa
// contra el servidor. Devuelve el error de la negociación y lo que el canal
// capturó.
func negociar(t *testing.T, o engine.TLSOptions, host, addr string) (error, *engine.TLSInfo) {
	t.Helper()
	canal := &canalTLS{}
	cfg, _, f := configTLS(o, host, canal)
	if f != nil {
		t.Fatalf("configTLS(%+v) falló: %s (%s)", o, f.Message, f.Detail)
	}
	if cfg == nil {
		t.Fatalf("configTLS(%+v) devolvió nil: no hay canal que negociar", o)
	}
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return err, canal.leer()
	}
	defer conn.Close()
	// En TLS 1.3 el cliente da la negociación por terminada antes de que el
	// servidor revise su certificado: el rechazo llega como alerta en la
	// primera lectura. El servidor de prueba cierra apenas negocia, así que
	// EOF es «aceptado» y cualquier otra cosa es el rechazo.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err != nil && !errors.Is(err, io.EOF) {
		return err, canal.leer()
	}
	return nil, canal.leer()
}

func TestVerifyCAAceptaLaCadenaSinMirarElHost(t *testing.T) {
	p := generarPKI(t)
	addr := servidorTLS(t, p, false)

	// verify-ca: la raíz firmó el certificado, el nombre no importa.
	err, info := negociar(t, engine.TLSOptions{Mode: engine.SSLVerifyCA, RootCert: p.raiz}, "127.0.0.1", addr)
	if err != nil {
		t.Fatalf("verify-ca con la raíz correcta falló: %v", err)
	}
	if info == nil || info.Subject != "CN=db.interna" {
		t.Errorf("el canal no capturó el certificado del servidor: %+v", info)
	}

	// verify-full contra 127.0.0.1: el certificado es de db.interna.
	err, _ = negociar(t, engine.TLSOptions{Mode: engine.SSLVerifyFull, RootCert: p.raiz}, "127.0.0.1", addr)
	if err == nil {
		t.Fatal("verify-full aceptó un certificado que no es del host")
	}
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("el error no dice contra qué host falló la verificación: %v", err)
	}

	// verify-full con el nombre correcto, sí.
	err, _ = negociar(t, engine.TLSOptions{Mode: engine.SSLVerifyFull, RootCert: p.raiz}, "db.interna", addr)
	if err != nil {
		t.Errorf("verify-full con el nombre del certificado falló: %v", err)
	}
}

func TestVerifyCARechazaOtraRaiz(t *testing.T) {
	p := generarPKI(t)
	addr := servidorTLS(t, p, false)
	err, _ := negociar(t, engine.TLSOptions{Mode: engine.SSLVerifyCA, RootCert: p.otraRaiz}, "127.0.0.1", addr)
	if err == nil {
		t.Fatal("verify-ca aceptó un certificado firmado por otra raíz")
	}
}

// `require` a secas no verifica nada y conecta igual; con una raíz cargada
// pasa a verificar la cadena, como en libpq.
func TestRequireConRaizVerificaLaCadena(t *testing.T) {
	p := generarPKI(t)
	addr := servidorTLS(t, p, false)

	err, info := negociar(t, engine.TLSOptions{Mode: engine.SSLRequire}, "127.0.0.1", addr)
	if err != nil {
		t.Fatalf("require sin raíz tiene que conectar contra cualquier certificado: %v", err)
	}
	if info == nil {
		t.Fatal("require conectó y el canal no capturó nada")
	}
	if info.SelfSigned {
		t.Error("el certificado del servidor lo firmó la raíz, no él mismo")
	}
	if !regexp.MustCompile(`^([0-9a-f]{2}:){31}[0-9a-f]{2}$`).MatchString(info.SHA256) {
		t.Errorf("la huella no tiene la forma de openssl: %q", info.SHA256)
	}
	if info.Version == "" || info.Cipher == "" || info.ValidUntil == "" || !strings.Contains(info.PEM, "BEGIN CERTIFICATE") {
		t.Errorf("faltan datos del canal: %+v", info)
	}
	if info.Expired {
		t.Error("el certificado vale un día y se marcó vencido")
	}

	err, _ = negociar(t, engine.TLSOptions{Mode: engine.SSLRequire, RootCert: p.otraRaiz}, "127.0.0.1", addr)
	if err == nil {
		t.Fatal("require con una raíz cargada tiene que verificar la cadena, y esta raíz no firmó el certificado")
	}
}

func TestElCertificadoDeClienteSeUsa(t *testing.T) {
	p := generarPKI(t)
	addr := servidorTLS(t, p, true)

	err, _ := negociar(t, engine.TLSOptions{Mode: engine.SSLVerifyCA, RootCert: p.raiz}, "127.0.0.1", addr)
	if err == nil {
		t.Fatal("el servidor exige certificado de cliente y sin uno tendría que fallar")
	}
	err, _ = negociar(t, engine.TLSOptions{
		Mode: engine.SSLVerifyCA, RootCert: p.raiz, ClientCert: p.cliCert, ClientKey: p.cliKey,
	}, "127.0.0.1", addr)
	if err != nil {
		t.Fatalf("con el certificado de cliente tendría que entrar: %v", err)
	}
}

func TestLosModosSinVerificacionYElApagado(t *testing.T) {
	canal := &canalTLS{}
	cfg, enClaro, f := configTLS(engine.TLSOptions{Mode: engine.SSLDisable}, "h", canal)
	if f != nil || cfg != nil || enClaro {
		t.Errorf("disable: cfg=%v enClaro=%v f=%v; se esperaba sin TLS", cfg, enClaro, f)
	}
	for _, modo := range []engine.SSLMode{"", engine.SSLPrefer, engine.SSLAllow} {
		cfg, enClaro, f = configTLS(engine.TLSOptions{Mode: modo}, "h", canal)
		if f != nil || cfg == nil || !cfg.InsecureSkipVerify || !enClaro {
			t.Errorf("%q: se esperaba cifrar sin verificar y poder seguir en claro; cfg=%+v enClaro=%v f=%v", modo, cfg, enClaro, f)
		}
	}
	cfg, enClaro, f = configTLS(engine.TLSOptions{Mode: engine.SSLRequire}, "h", canal)
	if f != nil || cfg == nil || !cfg.InsecureSkipVerify || enClaro {
		t.Errorf("require: cifra sin verificar y NO sigue en claro; cfg=%+v enClaro=%v f=%v", cfg, enClaro, f)
	}
	if _, _, f = configTLS(engine.TLSOptions{Mode: "verify-something"}, "h", canal); f == nil {
		t.Error("un modo desconocido tiene que fallar, no cifrar a medias")
	}
}

func TestUnArchivoQueNoSePuedeLeerSeDiceConSuRuta(t *testing.T) {
	canal := &canalTLS{}
	noExiste := filepath.Join(t.TempDir(), "no-existe.pem")
	_, _, f := configTLS(engine.TLSOptions{Mode: engine.SSLVerifyCA, RootCert: noExiste}, "h", canal)
	if f == nil || f.Kind != engine.FailureTLS || !strings.Contains(f.Message, noExiste) {
		t.Errorf("raíz inexistente: %+v", f)
	}

	basura := filepath.Join(t.TempDir(), "basura.pem")
	if err := os.WriteFile(basura, []byte("esto no es un certificado"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, f = configTLS(engine.TLSOptions{Mode: engine.SSLVerifyCA, RootCert: basura}, "h", canal)
	if f == nil || !strings.Contains(f.Message, basura) || !strings.Contains(f.Detail, "PEM") {
		t.Errorf("raíz que no es PEM: %+v", f)
	}

	p := generarPKI(t)
	_, _, f = configTLS(engine.TLSOptions{Mode: engine.SSLRequire, ClientCert: p.cliCert, ClientKey: p.raiz}, "h", canal)
	if f == nil || !strings.Contains(f.Message, "cliente") {
		t.Errorf("clave que no corresponde al certificado: %+v", f)
	}
}

// hostDe es lo que verify-full compara contra el certificado.
func TestHostDe(t *testing.T) {
	for dentro, fuera := range map[string]string{
		"db.interna:3306": "db.interna",
		"[::1]:3306":      "::1",
		"sin-puerto":      "sin-puerto",
	} {
		if got := hostDe(dentro); got != fuera {
			t.Errorf("hostDe(%q) = %q, se esperaba %q", dentro, got, fuera)
		}
	}
}

// El SNI viaja en todos los modos, como en pgx: un frente que elige el
// certificado por nombre presenta el equivocado si no lo recibe. Una IP no
// se manda, salvo con verify-full, donde ServerName es contra qué se verifica.
func TestElNombreDelHostViajaComoSNIEnTodosLosModos(t *testing.T) {
	canal := &canalTLS{}
	for _, modo := range []engine.SSLMode{engine.SSLPrefer, engine.SSLRequire, engine.SSLVerifyCA, engine.SSLVerifyFull} {
		cfg, _, f := configTLS(engine.TLSOptions{Mode: modo}, "db.interna", canal)
		if f != nil || cfg == nil || cfg.ServerName != "db.interna" {
			t.Errorf("%s: ServerName = %q, se esperaba db.interna", modo, cfg.ServerName)
		}
		cfg, _, _ = configTLS(engine.TLSOptions{Mode: modo}, "10.0.0.7", canal)
		quiere := ""
		if modo == engine.SSLVerifyFull {
			quiere = "10.0.0.7"
		}
		if cfg.ServerName != quiere {
			t.Errorf("%s con IP: ServerName = %q, se esperaba %q", modo, cfg.ServerName, quiere)
		}
	}
}
