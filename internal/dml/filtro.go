package dml

import (
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// escapeLike es el carácter con el que se escapan los comodines de LIKE.
//
// Se elige `!` y no `\` a propósito. La barra invertida es el escape POR
// DEFECTO de LIKE en Postgres y en MySQL, pero además es un escape de cadena en
// MySQL salvo con NO_BACKSLASH_ESCAPES: escribir `ESCAPE '\'` en el texto de la
// SQL depende entonces de una variable del servidor. `!` no significa nada en
// ninguno de los cuatro y se escribe igual siempre.
const escapeLike = '!'

// Where escribe la cláusula de un filtro de la grilla.
//
// Devuelve la SQL —sin el " WHERE" adelante— y los argumentos, que empiezan a
// numerarse en `desde+1` para poder engancharse después de otros parámetros.
// Los valores NUNCA se concatenan: lo único que entra en el texto es el nombre
// de la columna, citado por el motor, y los operadores, que salen de una lista
// cerrada.
func Where(conds []query.Condition, d Dialect, desde int) (string, []any, error) {
	if len(conds) == 0 {
		return "", nil, nil
	}
	var b strings.Builder
	var args []any
	marcador := func(v *string) string {
		args = append(args, v)
		return d.Placeholder(desde + len(args))
	}

	for i, c := range conds {
		if err := c.Validate(); err != nil {
			return "", nil, err
		}
		if i > 0 {
			// Todas las condiciones se combinan con AND. El OR no está: una
			// mezcla de AND y OR necesita paréntesis, y unos paréntesis que no
			// se ven en la pantalla son una consulta que quien la escribió no
			// puede leer. Cuando haga falta, el editor SQL está al lado.
			b.WriteString(" AND ")
		}
		col := d.QuoteIdent(c.Column)
		switch c.Operator {
		case query.OpEq:
			fmt.Fprintf(&b, "%s = %s", col, marcador(c.Values[0]))
		case query.OpNe:
			// `<> valor` deja afuera las filas con NULL, que es lo que dice el
			// estándar y lo que casi nadie espera: «no es igual a 3» no muestra
			// las que no tienen valor. Se agrega el OR para que las muestre,
			// que es lo que quiso decir quien filtró.
			fmt.Fprintf(&b, "(%s <> %s OR %s IS NULL)", col, marcador(c.Values[0]), col)
		case query.OpLt:
			fmt.Fprintf(&b, "%s < %s", col, marcador(c.Values[0]))
		case query.OpLte:
			fmt.Fprintf(&b, "%s <= %s", col, marcador(c.Values[0]))
		case query.OpGt:
			fmt.Fprintf(&b, "%s > %s", col, marcador(c.Values[0]))
		case query.OpGte:
			fmt.Fprintf(&b, "%s >= %s", col, marcador(c.Values[0]))

		case query.OpContains, query.OpStartsWith, query.OpEndsWith:
			patron := patronLike(c.Operator, *c.Values[0])
			fmt.Fprintf(&b, "%s LIKE %s ESCAPE '%c'", col, marcador(&patron), escapeLike)

		case query.OpIsNull:
			fmt.Fprintf(&b, "%s IS NULL", col)
		case query.OpIsNotNull:
			fmt.Fprintf(&b, "%s IS NOT NULL", col)

		case query.OpIn, query.OpNotIn:
			b.WriteString(col)
			if c.Operator == query.OpNotIn {
				b.WriteString(" NOT")
			}
			b.WriteString(" IN (")
			for j, v := range c.Values {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(marcador(v))
			}
			b.WriteString(")")

		case query.OpBetween:
			fmt.Fprintf(&b, "%s BETWEEN %s AND %s", col, marcador(c.Values[0]), marcador(c.Values[1]))

		default:
			return "", nil, fmt.Errorf("operador de filtro desconocido: %q", string(c.Operator))
		}
	}
	return b.String(), args, nil
}

// patronLike arma el patrón de LIKE escapando lo que el usuario escribió.
//
// Sin esto, buscar «50%» traería todo lo que empieza con 50, y buscar «a_b»
// traería «axb». El comodín lo pone el operador, no el valor.
func patronLike(op query.Operator, v string) string {
	var b strings.Builder
	if op == query.OpContains || op == query.OpEndsWith {
		b.WriteByte('%')
	}
	for i := 0; i < len(v); i++ {
		if v[i] == '%' || v[i] == '_' || v[i] == escapeLike {
			b.WriteByte(escapeLike)
		}
		b.WriteByte(v[i])
	}
	if op == query.OpContains || op == query.OpStartsWith {
		b.WriteByte('%')
	}
	return b.String()
}
