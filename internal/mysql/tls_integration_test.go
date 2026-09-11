package mysql_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/mysql"
)

// Lo que se supo del canal tiene que coincidir con lo que el servidor dice de
// la sesión: si Ssl_cipher está vacío el canal es nil, y si no, no. Son dos
// fuentes independientes —una devolución de llamada de tls.Config y una
// variable de estado del servidor— y por eso el test no necesita saber de
// antemano si el contenedor ofrece TLS: MySQL genera certificados solo desde
// la 8, MariaDB desde la 11.4, y las LTS viejas pueden no ofrecer nada.
func TestElCanalTLSCoincideConLoQueDiceElServidor(t *testing.T) {
	for _, m := range motores {
		t.Run(m.nombre, func(t *testing.T) {
			ctx := context.Background()
			abrirCon := func(tls engine.TLSOptions) (engine.Conn, *engine.Failure) {
				return mysql.Open(ctx, m.dsn, "pruebas", engine.OpenOptions{MaxConns: 1, TLS: tls})
			}

			c, f := abrirCon(engine.TLSOptions{Mode: engine.SSLPrefer})
			if f != nil {
				saltear(t, m.nombre, f)
			}
			defer c.Close()
			cifrado := cifradoDeLaSesion(t, c)
			canal := c.Server().TLS
			switch {
			case cifrado == "" && canal != nil:
				t.Fatalf("el servidor dice que la sesión va en claro y el canal dice %+v", canal)
			case cifrado != "" && canal == nil:
				t.Fatalf("el servidor dice que la sesión va cifrada (%s) y el canal es nil", cifrado)
			}

			// disable: en claro, siempre.
			c2, f := abrirCon(engine.TLSOptions{Mode: engine.SSLDisable})
			if f != nil {
				t.Fatalf("disable: %s — %s", f.Message, f.Detail)
			}
			defer c2.Close()
			if c2.Server().TLS != nil || cifradoDeLaSesion(t, c2) != "" {
				t.Errorf("con disable la sesión tendría que ir en claro: canal=%+v", c2.Server().TLS)
			}

			if canal == nil {
				t.Logf("%s no ofrece TLS: no hay certificado que verificar", m.nombre)
				return
			}

			// El certificado que genera el servidor no lo firmó ninguna raíz
			// del sistema: verify-full lo rechaza, y el fallo es de TLS.
			_, f = abrirCon(engine.TLSOptions{Mode: engine.SSLVerifyFull})
			if f == nil {
				t.Fatal("verify-full aceptó el certificado autogenerado con las raíces del sistema")
			}
			if f.Kind != engine.FailureTLS || f.Detail == "" {
				t.Errorf("el fallo tiene que ser de TLS y traer el detalle: %+v", f)
			}

			// Con el certificado del servidor cargado como raíz, verify-ca
			// acepta: es el camino para un servidor propio —probar con
			// require, mirar la huella, guardar el PEM—.
			raiz := filepath.Join(t.TempDir(), "servidor.pem")
			if err := os.WriteFile(raiz, []byte(canal.PEM), 0o600); err != nil {
				t.Fatal(err)
			}
			c3, f := abrirCon(engine.TLSOptions{Mode: engine.SSLVerifyCA, RootCert: raiz})
			if f != nil {
				t.Fatalf("verify-ca con el certificado del servidor como raíz falló: %s — %s", f.Message, f.Detail)
			}
			defer c3.Close()
			if c3.Server().TLS == nil || c3.Server().TLS.SHA256 != canal.SHA256 {
				t.Errorf("la huella cambió entre prefer y verify-ca: %+v", c3.Server().TLS)
			}
		})
	}
}

// cifradoDeLaSesion es lo que el servidor dice del canal de ESTA sesión.
// Vacío es en claro.
func cifradoDeLaSesion(t *testing.T, c engine.Conn) string {
	t.Helper()
	lote, f := c.Run(context.Background(), "SHOW STATUS LIKE 'Ssl_cipher'", engine.RunOptions{})
	if f != nil {
		t.Fatalf("SHOW STATUS: %s", f.Message)
	}
	if len(lote.Results) != 1 || len(lote.Results[0].Rows) != 1 || len(lote.Results[0].Rows[0]) != 2 {
		t.Fatalf("SHOW STATUS LIKE 'Ssl_cipher' devolvió %+v", lote.Results)
	}
	if v := lote.Results[0].Rows[0][1]; v != nil {
		return *v
	}
	return ""
}
