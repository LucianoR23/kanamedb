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

// Kind es el tipo de objeto del esquema.
//
// Están declarados todos los que el árbol va a mostrar, aunque la Iteración 1
// solo complete tablas: el modelo se define entero de una vez para no tener que
// migrar el contrato del frontend en cada iteración.
type Kind string

const (
	KindTable     Kind = "table"
	KindView      Kind = "view"
	KindMatView   Kind = "matview"
	KindFunction  Kind = "function"
	KindProcedure Kind = "procedure"
	KindTrigger   Kind = "trigger"
	KindEnum      Kind = "enum"
	KindSequence  Kind = "sequence"
)

// Snapshot es el esquema completo tal como se vio en un momento dado.
type Snapshot struct {
	// Database es la base a la que corresponde.
	Database string `json:"database"`
	// Schemas están ordenados: primero `public`, después el resto alfabético.
	Schemas []Schema `json:"schemas"`
	// CapturedAt permite mostrar cuán viejo es lo que se está viendo.
	CapturedAt time.Time `json:"capturedAt"`
}

// Schema es un esquema y lo que contiene.
type Schema struct {
	Name   string  `json:"name"`
	Owner  string  `json:"owner"`
	Tables []Table `json:"tables"`

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
	}
}
