// Package mysql implementa engine.Conn para MySQL y MariaDB.
//
// Los dos comparten paquete porque hablan el mismo protocolo y casi todo el
// catálogo, pero NO son el mismo motor y el código lo distingue donde importa.
// Probando 9.7 contra 12.3 quedó a la vista: MariaDB tiene secuencias, UUID,
// INET6, RETURNING y períodos de tiempo de aplicación, y MySQL ninguno; y donde
// coinciden difieren igual —agrandar un varchar es INSTANT en MariaDB y
// reescribe la tabla en MySQL—. Por eso hay dos engine.Kind y una sola
// implementación que pregunta cuál es cuando la respuesta cambia.
package mysql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// QuoteIdent cita un identificador para MySQL.
//
// Se usa siempre, aunque el nombre parezca inofensivo. Citar solo «cuando hace
// falta» obliga a decidir en cada uso si un nombre necesita comillas, y esa
// decisión se equivoca: `order` es palabra reservada, `Mi Tabla` tiene espacio y
// `a“b` es un nombre válido.
//
// Duplicar el acento invertido es todo el escape que hay. Con el modo SQL
// ANSI_QUOTES el motor además acepta comillas dobles, pero el acento funciona en
// los dos modos y las comillas dobles no: por eso siempre acento.
func QuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// QualifiedName arma base.tabla con las dos partes citadas.
//
// En MySQL «esquema» y «base» son la misma cosa, así que el primer componente
// es la base. Cuando viene vacío se devuelve solo la tabla, que es lo correcto:
// la conexión ya está posicionada en su base.
func QualifiedName(esquema, tabla string) string {
	if esquema == "" {
		return QuoteIdent(tabla)
	}
	return QuoteIdent(esquema) + "." + QuoteIdent(tabla)
}

// QuoteString cita un literal de texto para el modo normal de MySQL.
//
// Existe únicamente para el DDL, donde un comentario o un valor por defecto no
// pueden ir como parámetro. Todo lo demás va parametrizado. Ver CLAUDE.md.
func QuoteString(s string) string { return quoteString(s, false) }

// quoteString cita un literal sabiendo si el servidor trata la barra invertida
// como carácter de escape.
//
// Acá había un comentario que decía que duplicar la barra «es correcto en los
// dos modos». No lo es: con NO_BACKSLASH_ESCAPES la barra no escapa nada, así
// que una barra duplicada son DOS barras y un comentario que dice `C:\ruta` se
// guardaba como `C:\\ruta`. Comprobado contra MySQL 9.7.
//
// No es un agujero de inyección —duplicar la comilla simple sigue siendo
// correcto en los dos modos, y la comilla es lo único que puede cerrar el
// literal— pero sí corrupción silenciosa de un texto que escribió el usuario.
//
// No hay una forma de citar que sirva para los dos modos, así que hay que saber
// en cuál está el servidor: se lee @@sql_mode al conectar.
func quoteString(s string, sinEscapes bool) string {
	if sinEscapes {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	r := strings.NewReplacer(`\`, `\\`, `'`, `''`)
	return "'" + r.Replace(s) + "'"
}

// esMariaDB mira el texto de la versión.
//
// MariaDB pone su nombre ahí desde siempre —"12.3.3-MariaDB-ubu2404"— y es la
// única forma confiable de distinguirlas: el protocolo es el mismo y el número
// de versión no alcanza, porque las dos numeraciones se cruzaron.
func esMariaDB(version string) bool {
	return strings.Contains(strings.ToLower(version), "mariadb")
}

// kindDe decide qué motor es a partir de la versión que reportó el servidor.
func kindDe(version string) engine.Kind {
	if esMariaDB(version) {
		return engine.MariaDB
	}
	return engine.MySQL
}

// versionNum convierte "9.7.2" o "12.3.3-MariaDB-ubu2404" en 90702 / 120303.
//
// La regla es la misma que usa Postgres para server_version_num, así que los
// mínimos de engine se comparan igual en los cuatro motores.
func versionNum(version string) int {
	// Cortar en el primer carácter que no sea dígito ni punto: el sufijo de
	// distribución no aporta y rompería el parseo.
	fin := len(version)
	for i, r := range version {
		if (r < '0' || r > '9') && r != '.' {
			fin = i
			break
		}
	}
	partes := strings.Split(version[:fin], ".")
	num := 0
	for i := 0; i < 3; i++ {
		v := 0
		if i < len(partes) {
			v, _ = strconv.Atoi(partes[i])
		}
		switch i {
		case 0:
			num += v * 10000
		case 1:
			num += v * 100
		case 2:
			num += v
		}
	}
	return num
}

// display arma la versión corta para la barra de estado: "MySQL 9.7.2".
func display(k engine.Kind, version string) string {
	corta := version
	if i := strings.IndexAny(version, "-+ "); i > 0 {
		corta = version[:i]
	}
	return fmt.Sprintf("%s %s", k.Label(), corta)
}
