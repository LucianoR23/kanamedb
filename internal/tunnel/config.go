// Package tunnel abre conexiones a través de un servidor SSH.
//
// No escucha en ningún puerto. Un túnel SSH se implementa habitualmente
// escuchando en 127.0.0.1 y reenviando, pero este proyecto no abre sockets: un
// puerto en loopback es alcanzable desde cualquier pestaña del navegador, que es
// la misma razón por la que la aplicación no tiene servidor HTTP. En vez de eso,
// el cliente de la base disca a través del cliente SSH con una función de
// discado propia —pgx la acepta— y la conexión existe solo en memoria.
package tunnel

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// DefaultPort es el puerto de SSH.
const DefaultPort = 22

// AuthMethod es cómo se autentica el cliente contra el servidor SSH.
type AuthMethod string

const (
	// AuthAgent usa el agente del sistema. Es la mejor opción de las tres: la
	// clave privada nunca sale del agente y la aplicación nunca la ve.
	AuthAgent AuthMethod = "agent"
	// AuthKeyFile lee una clave privada de disco, referenciada por ruta.
	//
	// La clave se referencia, no se copia: vive en ~/.ssh de cada máquina y el
	// archivo de conexiones —que se sincroniza entre máquinas— guarda la ruta.
	AuthKeyFile AuthMethod = "key"
	// AuthPassword es contraseña. Se soporta porque hay bastiones que solo
	// aceptan eso, no porque sea buena idea.
	AuthPassword AuthMethod = "password"
)

// Config describe el salto SSH.
//
// No lleva la contraseña ni la frase de paso de la clave: esos salen del
// keychain en el momento de conectar y no se guardan acá, igual que en
// connection.Connection. Un struct que puede serializarse a TOML no puede tener
// un campo donde alguien meta un secreto sin querer.
type Config struct {
	// Enabled apagado deja el resto de los campos como configuración muerta
	// pero legible: apagar el túnel no debería obligar a reescribirlo.
	Enabled bool `toml:"enabled" json:"enabled"`

	Host string `toml:"host" json:"host"`
	Port int    `toml:"port" json:"port"`
	User string `toml:"user" json:"user"`

	Auth AuthMethod `toml:"auth" json:"auth"`

	// KeyPath es la ruta a la clave privada, solo con Auth = key.
	KeyPath string `toml:"key_path,omitempty" json:"keyPath,omitempty"`
}

// Normalize limpia y completa lo que se pueda deducir.
func (c Config) Normalize() Config {
	c.Host = strings.TrimSpace(c.Host)
	c.User = strings.TrimSpace(c.User)
	c.KeyPath = limpiarRuta(c.KeyPath)
	c.Auth = AuthMethod(strings.ToLower(strings.TrimSpace(string(c.Auth))))

	// Los defaults solo se aplican con el túnel encendido.
	//
	// Rellenarlos siempre metería `port = 22` y `auth = "agent"` en el archivo
	// de TODAS las conexiones, incluidas las que nunca van a usar un bastión:
	// configuración de algo que no existe, en un archivo que se lee a mano. Y
	// además haría que guardar y volver a leer una conexión la devolviera
	// distinta de como entró.
	if !c.Enabled {
		return c
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Auth == "" {
		// El agente es el default porque es el único donde la clave privada
		// nunca pasa por la aplicación.
		c.Auth = AuthAgent
	}
	// Una ruta de clave sin método de clave es configuración que no se usa;
	// dejarla no rompe nada y permite volver a `key` sin reescribirla.
	return c
}

// Validate comprueba que se pueda intentar conectar.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	c = c.Normalize()
	if c.Host == "" {
		return fmt.Errorf("el túnel SSH necesita un host")
	}
	if c.User == "" {
		return fmt.Errorf("el túnel SSH necesita un usuario")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("el puerto SSH %d está fuera de rango", c.Port)
	}
	switch c.Auth {
	case AuthAgent, AuthPassword:
	case AuthKeyFile:
		if c.KeyPath == "" {
			return fmt.Errorf("la autenticación por clave necesita la ruta de la clave privada")
		}
	default:
		return fmt.Errorf("método de autenticación SSH desconocido: %q", c.Auth)
	}
	return nil
}

// Address devuelve host:puerto, listo para discar.
func (c Config) Address() string {
	c = c.Normalize()
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Describe es usuario@host:puerto. Seguro para mostrar y para loguear: no lleva
// contraseña, ni frase de paso, ni el contenido de ninguna clave.
func (c Config) Describe() string {
	c = c.Normalize()
	return c.User + "@" + c.Address()
}

// limpiarRuta saca los espacios y las comillas que rodean a una ruta.
//
// Las comillas se sacan porque "Copiar como ruta" del Explorador de Windows las
// agrega, y es la forma más común de copiar una ruta en ese sistema. La cadena
// va directo a la API de archivos, no a una shell: ahí las comillas serían parte
// del nombre y el archivo no aparecería. Fallar en silencio con lo que hace todo
// el mundo es peor que ser tolerante acá.
//
// Solo se sacan si están de los dos lados. Una ruta que empieza con comilla y no
// termina con una es otra cosa, y adivinar sería peor que dejarla como está.
func limpiarRuta(p string) string {
	p = strings.TrimSpace(p)
	for _, c := range []string{`"`, "'"} {
		if len(p) >= 2 && strings.HasPrefix(p, c) && strings.HasSuffix(p, c) {
			p = strings.TrimSpace(p[1 : len(p)-1])
		}
	}
	return p
}
