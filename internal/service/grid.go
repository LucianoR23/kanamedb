package service

import (
	"context"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// GridEdits es una sesión de edición de la grilla: qué filas se tocaron, cuáles
// se marcaron para borrar y cuáles se agregaron, tal como la pantalla las
// tiene. El servicio las convierte en cambios del changeset.
//
// La conversión vive acá y no en el frontend por la regla de siempre: el
// contrato se define y se prueba en Go. Y hay dos cosas que conviene decidir
// de este lado: el ORDEN de las columnas en la sentencia —un mapa de
// TypeScript no lo tiene, y un UPDATE cuyo SET cambia de orden entre dos
// vistas previas idénticas es un error de programa— y qué identifica la fila.
type GridEdits struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`

	// Columns son las de la grilla, en su orden. Es el orden de las sentencias.
	Columns []string `json:"columns"`

	// Key son las columnas de la clave primaria. Sin clave no se edita: la
	// grilla lo sabe y lo dice, y StageGrid la compara contra la clave primaria
	// que dice el catálogo antes de convertir nada.
	Key []string `json:"key"`

	Updates []RowEdit   `json:"updates,omitempty"`
	Deletes [][]*string `json:"deletes,omitempty"`
	Inserts []NewRow    `json:"inserts,omitempty"`
}

// RowEdit es una fila leída y lo que se le cambió.
type RowEdit struct {
	// Before es la fila como se leyó, en el orden de Columns. De acá salen la
	// clave y los valores viejos.
	Before []*string `json:"before"`
	// After son SOLO las columnas que cambiaron, por nombre.
	After map[string]*string `json:"after"`
}

// NewRow es una fila agregada: las columnas que se cargaron, por nombre. Las
// demás toman su valor por defecto.
type NewRow struct {
	Values map[string]*string `json:"values"`
}

// StageGrid convierte una sesión de edición en cambios y los prepara, todos o
// ninguno. Devuelve las vistas en el orden: actualizaciones, borrados, altas.
//
// La clave que manda la grilla se comprueba contra la clave primaria REAL de
// la tabla, leída del catálogo. La grilla la saca del snapshot y casi siempre
// coincide, pero «casi siempre» no alcanza para un WHERE: una clave que no es
// clave alcanza más filas de las que debe, y aunque el conteo lo cazaría al
// aplicar, es mejor rechazarlo acá, con el nombre de la columna.
func (s *Session) StageGrid(ctx context.Context, e GridEdits, confirm string) ([]ChangeView, error) {
	sesion, err := s.abierta()
	if err != nil {
		return nil, err
	}
	if len(e.Updates)+len(e.Deletes) > 0 {
		pk, err := sesion.db.PrimaryKeyColumns(ctx, e.Schema, e.Table)
		if err != nil {
			return nil, fmt.Errorf("leer la clave primaria de %s: %w", e.Table, err)
		}
		if !mismasColumnas(pk, e.Key) {
			return nil, fmt.Errorf(
				"%s: la grilla identifica las filas por %v, pero la clave primaria de la tabla "+
					"es %v. Refrescá el esquema y volvé a abrir la tabla", e.Table, e.Key, pk)
		}
	}
	cs, err := cambiosDeGrilla(e)
	if err != nil {
		return nil, err
	}
	return s.StageMany(ctx, cs, confirm)
}

// mismasColumnas compara dos listas de columnas sin importar el orden.
func mismasColumnas(a, b []string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	vistas := make(map[string]bool, len(a))
	for _, c := range a {
		vistas[c] = true
	}
	for _, c := range b {
		if !vistas[c] {
			return false
		}
	}
	return true
}

// cambiosDeGrilla es la conversión pura, separada para poder probarla sin una
// base abierta.
func cambiosDeGrilla(e GridEdits) ([]change.Change, error) {
	if e.Table == "" {
		return nil, fmt.Errorf("falta la tabla")
	}
	if len(e.Columns) == 0 {
		return nil, fmt.Errorf("%s: la grilla no tiene columnas", e.Table)
	}
	indice := make(map[string]int, len(e.Columns))
	for i, c := range e.Columns {
		if c == "" {
			return nil, fmt.Errorf("%s: una columna de la grilla no tiene nombre", e.Table)
		}
		if _, repetida := indice[c]; repetida {
			return nil, fmt.Errorf("%s: la columna %q está dos veces en la grilla", e.Table, c)
		}
		indice[c] = i
	}
	if len(e.Key) == 0 {
		return nil, fmt.Errorf("%s no tiene clave primaria: sin ella no hay forma de "+
			"identificar la fila que se edita", e.Table)
	}
	for _, k := range e.Key {
		if _, ok := indice[k]; !ok {
			return nil, fmt.Errorf("%s: la columna de la clave %q no está en la grilla", e.Table, k)
		}
	}

	// La clave de una fila leída, en el orden de Key.
	claveDe := func(fila []*string) ([]change.Cell, error) {
		if len(fila) != len(e.Columns) {
			return nil, fmt.Errorf("%s: una fila tiene %d valores y la grilla %d columnas",
				e.Table, len(fila), len(e.Columns))
		}
		out := make([]change.Cell, 0, len(e.Key))
		for _, k := range e.Key {
			out = append(out, change.Cell{Column: k, Value: fila[indice[k]]})
		}
		return out, nil
	}
	// Las celdas de un mapa, en el orden de las columnas de la grilla.
	enOrden := func(m map[string]*string) ([]change.Cell, error) {
		out := make([]change.Cell, 0, len(m))
		for _, c := range e.Columns {
			v, ok := m[c]
			if !ok {
				continue
			}
			out = append(out, change.Cell{Column: c, Value: v})
		}
		if len(out) != len(m) {
			for c := range m {
				if _, ok := indice[c]; !ok {
					return nil, fmt.Errorf("%s: la columna %q no está en la grilla", e.Table, c)
				}
			}
		}
		return out, nil
	}

	var cs []change.Change
	base := change.Change{Schema: e.Schema, Table: e.Table, Source: "grid"}

	for _, u := range e.Updates {
		c := base
		c.Type = change.UpdateRow
		var err error
		if c.Key, err = claveDe(u.Before); err != nil {
			return nil, err
		}
		if c.Values, err = enOrden(u.After); err != nil {
			return nil, err
		}
		if len(c.Values) == 0 {
			return nil, fmt.Errorf("%s: una fila editada no tiene ningún valor cambiado", e.Table)
		}
		// La fila ENTERA como se leyó, no solo lo que cambió: la revisión muestra
		// todas las columnas y marca las tocadas, y con solo las tocadas no
		// podría.
		for i, col := range e.Columns {
			c.Previous = append(c.Previous, change.Cell{Column: col, Value: u.Before[i]})
		}
		cs = append(cs, c)
	}
	for _, fila := range e.Deletes {
		c := base
		c.Type = change.DeleteRow
		var err error
		if c.Key, err = claveDe(fila); err != nil {
			return nil, err
		}
		for i, col := range e.Columns {
			c.Previous = append(c.Previous, change.Cell{Column: col, Value: fila[i]})
		}
		cs = append(cs, c)
	}
	for _, n := range e.Inserts {
		c := base
		c.Type = change.InsertRow
		var err error
		if c.Values, err = enOrden(n.Values); err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("%s: no hay ninguna edición que preparar", e.Table)
	}
	return cs, nil
}
