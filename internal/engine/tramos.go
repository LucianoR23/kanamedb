package engine

// Tramo es un grupo de sentencias que se pueden mandar juntas, y si van juntas,
// si el motor garantiza que son todo o nada.
type Tramo struct {
	// Desde y Hasta son índices en la lista original: [Desde, Hasta).
	Desde, Hasta int
	// Transaccional dice si este tramo va adentro de un BEGIN de verdad.
	Transaccional bool
}

// Len es cuántas sentencias tiene el tramo.
func (t Tramo) Len() int { return t.Hasta - t.Desde }

// TramosDe parte una lista de sentencias en los grupos más grandes que el motor
// puede garantizar como todo o nada.
//
// Con DDL transaccional —Postgres, SQLite— es un solo tramo con todo adentro,
// que es lo que la casilla «Una sola transacción» promete y cumple.
//
// Sin DDL transaccional —MySQL, MariaDB— NO alcanza con «entonces no uso
// transacción». Mandar todo en un BEGIN igual sería peor que no usarlo: el
// primer DDL hace commit implícito de los datos que venían antes y saca a la
// conexión de la transacción, así que lo que sigue también se commitea solo y
// el ROLLBACK final no revierte nada. La interfaz habría prometido «todo o
// nada» y la base habría aplicado todo.
//
// Entonces se parte: cada corrida de sentencias de datos consecutivas es un
// tramo transaccional de verdad, y cada DDL queda solo. Un DDL solo igual es
// todo o nada por AtomicDDL, así que la garantía que se pierde es únicamente la
// de agrupar — y esa se pierde de todas formas, con la diferencia de que ahora
// se dice en vez de fingirse.
//
// `esDatos` dice si la sentencia i toca filas en vez de estructura.
func TramosDe(n int, caps Caps, esDatos func(i int) bool) []Tramo {
	if n == 0 {
		return nil
	}
	if caps.TransactionalDDL {
		return []Tramo{{Desde: 0, Hasta: n, Transaccional: true}}
	}

	var out []Tramo
	i := 0
	for i < n {
		if !esDatos(i) {
			// Un DDL solo. No se agrupa ni con el DDL de al lado: agruparlos no
			// daría ninguna garantía extra y sí daría la impresión de que sí.
			out = append(out, Tramo{Desde: i, Hasta: i + 1, Transaccional: false})
			i++
			continue
		}
		j := i
		for j < n && esDatos(j) {
			j++
		}
		// Una corrida de datos sí va en transacción: el DML de InnoDB es
		// transaccional de verdad, y eso es lo que hace que la grilla editable
		// de la Iteración 7 tenga la misma garantía que contra Postgres.
		out = append(out, Tramo{Desde: i, Hasta: j, Transaccional: j-i > 0})
		i = j
	}
	return out
}
