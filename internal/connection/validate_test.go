package connection

import (
	"errors"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/tunnel"
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
	for _, campo := range []string{"host", "port", "user", "sslMode", "engine"} {
		if _, ok := got[campo]; ok {
			t.Errorf("SQLite no debería exigir %q, y pidió: %v", campo, got)
		}
	}
}

// Y al revés: los tres motores de servidor sí piden host y usuario. Sin esto,
// aflojar la validación para SQLite la aflojaría para todos.
func TestValidateSiPideHostYUsuarioParaLosMotoresDeServidor(t *testing.T) {
	for _, e := range []Engine{Postgres, MySQL, MariaDB} {
		t.Run(string(e), func(t *testing.T) {
			c := Connection{Name: "x", Engine: e, Database: "d", Environment: Local}
			got := fieldErrors(t, c)
			for _, campo := range []string{"host", "user"} {
				if _, ok := got[campo]; !ok {
					t.Errorf("%s debería exigir %q; se obtuvo %v", e, campo, got)
				}
			}
		})
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

	c.Safety.ReadOnly = false
	if len(c.Warnings()) == 0 {
		t.Error("producción con escritura habilitada debería avisar")
	}

	c.Safety.ReadOnly = true
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
	c.Safety.ReadOnly = true

	avisos := c.Warnings()
	if len(avisos) == 0 {
		t.Fatal("producción con require —que cifra pero no verifica— debería avisar")
	}
	if !strings.Contains(avisos[0].Message, "verify-full") {
		t.Errorf("contra producción el aviso debería sugerir verify-full: %q", avisos[0].Message)
	}
}

func TestValidateElTunelSSH(t *testing.T) {
	base := func() Connection {
		return Connection{
			ID: "a1", Name: "n", Engine: Postgres, Environment: Local,
			Host: "db.interna", Port: 5432, Database: "d", User: "u",
			SSH: tunnel.Config{
				Enabled: true, Host: "bastion.interna", Port: 22, User: "ops",
				Auth: tunnel.AuthAgent,
			},
		}
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("una configuración de túnel válida dio error: %v", err)
	}

	casos := []struct {
		nombre string
		tocar  func(*Connection)
		campo  string
	}{
		{"sin host", func(c *Connection) { c.SSH.Host = "" }, "ssh.host"},
		{"sin usuario", func(c *Connection) { c.SSH.User = "" }, "ssh.user"},
		{"puerto fuera de rango", func(c *Connection) { c.SSH.Port = 70000 }, "ssh.port"},
		{"método desconocido", func(c *Connection) { c.SSH.Auth = "magia" }, "ssh.auth"},
		{"clave sin ruta", func(c *Connection) { c.SSH.Auth = tunnel.AuthKeyFile }, "ssh.keyPath"},
		{"bastión igual a la base", func(c *Connection) {
			c.SSH.Host = "db.interna"
			c.SSH.Port = 5432
			c.Port = 5432
		}, "ssh.host"},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			c := base()
			tc.tocar(&c)
			err := c.Validate()
			if err == nil {
				t.Fatal("no dio error")
			}
			var v *ValidationError
			if !errors.As(err, &v) {
				t.Fatalf("el error no es de validación: %v", err)
			}
			if !v.Has(tc.campo) {
				t.Errorf("el error no señala el campo %q: %v", tc.campo, err)
			}
		})
	}
}

// Con el túnel apagado, sus campos no tienen por qué estar completos: apagarlo
// no debería obligar a borrar la configuración para poder guardar.
func TestUnTunelApagadoNoExigeSusCampos(t *testing.T) {
	c := Connection{
		ID: "a1", Name: "n", Engine: Postgres, Environment: Local,
		Host: "db", Port: 5432, Database: "d", User: "u",
		SSH: tunnel.Config{Enabled: false, Host: "", User: "", Auth: "magia"},
	}
	if err := c.Validate(); err != nil {
		t.Errorf("un túnel apagado hizo fallar la validación: %v", err)
	}
}

// Sin carpeta es un estado válido —es el de toda conexión que existía antes de
// que hubiera carpetas—, y con carpeta el límite es el del nombre.
func TestLaCarpetaEsOpcionalYTieneElLimiteDelNombre(t *testing.T) {
	c := valid()
	c.Folder = ""
	if err := c.Validate(); err != nil {
		t.Errorf("una conexión sin carpeta fue rechazada: %v", err)
	}

	c.Folder = strings.Repeat("á", maxNameLength)
	if err := c.Validate(); err != nil {
		t.Errorf("una carpeta de %d caracteres fue rechazada: %v", maxNameLength, err)
	}

	c.Folder = strings.Repeat("á", maxNameLength+1)
	if _, ok := fieldErrors(t, c)["folder"]; !ok {
		t.Errorf("una carpeta de %d caracteres debería ser rechazada, y en el campo folder", maxNameLength+1)
	}
}

// El certificado de cliente y su clave van juntos o no van. pgx rechaza las
// mitades sueltas con un error que habla de archivos, no de que falta la otra.
func TestElCertificadoDeClienteVaConSuClave(t *testing.T) {
	c := valid()
	c.TLS.ClientCertPath = "C:/certs/cliente.crt"
	got := fieldErrors(t, c)
	if _, ok := got["tls.clientKeyPath"]; !ok {
		t.Errorf("certificado sin clave: se esperaba un error en tls.clientKeyPath, hay %v", got)
	}

	c = valid()
	c.TLS.ClientKeyPath = "C:/certs/cliente.key"
	got = fieldErrors(t, c)
	if _, ok := got["tls.clientCertPath"]; !ok {
		t.Errorf("clave sin certificado: se esperaba un error en tls.clientCertPath, hay %v", got)
	}

	c = valid()
	c.TLS.ClientCertPath = "C:/certs/cliente.crt"
	c.TLS.ClientKeyPath = "C:/certs/cliente.key"
	if err := c.Validate(); err != nil {
		t.Errorf("el par completo tiene que ser válido: %v", err)
	}

	// Y la raíz sola es válida: es el caso común de una CA interna.
	c = valid()
	c.TLS.RootCertPath = "~/.postgresql/root.crt"
	if err := c.Validate(); err != nil {
		t.Errorf("solo la raíz tiene que ser válido: %v", err)
	}

	// En SQLite no se valida nada de esto: Normalize lo borra antes.
	c = valid()
	c.Engine, c.Database = SQLite, "x.db"
	c.TLS.ClientCertPath = "C:/certs/cliente.crt"
	if err := c.Validate(); err != nil {
		t.Errorf("SQLite con un certificado suelto no puede fallar: %v", err)
	}
}

// Cargar la raíz es cargarla PARA que verifique: con ella, `require` deja de
// avisar que no verifica. Sin este cambio, la persona cargaba la raíz y el
// aviso seguía ahí, diciendo lo contrario de lo que pasaba.
func TestConRaizCargadaRequireYaNoAvisa(t *testing.T) {
	c := valid()
	c.SSLMode = SSLRequire
	if !tieneAviso(c, "sslMode") {
		t.Fatal("require sin raíz tiene que avisar que no verifica")
	}
	c.TLS.RootCertPath = "~/.postgresql/root.crt"
	if tieneAviso(c, "sslMode") {
		t.Errorf("require con raíz verifica la cadena y no debería avisar: %v", c.Warnings())
	}
	// prefer con raíz sigue sin verificar: la raíz no cambia ese modo.
	c.SSLMode = SSLPrefer
	if !tieneAviso(c, "sslMode") {
		t.Error("prefer con raíz sigue sin verificar y tiene que avisar")
	}
}

// Certificados cargados con el cifrado apagado no se usan, y se dice.
func TestAvisaSiHayCertificadosConElCifradoApagado(t *testing.T) {
	c := valid()
	c.SSLMode = SSLDisable
	c.TLS.RootCertPath = "~/ca.pem"
	if !tieneAviso(c, "tls.rootCertPath") {
		t.Errorf("disable con raíz cargada tiene que avisar; avisos: %v", c.Warnings())
	}
	c.TLS = TLS{}
	if tieneAviso(c, "tls.rootCertPath") {
		t.Error("sin certificados no hay nada que avisar")
	}
}

func tieneAviso(c Connection, campo string) bool {
	for _, w := range c.Warnings() {
		if w.Field == campo {
			return true
		}
	}
	return false
}

func TestValidaLaPestanaAdvanced(t *testing.T) {
	casos := []struct {
		nombre string
		tocar  func(*Advanced)
		campo  string
	}{
		{"nombre con dos puntos", func(a *Advanced) { a.ApplicationName = "app:x" }, "advanced.applicationName"},
		{"nombre con coma", func(a *Advanced) { a.ApplicationName = "app,x" }, "advanced.applicationName"},
		{"nombre largo", func(a *Advanced) { a.ApplicationName = strings.Repeat("n", 64) }, "advanced.applicationName"},
		{"search_path en dos líneas", func(a *Advanced) { a.SearchPath = "a,\nb" }, "advanced.searchPath"},
		{"pool de uno", func(a *Advanced) { a.PoolSize = 1 }, "advanced.poolSize"},
		{"pool negativo", func(a *Advanced) { a.PoolSize = -1 }, "advanced.poolSize"},
		{"pool enorme", func(a *Advanced) { a.PoolSize = 33 }, "advanced.poolSize"},
		{"sesión gigante", func(a *Advanced) { a.SessionSQL = strings.Repeat("SET x = 1;\n", 7000) }, "advanced.sessionSql"},
	}
	for _, c := range casos {
		con := valid()
		c.tocar(&con.Advanced)
		if _, ok := fieldErrors(t, con)[c.campo]; !ok {
			t.Errorf("%s: se esperaba un error en %s, hay %v", c.nombre, c.campo, fieldErrors(t, con))
		}
	}

	// Lo válido: 63 caracteres, un pool de 2 o de 32, cero como default.
	con := valid()
	con.Advanced = Advanced{ApplicationName: strings.Repeat("n", 63), PoolSize: 32, SearchPath: "a, b", SessionSQL: "SET lock_timeout = '3s'"}
	if err := con.Validate(); err != nil {
		t.Errorf("una pestaña Advanced válida dio error: %v", err)
	}
	con.Advanced.PoolSize = 2
	if err := con.Validate(); err != nil {
		t.Errorf("pool de 2: %v", err)
	}
}

// La SQL de sesión corre sin vista previa ni confirmación: si tiene algo que
// no sea configurar, se avisa, y contra producción se dice más fuerte.
func TestAvisaSiLaSQLDeSesionHaceAlgoQueNoEsConfigurar(t *testing.T) {
	c := valid()
	c.Advanced.SessionSQL = "SET lock_timeout = '3s';\n-- comentario\nRESET search_path;"
	if tieneAviso(c, "advanced.sessionSql") {
		t.Errorf("SET y RESET son configurar y no tienen que avisar: %v", c.Warnings())
	}

	c.Advanced.SessionSQL = "SET lock_timeout = '3s';\nDELETE FROM auditoria;\nINSERT INTO log VALUES (1);\nDELETE FROM otra;"
	var aviso *Warning
	for i, w := range c.Warnings() {
		if w.Field == "advanced.sessionSql" {
			aviso = &c.Warnings()[i]
		}
	}
	if aviso == nil {
		t.Fatalf("un DELETE en la SQL de sesión tiene que avisar: %v", c.Warnings())
	}
	if !strings.Contains(aviso.Message, "DELETE, INSERT") {
		t.Errorf("el aviso tiene que nombrar los comandos, sin repetir: %q", aviso.Message)
	}
	if strings.Contains(aviso.Message, "producción") {
		t.Errorf("en dev no corresponde la frase de producción: %q", aviso.Message)
	}
	c.Environment = Production
	if !strings.Contains(warningDe(c, "advanced.sessionSql"), "producción") {
		t.Errorf("contra producción el aviso tiene que decirlo: %q", warningDe(c, "advanced.sessionSql"))
	}
	// Con solo lectura, la escritura no corre: la conexión no abre, y el
	// aviso dice eso y no «escritura automática».
	c.Safety.ReadOnly = true
	if aviso := warningDe(c, "advanced.sessionSql"); !strings.Contains(aviso, "no va a abrir") || strings.Contains(aviso, "automática") {
		t.Errorf("con solo lectura el aviso tiene que decir que la conexión no abre: %q", aviso)
	}

	// En MySQL el comentario es con # y se saltea igual.
	c = valid()
	c.Engine = MySQL
	c.Advanced.SessionSQL = "# arranque\nSET NAMES utf8mb4;"
	if tieneAviso(c, "advanced.sessionSql") {
		t.Errorf("un SET con un comentario # arriba no tiene que avisar: %v", c.Warnings())
	}
}

func warningDe(c Connection, campo string) string {
	for _, w := range c.Warnings() {
		if w.Field == campo {
			return w.Message
		}
	}
	return ""
}
