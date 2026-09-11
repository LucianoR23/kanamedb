package connection

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// URI devuelve la cadena de conexión SIN contraseña, para mostrar y copiar.
//
// Es lo que va en la tarjeta "Connection URI" de S03. No confundir con DSN, que
// lleva la contraseña y es un secreto: esta se puede pegar en un chat.
func (c Connection) URI() string {
	c = c.Normalize()
	if c.Engine == SQLite {
		return c.Database
	}

	u := url.URL{
		Scheme: string(c.Engine),
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:   "/" + c.Database,
	}
	if c.Engine == Postgres {
		// `postgresql` es el esquema que usa la documentación de PostgreSQL y
		// el que la gente reconoce al pegarlo.
		u.Scheme = "postgresql"
	}
	if c.User != "" {
		// Solo el usuario: url.User, no url.UserPassword.
		u.User = url.User(c.User)
	}
	if c.SSLMode != "" {
		u.RawQuery = url.Values{"sslmode": {string(c.SSLMode)}}.Encode()
	}
	return u.String()
}

// Parsed es el resultado de interpretar una cadena de conexión pegada.
type Parsed struct {
	// Connection trae solo los campos que la cadena incluía. El resto queda en
	// cero para que quien llama decida si conserva lo que ya tenía.
	Connection Connection `json:"connection"`

	// Password sale acá y NUNCA dentro de Connection: el struct que se
	// serializa a disco no puede tener credenciales ni de paso.
	Password string `json:"password"`

	// Notices son avisos sobre cómo se interpretó la cadena. La UI los muestra
	// después de pegar. Vacío si no hay nada que aclarar.
	Notices []string `json:"notices"`
}

// ParseURI llena los campos de conexión a partir de una cadena pegada.
//
// Está detrás de "Paste to fill fields" en S03.
func ParseURI(raw string) (Parsed, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Parsed{}, fmt.Errorf("la cadena de conexión está vacía")
	}

	u, err := url.Parse(encodeNonASCII(raw))
	if err != nil {
		// El error de url.Parse cita la cadena entera, que puede llevar la
		// contraseña. No se propaga tal cual.
		if strings.Contains(err.Error(), "invalid URL escape") {
			return Parsed{}, fmt.Errorf(
				"la cadena tiene un %% suelto. En una URI el símbolo por ciento se escribe %%25; " +
					"si tu contraseña lleva uno, es más simple escribirla en el campo Password")
		}
		return Parsed{}, fmt.Errorf("no se pudo interpretar la cadena de conexión")
	}

	var out Parsed
	c := &out.Connection

	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql":
		c.Engine = Postgres
	case "mysql":
		c.Engine = MySQL
	case "mariadb":
		c.Engine = MariaDB
	case "sqlite", "sqlite3", "file":
		c.Engine = SQLite
	case "":
		return Parsed{}, fmt.Errorf("la cadena no dice qué motor es: falta el esquema, por ejemplo postgresql://")
	default:
		return Parsed{}, fmt.Errorf("motor desconocido en la cadena: %q", u.Scheme)
	}

	if c.Engine == SQLite {
		c.Database = strings.TrimPrefix(u.Path, "/")
		if c.Database == "" {
			c.Database = u.Opaque
		}
		return out, nil
	}

	c.Host = u.Hostname()
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Parsed{}, fmt.Errorf("el puerto %q no es válido", p)
		}
		c.Port = n
	} else {
		c.Port = c.Engine.DefaultPort()
	}

	c.Database = strings.TrimPrefix(u.Path, "/")

	if u.User != nil {
		c.User = u.User.Username()
		out.Password, _ = u.User.Password()
	}

	if modo := u.Query().Get("sslmode"); modo != "" {
		c.SSLMode = SSLMode(strings.ToLower(strings.TrimSpace(modo)))
	}
	// Los tres archivos de libpq tienen campo propio desde la pestaña TLS, así
	// que se conservan en vez de descartarse. Son rutas de la máquina de quien
	// armó la cadena; si es otra, la pestaña lo va a mostrar y se corrige ahí.
	c.TLS.RootCertPath = u.Query().Get("sslrootcert")
	c.TLS.ClientCertPath = u.Query().Get("sslcert")
	c.TLS.ClientKeyPath = u.Query().Get("sslkey")

	// El DSN se rearma desde los campos de la conexión, así que todo parámetro
	// que no tenga campo propio se pierde al guardar. Perderlo está bien;
	// perderlo en silencio no: quien pega la cadena que le dio su proveedor
	// asume que se respeta entera, y acá se respeta solo sslmode.
	if ignorados := paramsIgnorados(u.Query()); len(ignorados) > 0 {
		out.Notices = append(out.Notices,
			"Kaname no guarda estos parámetros de la cadena y se descartaron: "+
				strings.Join(ignorados, ", ")+".")
	}

	// channel_binding merece su propia frase porque es el único descartado que
	// cambia la seguridad de la conexión, y es el default de varios proveedores
	// alojados. pgx igual lo negocia cuando el servidor lo ofrece —su default
	// es "prefer"—; lo que se pierde es fallar cuando no lo ofrece, que es la
	// defensa contra un intermediario que lo saque de la lista.
	if strings.EqualFold(u.Query().Get("channel_binding"), "require") {
		out.Notices = append(out.Notices,
			"channel_binding=require se descartó: la conexión lo sigue usando si el servidor lo ofrece, "+
				"pero ya no falla si no lo ofrece. Con un sslmode que verifica el certificado da lo mismo; "+
				"con sslmode=require, no.")
	}

	// Una secuencia %XX en la contraseña es ambigua y no se puede resolver:
	// `%20` puede ser un espacio escapado o un por ciento seguido de "20". El
	// estándar dice que es lo primero, así que se interpreta así y se avisa.
	//
	// Sin este aviso, el usuario pega, prueba la conexión, recibe "credenciales
	// rechazadas" y no tiene cómo darse cuenta: el campo está enmascarado.
	if out.Password != "" && strings.Contains(rawUserinfo(raw), "%") {
		out.Notices = append(out.Notices,
			"La contraseña de la cadena tenía secuencias %XX y se interpretaron como caracteres escapados. "+
				"Si tu contraseña lleva un % literal, escribila en el campo Password.")
	}

	return out, nil
}

// paramsIgnorados devuelve, ordenados, los parámetros de la cadena que no
// sobreviven a guardar la conexión: todos menos los que tienen campo propio,
// que son el modo SSL y los tres archivos de certificados.
//
// Se ordenan para que el aviso sea el mismo siempre: el recorrido de un map en
// Go no tiene orden, y un mensaje que cambia de orden entre corridas parece un
// error distinto cada vez.
func paramsIgnorados(q url.Values) []string {
	nombres := make([]string, 0, len(q))
	for k := range q {
		if conCampoPropio(k) {
			continue
		}
		nombres = append(nombres, k)
	}
	slices.Sort(nombres)
	return nombres
}

func conCampoPropio(param string) bool {
	switch strings.ToLower(param) {
	case "sslmode", "sslrootcert", "sslcert", "sslkey":
		return true
	}
	return false
}

// rawUserinfo devuelve la parte usuario:contraseña de la cadena, tal como venía.
//
// Se mira la cadena original y no lo que devolvió url.Parse porque para eso
// justamente hay que ver los escapes antes de que se resuelvan.
func rawUserinfo(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return ""
	}
	resto := raw[i+3:]
	// La autoridad termina en la primera / ? o #.
	if j := strings.IndexAny(resto, "/?#"); j >= 0 {
		resto = resto[:j]
	}
	j := strings.LastIndex(resto, "@")
	if j < 0 {
		return ""
	}
	return resto[:j]
}

// encodeNonASCII escapa los bytes no ASCII de una cadena de conexión.
//
// Una URI no admite bytes >= 0x80 sin escapar, así que `url.Parse` rechaza
// `postgres://u:contraseña@host/db`. Rechazarla también sería correcto y
// bastante inútil: en una app en español, una contraseña con ñ o acentos es
// esperable, y quien la pega no tiene por qué saber de RFC 3986.
//
// Escapar solo los bytes >= 0x80 es seguro: nunca son válidos crudos y nunca
// forman parte de un escape `%XX`, así que no puede haber doble codificación.
func encodeNonASCII(raw string) string {
	necesita := false
	for i := 0; i < len(raw); i++ {
		if raw[i] >= 0x80 {
			necesita = true
			break
		}
	}
	if !necesita {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw) + 8)
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c >= 0x80 {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
