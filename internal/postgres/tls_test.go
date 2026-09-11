package postgres

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// conSSL devuelve el DSN de pruebas con otro modo y, si se pide, una raíz.
func conSSL(t *testing.T, dsn, modo, raiz string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("sslmode", modo)
	if raiz != "" {
		q.Set("sslrootcert", raiz)
	} else {
		q.Del("sslrootcert")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// El Postgres de pruebas tiene TLS encendido con un certificado autofirmado
// que se genera en el build de la imagen (docker/postgres). Con eso se prueba
// lo que la pestaña TLS promete: que después de probar se ve el certificado,
// que verify-full lo rechaza con las raíces del sistema y lo acepta con él
// mismo cargado como raíz, y que verify-ca con otra raíz lo rechaza.
func TestElCanalTLSSeDescribeYSeVerifica(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()

	// En claro, no hay canal que describir.
	info, f := Probe(ctx, conSSL(t, dsn, "disable", ""), "pruebas")
	if f != nil {
		t.Fatalf("disable: %s", f.Message)
	}
	if info.TLS != nil {
		t.Errorf("con sslmode=disable el canal tendría que ser nil: %+v", info.TLS)
	}

	// require: cifra sin verificar, y muestra qué presentó el servidor.
	info, f = Probe(ctx, conSSL(t, dsn, "require", ""), "pruebas")
	if f != nil {
		if os.Getenv("KANAME_REQUIRE_POSTGRES") != "" {
			t.Fatalf("require: %s — %s", f.Message, f.Detail)
		}
		t.Skipf("el Postgres de pruebas no ofrece TLS (%s); el de docker-compose.test.yml sí", f.Message)
	}
	if info.TLS == nil {
		t.Fatal("con require el canal es TLS y no se describió")
	}
	if !strings.Contains(info.TLS.Subject, "CN=kaname-test-postgres") || !info.TLS.SelfSigned {
		t.Errorf("el certificado de la imagen es autofirmado con CN=kaname-test-postgres: %+v", info.TLS)
	}
	if info.TLS.Version == "" || info.TLS.SHA256 == "" || info.TLS.PEM == "" {
		t.Errorf("faltan datos del canal: %+v", info.TLS)
	}

	// verify-full con las raíces del sistema: un autofirmado no pasa, y el
	// fallo es de TLS, con el detalle del driver.
	_, f = Probe(ctx, conSSL(t, dsn, "verify-full", ""), "pruebas")
	if f == nil {
		t.Fatal("verify-full aceptó un certificado autofirmado con las raíces del sistema")
	}
	if f.Kind != FailureTLS || f.Detail == "" {
		t.Errorf("el fallo tiene que ser de TLS y traer el detalle: %+v", f)
	}

	// El mismo certificado cargado como raíz: verify-full acepta, porque
	// vale para 127.0.0.1. Es el camino para un servidor propio: probar con
	// require, mirar la huella, guardar el PEM como raíz.
	raiz := filepath.Join(t.TempDir(), "servidor.crt")
	if err := os.WriteFile(raiz, []byte(info.TLS.PEM), 0o600); err != nil {
		t.Fatal(err)
	}
	verificado, f := Probe(ctx, conSSL(t, dsn, "verify-full", raiz), "pruebas")
	if f != nil {
		t.Fatalf("verify-full con el certificado del servidor como raíz falló: %s — %s", f.Message, f.Detail)
	}
	if verificado.TLS == nil || verificado.TLS.SHA256 != info.TLS.SHA256 {
		t.Errorf("la huella cambió entre la prueba y la conexión verificada: %+v", verificado.TLS)
	}

	// Y con una raíz que no firmó nada, verify-ca rechaza.
	_, f = Probe(ctx, conSSL(t, dsn, "verify-ca", otraRaiz(t)), "pruebas")
	if f == nil {
		t.Fatal("verify-ca aceptó un certificado que la raíz cargada no firmó")
	}
	if f.Kind != FailureTLS {
		t.Errorf("el fallo tiene que ser de TLS: %+v", f)
	}
}

// otraRaiz escribe una CA recién inventada que no firmó nada.
func otraRaiz(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plantilla := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Otra CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ruta := filepath.Join(t.TempDir(), "otra.pem")
	if err := os.WriteFile(ruta, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return ruta
}

// Una raíz que no está en esta máquina —una ruta con error de tipeo, o la
// libreta sincronizada a otra computadora— es un fallo de TLS que nombra el
// archivo, no «la cadena de conexión no es válida»: pgx abre los certificados
// durante el parseo y el error se perdía ahí.
func TestUnCertificadoQueNoExisteSeDiceConSuRuta(t *testing.T) {
	noExiste := filepath.Join(t.TempDir(), "no-existe.crt")
	dsn := conSSL(t, defaultTestDSN, "verify-full", noExiste)

	_, f := Probe(context.Background(), dsn, "pruebas")
	if f == nil {
		t.Fatal("una raíz inexistente tendría que fallar antes de conectar")
	}
	if f.Kind != FailureTLS {
		t.Errorf("Kind = %s, se esperaba tls: %+v", f.Kind, f)
	}
	if !strings.Contains(f.Detail, "no-existe.crt") {
		t.Errorf("el detalle no nombra el archivo: %+v", f)
	}
	if strings.Contains(f.Detail, "kaname:kaname") || strings.Contains(f.Message, "kaname:kaname") {
		t.Errorf("el fallo cita la cadena de conexión con la contraseña: %+v", f)
	}

	// Y la cadena de verdad inválida sigue siendo eso, sin detalle que la cite.
	_, f = Probe(context.Background(), "postgres://kaname:kaname@[::1:55432/x", "pruebas")
	if f == nil || f.Kind != FailureOther || strings.Contains(f.Detail+f.Message, "kaname:kaname") {
		t.Errorf("cadena inválida: %+v", f)
	}
}
