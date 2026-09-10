package dump

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Cobertura es lo que el volcado de estructura NO sabe escribir.
//
// Es la condición para que el volcado de estructura exista, y no un adorno. El
// problema de un export de esquema no es la dificultad —ya introspectamos el
// catálogo y ya renderizamos DDL— sino el SILENCIO: uno que se olvida de una
// política de RLS se ve idéntico a uno correcto, y quien lo restaura se entera
// meses después. Es el mismo defecto por el que se rechazó Atlas, y no vale
// hacérnoslo a nosotros.
//
// Por eso no alcanza con un aviso genérico. Kaname puede consultar el catálogo
// y contar lo que no sabe renderizar, así que dice «3 funciones (demo.tocar,
// demo.calcular, demo.auditar), 2 vistas y 1 política quedan fuera» — con
// nombre y apellido, en el encabezado del archivo Y en la pantalla.
type Cobertura struct {
	// Fuera son los objetos que el archivo no va a contener.
	Fuera []schema.Object `json:"fuera"`
}

// Vacia dice si el volcado cubre todo lo que hay.
func (c Cobertura) Vacia() bool { return len(c.Fuera) == 0 }

// grupo es un tipo de objeto con los suyos.
type grupo struct {
	Kind    schema.ObjectKind
	Nombres []string
}

// PorTipo agrupa lo que queda afuera, en un orden fijo.
//
// El orden es el de `ordenDeTipos` y no el alfabético ni el del catálogo: lo
// que más suele importar —las vistas y las funciones— va primero, y dos
// corridas sobre la misma base dan el mismo texto.
func (c Cobertura) PorTipo() []grupo {
	porKind := map[schema.ObjectKind][]string{}
	for _, o := range c.Fuera {
		porKind[o.Kind] = append(porKind[o.Kind], o.Completo())
	}
	out := make([]grupo, 0, len(porKind))
	for _, k := range ordenDeTipos {
		nombres, hay := porKind[k]
		if !hay {
			continue
		}
		sort.Strings(nombres)
		out = append(out, grupo{Kind: k, Nombres: nombres})
		delete(porKind, k)
	}
	// Un tipo que aparezca y no esté en la lista de arriba NO se descarta: se
	// muestra al final. Silenciar lo desconocido es exactamente el defecto que
	// esta estructura existe para evitar.
	resto := make([]schema.ObjectKind, 0, len(porKind))
	for k := range porKind {
		resto = append(resto, k)
	}
	sort.Slice(resto, func(i, j int) bool { return resto[i] < resto[j] })
	for _, k := range resto {
		nombres := porKind[k]
		sort.Strings(nombres)
		out = append(out, grupo{Kind: k, Nombres: nombres})
	}
	return out
}

var ordenDeTipos = []schema.ObjectKind{
	schema.ObjView,
	schema.ObjMatView,
	schema.ObjFunction,
	schema.ObjProcedure,
	schema.ObjTrigger,
	schema.ObjPolicy,
	schema.ObjType,
	schema.ObjSequence,
	schema.ObjExtension,
	schema.ObjEvent,
	schema.ObjColumn,
}

// Resumen es la frase de una línea: «3 funciones, 2 vistas y 1 política».
//
// Sin los nombres: es la que va en el botón que abre el detalle. La lista larga
// no puede vivir en un tooltip ni en una nota al pie, porque es la información
// que decide si el archivo sirve para lo que uno lo quiere usar.
func (c Cobertura) Resumen() string {
	if c.Vacia() {
		return ""
	}
	partes := make([]string, 0, 6)
	for _, g := range c.PorTipo() {
		n := len(g.Nombres)
		partes = append(partes, fmt.Sprintf("%d %s", n, etiqueta(g.Kind, n)))
	}
	return unirCon(partes, " y ")
}

// Detalle es el texto largo, con los nombres, para el encabezado del archivo y
// para el panel de la pantalla.
func (c Cobertura) Detalle() []string {
	out := make([]string, 0, len(c.PorTipo()))
	for _, g := range c.PorTipo() {
		n := len(g.Nombres)
		out = append(out, fmt.Sprintf("%d %s: %s", n, etiqueta(g.Kind, n), strings.Join(g.Nombres, ", ")))
	}
	return out
}

// etiqueta es el nombre del tipo en castellano, en singular o plural.
func etiqueta(k schema.ObjectKind, n int) string {
	singular, plural := "objeto", "objetos"
	switch k {
	case schema.ObjView:
		singular, plural = "vista", "vistas"
	case schema.ObjMatView:
		singular, plural = "vista materializada", "vistas materializadas"
	case schema.ObjFunction:
		singular, plural = "función", "funciones"
	case schema.ObjProcedure:
		singular, plural = "procedimiento", "procedimientos"
	case schema.ObjTrigger:
		singular, plural = "trigger", "triggers"
	case schema.ObjPolicy:
		singular, plural = "política de RLS", "políticas de RLS"
	case schema.ObjType:
		singular, plural = "tipo", "tipos"
	case schema.ObjSequence:
		singular, plural = "secuencia", "secuencias"
	case schema.ObjExtension:
		singular, plural = "extensión", "extensiones"
	case schema.ObjEvent:
		singular, plural = "evento", "eventos"
	case schema.ObjColumn:
		singular, plural = "columna", "columnas"
	}
	if n == 1 {
		return singular
	}
	return plural
}

// unirCon arma «a, b y c»: con la «y» antes del último, como se escribe.
func unirCon(partes []string, ultimo string) string {
	switch len(partes) {
	case 0:
		return ""
	case 1:
		return partes[0]
	}
	return strings.Join(partes[:len(partes)-1], ", ") + ultimo + partes[len(partes)-1]
}
