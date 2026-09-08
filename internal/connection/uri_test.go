package connection

import (
	"strings"
	"testing"
)

// La URI se muestra en pantalla y se copia al portapapeles: no puede llevar la
// contraseña ni por accidente.
func TestURINoLlevaContrasena(t *testing.T) {
	got := valid().URI()
	if strings.Contains(got, ":") && strings.Contains(got, "@") {
		// Tiene user@host, que está bien. Lo que no puede haber es user:algo@.
		antesDelArroba := got[strings.Index(got, "//")+2 : strings.Index(got, "@")]
		if strings.Contains(antesDelArroba, ":") {
			t.Errorf("la URI parece llevar una contraseña: %q", got)
		}
	}
	if got != "postgresql://app_rw@dev-db.internal:5432/shop_dev?sslmode=require" {
		t.Errorf("URI() = %q", got)
	}
}

func TestURIUsaElEsquemaQueLaGenteReconoce(t *testing.T) {
	if got := valid().URI(); !strings.HasPrefix(got, "postgresql://") {
		t.Errorf("URI() = %q, se esperaba el esquema postgresql://", got)
	}
}

func TestParseURICompletaLosCampos(t *testing.T) {
	c, pw, err := ParseURI("postgresql://app_rw:s3cr3t@stg-db.internal:6432/shop_stg?sslmode=verify-full")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if c.Engine != Postgres {
		t.Errorf("Engine = %q", c.Engine)
	}
	if c.Host != "stg-db.internal" {
		t.Errorf("Host = %q", c.Host)
	}
	if c.Port != 6432 {
		t.Errorf("Port = %d", c.Port)
	}
	if c.Database != "shop_stg" {
		t.Errorf("Database = %q", c.Database)
	}
	if c.User != "app_rw" {
		t.Errorf("User = %q", c.User)
	}
	if c.SSLMode != SSLVerifyFull {
		t.Errorf("SSLMode = %q", c.SSLMode)
	}
	if pw != "s3cr3t" {
		t.Errorf("la contraseña no se devolvió aparte: %q", pw)
	}
}

// La contraseña sale por su propio valor de retorno y NUNCA dentro del struct
// que se serializa a disco.
func TestParseURINoDejaLaContrasenaEnLaConexion(t *testing.T) {
	const pw = "ContraseñaQueNoDebeQuedar"
	c, devuelta, err := ParseURI("postgresql://u:" + pw + "@h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if devuelta != pw {
		t.Errorf("la contraseña devuelta = %q", devuelta)
	}
	// Ninguna representación de la conexión puede contenerla.
	for _, s := range []string{c.URI(), c.Describe(), c.String()} {
		if strings.Contains(s, pw) {
			t.Errorf("la contraseña quedó en %q", s)
		}
	}
}

func TestParseURIUsaElPuertoPorDefectoSiFalta(t *testing.T) {
	c, _, err := ParseURI("postgres://u@h/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if c.Port != 5432 {
		t.Errorf("Port = %d, se esperaba el default 5432", c.Port)
	}
}

func TestParseURIRechazaLoQueNoPuedeInterpretar(t *testing.T) {
	casos := map[string]string{
		"vacía":              "",
		"solo espacios":      "   ",
		"sin esquema":        "app_rw@host:5432/db",
		"motor desconocido":  "oracle://u@h/db",
		"puerto no numérico": "postgres://u@h:abc/db",
	}
	for nombre, entrada := range casos {
		t.Run(nombre, func(t *testing.T) {
			if _, _, err := ParseURI(entrada); err == nil {
				t.Errorf("ParseURI(%q) no falló", entrada)
			}
		})
	}
}

// El error de url.Parse cita la cadena entera. Si la cadena tenía contraseña,
// el mensaje la filtraría a un toast o a un log.
func TestElErrorDeParseNoFiltraLaContrasena(t *testing.T) {
	const pw = "SecretoQueNoDebeSalir"
	_, _, err := ParseURI("postgres://u:" + pw + "@h:99999999999999999999/db")
	if err == nil {
		t.Skip("esta URI resultó válida; el caso no aplica")
	}
	if strings.Contains(err.Error(), pw) {
		t.Errorf("el error filtra la contraseña: %v", err)
	}
}

// Pegar la URI que la app muestra tiene que reconstruir la misma conexión: es
// lo que hace útil el par Copy / Paste to fill fields de S03.
func TestURIYParseURISonInversas(t *testing.T) {
	original := valid()
	vuelta, pw, err := ParseURI(original.URI())
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if pw != "" {
		t.Errorf("la URI mostrada no debería traer contraseña, trajo %q", pw)
	}
	for _, campo := range []struct {
		nombre    string
		got, want any
	}{
		{"Engine", vuelta.Engine, original.Engine},
		{"Host", vuelta.Host, original.Host},
		{"Port", vuelta.Port, original.Port},
		{"Database", vuelta.Database, original.Database},
		{"User", vuelta.User, original.User},
		{"SSLMode", vuelta.SSLMode, original.SSLMode},
	} {
		if campo.got != campo.want {
			t.Errorf("%s = %v, se esperaba %v", campo.nombre, campo.got, campo.want)
		}
	}
}

// ParseURI llena solo lo que la cadena trae: el resto queda en cero para que
// quien llama decida si conserva lo que ya tenía en el formulario.
func TestParseURINoInventaCampos(t *testing.T) {
	c, _, err := ParseURI("postgres://h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if c.Name != "" {
		t.Errorf("Name = %q, la URI no lo trae", c.Name)
	}
	if c.ID != "" {
		t.Errorf("ID = %q, la URI no lo trae", c.ID)
	}
	if c.Environment != "" {
		t.Errorf("Environment = %q: la URI no dice el entorno, y adivinarlo sería peligroso", c.Environment)
	}
}

// En una app en español, una contraseña con ñ o acentos es esperable, y quien
// la pega no tiene por qué saber que una URI no admite bytes no ASCII.
func TestParseURIAceptaContrasenasConAcentos(t *testing.T) {
	casos := map[string]string{
		"eñe":      "contraseña",
		"acentos":  "máscláveñ",
		"japonés":  "秘密のパスワード",
		"emoji":    "clave🔐segura",
		"mezclado": "aB3-ñÁ_ü",
	}
	for nombre, pw := range casos {
		t.Run(nombre, func(t *testing.T) {
			c, devuelta, err := ParseURI("postgresql://app_rw:" + pw + "@h:5432/db")
			if err != nil {
				t.Fatalf("ParseURI() con contraseña %q falló: %v", pw, err)
			}
			if devuelta != pw {
				t.Errorf("la contraseña no sobrevivió: %q != %q", devuelta, pw)
			}
			if c.User != "app_rw" {
				t.Errorf("User = %q", c.User)
			}
			if c.Host != "h" {
				t.Errorf("Host = %q", c.Host)
			}
		})
	}
}

// Y no puede haber doble codificación: una contraseña ya escapada tiene que
// volver igual.
func TestParseURINoCodificaDosVeces(t *testing.T) {
	// %40 es una arroba escapada.
	_, pw, err := ParseURI("postgresql://u:a%40b@h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if pw != "a@b" {
		t.Errorf("la contraseña = %q, se esperaba a@b", pw)
	}
}

func TestParseURIAceptaHostYBaseConAcentos(t *testing.T) {
	c, _, err := ParseURI("postgresql://u@h:5432/contabilidad_españa")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if c.Database != "contabilidad_españa" {
		t.Errorf("Database = %q", c.Database)
	}
}
