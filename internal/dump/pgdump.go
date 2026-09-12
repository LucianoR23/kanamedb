package dump

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PgDumpOptions es lo que se le pide a `pg_dump`.
type PgDumpOptions struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Database string `json:"database"`

	// Schemas acota el volcado. Vacío es la base entera.
	Schemas []string `json:"schemas"`

	// Structure y Data mapean a `--schema-only` y `--data-only`. Los dos en
	// true no pasan ninguna de las dos, que es el default de `pg_dump`.
	Structure bool `json:"structure"`
	Data      bool `json:"data"`

	// Clean agrega `--clean`: DROP antes de cada CREATE.
	Clean bool `json:"clean"`
	// Custom usa el formato propio de Postgres (`-Fc`), el que lee
	// `pg_restore`. Sin esto sale SQL de texto.
	Custom bool `json:"custom"`

	// Salida es el archivo destino. Vacío deja el comando sin `--file`.
	Salida string `json:"salida"`

	// SSLMode y los tres certificados son los de la conexión de Kaname, con
	// las rutas ya resueltas. Viajan en la cadena de conexión que se le pasa a
	// `pg_dump`: sin ellos la herramienta arrancaba con el default de libpq
	// —`prefer`, que cifra si el servidor ofrece y no verifica nada— aunque la
	// conexión estuviera en `verify-full` con una raíz propia, y la
	// contraseña y el volcado entero salían por ese canal (K-04 de la
	// auditoría del 2026-09-11). Vacíos no se emiten.
	SSLMode     string `json:"sslMode"`
	SSLRootCert string `json:"sslRootCert"`
	SSLCert     string `json:"sslCert"`
	SSLKey      string `json:"sslKey"`
}

// Comando arma la línea de `pg_dump`, ya lista para copiar y pegar.
//
// Es la parte más barata de las tres piezas del volcado y la que más valor da
// sola: la mitad de los errores con `pg_dump` son al escribir la línea. Se
// arma y se MUESTRA aunque no se pueda ejecutar.
//
// La contraseña NO va acá, ni como `PGPASSWORD` ni en la URI. Va por la
// variable de entorno del proceso hijo cuando Kaname lo ejecuta, y cuando la
// línea se copia la pide `pg_dump` por su cuenta. Un comando que se copia
// termina en un chat, en un ticket y en el historial del shell.
func Comando(o PgDumpOptions) []string {
	args := []string{"pg_dump"}
	if o.Host != "" {
		args = append(args, "--host="+o.Host)
	}
	if o.Port != 0 {
		args = append(args, "--port="+strconv.Itoa(o.Port))
	}
	if o.User != "" {
		args = append(args, "--username="+o.User)
	}
	// Sin esto `pg_dump` abre un prompt interactivo que, lanzado desde una
	// aplicación de ventana, no tiene dónde aparecer: el proceso queda colgado
	// esperando una respuesta que nadie puede dar.
	args = append(args, "--no-password")

	for _, e := range o.Schemas {
		args = append(args, "--schema="+e)
	}
	// Los dos juntos NO se pasan: `--schema-only --data-only` es un error de
	// `pg_dump`, y pedir «todo» es justamente no pasar ninguno.
	switch {
	case o.Structure && !o.Data:
		args = append(args, "--schema-only")
	case o.Data && !o.Structure:
		args = append(args, "--data-only")
	}
	if o.Clean {
		args = append(args, "--clean", "--if-exists")
	}
	if o.Custom {
		args = append(args, "--format=custom")
	}
	if o.Salida != "" {
		args = append(args, "--file="+o.Salida)
	}
	// La base va en `--dbname=` como cadena de conexión de libpq y no como
	// argumento suelto al final. Es el único lugar donde caben el modo TLS y
	// los certificados —`pg_dump` no tiene banderas para eso—, y de paso un
	// nombre de base que empiece con `-` deja de parecerle una opción a
	// getopt. Sigue sin llevar contraseña: ver arriba.
	if conn := o.conninfo(); conn != "" {
		args = append(args, "--dbname="+conn)
	}
	return args
}

// conninfo arma la cadena `clave=valor …` de libpq con la base y el TLS.
func (o PgDumpOptions) conninfo() string {
	var partes []string
	for _, kv := range [][2]string{
		{"dbname", o.Database},
		{"sslmode", o.SSLMode},
		{"sslrootcert", o.SSLRootCert},
		{"sslcert", o.SSLCert},
		{"sslkey", o.SSLKey},
	} {
		if kv[1] != "" {
			partes = append(partes, kv[0]+"="+valorConninfo(kv[1]))
		}
	}
	return strings.Join(partes, " ")
}

// valorConninfo cita un valor como lo pide libpq: comillas simples cuando hay
// espacios o está vacío, y adentro `\` y `'` escapadas con barra.
func valorConninfo(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n'\\") {
		return v
	}
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// ComandoTexto es el comando como se pega en una terminal.
//
// Cita solo lo que hace falta: una línea llena de comillas es más difícil de
// leer, y esto se lee tanto como se corre.
func ComandoTexto(o PgDumpOptions) string {
	partes := Comando(o)
	out := make([]string, 0, len(partes))
	for _, p := range partes {
		out = append(out, citarSiHaceFalta(p))
	}
	return strings.Join(out, " ")
}

// citarSiHaceFalta pone comillas alrededor de lo que el shell partiría.
func citarSiHaceFalta(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'$`\\&|;<>()*?[]{}!#~") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s) + `"`
}

// Version es la de un `pg_dump` o la de un servidor, para poder compararlas.
type Version struct {
	Mayor int
	Menor int
	// Texto es lo que dijo la herramienta, tal cual.
	Texto string
}

// versionRE saca «17.2» de «pg_dump (PostgreSQL) 17.2» y de «PostgreSQL 18.3 on
// aarch64-unknown-linux-musl, compiled by gcc…».
//
// El primer número de dos partes que aparezca: los dos formatos lo tienen, y
// buscarlo así evita escribir un parser por cada forma en que estas
// herramientas deciden presentarse.
var versionRE = regexp.MustCompile(`(\d+)(?:\.(\d+))?`)

// ParseVersion saca la versión del texto que imprime la herramienta.
func ParseVersion(texto string) (Version, bool) {
	m := versionRE.FindStringSubmatch(texto)
	if m == nil {
		return Version{Texto: strings.TrimSpace(texto)}, false
	}
	v := Version{Texto: strings.TrimSpace(texto)}
	v.Mayor, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		v.Menor, _ = strconv.Atoi(m[2])
	}
	return v, v.Mayor > 0
}

func (v Version) String() string {
	if v.Menor > 0 {
		return fmt.Sprintf("%d.%d", v.Mayor, v.Menor)
	}
	return strconv.Itoa(v.Mayor)
}

// AlcanzaPara dice si este `pg_dump` sirve para volcar ese servidor.
//
// Esta comprobación es lo que separa una herramienta de una trampa. `pg_dump`
// soporta servidores MÁS VIEJOS que él, nunca más nuevos: uno de la 15 contra
// un servidor 18 falla —y en algunas combinaciones no falla, que es peor:
// escribe un archivo que parece completo y no lo está—. Comparar solo la
// versión mayor alcanza, porque es la única que cambia el formato del catálogo.
func (v Version) AlcanzaPara(servidor Version) bool {
	return v.Mayor >= servidor.Mayor
}
