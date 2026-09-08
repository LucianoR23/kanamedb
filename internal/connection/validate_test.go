package connection

import (
	"errors"
	"strings"
	"testing"
)

func fieldErrors(t *testing.T, c Connection) map[string]string {
	t.Helper()
	err := c.Validate()
	if err == nil {
		return nil
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() devolvió %T, se esperaba *ValidationError", err)
	}
	out := make(map[string]string, len(ve.Errors))
	for _, e := range ve.Errors {
		out[e.Field] = e.Message
	}
	return out
}

func TestValidateAceptaUnaConexionCompleta(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate() rechazó una conexión válida: %v", err)
	}
}

// La UI pinta el error debajo del input, así que necesita saber qué campo falló.
// Y los devuelve todos juntos: corregir un formulario de a un campo por viaje al
// backend es una mala experiencia.
func TestValidateReportaTodosLosCamposDeUnaVez(t *testing.T) {
	got := fieldErrors(t, Connection{Engine: Postgres, Port: 5432})

	for _, campo := range []string{"id", "name", "database", "host", "user"} {
		if _, ok := got[campo]; !ok {
			t.Errorf("falta el error del campo %q; se obtuvo %v", campo, got)
		}
	}
}

func TestValidatePorCampo(t *testing.T) {
	casos := []struct {
		nombre string
		ajuste func(*Connection)
		campo  string
	}{
		{"sin id", func(c *Connection) { c.ID = "" }, "id"},
		{"sin nombre", func(c *Connection) { c.Name = "" }, "name"},
		{"sin base", func(c *Connection) { c.Database = "" }, "database"},
		{"sin host", func(c *Connection) { c.Host = "" }, "host"},
		{"sin usuario", func(c *Connection) { c.User = "" }, "user"},
		{"puerto cero", func(c *Connection) { c.Port = 0 }, "port"},
		{"puerto negativo", func(c *Connection) { c.Port = -1 }, "port"},
		{"puerto fuera de rango", func(c *Connection) { c.Port = 65536 }, "port"},
		{"motor desconocido", func(c *Connection) { c.Engine = "oracle" }, "engine"},
		{"motor sin implementar", func(c *Connection) { c.Engine = MySQL }, "engine"},
		{"entorno desconocido", func(c *Connection) { c.Environment = "prod" }, "environment"},
		{"ssl desconocido", func(c *Connection) { c.SSLMode = "maybe" }, "sslMode"},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			c := valid()
			tc.ajuste(&c)
			got := fieldErrors(t, c)
			if _, ok := got[tc.campo]; !ok {
				t.Errorf("no reportó el campo %q; se obtuvo %v", tc.campo, got)
			}
		})
	}
}

func TestValidateNoPideHostNiPuertoParaSQLite(t *testing.T) {
	c := Connection{
		Name:        "local.db",
		Engine:      SQLite,
		Database:    "C:/tmp/local.db",
		Environment: Local,
	}
	got := fieldErrors(t, c)
	for _, campo := range []string{"host", "port", "user"} {
		if _, ok := got[campo]; ok {
			t.Errorf("SQLite no debería exigir %q", campo)
		}
	}
	// Sí debe quejarse de que todavía no está implementado.
	if _, ok := got["engine"]; !ok {
		t.Error("SQLite todavía no está implementado y debería avisarlo")
	}
}

func TestErrorDeValidacionNombraLosCampos(t *testing.T) {
	err := Connection{Engine: Postgres, Port: 5432}.Validate()
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	msg := err.Error()
	for _, campo := range []string{"name", "database", "host", "user"} {
		if !strings.Contains(msg, campo) {
			t.Errorf("el mensaje %q no menciona %q", msg, campo)
		}
	}
}

func TestAvisaCuandoSSLNoVerifica(t *testing.T) {
	casos := map[SSLMode]bool{
		SSLDisable:    true,
		SSLPrefer:     true,
		SSLRequire:    true,
		SSLVerifyCA:   false,
		SSLVerifyFull: false,
	}
	for modo, esperaAviso := range casos {
		t.Run(string(modo), func(t *testing.T) {
			c := valid()
			c.SSLMode = modo
			hay := false
			for _, w := range c.Warnings() {
				if w.Field == "sslMode" {
					hay = true
				}
			}
			if hay != esperaAviso {
				t.Errorf("aviso de sslMode = %v, se esperaba %v para %q", hay, esperaAviso, modo)
			}
		})
	}
}

func TestAvisaSobreProduccionConEscritura(t *testing.T) {
	c := valid()
	c.Environment = Production
	c.SSLMode = SSLVerifyFull

	c.ReadOnly = false
	if len(c.Warnings()) == 0 {
		t.Error("producción con escritura habilitada debería avisar")
	}

	c.ReadOnly = true
	if len(c.Warnings()) != 0 {
		t.Errorf("producción en solo lectura no debería avisar: %v", c.Warnings())
	}
}

// Los avisos no bloquean: la app no decide por el usuario, solo se asegura de
// que no se entere tarde.
func TestLosAvisosNoImpidenGuardar(t *testing.T) {
	c := valid()
	c.Environment = Production
	c.SSLMode = SSLDisable
	if err := c.Validate(); err != nil {
		t.Fatalf("los avisos no deberían invalidar la conexión: %v", err)
	}
	if len(c.Warnings()) < 2 {
		t.Errorf("se esperaban avisos de ssl y de escritura, hubo %d", len(c.Warnings()))
	}
}

func TestProduccionExigeConfirmacionDeEscritura(t *testing.T) {
	for env, quiere := range map[Environment]bool{
		Local: false, Dev: false, Staging: false, Production: true,
	} {
		if got := env.NeedsWriteConfirmation(); got != quiere {
			t.Errorf("%s.NeedsWriteConfirmation() = %v", env, got)
		}
	}
}

// El nombre se mide en caracteres, no en bytes. En una app en español los
// acentos ocupan dos bytes y el mensaje diría un número que no es el que ve el
// usuario ni el que cuenta el frontend.
func TestElLimiteDeNombreCuentaCaracteresNoBytes(t *testing.T) {
	nombre := strings.Repeat("á", maxNameLength)
	c := valid()
	c.Name = nombre

	if len(nombre) <= maxNameLength {
		t.Fatalf("el caso de prueba no sirve: %d bytes para %d caracteres", len(nombre), maxNameLength)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("un nombre de %d caracteres fue rechazado: %v", maxNameLength, err)
	}

	c.Name = strings.Repeat("á", maxNameLength+1)
	if _, ok := fieldErrors(t, c)["name"]; !ok {
		t.Errorf("un nombre de %d caracteres debería ser rechazado", maxNameLength+1)
	}
}

// Este era el agujero: con el campo vacío no avisaba nada, pero el modo
// efectivo es prefer, que no verifica el certificado.
func TestAvisaAunqueElModoSSLEsteVacio(t *testing.T) {
	c := valid()
	c.SSLMode = ""

	var aviso *Warning
	for i, w := range c.Warnings() {
		if w.Field == "sslMode" {
			aviso = &c.Warnings()[i]
		}
	}
	if aviso == nil {
		t.Fatalf("sin modo SSL configurado no avisó nada; avisos: %v", c.Warnings())
	}
	if !strings.Contains(aviso.Message, "prefer") {
		t.Errorf("el aviso no explica cuál es el modo efectivo: %q", aviso.Message)
	}
}

func TestAvisaSobreProduccionSinVerificarCertificado(t *testing.T) {
	c := valid()
	c.Environment = Production
	c.SSLMode = SSLRequire
	c.ReadOnly = true

	avisos := c.Warnings()
	if len(avisos) == 0 {
		t.Fatal("producción con require —que cifra pero no verifica— debería avisar")
	}
	if !strings.Contains(avisos[0].Message, "verify-full") {
		t.Errorf("contra producción el aviso debería sugerir verify-full: %q", avisos[0].Message)
	}
}
