package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// Control manual de transacciones en el editor SQL.
//
// Con auto-commit puesto —el default— cada sentencia toma su conexión del pool
// y se confirma sola, y BEGIN/COMMIT/ROLLBACK se rechazan porque no pueden
// cumplirse (K-03/C-01 de la auditoría del 2026-09-11). Con auto-commit
// sacado, la pestaña toma UNA conexión dedicada (engine.Session) y la retiene
// entre ejecuciones: ahí BEGIN, COMMIT y ROLLBACK significan lo que dicen.
//
// El estado vive en openSession y muere con la conexión: un cierre por
// inactividad, un Disconnect o un Connect a otra base revierten lo que cada
// pestaña tuviera abierto y sueltan las conexiones. Nunca vuelve al pool una
// conexión en transacción.
//
// Contra producción, confirmar pide el nombre de la base como cualquier
// escritura: por el botón (Commit), y un COMMIT tipeado en el editor se
// rechaza para que la pregunta no se pueda saltear.

// TransactionState es lo que la pestaña muestra.
type TransactionState struct {
	// Autocommit dice si la pestaña está en el modo normal.
	Autocommit bool `json:"autocommit"`
	// InTransaction dice si hay una transacción abierta —o abortada y sin
	// cerrar— en la conexión de la pestaña.
	InTransaction bool `json:"inTransaction"`
	// Statements es cuántas sentencias corrieron desde que se abrió.
	Statements int `json:"statements"`
	// NeedsConfirmation y ConfirmWord: confirmar pide el nombre de la base.
	NeedsConfirmation bool   `json:"needsConfirmation"`
	ConfirmWord       string `json:"confirmWord,omitempty"`
}

// pestana es una pestaña del editor con auto-commit sacado.
//
// Tiene su propio candado: una sentencia larga en una pestaña no puede
// bloquear a las demás ni a quien pregunta su estado. El del mapa se toma
// solo para buscar o crear.
type pestana struct {
	mu         sync.Mutex
	sesion     engine.Session
	sentencias int
}

// pestanas es el estado por pestaña de una conexión abierta.
type pestanas struct {
	mu    sync.Mutex
	porID map[string]*pestana
}

// cerrar revierte y suelta todas las conexiones dedicadas. Lo llama
// openSession.cerrar, antes de cerrar el pool.
func (p *pestanas) cerrar() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, t := range p.porID {
		t.mu.Lock()
		if t.sesion != nil {
			_ = t.sesion.Close(context.Background())
			t.sesion = nil
		}
		t.mu.Unlock()
		delete(p.porID, id)
	}
}

// enTransaccion cuenta las pestañas con una transacción abierta.
func (p *pestanas) enTransaccion() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, t := range p.porID {
		t.mu.Lock()
		if t.sesion != nil && t.sesion.InTransaction() {
			n++
		}
		t.mu.Unlock()
	}
	return n
}

// ErrTransactionOpen lo devuelve SetAutocommit cuando se quiere volver al
// modo normal con una transacción abierta.
var ErrTransactionOpen = errors.New("hay una transacción abierta en esta pestaña")

// estadoDe arma el estado visible de una pestaña. Se llama con t.mu tomado,
// o con t nil.
func (q *Queries) estadoDe(sesion *openSession, t *pestana) TransactionState {
	st := TransactionState{Autocommit: t == nil}
	if t != nil && t.sesion != nil {
		st.InTransaction = t.sesion.InTransaction()
		st.Statements = t.sentencias
	}
	if sesion.conn.RequiresWriteConfirmation() {
		st.NeedsConfirmation = true
		st.ConfirmWord = nombreDeLaBase(sesion)
	}
	return st
}

// SetAutocommit cambia el modo de una pestaña.
//
// Sacarlo no toma la conexión todavía: se toma en la primera ejecución, para
// no gastar una del pool en una pestaña que no corre nada. Ponerlo con una
// transacción abierta se niega: confirmar o revertir es una decisión, y
// cerrar la pestaña también la toma —revierte—, pero cambiar una casilla no.
func (q *Queries) SetAutocommit(ctx context.Context, tabID string, on bool) (TransactionState, error) {
	sesion, err := q.session.abierta()
	if err != nil {
		return TransactionState{}, err
	}
	if tabID == "" {
		return TransactionState{}, errors.New("falta el identificador de la pestaña")
	}
	p := &sesion.pestanas
	p.mu.Lock()
	t := p.porID[tabID]
	if !on && t == nil {
		t = &pestana{}
		p.porID[tabID] = t
	}
	p.mu.Unlock()
	if t == nil {
		return q.estadoDe(sesion, nil), nil
	}
	t.mu.Lock()
	if !on {
		defer t.mu.Unlock()
		return q.estadoDe(sesion, t), nil
	}
	if t.sesion != nil {
		if t.sesion.InTransaction() {
			defer t.mu.Unlock()
			return q.estadoDe(sesion, t), fmt.Errorf(
				"%w: confirmala o revertila antes de volver a auto-commit", ErrTransactionOpen)
		}
		_ = t.sesion.Close(ctx)
		t.sesion = nil
	}
	// El candado de la pestaña se suelta ANTES de tomar el del mapa: cerrar()
	// y enTransaccion() los toman al revés —mapa y después pestaña—, y con
	// los dos órdenes vivos, apagar auto-commit mientras vence la
	// inactividad dejaba a los dos esperando para siempre, con s.mu tomado
	// y todos los bindings detrás (review del 2026-09-12).
	t.mu.Unlock()
	p.mu.Lock()
	delete(p.porID, tabID)
	p.mu.Unlock()
	return q.estadoDe(sesion, nil), nil
}

// TransactionOf devuelve el estado de una pestaña sin tocar nada: es lo que
// la pestaña pregunta al montarse, por si la conexión cambió por debajo.
func (q *Queries) TransactionOf(tabID string) (TransactionState, error) {
	sesion, err := q.session.abierta()
	if err != nil {
		return TransactionState{}, err
	}
	p := &sesion.pestanas
	p.mu.Lock()
	t := p.porID[tabID]
	p.mu.Unlock()
	if t == nil {
		return q.estadoDe(sesion, nil), nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return q.estadoDe(sesion, t), nil
}

// CloseTab revierte lo que la pestaña tuviera abierto y suelta su conexión.
// Cerrar la pestaña es decidir: lo que no se confirmó no queda.
func (q *Queries) CloseTab(tabID string) error {
	sesion, err := q.session.abierta()
	if err != nil {
		// Sin sesión no hay nada que soltar: ya se soltó con ella.
		return nil
	}
	p := &sesion.pestanas
	p.mu.Lock()
	t := p.porID[tabID]
	delete(p.porID, tabID)
	p.mu.Unlock()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sesion == nil {
		return nil
	}
	err = t.sesion.Close(context.Background())
	t.sesion = nil
	return err
}

// Commit confirma la transacción de la pestaña por el botón. Contra una
// conexión que pide confirmar las escrituras, exige el nombre de la base.
func (q *Queries) Commit(ctx context.Context, tabID, confirm string) RunResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return RunResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}
	if err := confirmarEscritura(sesion, confirm, "para confirmar la transacción"); err != nil {
		return RunResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}
	return q.cerrarTransaccion(ctx, sesion, tabID, "COMMIT")
}

// Rollback revierte la transacción de la pestaña por el botón.
func (q *Queries) Rollback(ctx context.Context, tabID string) RunResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return RunResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}
	return q.cerrarTransaccion(ctx, sesion, tabID, "ROLLBACK")
}

func (q *Queries) cerrarTransaccion(ctx context.Context, sesion *openSession, tabID, sql string) RunResult {
	p := &sesion.pestanas
	p.mu.Lock()
	t := p.porID[tabID]
	p.mu.Unlock()
	if t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
	}
	if t == nil || t.sesion == nil {
		st := q.estadoDe(sesion, t)
		return RunResult{Failure: &engine.Failure{
			Kind: engine.FailureOther, Message: "Esta pestaña no tiene ninguna transacción abierta.",
		}, Transaction: &st}
	}
	inicio := time.Now()
	lote, f := t.sesion.Run(ctx, sql, engine.RunOptions{RowLimit: -1})
	t.sentencias = 0
	q.descartarSiMurio(t)
	st := q.estadoDe(sesion, t)
	if f != nil {
		return RunResult{Batch: lote, Failure: q.porElTunel(f), Transaction: &st}
	}
	if lote == nil {
		lote = &query.Batch{}
	}
	lote.ElapsedMs = time.Since(inicio).Milliseconds()
	// Postgres acepta el COMMIT de una transacción abortada y responde
	// ROLLBACK: la persona escribió el nombre de la base para confirmar y el
	// servidor descartó todo. Eso es un fallo, no un OK.
	if sql == "COMMIT" && len(lote.Results) > 0 && lote.Results[0].Command == "ROLLBACK" {
		return RunResult{Batch: lote, Transaction: &st, Failure: &engine.Failure{
			Kind: engine.FailureOther,
			Message: "La transacción estaba abortada por una sentencia que falló: el servidor " +
				"la revirtió en vez de confirmarla. No quedó nada de lo que corrió desde el BEGIN.",
		}}
	}
	return RunResult{OK: true, Batch: lote, Transaction: &st}
}

// descartarSiMurio suelta la conexión de una pestaña que ya no sirve —pgx y
// el driver de MySQL la cierran al cancelar—; la próxima ejecución toma otra.
// Se llama con t.mu tomado.
func (q *Queries) descartarSiMurio(t *pestana) bool {
	if t.sesion == nil || t.sesion.Alive() {
		return false
	}
	_ = t.sesion.Close(context.Background())
	t.sesion, t.sentencias = nil, 0
	return true
}

// correrEnPestana ejecuta las sentencias de una pestaña con auto-commit
// sacado, en su conexión dedicada. Devuelve nil si la pestaña está en modo
// normal, y entonces RunIn sigue por el camino de siempre.
func (q *Queries) correrEnPestana(
	ctx context.Context, sesion *openSession, tabID, sql string, sentencias []query.Statement,
) *RunResult {
	if tabID == "" {
		return nil
	}
	p := &sesion.pestanas
	p.mu.Lock()
	t := p.porID[tabID]
	p.mu.Unlock()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	// Un COMMIT tipeado contra una conexión que pide confirmar las escrituras
	// se rechaza: el botón es el que pregunta el nombre de la base, y la
	// pregunta no se puede saltear escribiendo la palabra en el editor.
	if sesion.conn.RequiresWriteConfirmation() {
		for i, st := range sentencias {
			if cmd := query.Command(st.SQL, sesion.db.Dialect()); cmd == "COMMIT" || cmd == "END" {
				estado := q.estadoDe(sesion, t)
				return &RunResult{Failure: &engine.Failure{
					Kind: engine.FailureOther,
					Message: "Esta conexión pide confirmar el nombre de la base para escribir: " +
						"confirmá la transacción con el botón, no con un COMMIT en el editor.",
					Hint:            "No se ejecutó ninguna sentencia del lote.",
					Statement:       i + 1,
					Line:            st.Line,
					TotalStatements: len(sentencias),
				}, Transaction: &estado}
			}
		}
	}

	if t.sesion == nil {
		s, err := sesion.db.Dedicated(ctx)
		if err != nil {
			estado := q.estadoDe(sesion, t)
			return &RunResult{Failure: &engine.Failure{
				Kind:    engine.FailureOther,
				Message: "No se pudo tomar una conexión dedicada para la pestaña.",
				Detail:  engine.Redact(err.Error()),
				Hint: "Cada pestaña sin auto-commit retiene una conexión del pool. Subí el " +
					"tamaño del pool en la pestaña Advanced o cerrá otra pestaña.",
			}, Transaction: &estado}
		}
		t.sesion = s
	}

	limite := sesion.conn.Safety.EffectiveRowLimit()
	inicio := time.Now()
	lote := &query.Batch{Results: make([]query.Result, 0, len(sentencias))}
	for i, st := range sentencias {
		habiaTransaccion := t.sesion.InTransaction()
		if cmd := query.Command(st.SQL, sesion.db.Dialect()); cmd == "BEGIN" || cmd == "START" {
			// Un BEGIN encadenado confirma la anterior en MySQL y en SQLite
			// falla: en los dos casos lo que se cuenta arranca de cero.
			t.sentencias = 0
		}
		uno, f := t.sesion.Run(ctx, st.SQL, engine.RunOptions{RowLimit: limite})
		if uno != nil {
			for _, r := range uno.Results {
				r.Line = st.Line
				lote.Results = append(lote.Results, r)
			}
		}
		if f != nil {
			lote.ElapsedMs = time.Since(inicio).Milliseconds()
			q.anotar(sesion.conn.ID, sql, lote, f)
			f.Statement = i + 1
			f.Line = st.Line
			f.TotalStatements = len(sentencias)
			if q.descartarSiMurio(t) && habiaTransaccion {
				// pgx y el driver de MySQL cierran la conexión al cancelar, y
				// con ella la transacción. Hay que decirlo: «cancelada» a
				// secas sugiere que lo anterior sigue pendiente.
				f.Hint = "La cancelación cerró la conexión de la pestaña y con ella la transacción " +
					"que tenía abierta: lo que no se había confirmado se perdió."
			} else if habiaTransaccion && !t.sesion.InTransaction() {
				// SQLite revierte la transacción entera si se interrumpe una
				// escritura adentro; MySQL la confirma antes de un DDL que
				// falla. Las dos son sorpresas, y se dicen.
				f.Hint = strings.TrimSpace(f.Hint + " La transacción que estaba abierta ya no lo está: " +
					"el motor la cerró al fallar esta sentencia.")
			}
			estado := q.estadoDe(sesion, t)
			if !estado.InTransaction {
				t.sentencias = 0
			}
			return &RunResult{Batch: lote, Failure: q.porElTunel(f), Transaction: &estado}
		}
		if t.sesion.InTransaction() {
			t.sentencias++
		} else {
			t.sentencias = 0
		}
	}
	lote.ElapsedMs = time.Since(inicio).Milliseconds()
	q.anotar(sesion.conn.ID, sql, lote, nil)
	estado := q.estadoDe(sesion, t)
	return &RunResult{OK: true, Batch: lote, Transaction: &estado}
}
