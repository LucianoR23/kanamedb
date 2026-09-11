package mysql

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// El driver de MySQL no habla el idioma de libpq: su `tls=` acepta `false`,
// `preferred`, `skip-verify` y `true`, y nada más. No tiene «verify-ca» —con
// `true` verifica también el nombre del host— y no lee certificados de la
// cadena de conexión. Así que el modo y los archivos llegan por
// engine.OpenOptions y acá se traducen a un *tls.Config, que es lo que el
// driver sí acepta.
//
// La traducción sigue a libpq, que es lo que la interfaz promete con esos
// nombres: `require` con una raíz cargada verifica la cadena, y `verify-ca`
// verifica la cadena sin mirar el nombre del host, como hace pgx.

// canalTLS es lo que se supo del canal al negociarlo.
//
// Existe porque el driver no expone la conexión TLS: la única forma de ver el
// certificado que presentó el servidor es una devolución de llamada en el
// *tls.Config, que se ejecuta en cada conexión que el pool abre. Todas ven el
// mismo servidor, así que la última gana.
type canalTLS struct {
	mu   sync.Mutex
	info *engine.TLSInfo
}

func (c *canalTLS) guardar(cs tls.ConnectionState) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.info = engine.TLSInfoOf(cs)
	return nil
}

func (c *canalTLS) leer() *engine.TLSInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

// configTLS arma la configuración del canal.
//
// Devuelve nil con `disable`: sin TLS. `enClaro` dice si el driver puede
// seguir en claro cuando el servidor no ofrece TLS, que es lo que `prefer` y
// `allow` significan. Un fallo es un archivo que no se pudo leer o que no es
// un PEM, y se dice cuál.
func configTLS(o engine.TLSOptions, host string, canal *canalTLS) (cfg *tls.Config, enClaro bool, f *engine.Failure) {
	modo := o.Mode
	if modo == "" {
		modo = engine.SSLPrefer
	}
	if modo == engine.SSLDisable {
		return nil, false, nil
	}

	cfg = &tls.Config{
		// Se llama en cada negociación, también con InsecureSkipVerify: es
		// lo que permite mostrar el certificado aun cuando no se verifica.
		VerifyConnection: canal.guardar,
	}
	// El nombre del host va SIEMPRE, no solo con verify-full: con
	// InsecureSkipVerify no verifica nada, pero es lo que viaja como SNI, y
	// un frente que elige el certificado por SNI —HAProxy, un gateway
	// administrado— presenta el equivocado o corta la negociación si no lo
	// recibe. pgx lo manda en todos los modos (sslsni=1); acá también. Una
	// IP no es un nombre y no se manda: SNI no admite direcciones.
	if net.ParseIP(host) == nil {
		cfg.ServerName = host
	}

	if o.RootCert != "" {
		raices, err := os.ReadFile(o.RootCert)
		if err != nil {
			return nil, false, archivoTLS("el certificado raíz", o.RootCert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raices) {
			return nil, false, archivoTLS("el certificado raíz", o.RootCert,
				errors.New("no tiene ningún certificado en formato PEM"))
		}
		cfg.RootCAs = pool
		// Con una raíz cargada, `require` verifica la cadena: es lo que hace
		// libpq, y lo que connection.VerifiesCertificate promete.
		if modo == engine.SSLRequire {
			modo = engine.SSLVerifyCA
		}
	}

	if o.ClientCert != "" || o.ClientKey != "" {
		par, err := tls.LoadX509KeyPair(o.ClientCert, o.ClientKey)
		if err != nil {
			return nil, false, archivoTLS("el certificado de cliente", o.ClientCert, err)
		}
		cfg.Certificates = []tls.Certificate{par}
	}

	switch modo {
	case engine.SSLAllow, engine.SSLPrefer:
		cfg.InsecureSkipVerify = true
		enClaro = true
	case engine.SSLRequire:
		cfg.InsecureSkipVerify = true
	case engine.SSLVerifyCA:
		// La verificación de la biblioteca estándar exige que el nombre
		// coincida; verify-ca no. Se apaga y se verifica la cadena a mano,
		// sin nombre, igual que pgx.
		cfg.InsecureSkipVerify = true
		raices := cfg.RootCAs
		cfg.VerifyPeerCertificate = func(crudos [][]byte, _ [][]*x509.Certificate) error {
			return verificarCadena(crudos, raices)
		}
	case engine.SSLVerifyFull:
		// La verificación estándar compara el certificado contra ServerName;
		// con una IP —que arriba no se puso— hay que ponerla igual, porque
		// ahí sí se compara contra las IP del certificado.
		cfg.ServerName = host
	default:
		return nil, false, &engine.Failure{
			Kind:    engine.FailureTLS,
			Message: fmt.Sprintf("No se reconoce el modo SSL «%s».", modo),
			Hint:    "Los modos son disable, allow, prefer, require, verify-ca y verify-full.",
		}
	}
	return cfg, enClaro, nil
}

// verificarCadena comprueba que el certificado del servidor lo firmó una de
// las raíces, sin mirar para qué nombre vale. Con raíces nil se usan las del
// sistema, como en verify-full.
func verificarCadena(crudos [][]byte, raices *x509.CertPool) error {
	certs := make([]*x509.Certificate, 0, len(crudos))
	for _, asn1 := range crudos {
		cert, err := x509.ParseCertificate(asn1)
		if err != nil {
			return fmt.Errorf("tls: el certificado del servidor no se pudo interpretar: %w", err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return errors.New("tls: el servidor no presentó ningún certificado")
	}
	opts := x509.VerifyOptions{Roots: raices, Intermediates: x509.NewCertPool()}
	for _, c := range certs[1:] {
		opts.Intermediates.AddCert(c)
	}
	_, err := certs[0].Verify(opts)
	return err
}

func archivoTLS(que, ruta string, err error) *engine.Failure {
	return &engine.Failure{
		Kind:    engine.FailureTLS,
		Message: fmt.Sprintf("No se pudo usar %s (%s).", que, ruta),
		Hint:    "Tiene que ser un archivo PEM legible desde esta máquina. La ruta se guarda, no el archivo.",
		Detail:  engine.Redact(err.Error()),
	}
}
