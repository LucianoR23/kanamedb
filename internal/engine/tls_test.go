package engine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func certificadoDePrueba(t *testing.T, desde, hasta time.Time) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plantilla := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "stg-db.internal", Organization: []string{"Corp"}},
		DNSNames:     []string{"stg-db.internal", "db"},
		IPAddresses:  []net.IP{net.ParseIP("10.0.0.7")},
		NotBefore:    desde,
		NotAfter:     hasta,
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// Lo que la pestaña TLS muestra sale de acá: sujeto, emisor, fechas, huella.
// La huella se compara contra la que pasa el DBA, así que tiene que ser
// exactamente el SHA-256 del DER, escrito como lo escribe openssl.
func TestTLSInfoOfDescribeElCertificado(t *testing.T) {
	desde := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	hasta := time.Date(2027, 2, 11, 23, 59, 0, 0, time.UTC)
	cert := certificadoDePrueba(t, desde, hasta)
	cs := tls.ConnectionState{
		Version:          tls.VersionTLS13,
		CipherSuite:      tls.TLS_AES_256_GCM_SHA384,
		PeerCertificates: []*x509.Certificate{cert},
	}

	relojTLS = func() time.Time { return time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC) }
	defer func() { relojTLS = time.Now }()

	info := TLSInfoOf(cs)
	if info == nil {
		t.Fatal("TLSInfoOf devolvió nil con un certificado presente")
	}
	if info.Subject != "CN=stg-db.internal,O=Corp" {
		t.Errorf("Subject = %q", info.Subject)
	}
	if !info.SelfSigned {
		t.Error("un certificado que se firmó a sí mismo tiene que decirlo")
	}
	if info.ValidFrom != "2026-01-10" || info.ValidUntil != "2027-02-11" {
		t.Errorf("fechas = %q .. %q", info.ValidFrom, info.ValidUntil)
	}
	if info.Expired {
		t.Error("el 2026-09-11 el certificado está vigente")
	}
	huella := sha256.Sum256(cert.Raw)
	quiere := strings.ToLower(hex.EncodeToString(huella[:]))
	if strings.ReplaceAll(info.SHA256, ":", "") != quiere || strings.Count(info.SHA256, ":") != 31 {
		t.Errorf("SHA256 = %q, se esperaba %q con dos puntos cada dos", info.SHA256, quiere)
	}
	if info.Version != "TLS 1.3" || info.Cipher != "TLS_AES_256_GCM_SHA384" {
		t.Errorf("canal = %q / %q", info.Version, info.Cipher)
	}
	if got := strings.Join(info.ValidFor, ","); got != "stg-db.internal,db,10.0.0.7" {
		t.Errorf("ValidFor = %v: tienen que estar los nombres y las IP", info.ValidFor)
	}
	if !strings.HasPrefix(info.PEM, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("PEM = %q…", info.PEM[:min(40, len(info.PEM))])
	}

	// Vencido: con `require` conecta igual, y esto es lo único que lo dice.
	relojTLS = func() time.Time { return time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC) }
	if !TLSInfoOf(cs).Expired {
		t.Error("el 2027-03-01 el certificado está vencido y no se marcó")
	}
	// Y todavía no vigente también cuenta como fuera de fechas.
	relojTLS = func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }
	if !TLSInfoOf(cs).Expired {
		t.Error("antes de NotBefore el certificado no vale y no se marcó")
	}

	if TLSInfoOf(tls.ConnectionState{}) != nil {
		t.Error("sin certificado no hay nada que describir")
	}
}
