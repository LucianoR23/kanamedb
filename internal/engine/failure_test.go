package engine

import (
	"strings"
	"testing"
)

// TestRedactTapaLasContrasenasDeLosTresFormatos.
//
// `Redact` se aplica a todo texto de origen ajeno antes de que llegue a un
// Failure, y esos textos terminan en toasts, logs y tickets. CLAUDE.md lo pone
// como requisito duro: nunca loguear una cadena de conexión.
//
// La expresión original exigía `esquema://`, que es la forma de Postgres. El
// DSN de MySQL no es un URI —`usuario:clave@tcp(host:puerto)/base`— así que no
// lo reconocía: mientras hubo un solo motor eso alcanzaba, y desde la Iteración
// 6 hay tres formatos.
func TestRedactTapaLasContrasenasDeLosTresFormatos(t *testing.T) {
	casos := []struct {
		nombre string
		texto  string
		clave  string
	}{
		{"postgres", "no se pudo abrir postgres://kaname:s3cr3t@host:5432/db", "s3cr3t"},
		{"mysql", `dial error kaname:s3cr3t@tcp(127.0.0.1:3306)/kaname_test`, "s3cr3t"},
		{"mysql por socket", `kaname:s3cr3t@unix(/var/run/mysqld.sock)/db`, "s3cr3t"},
		{"mysql por el túnel", `kaname:s3cr3t@kaname-tunnel-3(host:3306)/db`, "s3cr3t"},
		// Una contraseña con `@`. El DSN de MySQL se arma sin escaparla —el
		// driver parte por el ÚLTIMO `@`— así que la expresión tiene que ser
		// codiciosa hasta el `@` que precede a `tcp(`; con `[^@]*` no había
		// coincidencia y el DSN entero quedaba sin enmascarar (K-11).
		{"mysql con @ en la clave", `kaname:p@ss@w0rd@tcp(127.0.0.1:3306)/db`, "p@ss@w0rd"},
		{"postgres con @ escapado", `postgres://kaname:p%40ss@host:5432/db`, "p%40ss"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := Redact(c.texto)
			if strings.Contains(got, c.clave) {
				t.Errorf("la contraseña sigue ahí: %q", got)
			}
			// Y el usuario tiene que quedar: es lo que permite entender de qué
			// conexión habla el error.
			if !strings.Contains(got, "kaname") {
				t.Errorf("se tapó de más y el texto perdió el usuario: %q", got)
			}
		})
	}

	// Un texto sin credenciales no se toca.
	limpio := "la tabla «envios» no existe"
	if got := Redact(limpio); got != limpio {
		t.Errorf("Redact cambió un texto que no tenía credenciales: %q", got)
	}
}
