package schema

import "time"

// ReferenceAction es lo que pasa con las filas hijas cuando la fila padre se
// borra o cambia de clave.
//
// Los valores son los de Postgres en minúsculas y no los códigos de una letra
// del catálogo: la UI los muestra tal cual y un `c` en pantalla no le dice nada
// a nadie.
type ReferenceAction string

const (
	NoAction   ReferenceAction = "no action"
	Restrict   ReferenceAction = "restrict"
	Cascade    ReferenceAction = "cascade"
	SetNull    ReferenceAction = "set null"
	SetDefault ReferenceAction = "set default"
)

// ForeignKey es una clave foránea: en el ERD, una arista.
//
// Va en el Snapshot y no en el detalle por tabla porque el diagrama las necesita
// todas a la vez para dibujar una sola vista. Pedirlas tabla por tabla serían
// tantos viajes como tablas antes de poder mostrar la primera línea.
type ForeignKey struct {
	Name string `json:"name"`

	// Schema y Table son la tabla que DECLARA la clave.
	//
	// Parecen redundantes cuando la clave cuelga de su propia tabla, y son lo
	// único que la identifica cuando no: en `TableDetail.ReferencedBy` el
	// destino es siempre la tabla que se está mirando, así que sin el dueño dos
	// claves entrantes con el mismo nombre —cosa perfectamente legal en tablas
	// distintas— son indistinguibles.
	Schema string `json:"schema"`
	Table  string `json:"table"`

	// Columns son las columnas locales, en el orden en que las declara la
	// restricción. Una clave compuesta tiene más de una y el orden importa:
	// emparejan posicionalmente con RefColumns.
	Columns []string `json:"columns"`

	// RefSchema, RefTable y RefColumns son el destino. RefSchema se guarda
	// porque una clave puede apuntar a otro esquema, y el ERD muestra uno solo:
	// sin el nombre no hay forma de saber que la punta queda fuera del dibujo.
	RefSchema  string   `json:"refSchema"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`

	OnDelete ReferenceAction `json:"onDelete"`
	OnUpdate ReferenceAction `json:"onUpdate"`

	// Deferrable es "", "deferrable" o "initially deferred".
	Deferrable string `json:"deferrable"`

	// Optional dice si la relación admite no existir, y es lo que decide si la
	// arista va punteada.
	//
	// Es true cuando alguna columna local acepta NULL: con MATCH SIMPLE —el
	// default— una sola columna en NULL alcanza para que Postgres no exija la
	// fila padre, así que basta una para que el lado sea opcional.
	Optional bool `json:"optional"`
}

// Index es un índice de una tabla.
type Index struct {
	Name string `json:"name"`

	// Method es el access method: btree, gist, gin, hash, brin, spgist.
	Method string `json:"method"`

	Unique  bool `json:"unique"`
	Primary bool `json:"primary"`

	// Columns son las columnas CLAVE, con sus modificadores de orden
	// ("placed_at DESC") y con las expresiones de un índice funcional
	// ("lower(email)") tal como se declararon.
	Columns []string `json:"columns"`

	// Included son las columnas de INCLUDE: están guardadas en el índice pero
	// no participan de la búsqueda ni del orden. Van aparte de Columns porque
	// mezclarlas haría ver una columna incluida como si fuera clave, que es
	// exactamente la confusión que lleva a creer que un índice sirve para un
	// WHERE que no cubre.
	Included []string `json:"included,omitempty"`

	// Predicate es el WHERE de un índice parcial, o "" si es total. Un índice
	// parcial que se muestra como total es una mentira que cuesta una consulta
	// lenta descubrir.
	Predicate string `json:"predicate"`

	SizeBytes int64 `json:"sizeBytes"`

	// Scans es cuántas veces lo usó el planificador desde el último reset de
	// estadísticas. Cero significa "nunca desde el reset", que no es lo mismo
	// que "nunca": la UI tiene que decir de dónde sale el número.
	//
	// Es -1 cuando no hay estadísticas disponibles.
	Scans int64 `json:"scans"`

	// Valid es false cuando el índice quedó a medio construir —el caso típico es
	// un CREATE INDEX CONCURRENTLY que falló—. Sigue en el catálogo, ocupa lugar
	// y el planificador no lo usa nunca.
	//
	// Se muestra por la misma razón que el predicado de un índice parcial: un
	// índice inservible que se ve igual que uno bueno hace buscar el problema en
	// cualquier otro lado.
	Valid bool `json:"valid"`

	// Definition es el CREATE INDEX completo, para copiarlo.
	Definition string `json:"definition"`
}

// HasScans dice si Scans tiene un valor utilizable.
func (i Index) HasScans() bool { return i.Scans >= 0 }

// CheckConstraint es una restricción CHECK.
type CheckConstraint struct {
	Name string `json:"name"`

	// Expression es la expresión sola, sin el CHECK de alrededor.
	Expression string `json:"expression"`

	// Validated es false cuando la restricción se agregó con NOT VALID y las
	// filas que ya estaban nunca se comprobaron. La UI tiene que distinguirlo:
	// una restricción sin validar no garantiza lo que dice.
	Validated bool `json:"validated"`
}

// Trigger es un trigger de la tabla.
type Trigger struct {
	Name string `json:"name"`

	// Timing es "before", "after" o "instead of".
	Timing string `json:"timing"`

	// Events son "insert", "update", "delete", "truncate", en ese orden.
	Events []string `json:"events"`

	// Level es "row" o "statement".
	Level string `json:"level"`

	// Function es la función que ejecuta, calificada si no está en el mismo
	// esquema: recalc_totals() o audit.log_change().
	Function string `json:"function"`

	// Enabled es false cuando el trigger está deshabilitado (ALTER TABLE ...
	// DISABLE TRIGGER). Un trigger deshabilitado que se ve igual que uno activo
	// hace perder tardes enteras.
	Enabled bool `json:"enabled"`

	Definition string `json:"definition"`
}

// DetailColumn es una columna con todo lo que la pantalla de estructura muestra.
//
// No reusa Column porque Column viaja en el Snapshot entero —que se lee al
// conectar y alimenta el árbol y el autocompletado— y estos campos son texto
// libre que puede ser largo. Un default que es una expresión de tres líneas por
// cada columna de doscientas tablas es peso que la mayoría de las pantallas no
// usa nunca.
type DetailColumn struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Nullable bool   `json:"nullable"`
	Position int    `json:"position"`

	// Default es la expresión tal como la guarda el catálogo, o "" si no tiene.
	Default string `json:"default"`

	Comment string `json:"comment,omitempty"`

	PrimaryKey bool `json:"primaryKey"`
	ForeignKey bool `json:"foreignKey"`

	// Generated es "", "stored" o "virtual".
	//
	// La distinción no es cosmética: VIRTUAL la agregó PostgreSQL 18 y es una
	// de las tres cosas que Atlas no sabe leer —la regenera como STORED, que es
	// SQL válida y una tabla distinta—. Marcarla acá es lo que le va a permitir
	// a la Iteración 5 negarse a generar DDL en vez de romper en silencio. Ver
	// kaname-plan.md § 6.
	Generated string `json:"generated,omitempty"`

	// Identity es "", "always" o "by default".
	Identity string `json:"identity,omitempty"`

	// NotNullNotValid marca un NOT NULL agregado con NOT VALID: la columna no
	// admite nulos nuevos, pero las filas que ya estaban nunca se comprobaron y
	// pueden tener NULL. Es de PostgreSQL 18 y siempre false en versiones
	// anteriores, donde no existe.
	//
	// Es la segunda cosa que Atlas no ve: reporta la columna como NOT NULL a
	// secas y cree que el dato ya cumple.
	NotNullNotValid bool `json:"notNullNotValid,omitempty"`
}

// TableDetail es todo lo que sabe el catálogo de una tabla.
//
// Se pide por tabla y a demanda, no con el snapshot: índices, constraints y
// triggers de doscientas tablas es un orden de magnitud más de datos que el
// árbol, y la pantalla de estructura muestra una tabla por vez.
type TableDetail struct {
	Schema  string `json:"schema"`
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`

	// RowEstimate es la estimación del planificador, igual que en Table. Es -1
	// cuando la tabla nunca fue analizada.
	RowEstimate int64 `json:"rowEstimate"`

	// TotalBytes es la tabla con sus índices y su TOAST, que es lo que ocupa de
	// verdad en disco. Es lo que muestra el encabezado.
	TotalBytes int64 `json:"totalBytes"`

	// TableBytes es solo el heap, sin índices ni TOAST. Con los dos números se
	// puede decir cuánto pesan los índices, que es la pregunta que aparece
	// cuando el total sorprende.
	TableBytes int64 `json:"tableBytes"`

	Columns     []DetailColumn `json:"columns"`
	Indexes     []Index        `json:"indexes"`
	ForeignKeys []ForeignKey   `json:"foreignKeys"`

	// ReferencedBy son las claves foráneas de OTRAS tablas que apuntan a esta.
	// Sus campos RefSchema/RefTable son esta misma tabla; Columns son las de la
	// tabla que referencia. La pantalla las muestra en la misma lista que las
	// salientes porque para entender qué pasa al borrar una fila hacen falta las
	// dos direcciones.
	ReferencedBy []ForeignKey `json:"referencedBy"`

	Checks   []CheckConstraint `json:"checks"`
	Triggers []Trigger         `json:"triggers"`

	CapturedAt time.Time `json:"capturedAt"`
}

// IndexBytes es cuánto ocupan los índices y el TOAST juntos.
func (d TableDetail) IndexBytes() int64 {
	if d.TotalBytes < d.TableBytes {
		return 0
	}
	return d.TotalBytes - d.TableBytes
}

// TypeOption es un tipo que se puede elegir para una columna.
//
// Sale del catálogo de la base conectada, no de una lista escrita a mano: los
// tipos de PostgreSQL no son un conjunto cerrado. Cada extensión agrega los
// suyos y cada enum o dominio definido en esta base es uno más, así que una
// lista fija sería peor que dejar escribir a mano — no habría forma de elegir
// un tipo propio.
type TypeOption struct {
	// Name es el nombre SQL canónico: "integer", no "int4"; "character
	// varying", no "varchar". Es el que se lee en cualquier otra herramienta.
	// Los tipos de otros esquemas vienen calificados.
	Name string `json:"name"`

	Schema string `json:"schema"`

	// BuiltIn distingue lo que trae PostgreSQL de lo que definió esta base. Los
	// propios se ofrecen primero: son los que nadie recuerda de memoria.
	BuiltIn bool `json:"builtIn"`

	// Kind es "base", "enum", "domain" o "range".
	Kind string `json:"kind"`

	// AcceptsModifier dice si el tipo admite paréntesis con parámetros:
	// numeric(10,2), character varying(255), time(3). Decide si la interfaz
	// ofrece el campo del modificador o lo esconde.
	AcceptsModifier bool `json:"acceptsModifier"`

	Comment string `json:"comment,omitempty"`
}

// ObjectKind es qué clase de objeto es.
//
// Existe para el volcado: es el vocabulario con el que cada motor dice qué hay
// en el esquema que Kaname no sabe renderizar. No es la lista de todo lo que
// existe en una base — es la lista de lo que hay que poder NOMBRAR cuando se
// queda afuera.
type ObjectKind string

const (
	ObjView      ObjectKind = "view"
	ObjMatView   ObjectKind = "materializedView"
	ObjFunction  ObjectKind = "function"
	ObjProcedure ObjectKind = "procedure"
	ObjTrigger   ObjectKind = "trigger"
	ObjPolicy    ObjectKind = "policy"
	ObjType      ObjectKind = "type"
	ObjSequence  ObjectKind = "sequence"
	ObjExtension ObjectKind = "extension"
	ObjEvent     ObjectKind = "event"

	// ObjColumn es una COLUMNA que el volcado no pudo escribir como era: una
	// generada —cuya expresión el catálogo no nos da— o una que se numera sola
	// en un motor donde eso no se puede escribir junto con el resto.
	//
	// Es la única clase que no sale del catálogo sino del propio volcado: es lo
	// que ÉL dejó afuera. Sin ella, un archivo que pierde el autoincremento de
	// la clave primaria se veía idéntico a uno completo.
	ObjColumn ObjectKind = "column"
)

// Object es un objeto del catálogo, nombrado.
//
// Se llama con nombre y apellido a propósito: «quedan 3 funciones afuera» no
// deja decidir nada, y «quedan demo.tocar, demo.calcular y demo.auditar» sí.
type Object struct {
	Kind   ObjectKind `json:"kind"`
	Schema string     `json:"schema"`
	Name   string     `json:"name"`

	// Table es la tabla a la que cuelga, cuando el objeto no vive solo: un
	// trigger o una política de RLS. Vacío en los demás.
	Table string `json:"table,omitempty"`

	// Args son los argumentos que identifican al objeto cuando el nombre no
	// alcanza.
	//
	// Es la forma de identidad del propio motor —la que hay que escribir en un
	// `DROP FUNCTION`— y no una lista de tipos pelada: lleva los nombres de los
	// parámetros y los modos cuando los hay, así que una función se ve como
	// `m demo.mood, VARIADIC extra integer[]` y no como `demo.mood, integer[]`.
	// Se deja así porque es lo que el motor considera la identidad, y porque
	// recortarla a los tipos sería reescribirla a mano para que después no
	// coincida con lo que el catálogo devuelve.
	//
	// Existe por las funciones sobrecargadas de Postgres. `demo.calcular(int)`
	// y `demo.calcular(text)` son DOS objetos distintos, y con el nombre solo
	// salían como dos entradas idénticas en la cobertura del volcado —«quedan
	// afuera demo.calcular y demo.calcular»— y serían dos nodos indistinguibles
	// en el árbol. Peor: no habría forma de pedir la definición de una sin
	// ambigüedad, que es justo lo que el editor necesita.
	//
	// Vacío en todo lo demás y en los motores que no tienen sobrecarga.
	Args string `json:"args,omitempty"`
}

// Completo es cómo se nombra en un aviso: `esquema.nombre`, con los argumentos
// si los tiene y con la tabla entre paréntesis cuando el objeto cuelga de una.
func (o Object) Completo() string {
	nombre := o.Name
	if o.Schema != "" {
		nombre = o.Schema + "." + o.Name
	}
	if o.Args != "" {
		nombre += "(" + o.Args + ")"
	}
	if o.Table != "" {
		nombre += " (" + o.Table + ")"
	}
	return nombre
}

// ObjectDefinition es la definición de un objeto, lista para mostrar en un
// editor y para volver a ejecutar.
type ObjectDefinition struct {
	Object Object `json:"object"`

	// SQL es un CREATE COMPLETO, y eso es una decisión y no lo que dan los
	// motores.
	//
	// Cada uno devuelve una forma distinta: `pg_get_viewdef` da solo el SELECT,
	// `pg_get_functiondef` da el CREATE entero, `SHOW CREATE VIEW` de MySQL da
	// el CREATE con su ALGORITHM y su DEFINER, y `sqlite_master.sql` da el
	// texto tal como se escribió. Se normalizan todas a un CREATE completo
	// porque es lo único que hace verdadera la promesa del editor: **lo que se
	// ve es lo que se ejecuta**. Mostrar un cuerpo y ejecutar otra cosa
	// alrededor deja al usuario revisando un texto que no es el que corre, y la
	// revisión es la garantía de fondo de todo el apply.
	SQL string `json:"sql"`

	// Values son los valores de un enum en su orden de declaración, que en
	// Postgres es el orden en que ordenan y comparan.
	//
	// Van aparte del SQL porque el editor de enums no es un editor de texto:
	// agregar un valor es `ALTER TYPE … ADD VALUE`, que no destruye nada,
	// mientras que sacar uno obliga a recrear el tipo y a reescribir cada
	// columna que lo use. Esa diferencia no se ve en un CREATE.
	Values []string `json:"values,omitempty"`
}
