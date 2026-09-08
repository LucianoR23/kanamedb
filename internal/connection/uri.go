package connection

import (
	"fmt"
	"net"
	"net/url"
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

// ParseURI llena los campos de conexión a partir de una cadena pegada.
//
// Está detrás de "Paste to fill fields" en S03. Devuelve solo los campos que la
// URI trae; el resto queda en cero para que quien llama decida si conserva lo
// que ya tenía.
//
// Si la URI trae contraseña, se devuelve aparte y NUNCA dentro de la Connection:
// el struct que se serializa a disco no puede tener credenciales ni de paso.
func ParseURI(raw string) (Connection, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Connection{}, "", fmt.Errorf("la cadena de conexión está vacía")
	}

	u, err := url.Parse(encodeNonASCII(raw))
	if err != nil {
		// El error de url.Parse cita la cadena entera, que puede llevar la
		// contraseña. No se propaga.
		return Connection{}, "", fmt.Errorf("no se pudo interpretar la cadena de conexión")
	}

	var c Connection
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
		return Connection{}, "", fmt.Errorf("la cadena no dice qué motor es: falta el esquema, por ejemplo postgresql://")
	default:
		return Connection{}, "", fmt.Errorf("motor desconocido en la cadena: %q", u.Scheme)
	}

	if c.Engine == SQLite {
		c.Database = strings.TrimPrefix(u.Path, "/")
		if c.Database == "" {
			c.Database = u.Opaque
		}
		return c, "", nil
	}

	c.Host = u.Hostname()
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Connection{}, "", fmt.Errorf("el puerto %q no es válido", p)
		}
		c.Port = n
	} else {
		c.Port = c.Engine.DefaultPort()
	}

	c.Database = strings.TrimPrefix(u.Path, "/")

	var password string
	if u.User != nil {
		c.User = u.User.Username()
		password, _ = u.User.Password()
	}

	if modo := u.Query().Get("sslmode"); modo != "" {
		c.SSLMode = SSLMode(strings.ToLower(strings.TrimSpace(modo)))
	}

	return c, password, nil
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
