// Package sqlite implementa engine.Conn para SQLite.
//
// Es el motor más distinto de los cuatro, y no por ser el más chico. Las
// diferencias que cambian el código son tres:
//
//  1. No hay servidor. No hay host, ni puerto, ni usuario, ni TLS: hay un
//     archivo, y el permiso lo da el sistema de archivos. Todo el vocabulario
//     de fallos de conexión —«el servidor rechazó las credenciales»— acá no
//     significa nada, así que este paquete habla de archivos.
//
//  2. Casi no hay ALTER TABLE. Agregar, renombrar y borrar una columna sí;
//     todo lo demás —cambiar un tipo, exigir que no sea nula, agregar un
//     CHECK o una clave foránea— se hace creando una tabla nueva, copiando
//     las filas y tirando la vieja. Eso es rebuild.go, y es la mitad del
//     paquete.
//
//  3. El código de error casi no dice nada. SQLITE_ERROR es 1 y ahí caen la
//     tabla que no existe, la columna repetida y el error de sintaxis por
//     igual. La clasificación tiene que leer el mensaje, que es lo que en los
//     otros motores se evita a propósito. Ver errors.go.
package sqlite

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// QuoteIdent cita un identificador para SQLite.
//
// SQLite acepta cuatro formas de citar —comillas dobles, acentos invertidos,
// corchetes y hasta comillas simples cuando el contexto no deja lugar a dudas—
// pero solo una es la del estándar y es la que se escribe siempre: comillas
// dobles, duplicadas para escapar.
//
// Se cita SIEMPRE, aunque el nombre parezca inofensivo, por el mismo motivo
// que en los otros motores: decidir caso por caso si un nombre necesita
// comillas es una decisión que se equivoca.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QualifiedName arma el nombre de una tabla.
//
// El parámetro `esquema` existe para que la firma sea la misma que la de los
// otros motores y SIEMPRE se ignora, salvo que sea un esquema adjunto de
// verdad. SQLite no tiene esquemas: tiene bases adjuntas con ATTACH, que es
// otra cosa, y la conexión de Kaname abre una sola.
func QualifiedName(esquema, tabla string) string {
	if esquema == "" || esquema == "main" {
		return QuoteIdent(tabla)
	}
	return QuoteIdent(esquema) + "." + QuoteIdent(tabla)
}

// QuoteString cita un literal de texto.
//
// Solo para el DDL, donde un valor por defecto no puede ir como parámetro.
// Todo lo demás va parametrizado. Ver CLAUDE.md.
//
// La comilla simple duplicada es TODO el escape que existe en SQLite: a
// diferencia de MySQL, la barra invertida no es carácter de escape y escaparla
// dejaría una barra de más en el dato.
func QuoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// versionNum convierte "3.53.4" en 35304, con la misma regla que usan los
// otros tres motores: mayor*10000 + menor*100 + parche.
func versionNum(version string) int {
	partes := strings.Split(version, ".")
	num := 0
	for i := 0; i < 3 && i < len(partes); i++ {
		v, _ := strconv.Atoi(partes[i])
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

// display arma la versión corta para la barra de estado: "SQLite 3.53.4".
func display(version string) string {
	return fmt.Sprintf("%s %s", engine.SQLite.Label(), version)
}
