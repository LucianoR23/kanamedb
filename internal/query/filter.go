package query

import "fmt"

// Operator es cómo se compara una columna con un valor.
//
// La lista es corta y deliberada: son las comparaciones que se pueden escribir
// igual en los cuatro motores. Nada de `ILIKE` —solo Postgres— ni de
// expresiones regulares, que cada motor escribe distinto y con su propia
// sintaxis. Una operación que no se pueda cumplir en los cuatro no entra:
// ofrecerla y que falle en tres es peor que no ofrecerla.
type Operator string

const (
	OpEq  Operator = "eq"
	OpNe  Operator = "ne"
	OpLt  Operator = "lt"
	OpLte Operator = "lte"
	OpGt  Operator = "gt"
	OpGte Operator = "gte"

	// Los tres de texto se escriben con LIKE y un patrón, y el patrón lo arma
	// el renderizador escapando lo que el usuario haya escrito: un `%` tecleado
	// a mano es un porcentaje, no «cualquier cosa».
	OpContains   Operator = "contains"
	OpStartsWith Operator = "startsWith"
	OpEndsWith   Operator = "endsWith"

	OpIsNull    Operator = "isNull"
	OpIsNotNull Operator = "isNotNull"

	OpIn    Operator = "in"
	OpNotIn Operator = "notIn"

	OpBetween Operator = "between"
)

// OperatorInfo describe un operador para que la interfaz no tenga que saber
// cuántos valores pide ni cómo se llama en castellano.
type OperatorInfo struct {
	Key   Operator `json:"key"`
	Label string   `json:"label"`
	// Values es cuántos valores necesita: 0, 1, 2 (between) o -1 (una lista).
	Values int `json:"values"`
}

// Operators es la lista en el orden en que se ofrece.
var Operators = []OperatorInfo{
	{OpEq, "es igual a", 1},
	{OpNe, "no es igual a", 1},
	{OpContains, "contiene", 1},
	{OpStartsWith, "empieza con", 1},
	{OpEndsWith, "termina con", 1},
	{OpGt, "es mayor que", 1},
	{OpGte, "es mayor o igual que", 1},
	{OpLt, "es menor que", 1},
	{OpLte, "es menor o igual que", 1},
	{OpBetween, "está entre", 2},
	{OpIn, "está en la lista", -1},
	{OpNotIn, "no está en la lista", -1},
	{OpIsNull, "es NULL", 0},
	{OpIsNotNull, "no es NULL", 0},
}

// Arity es cuántos valores pide el operador, o -1 si es una lista.
func (o Operator) Arity() (int, error) {
	for _, i := range Operators {
		if i.Key == o {
			return i.Values, nil
		}
	}
	return 0, fmt.Errorf("operador de filtro desconocido: %q", string(o))
}

// Condition es una condición sobre una columna.
//
// Los valores viajan aparte de la SQL y SIEMPRE terminan como parámetros. Es la
// misma regla que los cambios de datos: lo único que se concatena es el nombre
// de la columna, citado por el motor.
type Condition struct {
	Column   string   `json:"column"`
	Operator Operator `json:"operator"`
	// Values son los valores del operador. Un nil adentro es NULL, aunque
	// ningún operador lo necesite hoy: `= NULL` no compara, para eso está
	// isNull.
	Values []*string `json:"values"`
}

// Validate comprueba que la condición tenga sentido antes de llegar al motor.
func (c Condition) Validate() error {
	if c.Column == "" {
		return fmt.Errorf("un filtro sin columna no se puede aplicar")
	}
	n, err := c.Operator.Arity()
	if err != nil {
		return err
	}
	switch {
	case n == -1 && len(c.Values) == 0:
		return fmt.Errorf("«%s» sobre %s necesita al menos un valor", c.Operator.Label(), c.Column)
	case n >= 0 && len(c.Values) != n:
		return fmt.Errorf("«%s» sobre %s necesita %d %s y tiene %d",
			c.Operator.Label(), c.Column, n, plural(n, "valor", "valores"), len(c.Values))
	}
	for _, v := range c.Values {
		if v == nil {
			return fmt.Errorf("«%s» sobre %s no acepta NULL como valor: para eso están «es NULL» y «no es NULL»",
				c.Operator.Label(), c.Column)
		}
	}
	return nil
}

// Label es el nombre del operador en castellano, o el propio código si no está
// en la lista.
func (o Operator) Label() string {
	for _, i := range Operators {
		if i.Key == o {
			return i.Label
		}
	}
	return string(o)
}

func plural(n int, uno, varios string) string {
	if n == 1 {
		return uno
	}
	return varios
}
