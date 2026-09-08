// Package query define el resultado de ejecutar SQL, independiente del motor.
//
// Es el equivalente de `internal/schema` para datos en vez de estructura: la
// interfaz habla este vocabulario y nunca ve tipos de pgx. Cuando lleguen MySQL
// y SQLite, cada driver arma este mismo Result y S07 no se entera.
package query

// Class agrupa los tipos del motor en las pocas categorías que la interfaz
// necesita distinguir: alinear a la derecha, elegir un ícono de columna y
// decidir si un valor merece abrirse en el visor de celda.
//
// Deliberadamente pocas. La interfaz no tiene por qué saber la diferencia entre
// int2 e int8 —los dos se alinean a la derecha— y una lista larga se
// desactualiza sola en cuanto alguien define un tipo propio.
type Class string

const (
	ClassText     Class = "text"
	ClassNumber   Class = "number"
	ClassBool     Class = "bool"
	ClassTemporal Class = "temporal"
	ClassJSON     Class = "json"
	ClassBinary   Class = "binary"

	// ClassEnum y ClassArray están separadas de ClassOther porque la interfaz
	// las trata distinto: un enum se edita con un selector de valores y un
	// array se muestra elemento por elemento. Postgres ya las distingue en
	// typcategory, así que no hay que adivinarlas.
	ClassEnum  Class = "enum"
	ClassArray Class = "array"

	// ClassOther es el destino de los dominios y de todo lo que el usuario haya
	// definido y no entre en las anteriores. Se muestra como texto sin
	// pretender saber más de lo que sabemos.
	ClassOther Class = "other"
)

// Column describe una columna del resultado.
type Column struct {
	Name string `json:"name"`
	// DataType es el nombre del tipo tal como lo llama el motor: "int4",
	// "timestamptz", o el nombre del enum que definió el usuario.
	DataType string `json:"dataType"`
	Class    Class  `json:"class"`
}

// Result es lo que devuelve una ejecución.
//
// Los valores viajan como *string y no como string por una razón que no es
// cosmética: en una base, NULL y la cadena vacía son cosas distintas, y una
// grilla que las muestre igual miente sobre los datos. `nil` es NULL; un
// puntero a "" es una cadena vacía de verdad.
//
// Y viajan como texto, no como números o fechas de Go, porque el texto lo
// genera el servidor. Formatear una fecha del lado de Go sería inventar una
// representación distinta de la que devuelve cualquier otro cliente contra la
// misma base, y las diferencias aparecerían justo en los casos raros: zonas
// horarias, infinity, precisión de los numeric.
type Result struct {
	Columns []Column    `json:"columns"`
	Rows    [][]*string `json:"rows"`

	// ReturnsRows separa "la consulta no devolvió filas" de "la consulta no
	// devuelve filas". Un UPDATE sin RETURNING no tiene resultado que mostrar y
	// la interfaz tiene que decir "3 filas afectadas", no dibujar una grilla
	// vacía como si la consulta no hubiera encontrado nada.
	ReturnsRows bool `json:"returnsRows"`

	// AffectedRows es lo que informó el motor para INSERT, UPDATE y DELETE.
	AffectedRows int64 `json:"affectedRows"`

	// Command es el tag crudo del motor: "SELECT 12", "UPDATE 3", "CREATE TABLE".
	Command string `json:"command"`

	// Statements son los tags de TODAS las sentencias que se ejecutaron, en
	// orden. Es lo que alimenta la pestaña Messages.
	//
	// Existe porque un editor de SQL recibe varias sentencias separadas por
	// punto y coma y las ejecuta todas. Mostrar solo el resultado de una y
	// callar el resto sería el peor fallo posible acá: `select 1; drop table x;`
	// mostraría la fila del select como si no hubiera pasado nada más.
	//
	// Cuando una sentencia falla, esta lista trae las que el servidor alcanzó
	// a confirmar antes — pero OJO: eso no significa que hayan quedado
	// aplicadas. Postgres corre el lote en una transacción implícita y revierte
	// todo. Verificado: un `create table` que figura acá con su tag no deja la
	// tabla creada si una sentencia posterior falla. El aviso que lo explica va
	// en Failure.Hint.
	Statements []string `json:"statements"`

	// Truncated dice que la lectura se cortó en RowLimit y que hay más filas
	// del otro lado. Sin esto, un límite silencioso es peor que no tener
	// límite: quien mira la grilla cree que vio todo.
	Truncated bool `json:"truncated"`
	RowLimit  int  `json:"rowLimit"`

	ElapsedMs int64 `json:"elapsedMs"`
}

// RowCount es la cantidad de filas efectivamente leídas.
func (r Result) RowCount() int { return len(r.Rows) }
