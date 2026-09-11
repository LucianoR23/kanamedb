package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func certificadoPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plantilla := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "db.interna"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// El binding escribe un certificado y nada más: es lo que impide que un
// texto cualquiera termine en un archivo cualquiera desde la interfaz.
func TestSaveCertificateSoloEscribeUnCertificado(t *testing.T) {
	s := nuevo(t)
	dir := t.TempDir()

	ruta := filepath.Join(dir, "servidor.crt")
	pemText := certificadoPEM(t)
	if err := s.SaveCertificate(pemText, ruta); err != nil {
		t.Fatalf("SaveCertificate() error: %v", err)
	}
	data, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != pemText {
		t.Errorf("el archivo no es el certificado que se pasó:\n%s", data)
	}

	for nombre, texto := range map[string]string{
		"texto suelto": "DROP TABLE clientes;",
		"otro tipo de bloque": string(pem.EncodeToMemory(&pem.Block{
			Type: "PRIVATE KEY", Bytes: []byte("no importa"),
		})),
		"certificado que no se interpreta": string(pem.EncodeToMemory(&pem.Block{
			Type: "CERTIFICATE", Bytes: []byte("basura"),
		})),
		// Un certificado con basura DESPUÉS: solo se escribe el bloque.
	} {
		otra := filepath.Join(dir, strings.ReplaceAll(nombre, " ", "-"))
		if err := s.SaveCertificate(texto, otra); err == nil {
			t.Errorf("%s: se escribió sin ser un certificado", nombre)
		}
		if _, err := os.Stat(otra); err == nil {
			t.Errorf("%s: el archivo quedó creado igual", nombre)
		}
	}

	// Lo que rodea al bloque no se escribe: el archivo es el certificado.
	conCola := filepath.Join(dir, "con-cola.crt")
	if err := s.SaveCertificate(pemText+"\nesto no es parte del certificado\n", conCola); err != nil {
		t.Fatalf("SaveCertificate() con cola: %v", err)
	}
	data, _ = os.ReadFile(conCola)
	if string(data) != pemText {
		t.Errorf("se escribió más que el certificado:\n%s", data)
	}
}
