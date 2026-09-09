package engine

// Las capacidades de cada motor, en un solo lugar.
//
// Están acá y no en cada paquete de motor a propósito: puestas una al lado de
// la otra, la tabla ES la documentación de en qué se diferencian, y agregar un
// motor obliga a contestar todas las preguntas en vez de olvidarse de tres.
//
// Cada valor está comprobado contra el motor de verdad, no leído de un manual.
var capacidades = map[Kind]Caps{
	Postgres: {
		TransactionalDDL:      true,
		RebuildsTableOnAlter:  false,
		Schemas:               true,
		MultipleDatabases:     true,
		CheckConstraints:      true,
		GeneratedColumns:      true,
		PartialIndexes:        true,
		DeferrableConstraints: true,
		// Postgres trunca en silencio a 63 bytes. Ver kaname-plan.md § 6.
		MaxIdentifier: 63,
	},
	MySQL: {
		// Comprobado contra MySQL 9.7.2: un CREATE TABLE dentro de una
		// transacción sobrevive al ROLLBACK. Hay commit implícito antes de
		// cada DDL y no hay forma de apagarlo.
		TransactionalDDL:      false,
		RebuildsTableOnAlter:  false,
		Schemas:               false,
		MultipleDatabases:     true,
		CheckConstraints:      true,
		GeneratedColumns:      true,
		PartialIndexes:        false,
		DeferrableConstraints: false,
		MaxIdentifier:         64,
	},
	MariaDB: {
		// Comprobado contra MariaDB 12.3.3: igual que MySQL.
		TransactionalDDL:      false,
		RebuildsTableOnAlter:  false,
		Schemas:               false,
		MultipleDatabases:     true,
		CheckConstraints:      true,
		GeneratedColumns:      true,
		PartialIndexes:        false,
		DeferrableConstraints: false,
		MaxIdentifier:         64,
	},
	SQLite: {
		TransactionalDDL: true,
		// Casi cualquier ALTER que no sea agregar o renombrar una columna se
		// hace creando una tabla nueva, copiando y renombrando. El costo no es
		// «solo metadatos» sino el tamaño de la tabla, y eso se avisa.
		RebuildsTableOnAlter:  true,
		Schemas:               false,
		MultipleDatabases:     false,
		CheckConstraints:      true,
		GeneratedColumns:      true,
		PartialIndexes:        true,
		DeferrableConstraints: false,
		// SQLite no impone límite. Se pone uno igual, generoso: un nombre de
		// mil caracteres no es un caso de uso, es un accidente.
		MaxIdentifier: 255,
	},
}

// CapsOf devuelve las capacidades de un motor.
//
// Un motor desconocido devuelve el conjunto más conservador posible, no el
// vacío: `Caps{}` diría «no soporta DDL transaccional» —correcto— pero también
// «no soporta CHECK», y la interfaz escondería pestañas que quizás existen. Con
// todo apagado se degrada a lo mínimo, que es la forma segura de equivocarse.
func CapsOf(k Kind) Caps {
	c, ok := capacidades[k]
	if !ok {
		return Caps{MaxIdentifier: 63}
	}
	return c
}

// Kinds son los motores que existen, en el orden en que se muestran.
func Kinds() []Kind { return []Kind{Postgres, MySQL, MariaDB, SQLite} }
