package connection

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func valid() Connection {
	return Connection{
		ID:          "abc123",
		Name:        "shop dev",
		Engine:      Postgres,
		Host:        "dev-db.internal",
		Port:        5432,
		Database:    "shop_dev",
		User:        "app_rw",
		Environment: Dev,
		SSLMode:     SSLRequire,
	}
}

func TestNewIDEsUnicoYNoVacio(t *testing.T) {
	vistos := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatalf("NewID() error: %v", err)
		}
		if id == "" {
			t.Fatal("NewID() devolvió cadena vacía")
		}
		if vistos[id] {
			t.Fatalf("NewID() repitió %q", id)
		}
		vistos[id] = true
	}
}

// La contraseña vive en el keychain. Si alguien agrega un campo para guardarla
// en la estructura que se serializa a disco, este test tiene que romper.
func TestConnectionNoTieneCampoDeContrasena(t *testing.T) {
	prohibidos := []string{"password", "passwd", "secret", "token", "credential"}

	// %+v es lo que sale de un log descuidado. Como Connection implementa
	// Stringer, tiene que imprimir la descripción segura y nada más.
	rendered := strings.ToLower(fmt.Sprintf("%+v %v %s", valid(), valid(), valid()))
	for _, p := range prohibidos {
		if strings.Contains(rendered, p) {
			t.Errorf("la representación de Connection contiene %q: %s", p, rendered)
		}
	}
}

func TestDescribeNoFiltraNada(t *testing.T) {
	got := valid().Describe()
	want := "app_rw@dev-db.internal:5432/shop_dev"
	if got != want {
		t.Errorf("Describe() = %q, se esperaba %q", got, want)
	}
}

func TestDescribeConIPv6(t *testing.T) {
	c := valid()
	c.Host = "::1"
	if got, want := c.Describe(), "app_rw@[::1]:5432/shop_dev"; got != want {
		t.Errorf("Describe() = %q, se esperaba %q", got, want)
	}
}

func TestDSNIncluyeLaContrasenaYLosParametros(t *testing.T) {
	dsn, err := valid().DSN("s3cr3t")
	if err != nil {
		t.Fatalf("DSN() error: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("el DSN no es una URL válida: %v", err)
	}
	if u.Scheme != "postgres" {
		t.Errorf("scheme = %q, se esperaba postgres", u.Scheme)
	}
	if u.Host != "dev-db.internal:5432" {
		t.Errorf("host = %q", u.Host)
	}
	if u.Path != "/shop_dev" {
		t.Errorf("path = %q", u.Path)
	}
	if pw, _ := u.User.Password(); pw != "s3cr3t" {
		t.Errorf("contraseña = %q", pw)
	}
	if got := u.Query().Get("sslmode"); got != "require" {
		t.Errorf("sslmode = %q", got)
	}
	if got := u.Query().Get("application_name"); got != "kaname" {
		t.Errorf("application_name = %q", got)
	}
}

// Una contraseña con caracteres que son separadores de URL tiene que quedar
// escapada. Sin esto, una contraseña con `@` mandaría la conexión a otro host.
func TestDSNEscapaContrasenasHostiles(t *testing.T) {
	casos := []string{
		"pa@ss",
		"pa/ss",
		"pa:ss",
		"p@ss/word:1#2?3",
		"evil@attacker.example",
		"ñ áéí ü",
	}
	for _, pw := range casos {
		t.Run(pw, func(t *testing.T) {
			dsn, err := valid().DSN(pw)
			if err != nil {
				t.Fatalf("DSN() error: %v", err)
			}
			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("el DSN no parsea: %v", err)
			}
			if u.Host != "dev-db.internal:5432" {
				t.Fatalf("la contraseña cambió el host: %q", u.Host)
			}
			got, _ := u.User.Password()
			if got != pw {
				t.Errorf("la contraseña no sobrevivió el round-trip: %q != %q", got, pw)
			}
		})
	}
}

func TestDSNUsaElPuertoPorDefectoSiFaltaba(t *testing.T) {
	c := valid()
	c.Port = 0
	dsn, err := c.DSN("x")
	if err != nil {
		t.Fatalf("DSN() error: %v", err)
	}
	if !strings.Contains(dsn, ":5432/") {
		t.Errorf("no usó el puerto por defecto: %s", dsn)
	}
}

func TestDSNRechazaMotoresNoImplementados(t *testing.T) {
	for _, e := range []Engine{MySQL, MariaDB, SQLite} {
		c := valid()
		c.Engine = e
		if _, err := c.DSN("x"); err == nil {
			t.Errorf("DSN() con motor %s no devolvió error", e)
		}
	}
}

func TestNormalizeCompletaYLimpia(t *testing.T) {
	c := Connection{
		Name:     "  shop dev  ",
		Engine:   Postgres,
		Host:     " db.local ",
		Database: " shop ",
		User:     " rw ",
	}.Normalize()

	if c.Name != "shop dev" {
		t.Errorf("Name = %q", c.Name)
	}
	if c.Host != "db.local" {
		t.Errorf("Host = %q", c.Host)
	}
	if c.Port != 5432 {
		t.Errorf("Port = %d, se esperaba el default 5432", c.Port)
	}
	if c.Environment != Local {
		t.Errorf("Environment = %q, el default seguro es local", c.Environment)
	}
	if c.SSLMode != SSLPrefer {
		t.Errorf("SSLMode = %q", c.SSLMode)
	}
}

// Si no se dice nada, la conexión NO puede quedar marcada como producción.
func TestNormalizeNuncaAsumeProduccion(t *testing.T) {
	c := Connection{Engine: Postgres}.Normalize()
	if c.Environment == Production {
		t.Fatal("Normalize() marcó la conexión como producción por defecto")
	}
}
