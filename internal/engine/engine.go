// Package engine es la costura entre el servicio y cada motor.
//
// Existe porque a partir de la Iteración 6 hay cuatro, y hasta ahora
// `internal/service` llamaba a `internal/postgres` por su nombre: tenía un
// *pgxpool.Pool en su struct y un pgconn.CommandTag en la firma de su
// ejecutor. Eso funcionaba con un motor y no escala a cuatro.
//
// La costura NO va al nivel del driver. Se evaluó unificar todo detrás de
// database/sql —que es lo que usan MySQL y SQLite— y se descartó: pgx da
// pgx.Batch, y la lectura del detalle de una tabla son siete consultas en un
// solo viaje de ida y vuelta. Sobre un túnel SSH esa diferencia se siente. La
// costura va más arriba, al nivel de lo que el servicio de verdad pide:
// introspección, detalle, consultas, DDL y clasificación de errores.
package engine

import "fmt"

// Kind es qué motor es. MariaDB va aparte de MySQL a propósito: hace años que
// no son el mismo servidor, y las diferencias caen justo donde este programa
// mira —tipos, catálogo, sintaxis de ALTER—.
type Kind string

const (
	Postgres Kind = "postgres"
	MySQL    Kind = "mysql"
	MariaDB  Kind = "mariadb"
	SQLite   Kind = "sqlite"
)

func (k Kind) String() string { return string(k) }

// Label es cómo se llama el motor en la interfaz.
func (k Kind) Label() string {
	switch k {
	case Postgres:
		return "PostgreSQL"
	case MySQL:
		return "MySQL"
	case MariaDB:
		return "MariaDB"
	case SQLite:
		return "SQLite"
	}
	return string(k)
}

// DefaultPort es el puerto habitual del motor. Cero para SQLite, que es un
// archivo y no escucha en ningún lado.
func (k Kind) DefaultPort() int {
	switch k {
	case Postgres:
		return 5432
	case MySQL, MariaDB:
		return 3306
	}
	return 0
}

// EsArchivo dice si el motor es un archivo local en vez de un servidor. Cambia
// el formulario entero de S03: sin host, sin puerto, sin usuario, sin SSL, y
// con un selector de archivo donde iría el nombre de la base.
func (k Kind) EsArchivo() bool { return k == SQLite }

// Caps son las diferencias entre motores que la INTERFAZ tiene que saber.
//
// No es una lista de curiosidades: cada campo de acá apaga, enciende o cambia
// algo que se ve en pantalla. Si un motor no soporta algo y la interfaz lo
// ofrece igual, el usuario se entera cuando falla el apply, que es tarde.
//
// La regla para agregar un campo: solo entra si hay una pantalla que cambia
// según su valor. Una diferencia que no cambia nada de lo que se ve no va acá,
// va adentro del motor que la implementa.
type Caps struct {
	// TransactionalDDL dice si un CREATE/ALTER/DROP puede revertirse.
	//
	// Es el campo más importante de todos porque es el único que puede volver
	// MENTIROSA una promesa que ya está en pantalla: la casilla «Una sola
	// transacción» de S15 promete «todo o nada», y contra MySQL o MariaDB eso
	// es falso. Comprobado, no supuesto: un CREATE TABLE dentro de una
	// transacción sobrevive al ROLLBACK en MySQL 9.7 y en MariaDB 12.3, porque
	// las dos hacen commit implícito antes de cada DDL.
	//
	// Postgres y SQLite sí lo soportan de verdad.
	//
	// Y hay una consecuencia que no se deduce del nombre, que es la peligrosa:
	// en MySQL y MariaDB el DDL no solo NO se revierte — además **commitea lo
	// que venía antes**. Un ALTER en el medio de una transacción hace commit
	// implícito de todo lo anterior y deja la conexión fuera de la
	// transacción, así que lo que venga después también se commitea solo. Un
	// ROLLBACK al final no revierte nada.
	//
	// Comprobado: BEGIN, UPDATE, ALTER, UPDATE, ROLLBACK deja los DOS updates
	// aplicados. Es la trampa exacta que espera a la Iteración 7, donde el
	// changeset mezcla datos y esquema en un solo apply.
	//
	// Por eso, cuando esto es false, las sentencias NO se mandan en un solo
	// BEGIN: se parten en tramos. Ver TramosDe.
	TransactionalDDL bool

	// AtomicDDL dice que CADA sentencia, por separado, es todo o nada.
	//
	// Es distinto de TransactionalDDL y confundirlas sería injusto con MySQL y
	// MariaDB: las dos tienen «atomic DDL» desde hace años —MySQL 8.0, MariaDB
	// 10.6— y eso significa que un ALTER que agrega dos columnas y falla en la
	// segunda no deja puesta la primera. Comprobado contra las dos.
	//
	// Cambia lo que S15 dice al fallar. Sin transacción, «las anteriores YA
	// quedaron aplicadas» es cierto, pero «la que falló quedó a medias» sería
	// falso y asusta de más: la que falló no dejó nada.
	AtomicDDL bool

	// Sequences dice si existen como objeto propio. Postgres y MariaDB sí;
	// MySQL no las tiene y SQLite tampoco. El árbol de objetos muestra un nodo
	// que en dos de los cuatro motores no puede existir.
	Sequences bool

	// RebuildsTableOnAlter dice que cambiar una columna reescribe la tabla
	// entera en vez de tocar metadatos. Es SQLite, que para casi cualquier
	// ALTER crea una tabla nueva, copia, y renombra. Cambia lo que S15 tiene
	// que advertir: el costo no es «solo metadatos» sino el tamaño de la tabla.
	RebuildsTableOnAlter bool

	// Schemas dice si el motor tiene esquemas dentro de una base. Postgres sí;
	// en MySQL y MariaDB «schema» y «database» son la misma cosa, y SQLite no
	// tiene ninguno de los dos. El árbol de objetos y el selector del diagrama
	// cambian de forma según esto.
	Schemas bool

	// MultipleDatabases dice si una conexión puede ver más de una base.
	MultipleDatabases bool

	// CheckConstraints, GeneratedColumns y PartialIndexes son las tres cosas
	// que la pantalla de estructura muestra como pestañas o columnas y que no
	// existen en todos lados.
	CheckConstraints bool
	GeneratedColumns bool
	PartialIndexes   bool

	// DeferrableConstraints es de Postgres nada más, y decide si el diálogo de
	// clave foránea ofrece esa casilla.
	DeferrableConstraints bool

	// MaxIdentifier es cuántos bytes admite un nombre antes de que el motor lo
	// trunque o lo rechace. Se valida ANTES de renderizar: Postgres trunca en
	// silencio a 63, que es un bug esperando, y por eso ya se rechaza.
	MaxIdentifier int

	// ExplainPrefix es lo que se antepone a una sentencia para pedir su plan
	// SIN ejecutarla: `EXPLAIN` en Postgres, MySQL y MariaDB, `EXPLAIN QUERY
	// PLAN` en SQLite (un `EXPLAIN` a secas ahí devuelve el bytecode de la
	// máquina virtual, que no le sirve a nadie).
	//
	// La única decisión de seguridad del plan de ejecución es que esta
	// variante no corre nada. `EXPLAIN ANALYZE` sí corre —un DELETE incluido—
	// y por eso no está acá ni como opción: un botón que a veces ejecuta y a
	// veces no es exactamente la clase de cosa que esta aplicación no hace.
	ExplainPrefix string
}

// Statement es cómo se cita un identificador en este motor. Va acá y no en el
// frontend porque es específico del motor, y la interfaz no tiene forma de
// saberlo. Ver CLAUDE.md.
type Quoter interface {
	QuoteIdent(s string) string
}

// ErrUnsupported lo devuelve un motor cuando la operación no existe en él.
//
// Nunca debería llegar a verse: la interfaz solo ofrece lo que Caps declara. Si
// aparece, es un error de programa y el mensaje tiene que decir cuál motor y
// qué operación, para poder arreglarlo sin adivinar.
type ErrUnsupported struct {
	Engine    Kind
	Operation string
	Motivo    string
}

func (e *ErrUnsupported) Error() string {
	if e.Motivo != "" {
		return fmt.Sprintf("%s no soporta %s: %s", e.Engine.Label(), e.Operation, e.Motivo)
	}
	return fmt.Sprintf("%s no soporta %s", e.Engine.Label(), e.Operation)
}
