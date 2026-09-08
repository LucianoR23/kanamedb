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
	got, err := ParseURI("postgresql://app_rw:s3cr3t@stg-db.internal:6432/shop_stg?sslmode=verify-full")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Connection.Engine != Postgres {
		t.Errorf("Engine = %q", got.Connection.Engine)
	}
	if got.Connection.Host != "stg-db.internal" {
		t.Errorf("Host = %q", got.Connection.Host)
	}
	if got.Connection.Port != 6432 {
		t.Errorf("Port = %d", got.Connection.Port)
	}
	if got.Connection.Database != "shop_stg" {
		t.Errorf("Database = %q", got.Connection.Database)
	}
	if got.Connection.User != "app_rw" {
		t.Errorf("User = %q", got.Connection.User)
	}
	if got.Connection.SSLMode != SSLVerifyFull {
		t.Errorf("SSLMode = %q", got.Connection.SSLMode)
	}
	if got.Password != "s3cr3t" {
		t.Errorf("la contraseña no se devolvió aparte: %q", got.Password)
	}
}

// La contraseña sale por su propio valor de retorno y NUNCA dentro del struct
// que se serializa a disco.
func TestParseURINoDejaLaContrasenaEnLaConexion(t *testing.T) {
	const pw = "ContraseñaQueNoDebeQuedar"
	got, err := ParseURI("postgresql://u:" + pw + "@h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Password != pw {
		t.Errorf("la contraseña devuelta = %q", got.Password)
	}
	// Ninguna representación de la conexión puede contenerla.
	for _, s := range []string{got.Connection.URI(), got.Connection.Describe(), got.Connection.String()} {
		if strings.Contains(s, pw) {
			t.Errorf("la contraseña quedó en %q", s)
		}
	}
}

func TestParseURIUsaElPuertoPorDefectoSiFalta(t *testing.T) {
	got, err := ParseURI("postgres://u@h/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Connection.Port != 5432 {
		t.Errorf("Port = %d, se esperaba el default 5432", got.Connection.Port)
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
			if _, err := ParseURI(entrada); err == nil {
				t.Errorf("ParseURI(%q) no falló", entrada)
			}
		})
	}
}

// El error de url.Parse cita la cadena entera. Si la cadena tenía contraseña,
// el mensaje la filtraría a un toast o a un log.
func TestElErrorDeParseNoFiltraLaContrasena(t *testing.T) {
	const pw = "SecretoQueNoDebeSalir"
	_, err := ParseURI("postgres://u:" + pw + "@h:99999999999999999999/db")
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
	got, err := ParseURI(original.URI())
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Password != "" {
		t.Errorf("la URI mostrada no debería traer contraseña, trajo %q", got.Password)
	}
	for _, campo := range []struct {
		nombre    string
		got, want any
	}{
		{"Engine", got.Connection.Engine, original.Engine},
		{"Host", got.Connection.Host, original.Host},
		{"Port", got.Connection.Port, original.Port},
		{"Database", got.Connection.Database, original.Database},
		{"User", got.Connection.User, original.User},
		{"SSLMode", got.Connection.SSLMode, original.SSLMode},
	} {
		if campo.got != campo.want {
			t.Errorf("%s = %v, se esperaba %v", campo.nombre, campo.got, campo.want)
		}
	}
}

// ParseURI llena solo lo que la cadena trae: el resto queda en cero para que
// quien llama decida si conserva lo que ya tenía en el formulario.
func TestParseURINoInventaCampos(t *testing.T) {
	got, err := ParseURI("postgres://h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Connection.Name != "" {
		t.Errorf("Name = %q, la URI no lo trae", got.Connection.Name)
	}
	if got.Connection.ID != "" {
		t.Errorf("ID = %q, la URI no lo trae", got.Connection.ID)
	}
	if got.Connection.Environment != "" {
		t.Errorf("Environment = %q: la URI no dice el entorno, y adivinarlo sería peligroso", got.Connection.Environment)
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
			got, err := ParseURI("postgresql://app_rw:" + pw + "@h:5432/db")
			if err != nil {
				t.Fatalf("ParseURI() con contraseña %q falló: %v", pw, err)
			}
			if got.Password != pw {
				t.Errorf("la contraseña no sobrevivió: %q != %q", got.Password, pw)
			}
			if got.Connection.User != "app_rw" {
				t.Errorf("User = %q", got.Connection.User)
			}
			if got.Connection.Host != "h" {
				t.Errorf("Host = %q", got.Connection.Host)
			}
		})
	}
}

// Y no puede haber doble codificación: una contraseña ya escapada tiene que
// volver igual.
func TestParseURINoCodificaDosVeces(t *testing.T) {
	// %40 es una arroba escapada.
	got, err := ParseURI("postgresql://u:a%40b@h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Password != "a@b" {
		t.Errorf("la contraseña = %q, se esperaba a@b", got.Password)
	}
}

func TestParseURIAceptaHostYBaseConAcentos(t *testing.T) {
	got, err := ParseURI("postgresql://u@h:5432/contabilidad_españa")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Connection.Database != "contabilidad_españa" {
		t.Errorf("Database = %q", got.Connection.Database)
	}
}

// Una secuencia %XX en la contraseña es ambigua y no se puede resolver: `%20`
// puede ser un espacio escapado o un por ciento seguido de "20". Se interpreta
// como manda el estándar y se avisa, porque si no el usuario pega, prueba la
// conexión, recibe "credenciales rechazadas" y no tiene cómo darse cuenta: el
// campo está enmascarado.
func TestAvisaCuandoLaContrasenaTeniaEscapesAmbiguos(t *testing.T) {
	got, err := ParseURI("postgresql://u:100%descuento@h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if len(got.Notices) == 0 {
		t.Fatalf("no avisó nada; la contraseña quedó como %q", got.Password)
	}
	if !strings.Contains(got.Notices[0], "%") {
		t.Errorf("el aviso no explica el problema: %q", got.Notices[0])
	}
	// Y la interpretación es la del estándar: %de es el byte 0xDE.
	if got.Password == "100%descuento" {
		t.Error("no debería adivinar: es un escape hexadecimal válido y el estándar dice cómo se lee")
	}
}

// Sin escapes en la contraseña, no hay nada que aclarar.
func TestNoAvisaSiNoHayEscapes(t *testing.T) {
	got, err := ParseURI("postgresql://u:simple123@h:5432/db")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if len(got.Notices) != 0 {
		t.Errorf("avisó sin motivo: %v", got.Notices)
	}
}

// Un % suelto no es un escape válido y url.Parse falla. El mensaje tiene que
// decir qué hacer, no "no se pudo interpretar".
func TestElErrorDeUnPorcientoSueltoExplicaQueHacer(t *testing.T) {
	_, err := ParseURI("postgresql://u:a%b@h:5432/db")
	if err == nil {
		t.Fatal("ParseURI() no falló con un % suelto")
	}
	if !strings.Contains(err.Error(), "%25") {
		t.Errorf("el mensaje no dice cómo escribir el por ciento: %v", err)
	}
	if !strings.Contains(err.Error(), "Password") {
		t.Errorf("el mensaje no ofrece la salida simple: %v", err)
	}
}

// Kaname rearma el DSN desde los campos, así que lo que no tiene campo se
// pierde. La cadena que dan los proveedores alojados trae varios de esos, y
// perderlos callado hace que la conexión sea distinta de la que el usuario
// pegó sin que nada se lo diga.
func TestParseURIAvisaQueDescartaParametros(t *testing.T) {
	// Tal cual la entrega Neon, más un par para verificar el orden.
	raw := "postgres://usuario:secreto@ep-abc-123.us-east-2.aws.neon.tech/neondb" +
		"?sslmode=require&channel_binding=require&connect_timeout=10&application_name=psql"

	got, err := ParseURI(raw)
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}

	// sslmode sí tiene campo: ese no se descarta.
	if got.Connection.SSLMode != SSLRequire {
		t.Errorf("SSLMode = %q, se esperaba require", got.Connection.SSLMode)
	}

	juntos := strings.Join(got.Notices, "\n")
	if !strings.Contains(juntos, "channel_binding") {
		t.Errorf("ningún aviso menciona channel_binding.\nAvisos:\n%s", juntos)
	}
	// El orden tiene que ser estable: el recorrido de un map no lo es.
	const esperado = "application_name, channel_binding, connect_timeout"
	if !strings.Contains(juntos, esperado) {
		t.Errorf("la lista de descartados no es %q.\nAvisos:\n%s", esperado, juntos)
	}
	// sslmode no se descartó, así que no puede figurar en esa lista.
	if strings.Contains(juntos, "sslmode,") || strings.Contains(juntos, ", sslmode") {
		t.Errorf("sslmode figura como descartado y sí se guarda.\nAvisos:\n%s", juntos)
	}
}

// Sin parámetros de más no hay nada que avisar: un aviso que aparece siempre
// deja de leerse.
func TestParseURINoAvisaCuandoNoDescartaNada(t *testing.T) {
	got, err := ParseURI("postgres://u:p@host:5432/db?sslmode=verify-full")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	for _, n := range got.Notices {
		if strings.Contains(n, "descartaron") || strings.Contains(n, "channel_binding") {
			t.Errorf("avisó de descartes sin haber descartado nada: %q", n)
		}
	}
}
