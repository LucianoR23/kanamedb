// Package schema define el modelo canónico del esquema de una base.
//
// Es deliberadamente nuestro y no el de Atlas. Atlas cubre tablas, columnas,
// índices y claves, pero vistas materializadas, procedures, triggers,
// secuencias, tipos compuestos y RLS de Postgres son features de su plan Pro.
// Con dos modelos inevitables, la UI habla solo con este: si el open-core de
// Atlas se mueve, cambia un adaptador y no el frontend. Ver kaname-plan.md § 3.
package schema

import (
	"sort"
	"strings"
	"time"
)

// Snapshot es el esquema completo tal como se vio en un momento dado.
type Snapshot struct {
	// Database es la base a la que corresponde.
	Database string `json:"database"`
	// Schemas están ordenados: primero `public`, después el resto alfabético.
	Schemas []Schema `json:"schemas"`
	// CapturedAt permite mostrar cuán viejo es lo que se está viendo.
	CapturedAt time.Time `json:"capturedAt"`

	// ObjectsError es por qué no se pudo leer la lista de objetos, si pasó.
	//
	// No rompe la conexión: el árbol de tablas es útil igual, y hacer fallar un
	// «Conectar» entero porque una consulta al catálogo no se pudo leer sería
	// una regresión sobre lo que funcionaba. Pero tampoco se calla, que es la
	// otra forma de equivocarse: sin esto, un esquema lleno de vistas se vería
	// exactamente igual que uno que no tiene ninguna.
	ObjectsError string `json:"objectsError,omitempty"`
}

// Schema es un esquema y lo que contiene.
type Schema struct {
	Name   string  `json:"name"`
	Owner  string  `json:"owner"`
	Tables []Table `json:"tables"`

	// Objects son los objetos que NO son tablas: vistas, vistas
	// materializadas, funciones, procedimientos, triggers, políticas, enums,
	// dominios, tipos compuestos y secuencias.
	//
	// Viajan con el snapshot y no a demanda por dos motivos. El árbol los
	// muestra todos juntos, y el buscador de arriba filtra sobre TODO lo que
	// hay: una lista que se carga al abrir un nodo no se puede buscar sin
	// abrirlos todos. Y son solo nombres —la definición, que sí puede pesar
	// kilobytes, se pide de a una cuando alguien la abre—.
	Objects []Object `json:"objects,omitempty"`

	// Comment es la descripción que tenga el esquema en el catálogo.
	Comment string `json:"comment,omitempty"`
}

// Table es una tabla.
type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`

	// Partitioned indica que es una tabla particionada. Sus particiones no
	// aparecen como tablas propias en el árbol: son detalle de implementación.
	Partitioned bool `json:"partitioned"`

	// HasPrimaryKey decide si la grilla puede editarse. Sin PK no hay forma
	// segura de identificar una fila para un UPDATE o un DELETE.
	HasPrimaryKey bool `json:"hasPrimaryKey"`

	// Readable es false cuando el usuario ve la tabla en el catálogo pero no
	// puede hacerle SELECT.
	//
	// Se muestra igual y no se esconde: una tabla que desaparece del árbol es
	// más confuso que una marcada como no legible, y con permisos de DDL puede
	// interesar su estructura aunque no sus datos. Pero la UI tiene que decirlo
	// antes de que el usuario haga clic y reciba un error.
	Readable bool `json:"readable"`

	// RowEstimate es una ESTIMACIÓN del planificador, no un conteo.
	//
	// Un `count(*)` exacto recorre la tabla entera; en un esquema de 200 tablas
	// eso son 200 recorridos cada vez que se abre el árbol. La estimación es
	// gratis. La UI tiene que mostrarla como aproximada.
	//
	// Es -1 cuando la tabla nunca fue analizada y no hay estimación.
	RowEstimate int64 `json:"rowEstimate"`

	// Columns son las columnas de la tabla.
	//
	// Se traen junto con el esquema y no por tabla a demanda porque el
	// autocompletado del editor SQL las necesita todas a la vez: pedirlas al
	// escribir haría que la primera sugerencia de cada tabla llegue tarde, que
	// es como se siente un autocompletado roto.
	Columns []Column `json:"columns,omitempty"`

	// ForeignKeys son las claves foráneas SALIENTES de esta tabla.
	//
	// Están en el snapshot y no en el detalle por tabla porque son las aristas
	// del ERD, y el diagrama las necesita todas juntas para dibujar una vista.
	// Pedirlas tabla por tabla serían tantos viajes como tablas antes de poder
	// mostrar la primera línea.
	ForeignKeys []ForeignKey `json:"foreignKeys,omitempty"`
}

// Column es una columna de una tabla.
type Column struct {
	Name string `json:"name"`

	// DataType es el tipo tal como lo escribe el motor, con modificadores:
	// "bigint", "character varying(255)", "numeric(10,2)". Es el texto que va
	// en el panel de esquema y en el autocompletado.
	DataType string `json:"dataType"`

	Nullable   bool `json:"nullable"`
	HasDefault bool `json:"hasDefault"`

	// PrimaryKey y ForeignKey alimentan las etiquetas PK y FK del encabezado de
	// la grilla. Son del catálogo, no deducidas del nombre: una columna que se
	// llama `id` no es necesariamente clave, y una clave puede llamarse
	// cualquier cosa.
	PrimaryKey bool `json:"primaryKey"`
	ForeignKey bool `json:"foreignKey"`

	// Position es attnum: el orden en que las declara la tabla.
	Position int `json:"position"`

	// AutoIncrement dice que la columna se numera sola: identity o serial en
	// Postgres, AUTO_INCREMENT en MySQL, INTEGER PRIMARY KEY en SQLite. Está
	// en el snapshot y no solo en el detalle porque la comparación de esquemas
	// crea tablas desde acá, y una tabla que se creaba «igual» sin el
	// autoincremento escondía la deriva adentro de la corrección (C-08 de la
	// auditoría del 2026-09-11).
	AutoIncrement bool `json:"autoIncrement,omitempty"`
}

// HasRowEstimate dice si RowEstimate tiene un valor utilizable.
func (t Table) HasRowEstimate() bool { return t.RowEstimate >= 0 }

// TotalTables cuenta las tablas de todos los esquemas.
func (s Snapshot) TotalTables() int {
	n := 0
	for _, sc := range s.Schemas {
		n += len(sc.Tables)
	}
	return n
}

// FindSchema devuelve un esquema por nombre.
func (s Snapshot) FindSchema(name string) (Schema, bool) {
	for _, sc := range s.Schemas {
		if sc.Name == name {
			return sc, true
		}
	}
	return Schema{}, false
}

// sortSchemas ordena los esquemas dejando `public` primero.
//
// En Postgres `public` es donde está casi todo lo que le interesa a quien abre
// la app; mandarlo al lugar alfabético que le toca es correcto y molesto.
func sortSchemas(schemas []Schema) {
	sort.SliceStable(schemas, func(i, j int) bool {
		a, b := schemas[i].Name, schemas[j].Name
		if a == "public" {
			return b != "public"
		}
		if b == "public" {
			return false
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
}

// Normalize deja el snapshot en un orden estable y predecible.
//
// La UI compara snapshots para detectar cambios; sin un orden fijo, dos lecturas
// idénticas del mismo esquema podrían verse distintas.
func (s *Snapshot) Normalize() {
	sortSchemas(s.Schemas)
	for i := range s.Schemas {
		tablas := s.Schemas[i].Tables
		sort.SliceStable(tablas, func(a, b int) bool {
			return strings.ToLower(tablas[a].Name) < strings.ToLower(tablas[b].Name)
		})
		// Los objetos se ordenan por CLASE y después por nombre, que es como
		// los agrupa el árbol. Ordenarlos solo por nombre dejaría una vista
		// entre dos funciones y obligaría a la UI a reordenarlos para dibujar
		// los grupos — dos ordenamientos para la misma lista.
		objetos := s.Schemas[i].Objects
		sort.SliceStable(objetos, func(a, b int) bool {
			if objetos[a].Kind != objetos[b].Kind {
				return objetos[a].Kind < objetos[b].Kind
			}
			ka := strings.ToLower(objetos[a].Name + "\x00" + objetos[a].Args)
			kb := strings.ToLower(objetos[b].Name + "\x00" + objetos[b].Args)
			return ka < kb
		})
	}
}
