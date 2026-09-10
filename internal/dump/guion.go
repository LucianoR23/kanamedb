package dump

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// anchoDeLinea es cuánto puede medir el texto de una línea del encabezado, sin
// contar el `-- ` que la abre. Setenta y cinco deja el renglón entero por
// debajo de las ochenta columnas de una terminal.
const anchoDeLinea = 75

// Info es lo que el encabezado del archivo cuenta de sí mismo.
type Info struct {
	// Origen es `usuario@host:puerto/base`, SIN credenciales. Nunca la DSN.
	Origen string
	// Motor es «PostgreSQL 18.3».
	Motor string
	// Base es el nombre de la base.
	Base string
	// Esquemas son los que entran en el archivo.
	Esquemas []string
	// Cuando es la fecha del volcado.
	Cuando time.Time
	// Version es la de Kaname, para poder saber qué lo escribió.
	Version string

	// Estructura y Datos dicen qué lleva el archivo.
	Estructura bool
	Datos      bool

	// Cobertura es lo que NO lleva. Va en el encabezado con nombre y apellido.
	Cobertura Cobertura

	// Ciclos son los grupos de tablas que se apuntan entre sí. Un volcado de
	// datos con un ciclo no tiene ningún orden de inserción que funcione.
	Ciclos [][]Ref
}

// Encabezado escribe el comentario del principio del archivo.
//
// Es lo primero que ve quien abre el volcado dentro de seis meses, y tiene que
// contestarle tres cosas sin que abra nada más: de dónde salió, qué lleva y
// —sobre todo— QUÉ NO LLEVA. La última es la que hace que el archivo se pueda
// usar para decidir; sin ella, un volcado incompleto se ve idéntico a uno
// completo.
//
// No lleva credenciales. `Origen` es `usuario@host:puerto/base`: la contraseña
// no entra ni en el archivo ni en los logs. Ver CLAUDE.md.
func Encabezado(w io.Writer, i Info) error {
	var b strings.Builder
	// Sin espacio colgando cuando la línea va vacía: un archivo con espacios al
	// final de los renglones ensucia cualquier diff contra el volcado de ayer,
	// que es justamente para lo que se guarda uno.
	linea := func(f string, args ...any) {
		texto := strings.TrimRight(fmt.Sprintf(f, args...), " \t")
		if texto == "" {
			b.WriteString("--\n")
			return
		}
		b.WriteString("-- ")
		b.WriteString(texto)
		b.WriteByte('\n')
	}

	// Cada campo se envuelve con sangría colgante: el valor puede ser largo de
	// verdad —el `version()` de Postgres trae el compilador y la arquitectura,
	// y una ruta de SQLite mide lo que mida— y esto se lee en una terminal de
	// ochenta columnas.
	campo := func(etiqueta, valor string) {
		sangria := strings.Repeat(" ", len([]rune(etiqueta)))
		for k, l := range envolver(valor, anchoDeLinea-len([]rune(etiqueta))) {
			if k == 0 {
				linea("%s%s", etiqueta, l)
				continue
			}
			linea("%s%s", sangria, l)
		}
	}

	b.WriteString("--\n")
	campo("Volcado de ", i.Base)
	linea("")
	campo("Generado por ", fmt.Sprintf(
		"Kaname %s el %s", i.Version, i.Cuando.Format("2006-01-02 15:04:05 -0700")))
	campo("Origen:  ", i.Origen)
	campo("Motor:   ", i.Motor)
	if len(i.Esquemas) > 0 {
		campo("Esquema: ", strings.Join(i.Esquemas, ", "))
	}
	campo("Lleva:   ", queLleva(i))
	b.WriteString("--\n")

	if i.Estructura {
		linea("")
		if i.Cobertura.Vacia() {
			linea("ESTE ARCHIVO NO DEJA NADA AFUERA.")
			linea("")
			linea("En estos esquemas no hay vistas, funciones, triggers ni nada")
			linea("que Kaname no sepa escribir. Se comprobó contra el catálogo,")
			linea("no se está suponiendo.")
		} else {
			linea("LO QUE NO ESTÁ EN ESTE ARCHIVO")
			linea("")
			linea("Kaname escribe tablas, columnas, claves, restricciones e")
			linea("índices. Lo que sigue existe en la base y NO está acá, así")
			linea("que restaurar este archivo no deja la base como estaba:")
			linea("")
			for _, d := range i.Cobertura.Detalle() {
				for _, l := range envolver(d, anchoDeLinea-2) {
					linea("  %s", l)
				}
			}
		}
		b.WriteString("--\n")
	}

	if i.Datos && len(i.Ciclos) > 0 {
		linea("")
		linea("OJO: HAY TABLAS QUE SE APUNTAN ENTRE SÍ")
		linea("")
		linea("Las filas están ordenadas para que cada tabla entre después de")
		linea("aquellas de las que depende. Con un ciclo eso es imposible: no")
		linea("existe ningún orden que funcione. Estos grupos van a fallar al")
		linea("cargarse salvo que difieras las restricciones:")
		linea("")
		for _, grupo := range i.Ciclos {
			nombres := make([]string, 0, len(grupo))
			for _, r := range grupo {
				nombres = append(nombres, r.Completo())
			}
			for _, l := range envolver(strings.Join(nombres, " ↔ "), anchoDeLinea-2) {
				linea("  %s", l)
			}
		}
		b.WriteString("--\n")
	}

	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())
	return err
}

func queLleva(i Info) string {
	switch {
	case i.Estructura && i.Datos:
		return "la estructura y los datos"
	case i.Estructura:
		return "solo la estructura"
	case i.Datos:
		return "solo los datos"
	}
	return "nada"
}

// Completo es `esquema.tabla`, o solo la tabla cuando no hay esquema —SQLite—.
func (r Ref) Completo() string {
	if r.Schema == "" {
		return r.Table
	}
	return r.Schema + "." + r.Table
}

// Seccion escribe el comentario que separa una parte de la siguiente.
func Seccion(w io.Writer, titulo string) error {
	_, err := fmt.Fprintf(w, "\n--\n-- %s\n--\n\n", titulo)
	return err
}

// envolver corta un texto largo en líneas de a lo sumo `ancho` caracteres, sin
// partir palabras.
//
// El encabezado es un comentario que alguien va a leer en una terminal de 80
// columnas: una lista de treinta funciones en una sola línea no se lee. Corta
// por espacios y nunca deja una línea vacía.
func envolver(texto string, ancho int) []string {
	palabras := strings.Fields(texto)
	if len(palabras) == 0 {
		return []string{""}
	}
	var out []string
	actual := palabras[0]
	for _, p := range palabras[1:] {
		// `+1` por el espacio que hay que meter entre las dos.
		if len([]rune(actual))+1+len([]rune(p)) > ancho {
			out = append(out, actual)
			actual = p
			continue
		}
		actual += " " + p
	}
	return append(out, actual)
}
