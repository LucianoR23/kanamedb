// Package change modela los cambios pendientes: qué se quiere hacer, no cómo se
// escribe en SQL.
//
// Es deliberadamente un conjunto ACOTADO de operaciones y no un diff entre dos
// esquemas. La diferencia no es de estilo:
//
// Un differ existe para averiguar la diferencia entre dos esquemas. Acá no hay
// nada que averiguar — quien edita el diagrama hizo la diferencia a mano,
// operación por operación—, y pasarla por un differ obliga a reconstruir el
// esquema entero, entregárselo y aceptar su interpretación de vuelta. Ahí es
// donde se pierde lo que el modelo intermedio no sabe representar: es
// exactamente el camino por el que una columna VIRTUAL de PostgreSQL 18 vuelve
// convertida en STORED, que es SQL válida y otra tabla. Ver kaname-plan.md § 6.
//
// La propiedad que se gana es estructural, no una verificación más: **una
// operación que no sabemos expresar es una operación que la interfaz no
// ofrece**. Nunca se genera SQL aproximada.
//
// El modelo contempla cambios de DATOS desde el arranque aunque la Iteración 5
// solo produzca cambios de esquema. La pantalla de cambios pendientes muestra
// los dos en una sola lista, una transacción y un solo apply, así que separarlos
// obligaría a fusionar dos tuberías cuando llegue la grilla editable.
package change

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Kind separa los cambios de esquema de los de datos.
//
// No es cosmético: deciden cosas distintas. Un DDL que falla a la mitad puede
// dejar el esquema inconsistente; un DML que falla deja filas sin tocar. Y en
// motores sin DDL transaccional —MySQL— la diferencia decide si el «todo o
// nada» se puede prometer.
type Kind string

const (
	KindSchema Kind = "schema"
	KindData   Kind = "data"
)

// Op es la etiqueta que la interfaz muestra al lado de cada sentencia.
type Op string

const (
	OpCreate Op = "CREATE"
	OpAlter  Op = "ALTER"
	OpDrop   Op = "DROP"
	OpInsert Op = "INSERT"
	OpUpdate Op = "UPDATE"
	OpDelete Op = "DELETE"
)

// Type es la operación concreta.
//
// Están todas las que el editor de diagrama y la pantalla de estructura pueden
// producir, y ninguna más. Agregar una capacidad a la interfaz obliga a agregar
// una constante acá y su renderizado en cada motor, que es justamente el punto:
// no hay forma de que la interfaz ofrezca algo que no se sabe escribir.
type Type string

const (
	CreateTable     Type = "createTable"
	DropTable       Type = "dropTable"
	RenameTable     Type = "renameTable"
	SetTableComment Type = "setTableComment"

	AddColumn        Type = "addColumn"
	DropColumn       Type = "dropColumn"
	RenameColumn     Type = "renameColumn"
	SetColumnType    Type = "setColumnType"
	SetNotNull       Type = "setNotNull"
	DropNotNull      Type = "dropNotNull"
	SetDefault       Type = "setDefault"
	DropDefault      Type = "dropDefault"
	SetColumnComment Type = "setColumnComment"

	AddPrimaryKey  Type = "addPrimaryKey"
	AddForeignKey  Type = "addForeignKey"
	AddCheck       Type = "addCheck"
	AddUnique      Type = "addUnique"
	DropConstraint Type = "dropConstraint"

	AddIndex  Type = "addIndex"
	DropIndex Type = "dropIndex"

	// Los tres cambios de DATOS que produce la grilla. Cada uno es UNA fila:
	// editar tres celdas de la misma fila es un updateRow con tres Values y no
	// tres cambios, porque es una sola sentencia y se revisa como tal.
	InsertRow Type = "insertRow"
	UpdateRow Type = "updateRow"
	DeleteRow Type = "deleteRow"
)

// Cell es el valor de una columna en un cambio de datos.
//
// El valor es texto o NULL, igual que en query.Result y por la misma razón: el
// texto lo produce el servidor y el servidor lo vuelve a leer. Un `*string`
// nulo es NULL; un puntero a "" es la cadena vacía. Se comprobó contra los
// cuatro motores que un parámetro de texto entra en cualquier tipo de columna
// —numeric, boolean, timestamptz, jsonb, bytea, int[]— y que lo que el servidor
// devuelve como texto vuelve a entrar tal cual.
//
// Los valores NUNCA se concatenan en la SQL que se ejecuta: viajan como
// parámetros. Ver Statement.Bound.
type Cell struct {
	Column string  `json:"column"`
	Value  *string `json:"value"`
}

// Column describe una columna en un CREATE TABLE o en un ADD COLUMN.
//
// No lleva `Generated` ni `Identity`: el editor no las ofrece todavía, y el
// modelo no promete lo que no se puede escribir. Cuando se ofrezcan, se agregan
// acá y en el renderizado del motor a la vez.
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Nullable bool   `json:"nullable"`

	// Default es la expresión tal cual va en el DDL, o "" si no tiene.
	//
	// Es una EXPRESIÓN, no un valor: `now()` y `'now()'` son cosas distintas y
	// la interfaz tiene que dejar escribir las dos. Por eso no se cita acá.
	Default string `json:"default,omitempty"`

	Comment string `json:"comment,omitempty"`
}

// Change es una operación pendiente.
//
// Es un union etiquetado por `Type` y no una interfaz con métodos porque tiene
// que cruzar el puente a TypeScript como dato. Cada tipo usa los campos que le
// corresponden y deja el resto vacíos; qué campos usa cada uno está en la tabla
// de `Validate`, que es la única fuente de esa verdad.
type Change struct {
	// ID identifica la operación dentro del changeset, para poder excluirla o
	// sacarla. Lo asigna el Set, no quien la crea.
	ID string `json:"id"`

	Type   Type   `json:"type"`
	Schema string `json:"schema"`
	Table  string `json:"table"`

	// Source dice de dónde salió la edición: "erd", "structure", "grid". La
	// pantalla de pendientes lo muestra para que se pueda volver al lugar donde
	// se hizo el cambio.
	Source string `json:"source"`

	// Column es la columna afectada, o la definición completa en addColumn.
	Column *Column `json:"column,omitempty"`

	// Columns son las columnas de un CREATE TABLE, de un índice o de una clave.
	Columns []Column `json:"columns,omitempty"`

	// Names son nombres de columna sueltos: las de un índice, las de una clave.
	Names []string `json:"names,omitempty"`

	// Name es el nombre de la restricción o del índice.
	Name string `json:"name,omitempty"`

	// NewName es el destino de un rename.
	NewName string `json:"newName,omitempty"`

	// DataType es el tipo destino de un setColumnType.
	DataType string `json:"dataType,omitempty"`

	// Expression es el texto de un CHECK, o el USING de un cambio de tipo.
	Expression string `json:"expression,omitempty"`

	// Comment es el texto de un comentario. Vacío borra el comentario, que es
	// lo mismo que dice Postgres con COMMENT IS NULL.
	Comment string `json:"comment,omitempty"`

	// RefSchema, RefTable y RefNames son el destino de una clave foránea.
	RefSchema string   `json:"refSchema,omitempty"`
	RefTable  string   `json:"refTable,omitempty"`
	RefNames  []string `json:"refNames,omitempty"`

	// OnDelete y OnUpdate son las acciones referenciales, en el mismo
	// vocabulario que usa la introspección: "cascade", "restrict", …
	OnDelete string `json:"onDelete,omitempty"`
	OnUpdate string `json:"onUpdate,omitempty"`

	// Unique marca un índice único.
	Unique bool `json:"unique,omitempty"`

	// Method es el access method de un índice: btree, gin, gist…
	Method string `json:"method,omitempty"`

	// Included son las columnas de INCLUDE de un índice.
	Included []string `json:"included,omitempty"`

	// Where es el predicado de un índice parcial.
	Where string `json:"where,omitempty"`

	// Cascade pide arrastrar los objetos dependientes al borrar.
	//
	// Es una decisión que se toma en la interfaz y con aviso: un DROP CASCADE
	// borra cosas que no están en la pantalla.
	Cascade bool `json:"cascade,omitempty"`

	// Values son los valores nuevos de un cambio de datos: en insertRow, las
	// columnas que se cargaron —las demás toman su default—; en updateRow, solo
	// las que cambiaron.
	Values []Cell `json:"values,omitempty"`

	// Key identifica la fila en updateRow y deleteRow: las columnas de la clave
	// primaria con el valor que tenían al leerla.
	//
	// Solo la clave primaria, y no «todas las columnas viejas»: una tabla sin
	// clave no se edita desde la grilla. Y la sentencia tiene que tocar
	// exactamente UNA fila; si toca cero es que la fila ya no está, y si toca
	// más de una la clave no era clave. Las dos cosas se comprueban al
	// ejecutar, y las dos revierten. Ver Statement.Bound.
	Key []Cell `json:"key,omitempty"`

	// Previous es lo que había antes, para que la revisión pueda mostrar
	// «apodo: juan → juanci» en vez de solo la sentencia. En updateRow y en
	// deleteRow es la fila ENTERA como se leyó, para que la revisión muestre
	// todas las columnas y marque las tocadas.
	//
	// No entra en ninguna sentencia. Es para leer.
	Previous []Cell `json:"previous,omitempty"`

	// Excluded deja la operación en el changeset pero fuera de este apply.
	Excluded bool `json:"excluded,omitempty"`
}

// Kind deduce si el cambio es de esquema o de datos.
func (c Change) Kind() Kind {
	switch c.Type {
	case CreateTable, DropTable, RenameTable, SetTableComment,
		AddColumn, DropColumn, RenameColumn, SetColumnType,
		SetNotNull, DropNotNull, SetDefault, DropDefault, SetColumnComment,
		AddPrimaryKey, AddForeignKey, AddCheck, AddUnique, DropConstraint,
		AddIndex, DropIndex:
		return KindSchema
	default:
		return KindData
	}
}

// Op devuelve la etiqueta de la operación.
func (c Change) Op() Op {
	switch c.Type {
	case CreateTable:
		return OpCreate
	case DropTable, DropColumn, DropConstraint, DropIndex:
		return OpDrop
	case InsertRow:
		return OpInsert
	case UpdateRow:
		return OpUpdate
	case DeleteRow:
		return OpDelete
	default:
		return OpAlter
	}
}

// Destructive dice si la operación puede perder datos que no se recuperan
// deshaciendo el cambio.
//
// No es lo mismo que "riesgosa": un SET NOT NULL bloquea la tabla y puede
// fallar, pero no borra nada. Estas sí, y son las que la interfaz marca en rojo
// y las que exigen confirmación extra contra producción.
func (c Change) Destructive() bool {
	switch c.Type {
	case DropTable, DropColumn:
		return true
	case DropConstraint, DropIndex:
		// No borran filas, pero sí una garantía o una estructura que puede
		// tardar horas en reconstruirse. Cascade los vuelve francamente
		// destructivos porque arrastran objetos que no están a la vista.
		return c.Cascade
	case SetColumnType:
		// Un cambio de tipo puede truncar: numeric(10,2) a integer descarta los
		// decimales de todas las filas, sin aviso del motor.
		return true
	case DeleteRow:
		// Una fila borrada no vuelve. Un updateRow no entra acá: el valor viejo
		// está en Previous y se puede volver a escribir.
		return true
	default:
		return false
	}
}

// Target es el objeto que toca la operación, para agrupar en la pantalla.
func (c Change) Target() string {
	if c.Schema == "" {
		return c.Table
	}
	return c.Schema + "." + c.Table
}

// Validate comprueba que el cambio tenga lo que su tipo necesita.
//
// Es la única tabla de qué campo usa cada operación. Un cambio que no valida no
// llega al renderizado: preferimos un error acá, con el nombre de la operación,
// que una sentencia a medio armar.
func (c Change) Validate() error {
	if c.Table == "" {
		return fmt.Errorf("%s: falta la tabla", c.Type)
	}
	falta := func(campo string) error {
		return fmt.Errorf("%s sobre %s: falta %s", c.Type, c.Target(), campo)
	}

	switch c.Type {
	case CreateTable:
		if len(c.Columns) == 0 {
			return falta("al menos una columna")
		}
		for _, col := range c.Columns {
			if col.Name == "" || col.DataType == "" {
				return falta("el nombre o el tipo de una columna")
			}
		}
	case DropTable:
		// La tabla alcanza.
	case RenameTable, RenameColumn:
		if c.NewName == "" {
			return falta("el nombre nuevo")
		}
		if c.Type == RenameColumn && c.Column == nil {
			return falta("la columna")
		}
	case SetTableComment:
		// El comentario vacío es válido: borra el que haya.
	case AddColumn:
		if c.Column == nil || c.Column.Name == "" || c.Column.DataType == "" {
			return falta("la definición de la columna")
		}
		if !c.Column.Nullable && c.Column.Default == "" {
			// Postgres lo rechaza en una tabla con filas, y el error que da no
			// explica qué hacer. Mejor decirlo antes de generar la sentencia.
			return fmt.Errorf(
				"%s sobre %s: una columna NOT NULL nueva necesita un valor por defecto, "+
					"o las filas que ya están no tendrían qué poner", c.Type, c.Target())
		}
	case DropColumn, SetNotNull, DropNotNull, DropDefault, SetColumnComment:
		if c.Column == nil || c.Column.Name == "" {
			return falta("la columna")
		}
	case SetColumnType:
		if c.Column == nil || c.Column.Name == "" {
			return falta("la columna")
		}
		if c.DataType == "" {
			return falta("el tipo nuevo")
		}
	case SetDefault:
		if c.Column == nil || c.Column.Name == "" {
			return falta("la columna")
		}
		if c.Column.Default == "" {
			return fmt.Errorf("%s sobre %s: el valor por defecto está vacío; "+
				"para sacarlo, la operación es %s", c.Type, c.Target(), DropDefault)
		}
	case AddPrimaryKey, AddUnique:
		if len(c.Names) == 0 {
			return falta("las columnas")
		}
	case AddForeignKey:
		if len(c.Names) == 0 || len(c.RefNames) == 0 {
			return falta("las columnas")
		}
		if len(c.Names) != len(c.RefNames) {
			return fmt.Errorf(
				"%s sobre %s: %d columnas locales contra %d referenciadas; emparejan por posición",
				c.Type, c.Target(), len(c.Names), len(c.RefNames))
		}
		if c.RefTable == "" {
			return falta("la tabla referenciada")
		}
	case AddCheck:
		if c.Expression == "" {
			return falta("la expresión")
		}
	case DropConstraint, DropIndex:
		if c.Name == "" {
			return falta("el nombre")
		}
	case AddIndex:
		if len(c.Names) == 0 {
			return falta("las columnas")
		}
	case InsertRow:
		// Sin Values es válido: la fila entera toma sus defaults.
		if err := celdas(c.Values, "los valores"); err != nil {
			return fmt.Errorf("%s sobre %s: %w", c.Type, c.Target(), err)
		}
	case UpdateRow:
		if len(c.Values) == 0 {
			return falta("los valores nuevos")
		}
		if err := celdas(c.Values, "los valores"); err != nil {
			return fmt.Errorf("%s sobre %s: %w", c.Type, c.Target(), err)
		}
		if err := clave(c.Key); err != nil {
			return fmt.Errorf("%s sobre %s: %w", c.Type, c.Target(), err)
		}
	case DeleteRow:
		if err := clave(c.Key); err != nil {
			return fmt.Errorf("%s sobre %s: %w", c.Type, c.Target(), err)
		}
	default:
		return fmt.Errorf("operación desconocida: %q", c.Type)
	}
	return nil
}

// celdas comprueba que cada columna tenga nombre y que ninguna se repita.
//
// La repetición se rechaza acá y no se deja al motor: `SET a = 1, a = 2` es un
// error en Postgres pero MySQL lo acepta y se queda con el último, y una
// sentencia que hace cosas distintas según el motor no es una sentencia que
// queramos escribir.
func celdas(cs []Cell, que string) error {
	vistas := make(map[string]bool, len(cs))
	for _, c := range cs {
		if c.Column == "" {
			return fmt.Errorf("una columna de %s no tiene nombre", que)
		}
		if vistas[c.Column] {
			return fmt.Errorf("la columna %q aparece dos veces en %s", c.Column, que)
		}
		vistas[c.Column] = true
	}
	return nil
}

// clave exige que la fila esté identificada.
func clave(k []Cell) error {
	if len(k) == 0 {
		return errors.New("falta la clave que identifica la fila")
	}
	return celdas(k, "la clave")
}

// Set es el changeset de una sesión: qué se editó y todavía no se aplicó.
//
// Es seguro para uso concurrente porque los servicios de Wails corren en
// goroutines distintas.
type Set struct {
	mu        sync.RWMutex
	cambios   []Change
	siguiente int
}

// Add agrega un cambio y devuelve su identificador.
func (s *Set) Add(c Change) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.siguiente++
	c.ID = fmt.Sprintf("c%d", s.siguiente)
	s.cambios = append(s.cambios, c)
	return c.ID, nil
}

// Remove saca un cambio del changeset. Devuelve false si no estaba.
func (s *Set) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.cambios {
		if c.ID == id {
			s.cambios = append(s.cambios[:i], s.cambios[i+1:]...)
			return true
		}
	}
	return false
}

// SetExcluded deja un cambio dentro del changeset pero fuera del apply.
func (s *Set) SetExcluded(id string, excluir bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cambios {
		if s.cambios[i].ID == id {
			s.cambios[i].Excluded = excluir
			return true
		}
	}
	return false
}

// Clear vacía el changeset.
func (s *Set) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cambios = nil
}

// List devuelve los cambios en el orden en que se hicieron.
//
// Ese orden es el que la pantalla muestra; el orden de EJECUCIÓN lo decide
// Ordered, que es otra cosa.
func (s *Set) List() []Change {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Change(nil), s.cambios...)
}

// Len es cuántos cambios hay, incluidos los excluidos.
func (s *Set) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cambios)
}

// Ordered devuelve los cambios INCLUIDOS en el orden en que hay que ejecutarlos.
//
// El orden en que se editó no sirve: nadie crea una tabla antes de acordarse de
// que la necesitaba. Se ordena por fase, y dentro de cada fase se conserva el
// orden de edición para que el resultado sea predecible.
//
// Las fases salen de qué depende de qué:
//
//  1. Crear tablas — todo lo demás puede referirlas.
//  2. Agregar y modificar columnas — las restricciones y los índices las usan.
//  3. Restricciones e índices nuevos — necesitan las columnas ya puestas.
//  4. Datos — sobre el esquema ya definitivo.
//  5. Borrar restricciones e índices — antes de las columnas que los sostienen.
//  6. Borrar columnas.
//  7. Borrar y renombrar tablas — lo último: renombrar antes rompería todas las
//     referencias de las sentencias anteriores, que están escritas con el nombre
//     viejo.
func (s *Set) Ordered() []Change {
	s.mu.RLock()
	defer s.mu.RUnlock()

	incluidos := make([]Change, 0, len(s.cambios))
	for _, c := range s.cambios {
		if !c.Excluded {
			incluidos = append(incluidos, c)
		}
	}
	sort.SliceStable(incluidos, func(i, j int) bool {
		return fase(incluidos[i].Type) < fase(incluidos[j].Type)
	})
	return incluidos
}

func fase(t Type) int {
	switch t {
	case CreateTable:
		return 1
	case AddColumn, SetColumnType, SetDefault, DropDefault, DropNotNull,
		SetColumnComment, SetTableComment, RenameColumn:
		return 2
	case AddPrimaryKey, AddUnique, AddForeignKey, AddCheck, AddIndex, SetNotNull:
		return 3
	case DropConstraint, DropIndex:
		return 5
	case DropColumn:
		return 6
	case DropTable, RenameTable:
		return 7
	default:
		// Los cambios de datos van entre el esquema nuevo y el que se borra.
		return 4
	}
}

// Summary son los números que la pantalla de pendientes muestra arriba.
type Summary struct {
	Total       int `json:"total"`
	Included    int `json:"included"`
	Schema      int `json:"schema"`
	Data        int `json:"data"`
	Destructive int `json:"destructive"`
}

// Summarize cuenta el changeset. Solo cuenta lo incluido, salvo Total.
func (s *Set) Summarize() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := Summary{Total: len(s.cambios)}
	for _, c := range s.cambios {
		if c.Excluded {
			continue
		}
		out.Included++
		if c.Kind() == KindSchema {
			out.Schema++
		} else {
			out.Data++
		}
		if c.Destructive() {
			out.Destructive++
		}
	}
	return out
}

// Tables son las tablas que toca el changeset, ordenadas.
func (s *Set) Tables() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vistas := map[string]bool{}
	var out []string
	for _, c := range s.cambios {
		t := c.Target()
		if !vistas[t] {
			vistas[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// Describe resume el changeset en una línea, para logs y mensajes.
//
// Nunca incluye valores de datos: los nombres de tabla sí, el contenido de las
// filas no. Ver CLAUDE.md.
func (s *Set) Describe() string {
	r := s.Summarize()
	if r.Total == 0 {
		return "sin cambios pendientes"
	}
	partes := []string{fmt.Sprintf("%d cambios", r.Included)}
	if r.Destructive > 0 {
		partes = append(partes, fmt.Sprintf("%d destructivos", r.Destructive))
	}
	if excl := r.Total - r.Included; excl > 0 {
		partes = append(partes, fmt.Sprintf("%d excluidos", excl))
	}
	return strings.Join(partes, ", ")
}
