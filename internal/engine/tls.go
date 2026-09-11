package engine

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"time"
)

// SSLMode es el modo TLS de la conexión, con la semántica de libpq.
//
// Vive acá y no en `connection` porque lo leen dos lados: la libreta, que lo
// guarda, y el motor de MySQL, que tiene que traducirlo a un *tls.Config
// propio —su driver no tiene «verify-ca» y no lee archivos de una cadena de
// conexión—. Son los seis nombres de libpq en los tres motores de servidor:
// quien viene de Postgres ya los conoce, y una segunda enumeración con otros
// nombres para MySQL sería una tabla de conversión que alguien olvidaría
// actualizar.
type SSLMode string

const (
	SSLDisable    SSLMode = "disable"
	SSLAllow      SSLMode = "allow"
	SSLPrefer     SSLMode = "prefer"
	SSLRequire    SSLMode = "require"
	SSLVerifyCA   SSLMode = "verify-ca"
	SSLVerifyFull SSLMode = "verify-full"
)

// Known dice si el modo SSL es uno de los definidos por libpq.
func (s SSLMode) Known() bool {
	switch s {
	case SSLDisable, SSLAllow, SSLPrefer, SSLRequire, SSLVerifyCA, SSLVerifyFull:
		return true
	}
	return false
}

// Verifies dice si el modo valida realmente el certificado del servidor.
// `require` cifra pero no verifica nada: no protege contra un intermediario.
//
// Es la propiedad del modo a secas. La de la CONEXIÓN es
// connection.VerifiesCertificate, que además mira si hay una raíz cargada:
// con una, `require` pasa a verificar la cadena, como en libpq.
func (s SSLMode) Verifies() bool {
	return s == SSLVerifyCA || s == SSLVerifyFull
}

// TLSOptions es cómo cifrar la conexión a un servidor.
//
// Lo lee el motor de MySQL, que arma su *tls.Config con esto. Postgres no lo
// mira: pgx lee el modo y los archivos del DSN, que es donde libpq los espera,
// y `connection.DSN` los pone ahí. Que el mismo dato viaje por dos caminos no
// es descuido: cada driver lo recibe en el idioma que entiende, y el que lo
// arma es uno solo, el servicio.
type TLSOptions struct {
	// Mode vacío vale `prefer`, como en libpq.
	Mode SSLMode

	// RootCert, ClientCert y ClientKey son rutas a archivos PEM, ya con el `~`
	// resuelto. Rutas y no contenidos: viven en esta máquina y la libreta
	// solo las nombra. RootCert vacío usa las raíces del sistema.
	RootCert   string
	ClientCert string
	ClientKey  string
}

// TLSInfo describe el certificado que presentó el servidor y el canal que se
// negoció. Es lo que la pestaña TLS de S03 muestra después de probar: quién
// dice ser, quién lo firmó, hasta cuándo y la huella para compararla con la
// que el DBA pasó por otro canal.
//
// Nada de esto es secreto: es lo que el servidor le muestra a cualquiera que
// se conecte.
type TLSInfo struct {
	// Subject e Issuer son el nombre distinguido en su forma corta:
	// `CN=stg-db.internal,O=Corp`.
	Subject string `json:"subject"`
	Issuer  string `json:"issuer"`
	// ValidFor son los nombres y direcciones para los que vale el
	// certificado. Es lo que explica un fallo de verify-full: el certificado
	// es de otro host.
	ValidFor []string `json:"validFor"`
	// ValidFrom y ValidUntil en `AAAA-MM-DD`, UTC. Un día, no un instante: la
	// pantalla compara fechas, y la hora solo confundiría por la zona.
	ValidFrom  string `json:"validFrom"`
	ValidUntil string `json:"validUntil"`
	// Expired dice si hoy está fuera de esas fechas. Con `require` un
	// certificado vencido conecta igual —no se verifica nada— y esto es lo
	// único que lo dice.
	Expired bool `json:"expired"`
	// SelfSigned es que el certificado se firmó a sí mismo: emisor y sujeto
	// son el mismo. Es la forma que tiene un servidor recién instalado.
	SelfSigned bool `json:"selfSigned"`
	// SHA256 es la huella del certificado, `9f:2c:41:…`, en minúsculas.
	SHA256 string `json:"sha256"`
	// Version y Cipher son los del canal: `TLS 1.3`, `TLS_AES_256_GCM_SHA384`.
	Version string `json:"version"`
	Cipher  string `json:"cipher"`
	// PEM es el certificado tal cual, para guardarlo como raíz de confianza
	// si es autofirmado y se lo verificó por otro canal.
	PEM string `json:"pem"`
}

// TLSInfoOf describe un canal TLS ya negociado. Nil si no hubo certificado,
// que con un canal negociado no pasa: el servidor siempre presenta uno.
func TLSInfoOf(cs tls.ConnectionState) *TLSInfo {
	if len(cs.PeerCertificates) == 0 {
		return nil
	}
	cert := cs.PeerCertificates[0]
	huella := sha256.Sum256(cert.Raw)

	return &TLSInfo{
		Subject:    cert.Subject.String(),
		Issuer:     cert.Issuer.String(),
		ValidFor:   nombresDe(cert),
		ValidFrom:  cert.NotBefore.UTC().Format("2006-01-02"),
		ValidUntil: cert.NotAfter.UTC().Format("2006-01-02"),
		Expired:    vencido(cert),
		SelfSigned: cert.Issuer.String() == cert.Subject.String(),
		SHA256:     conDosPuntos(hex.EncodeToString(huella[:])),
		Version:    tls.VersionName(cs.Version),
		Cipher:     tls.CipherSuiteName(cs.CipherSuite),
		PEM:        string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})),
	}
}

// nombresDe junta los nombres DNS y las IP del certificado, en ese orden.
func nombresDe(cert *x509.Certificate) []string {
	out := append([]string(nil), cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		out = append(out, ip.String())
	}
	return out
}

// conDosPuntos escribe la huella como la muestran openssl y los navegadores:
// pares separados por dos puntos. Así se compara de un vistazo con la que
// pasó el DBA.
func conDosPuntos(hexa string) string {
	var b strings.Builder
	b.Grow(len(hexa) + len(hexa)/2)
	for i := 0; i < len(hexa); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hexa[i : i+2])
	}
	return b.String()
}

// relojTLS es «ahora» para decidir si un certificado venció. Es variable para
// que un test pueda pararse en una fecha sin esperar a que un certificado
// real venza.
var relojTLS = time.Now

func vencido(cert *x509.Certificate) bool {
	ahora := relojTLS()
	return ahora.Before(cert.NotBefore) || ahora.After(cert.NotAfter)
}
