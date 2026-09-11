package connection

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
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

// La contraseña vive en el keychain, nunca en la estructura que se serializa a
// disco. Este test reflexiona sobre los campos reales porque mirar la salida de
// fmt no sirve: Connection implementa Stringer, así que fmt rutea también %+v
// por String() y un campo Password quedaría igual de invisible.
func TestConnectionNoTieneNingunCampoDeSecreto(t *testing.T) {
	prohibidos := []string{"password", "passwd", "secret", "token", "credential"}
	tipo := reflect.TypeOf(Connection{})

	for i := 0; i < tipo.NumField(); i++ {
		f := tipo.Field(i)
		candidatos := []string{
			strings.ToLower(f.Name),
			strings.ToLower(f.Tag.Get("toml")),
			strings.ToLower(f.Tag.Get("json")),
		}
		for _, c := range candidatos {
			for _, p := range prohibidos {
				if strings.Contains(c, p) {
					t.Errorf("Connection.%s parece guardar un secreto (%q): las contraseñas van al keychain", f.Name, c)
				}
			}
		}
	}
}

// Y además, un log descuidado de la estructura completa no puede filtrar nada.
func TestFormatearUnaConnectionUsaLaDescripcionSegura(t *testing.T) {
	got := fmt.Sprintf("%+v", valid())
	if got != valid().Describe() {
		t.Errorf("%%+v = %q, se esperaba la descripción segura %q", got, valid().Describe())
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

// El DSN de cada motor tiene un formato propio, y no son variaciones del
// mismo: el de MySQL no es una URI, y el de SQLite no tiene ni host ni usuario.
// Confundirlos no da un error claro sino una conexión que no abre.
func TestDSNPorMotor(t *testing.T) {
	casos := []struct {
		motor    Engine
		ajuste   func(*Connection)
		contiene []string
		noTiene  []string
	}{
		{
			motor:    Postgres,
			contiene: []string{"postgres://", "sslmode=", "application_name=kaname", ":5432/"},
		},
		{
			motor:    MySQL,
			contiene: []string{"@tcp(", ":3306)/", "multiStatements=false"},
			// El formato de go-sql-driver NO es una URI. Si apareciera un
			// esquema, es que se armó con el molde de Postgres.
			// Y sin parseTime: las fechas tienen que llegar como las escribe el
			// servidor, no reformateadas por Go. Ver dsnMySQL.
			// Y sin `tls=`: el cifrado de MySQL se arma en internal/mysql a
			// partir de TLSOptions, no del DSN. Un `tls=` acá sería un segundo
			// lugar que decide lo mismo.
			noTiene: []string{"mysql://", "parseTime", "tls="},
		},
		{
			motor:    MariaDB,
			contiene: []string{"@tcp(", ":3306)/"},
			noTiene:  []string{"mariadb://"},
		},
		{
			motor:    SQLite,
			ajuste:   func(c *Connection) { c.Database = "C:/tmp/local.db" },
			contiene: []string{"file:", "C:/tmp/local.db", "foreign_keys%281%29"},
			// Sin host, sin puerto y sin usuario: no existen para un archivo.
			noTiene: []string{"@", "5432", "3306"},
		},
	}

	for _, c := range casos {
		t.Run(string(c.motor), func(t *testing.T) {
			con := valid()
			con.Engine = c.motor
			con.Port = 0 // que use el default del motor
			if c.ajuste != nil {
				c.ajuste(&con)
			}
			dsn, err := con.DSN("secreta")
			if err != nil {
				t.Fatalf("DSN() error: %v", err)
			}
			for _, q := range c.contiene {
				if !strings.Contains(dsn, q) {
					t.Errorf("el DSN no contiene %q: %s", q, dsn)
				}
			}
			for _, q := range c.noTiene {
				if strings.Contains(dsn, q) {
					t.Errorf("el DSN no debería contener %q: %s", q, dsn)
				}
			}
		})
	}
}

// Las claves foráneas de SQLite vienen APAGADAS por defecto y hay que
// encenderlas por conexión. Sin eso el diagrama dibujaría relaciones que la
// base no hace cumplir, que es peor que no dibujarlas.
func TestDSNDeSQLiteEnciendeLasClavesForaneas(t *testing.T) {
	c := valid()
	c.Engine = SQLite
	c.Database = "/tmp/x.db"
	dsn, err := c.DSN("")
	if err != nil {
		t.Fatalf("DSN() error: %v", err)
	}
	if !strings.Contains(dsn, "foreign_keys%281%29") {
		t.Errorf("el DSN no enciende foreign_keys: %s", dsn)
	}
}

// Una contraseña con @ tiene que sobrevivir. El formato de MySQL no escapa
// nada, pero el driver parte por el ÚLTIMO @, así que funciona igual.
func TestDSNDeMySQLAguantaUnaContrasenaConArroba(t *testing.T) {
	c := valid()
	c.Engine = MySQL
	dsn, err := c.DSN("pa@ss")
	if err != nil {
		t.Fatalf("DSN() error: %v", err)
	}
	if !strings.Contains(dsn, "pa@ss@tcp(") {
		t.Errorf("la contraseña no quedó antes del último @: %s", dsn)
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

func TestDSNFallaSinUsuarioEnVezDeDescartarLaContrasena(t *testing.T) {
	c := valid()
	c.User = ""
	dsn, err := c.DSN("s3cr3t")
	if err == nil {
		t.Fatalf("DSN() sin usuario no devolvió error, devolvió %q", dsn)
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("el error filtra la contraseña: %v", err)
	}
}

func TestNormalizeLimpiaLosEnums(t *testing.T) {
	c := Connection{
		ID:          "a1",
		Name:        "x",
		Engine:      " Postgres ",
		Host:        "h",
		Database:    "d",
		User:        "u",
		Environment: "  DEV ",
		SSLMode:     " Require ",
	}.Normalize()

	if c.Engine != Postgres {
		t.Errorf("Engine = %q, se esperaba %q", c.Engine, Postgres)
	}
	if c.Environment != Dev {
		t.Errorf("Environment = %q, se esperaba %q", c.Environment, Dev)
	}
	if c.SSLMode != SSLRequire {
		t.Errorf("SSLMode = %q, se esperaba %q", c.SSLMode, SSLRequire)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("una conexión con enums sucios pero válidos fue rechazada: %v", err)
	}
}

// Un SSLMode vacío no es "sin configurar": es prefer, que no verifica el
// certificado y acepta seguir en claro.
func TestElModoSSLEfectivoDeUnCampoVacioEsPrefer(t *testing.T) {
	c := valid()
	c.SSLMode = ""
	if got := c.EffectiveSSLMode(); got != SSLPrefer {
		t.Errorf("EffectiveSSLMode() = %q, se esperaba %q", got, SSLPrefer)
	}
	if c.EffectiveSSLMode().Verifies() {
		t.Error("prefer no verifica el certificado y no debería decir que sí")
	}
}

// Sin el parámetro de base, PostgreSQL usa el NOMBRE DEL USUARIO como base por
// defecto. La app se conectaría en silencio a una base distinta de la que el
// usuario cree, que es peor que fallar.
func TestDSNFallaSinBaseEnVezDeConectarACualquiera(t *testing.T) {
	c := valid()
	c.Database = ""
	dsn, err := c.DSN("x")
	if err == nil {
		t.Fatalf("DSN() sin base no devolvió error, devolvió %q", dsn)
	}
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("el error no nombra el campo: %v", err)
	}
}

// El DSN es la última puerta antes de la red: el archivo de conexiones se edita
// a mano y llega hasta acá sin garantías.
func TestDSNValidaLoQueNecesitaParaConectar(t *testing.T) {
	casos := map[string]func(*Connection){
		"sin host":          func(c *Connection) { c.Host = "" },
		"sin base":          func(c *Connection) { c.Database = "" },
		"sin usuario":       func(c *Connection) { c.User = "" },
		"puerto inválido":   func(c *Connection) { c.Port = 70000 },
		"ssl desconocido":   func(c *Connection) { c.SSLMode = "sí-porfa" },
		"motor desconocido": func(c *Connection) { c.Engine = "oracle" },
	}
	for nombre, romper := range casos {
		t.Run(nombre, func(t *testing.T) {
			c := valid()
			romper(&c)
			if _, err := c.DSN("x"); err == nil {
				t.Error("DSN() no falló")
			}
		})
	}
}

// Pero no exige nombre ni identificador: eso es de la libreta de conexiones,
// no del protocolo.
func TestDSNNoExigeNombreNiIdentificador(t *testing.T) {
	c := valid()
	c.ID = ""
	c.Name = ""
	if _, err := c.DSN("x"); err != nil {
		t.Errorf("DSN() exigió datos que no hacen falta para conectar: %v", err)
	}
}

// La carpeta agrupa por igualdad exacta, así que lo que no se ve no puede
// separar dos conexiones del mismo proyecto.
func TestNormalizeLimpiaLaCarpeta(t *testing.T) {
	casos := map[string]string{
		"  Ahorra  ":     "Ahorra",
		"Ahorra   app":   "Ahorra app",
		"Ahorra\tapp\n":  "Ahorra app",
		"":               "",
		"   ":            "",
		"Nutrigo · 2026": "Nutrigo · 2026",
	}
	for crudo, quiere := range casos {
		c := valid()
		c.Folder = crudo
		if got := c.Normalize().Folder; got != quiere {
			t.Errorf("Normalize() con Folder=%q dio %q, se esperaba %q", crudo, got, quiere)
		}
	}
}

// Los certificados de Postgres van en el DSN con los nombres de libpq, que es
// donde pgx los lee, y con el `~` resuelto: pgx abre la ruta tal cual.
func TestElDSNDePostgresLlevaLosCertificadosResueltos(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("sin directorio del usuario:", err)
	}
	con := valid()
	con.TLS = TLS{
		RootCertPath:   "~/.postgresql/root.crt",
		ClientCertPath: `"C:\certs\cliente.crt"`, // con comillas del Explorador
		ClientKeyPath:  "C:/certs/cliente.key",
	}
	dsn, err := con.DSN("secreta")
	if err != nil {
		t.Fatalf("DSN() error: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if got, want := q.Get("sslrootcert"), filepath.Join(home, ".postgresql", "root.crt"); got != want {
		t.Errorf("sslrootcert = %q, se esperaba %q", got, want)
	}
	if got := q.Get("sslcert"); got != `C:\certs\cliente.crt` {
		t.Errorf("sslcert = %q: las comillas tenían que salir", got)
	}
	if got := q.Get("sslkey"); got != "C:/certs/cliente.key" {
		t.Errorf("sslkey = %q", got)
	}

	// Y sin certificados, ninguno de los tres parámetros aparece: pgx tiene
	// defaults por directorio del usuario y un parámetro vacío los pisaría.
	dsn, _ = valid().DSN("secreta")
	for _, p := range []string{"sslrootcert", "sslcert", "sslkey"} {
		if strings.Contains(dsn, p) {
			t.Errorf("el DSN sin certificados no debería llevar %q: %s", p, dsn)
		}
	}
}

// TLSOptions es lo que recibe el motor de MySQL: el modo efectivo y las rutas
// resueltas. SQLite no tiene canal que cifrar y recibe el cero.
func TestTLSOptionsResuelveLasRutasYElModo(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("sin directorio del usuario:", err)
	}
	con := valid()
	con.Engine = MySQL
	con.SSLMode = ""
	con.TLS.RootCertPath = " ~/ca.pem "
	got, err := con.TLSOptions()
	if err != nil {
		t.Fatalf("TLSOptions() error: %v", err)
	}
	want := engine.TLSOptions{Mode: SSLPrefer, RootCert: filepath.Join(home, "ca.pem")}
	if got != want {
		t.Errorf("TLSOptions() = %+v, se esperaba %+v", got, want)
	}

	con.Engine = SQLite
	con.Database = "C:/tmp/x.db"
	got, err = con.TLSOptions()
	if err != nil || got != (engine.TLSOptions{}) {
		t.Errorf("con SQLite TLSOptions() = %+v, %v; se esperaba el cero", got, err)
	}
}

// `require` a secas no verifica; con una raíz cargada, sí —es lo que hace
// libpq—. El aviso de la interfaz sale de acá, así que si esto mintiera, el
// aviso aparecería justo después de cargar la raíz para que verifique.
func TestVerifiesCertificateMiraLaRaizAdemasDelModo(t *testing.T) {
	casos := []struct {
		modo SSLMode
		raiz string
		want bool
	}{
		{SSLDisable, "", false},
		{SSLPrefer, "", false},
		{SSLPrefer, "~/ca.pem", false},
		{SSLRequire, "", false},
		{SSLRequire, "~/ca.pem", true},
		{SSLVerifyCA, "", true},
		{SSLVerifyFull, "", true},
	}
	for _, c := range casos {
		con := valid()
		con.SSLMode = c.modo
		con.TLS.RootCertPath = c.raiz
		if got := con.VerifiesCertificate(); got != c.want {
			t.Errorf("%s con raíz %q: VerifiesCertificate() = %v, se esperaba %v", c.modo, c.raiz, got, c.want)
		}
	}
	con := valid()
	con.Engine, con.Database, con.SSLMode = SQLite, "x.db", SSLVerifyFull
	if con.VerifiesCertificate() {
		t.Error("SQLite no tiene certificado que verificar")
	}
}

// SQLite es un archivo: los certificados no tienen sentido y Normalize los
// borra, como hace con el host y el usuario.
func TestNormalizeBorraLosCertificadosDeSQLite(t *testing.T) {
	con := valid()
	con.Engine, con.Database = SQLite, "C:/tmp/x.db"
	con.TLS = TLS{RootCertPath: "ca.pem", ClientCertPath: "c.crt", ClientKeyPath: "c.key"}
	if got := con.Normalize().TLS; got != (TLS{}) {
		t.Errorf("TLS de SQLite = %+v, se esperaba vacío", got)
	}
}

// El nombre de la aplicación y el search_path van en el DSN de Postgres como
// parámetros de arranque, así llegan a cada conexión del pool. Sin nombre,
// `kaname`; sin search_path, nada —el servidor usa el suyo—.
func TestElDSNDePostgresLlevaLoAvanzado(t *testing.T) {
	con := valid()
	con.Advanced = Advanced{SearchPath: "kn_app, public", ApplicationName: "lemy en dev"}
	dsn, err := con.DSN("secreta")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	if got := u.Query().Get("search_path"); got != "kn_app, public" {
		t.Errorf("search_path = %q", got)
	}
	if got := u.Query().Get("application_name"); got != "lemy en dev" {
		t.Errorf("application_name = %q", got)
	}
	// Y los espacios van como %20: pgx decodifica como libpq, que no entiende
	// el + de url.Values, y el nombre llegaba al servidor como «lemy+en+dev».
	if strings.Contains(u.RawQuery, "+") || !strings.Contains(u.RawQuery, "lemy%20en%20dev") {
		t.Errorf("la cadena lleva + por espacio: %s", u.RawQuery)
	}

	dsn, _ = valid().DSN("secreta")
	u, _ = url.Parse(dsn)
	if got := u.Query().Get("application_name"); got != "kaname" {
		t.Errorf("sin nombre, application_name = %q, se esperaba kaname", got)
	}
	if u.Query().Has("search_path") {
		t.Errorf("sin search_path no tiene que viajar ninguno: %s", dsn)
	}
}

// En MySQL el nombre va como atributo de sesión, con la clave que miran el
// cliente de línea de comandos y Workbench. El search_path no existe y no
// sobrevive a Normalize.
func TestElDSNDeMySQLLlevaElNombreComoAtributo(t *testing.T) {
	con := valid()
	con.Engine, con.Port = MySQL, 0
	con.Advanced = Advanced{ApplicationName: "lemy", SearchPath: "public"}
	dsn, err := con.DSN("secreta")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "connectionAttributes=program_name%3Alemy") {
		t.Errorf("el DSN no lleva el atributo program_name: %s", dsn)
	}
	if strings.Contains(dsn, "search_path") {
		t.Errorf("MySQL no tiene search_path y el DSN lo lleva: %s", dsn)
	}
	if got := con.Normalize().Advanced.SearchPath; got != "" {
		t.Errorf("Normalize dejó search_path=%q en una conexión de MySQL", got)
	}
}

// SQLite no se presenta ante nadie ni tiene search_path; la SQL de sesión y
// el tamaño del pool sí valen.
func TestNormalizeDejaEnSQLiteSoloLoQueAplica(t *testing.T) {
	con := valid()
	con.Engine, con.Database = SQLite, "C:/tmp/x.db"
	con.Advanced = Advanced{SearchPath: "main", ApplicationName: "x", PoolSize: 2, SessionSQL: " PRAGMA cache_size = -2000; "}
	got := con.Normalize().Advanced
	want := Advanced{PoolSize: 2, SessionSQL: "PRAGMA cache_size = -2000;"}
	if got != want {
		t.Errorf("Advanced = %+v, se esperaba %+v", got, want)
	}
}
