package query

import "strings"

// Items parte el literal de un array en sus elementos.
//
// Existe porque el visor de celda muestra el valor crudo —`{a,"b,c",NULL}`— y
// leerlo así es adivinar dónde termina cada elemento. El parseo va en Go y no
// en el frontend por una razón concreta: tiene casos borde con comillas y
// escapes, y lógica con casos borde sin tests es lógica rota que nadie ve.
//
// `nulos` marca qué elementos son NULL, que no es lo mismo que la cadena
// "NULL": en Postgres `{NULL}` es un elemento nulo y `{"NULL"}` es la palabra.
// La diferencia se pierde si se devuelven solo cadenas.
type Items struct {
	Values []string `json:"values"`
	Nulls  []bool   `json:"nulls"`
}

// Len es cuántos elementos hay.
func (i Items) Len() int { return len(i.Values) }

// ParsePostgresArray parte el literal de un array de Postgres.
//
// La forma es `{a,b,c}`, con los elementos entre comillas dobles cuando llevan
// una coma, una llave, comillas, una barra invertida o espacios en los bordes,
// o cuando su texto es exactamente NULL. Adentro de las comillas, `\` escapa el
// carácter siguiente. Un array multidimensional —`{{1,2},{3,4}}`— se devuelve
// con cada fila como un elemento, sin abrirla: mostrar dos niveles en una lista
// plana sería mentir sobre la forma.
//
// El segundo valor dice si el texto parecía un array. Un valor que no empieza
// con `{` no lo es, y el visor lo muestra como texto en vez de inventar
// elementos.
func ParsePostgresArray(s string) (Items, bool) {
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return Items{}, false
	}
	cuerpo := s[1 : len(s)-1]
	out := Items{Values: []string{}, Nulls: []bool{}}
	if strings.TrimSpace(cuerpo) == "" {
		// `{}` es un array vacío, y es distinto de `{""}`, que tiene un
		// elemento que es la cadena vacía.
		return out, true
	}

	var b strings.Builder
	enComillas := false
	huboComillas := false
	nivel := 0
	agregar := func() {
		texto := b.String()
		// Sin comillas, `NULL` en cualquier combinación de mayúsculas es el
		// nulo; con comillas es la palabra. Los espacios de los bordes solo se
		// recortan cuando no había comillas.
		if !huboComillas {
			texto = strings.TrimSpace(texto)
			if strings.EqualFold(texto, "NULL") {
				out.Values = append(out.Values, "")
				out.Nulls = append(out.Nulls, true)
				b.Reset()
				huboComillas = false
				return
			}
		}
		out.Values = append(out.Values, texto)
		out.Nulls = append(out.Nulls, false)
		b.Reset()
		huboComillas = false
	}

	for i := 0; i < len(cuerpo); i++ {
		c := cuerpo[i]
		switch {
		case enComillas && c == '\\' && i+1 < len(cuerpo):
			i++
			b.WriteByte(cuerpo[i])
		case c == '"':
			enComillas = !enComillas
			huboComillas = true
		case enComillas:
			b.WriteByte(c)
		case c == '{':
			nivel++
			b.WriteByte(c)
		case c == '}':
			nivel--
			b.WriteByte(c)
		case c == ',' && nivel == 0:
			agregar()
		default:
			b.WriteByte(c)
		}
	}
	agregar()
	return out, true
}

// ParseMySQLSet parte el valor de una columna SET de MySQL.
//
// Es una lista separada por comas y nada más: un SET no puede tener comas
// adentro de un valor —MySQL lo prohíbe al crear la columna— así que no hay
// comillas ni escapes que interpretar. Una cadena vacía es el conjunto vacío,
// no un elemento vacío.
func ParseMySQLSet(s string) (Items, bool) {
	out := Items{Values: []string{}, Nulls: []bool{}}
	if s == "" {
		return out, true
	}
	for _, p := range strings.Split(s, ",") {
		out.Values = append(out.Values, p)
		out.Nulls = append(out.Nulls, false)
	}
	return out, true
}
