package dump

import (
	"fmt"
	"io"
	"strings"
	"time"
)

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
	linea := func(f string, args ...any) {
		b.WriteString("-- ")
		fmt.Fprintf(&b, f, args...)
		b.WriteByte('\n')
	}

	b.WriteString("--\n")
	linea("Volcado de %s", i.Base)
	linea("")
	linea("Generado por Kaname %s el %s", i.Version, i.Cuando.Format("2006-01-02 15:04:05 -0700"))
	linea("Origen:  %s", i.Origen)
	linea("Motor:   %s", i.Motor)
	if len(i.Esquemas) > 0 {
		linea("Esquema: %s", strings.Join(i.Esquemas, ", "))
	}
	linea("Lleva:   %s", queLleva(i))
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
				for _, l := range envolver(d, 68) {
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
			for _, l := range envolver(strings.Join(nombres, " ↔ "), 68) {
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
