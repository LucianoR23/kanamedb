package service

import (
	"context"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/history"
)

// History es el historial de consultas y las consultas guardadas.
//
// Se registra como servicio aparte y no como métodos de Session porque no
// necesita una conexión abierta: el historial de una base se puede mirar
// —y borrar— sin conectarse a ella.
type History struct {
	store   *history.Store
	session *Session
}

// NewHistory arma el servicio.
func NewHistory(st *history.Store, sesion *Session) *History {
	return &History{store: st, session: sesion}
}

// HistoryEntry es una corrida, tal como la ve la interfaz.
type HistoryEntry struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionId"`
	SQL          string `json:"sql"`

	// RanAt va como texto RFC 3339. Un time.Time cruza el puente como string
	// igual, y mandarlo ya formateado deja explícito que del otro lado es texto.
	RanAt     string `json:"ranAt"`
	Runs      int    `json:"runs"`
	ElapsedMs int64  `json:"elapsedMs"`
	Rows      int64  `json:"rows"`
	Failed    bool   `json:"failed"`
	Error     string `json:"error,omitempty"`
}

// SavedQuery es una consulta guardada.
type SavedQuery struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SQL          string `json:"sql"`
	ConnectionID string `json:"connectionId,omitempty"`
	SavedAt      string `json:"savedAt"`
}

// List devuelve el historial de la conexión abierta.
//
// De la ABIERTA y no de todas: la misma consulta contra dos bases distintas es
// otra cosa, y ver el historial de producción mientras se trabaja contra dev es
// la clase de confusión que termina con un DELETE en el lugar equivocado.
func (h *History) List(_ context.Context, limit int) ([]HistoryEntry, error) {
	entradas, err := h.store.List(h.conexion(), limit)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryEntry, 0, len(entradas))
	for _, e := range entradas {
		out = append(out, HistoryEntry{
			ID: e.ID, ConnectionID: e.ConnectionID, SQL: e.SQL,
			RanAt: e.RanAt.Format(formatoFecha), Runs: e.Runs,
			ElapsedMs: e.ElapsedMs, Rows: e.Rows, Failed: e.Failed, Error: e.Error,
		})
	}
	return out, nil
}

// Clear borra el historial de la conexión abierta.
func (h *History) Clear(_ context.Context) error {
	conn := h.conexion()
	if conn == "" {
		return fmt.Errorf("no hay ninguna conexión abierta")
	}
	return h.store.Clear(conn)
}

// Saved devuelve las consultas guardadas, las de esta conexión primero.
func (h *History) Saved(_ context.Context) ([]SavedQuery, error) {
	guardadas, err := h.store.Saved(h.conexion())
	if err != nil {
		return nil, err
	}
	out := make([]SavedQuery, 0, len(guardadas))
	for _, q := range guardadas {
		out = append(out, SavedQuery{
			ID: q.ID, Name: q.Name, SQL: q.SQL,
			ConnectionID: q.ConnectionID, SavedAt: q.SavedAt.Format(formatoFecha),
		})
	}
	return out, nil
}

// Save guarda una consulta con nombre.
func (h *History) Save(_ context.Context, q SavedQuery) (SavedQuery, error) {
	guardada, err := h.store.Save(history.Saved{
		ID: q.ID, Name: q.Name, SQL: q.SQL, ConnectionID: h.conexion(),
	})
	if err != nil {
		return SavedQuery{}, err
	}
	return SavedQuery{
		ID: guardada.ID, Name: guardada.Name, SQL: guardada.SQL,
		ConnectionID: guardada.ConnectionID, SavedAt: guardada.SavedAt.Format(formatoFecha),
	}, nil
}

// DeleteSaved borra una consulta guardada.
func (h *History) DeleteSaved(_ context.Context, id string) error {
	return h.store.DeleteSaved(id)
}

// conexion es contra qué conexión se está trabajando, o "" si no hay ninguna.
func (h *History) conexion() string {
	if h.session == nil {
		return ""
	}
	sesion, err := h.session.abierta()
	if err != nil {
		return ""
	}
	return sesion.conn.ID
}

const formatoFecha = "2006-01-02T15:04:05Z07:00"
