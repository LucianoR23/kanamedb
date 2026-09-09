package service

import (
	"context"
	"sync"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// Queries ejecuta SQL contra la sesión abierta.
//
// Es un servicio aparte de Session porque son dos responsabilidades: Session
// sostiene la conexión y el esquema; esto ejecuta y cancela.
type Queries struct {
	session *Session

	mu sync.Mutex
	// enCurso mapea el identificador que eligió la interfaz a la función que
	// corta esa ejecución.
	//
	// Es un mapa y no una sola función porque el editor puede tener varias
	// pestañas corriendo. Con una sola, apretar "cancelar" en una pestaña
	// cortaría la consulta de otra — un error que además es intermitente, así
	// que aparecería en producción y no en una prueba.
	enCurso map[string]context.CancelFunc
}

func NewQueries(s *Session) *Queries {
	return &Queries{session: s, enCurso: map[string]context.CancelFunc{}}
}

// RunResult es el resultado de ejecutar.
//
// Struct con `ok` y no un par, por lo mismo que ConnectResult: un par se puede
// ignorar a medias, y una consulta fallida terminaría dibujando una grilla
// vacía como si no hubiera encontrado filas.
type RunResult struct {
	OK bool `json:"ok"`

	// Batch trae TODOS los resultados del lote, no solo el último. La interfaz
	// deja elegir cuál mirar: con `select 1; select 2;` los dos existen, y
	// quedarse con uno obligaría a volver a ejecutar para ver el otro.
	Batch   *query.Batch    `json:"batch,omitempty"`
	Failure *engine.Failure `json:"failure,omitempty"`
}

// TableDataResult agrega a RunResult qué orden se usó.
type TableDataResult struct {
	OK bool `json:"ok"`

	// Result y no Batch: leer una tabla es una sola sentencia, y obligar a la
	// interfaz a desenvolver un lote de uno sería ceremonia sin motivo.
	Result  *query.Result   `json:"result,omitempty"`
	Failure *engine.Failure `json:"failure,omitempty"`

	// OrderedBy son las columnas que hacen determinista el paginado. Vacío
	// significa que la tabla no tiene clave primaria y que "cargar más" puede
	// repetir o saltear filas: la interfaz tiene que decirlo, no taparlo.
	OrderedBy []string `json:"orderedBy"`
}

// TableDataRequest es la página de tabla que se pide.
type TableDataRequest struct {
	// RunID lo elige la interfaz y sirve para cancelar esta ejecución y no otra.
	RunID  string `json:"runId"`
	Schema string `json:"schema"`
	Table  string `json:"table"`

	// OrderBy vacío hace que el servicio resuelva la clave primaria.
	OrderBy    []string `json:"orderBy"`
	Descending bool     `json:"descending"`

	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Run ejecuta la SQL que escribió el usuario.
func (q *Queries) Run(ctx context.Context, runID, sql string) RunResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return RunResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: err.Error(),
		}}
	}

	ctx, listo := q.registrar(ctx, runID)
	defer listo()

	lote, f := sesion.db.Run(ctx, sql, engine.RunOptions{
		RowLimit: sesion.conn.Safety.EffectiveRowLimit(),
	})
	if f != nil {
		// El lote parcial viaja igual: dice qué alcanzó a procesar el servidor
		// antes del error, que es distinto de "no pasó nada".
		return RunResult{Batch: lote, Failure: q.porElTunel(f)}
	}
	return RunResult{OK: true, Batch: lote}
}

// TableData lee una página de una tabla para S10.
func (q *Queries) TableData(ctx context.Context, req TableDataRequest) TableDataResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return TableDataResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: err.Error(),
		}}
	}

	ctx, listo := q.registrar(ctx, req.RunID)
	defer listo()

	orden := req.OrderBy
	if len(orden) == 0 {
		// Sin orden, LIMIT/OFFSET no define qué filas vuelven. La clave primaria
		// es el único orden que se puede elegir sin preguntar y que además es
		// único, que es lo que hace determinista el paginado.
		//
		// Que falle no es motivo para no mostrar los datos: se sigue sin orden y
		// OrderedBy queda vacío, que es la señal de que el paginado es
		// aproximado.
		if pk, err := sesion.db.PrimaryKeyColumns(ctx, req.Schema, req.Table); err == nil {
			orden = pk
		}
	}

	limite := req.Limit
	if limite <= 0 {
		limite = sesion.conn.Safety.EffectiveRowLimit()
	}

	res, f := sesion.db.Page(ctx, req.Schema, req.Table, engine.PageOptions{
		OrderBy:    orden,
		Descending: req.Descending,
		Limit:      limite,
		Offset:     req.Offset,
	})
	if f != nil {
		return TableDataResult{Failure: q.porElTunel(f)}
	}
	return TableDataResult{OK: true, Result: res, OrderedBy: orden}
}

// CountResult es el conteo exacto de una tabla.
type CountResult struct {
	OK      bool            `json:"ok"`
	Count   int64           `json:"count"`
	Failure *engine.Failure `json:"failure,omitempty"`
}

// TableCount cuenta las filas, exacto.
//
// Va aparte de TableData a propósito: en una tabla grande el conteo recorre
// todo, y la grilla tiene que poder mostrar las primeras filas sin esperarlo.
func (q *Queries) TableCount(ctx context.Context, runID, schema, table string) CountResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return CountResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: err.Error(),
		}}
	}

	ctx, listo := q.registrar(ctx, runID)
	defer listo()

	n, f := sesion.db.Count(ctx, schema, table)
	if f != nil {
		return CountResult{Failure: q.porElTunel(f)}
	}
	return CountResult{OK: true, Count: n}
}

// Cancel corta la ejecución con ese identificador.
//
// Cancelar algo que ya terminó no es un error: entre que el usuario aprieta y
// la llamada llega, la consulta pudo haber vuelto sola.
func (q *Queries) Cancel(runID string) {
	q.mu.Lock()
	cancel := q.enCurso[runID]
	q.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Running dice cuántas ejecuciones hay en curso. Para tests y diagnóstico.
func (q *Queries) Running() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.enCurso)
}

// registrar deja la ejecución cancelable por runID y devuelve la limpieza.
func (q *Queries) registrar(ctx context.Context, runID string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	q.mu.Lock()
	q.enCurso[runID] = cancel
	q.mu.Unlock()

	return ctx, func() {
		q.mu.Lock()
		delete(q.enCurso, runID)
		q.mu.Unlock()
		// Después de borrar del mapa: cancelar libera los recursos del context
		// y ya no puede alcanzar a una ejecución posterior con el mismo runID.
		cancel()
	}
}

// porElTunel reetiqueta un fallo cuando la causa real fue el túnel.
//
// Un túnel que se murió se manifiesta como un error de red de la base:
// "connection refused", "connection reset". Sin esta comprobación, la interfaz
// solo puede ofrecer una lista de posibles causas —el servidor, la red, el
// túnel— y quien lo lee tiene que descartarlas de a una. Preguntarle al cliente
// SSH si sigue vivo convierte esa lista en una respuesta.
func (q *Queries) porElTunel(f *engine.Failure) *engine.Failure {
	if f == nil || !q.session.TunnelDown() {
		return f
	}
	return &engine.Failure{
		Kind:    engine.FailureTunnel,
		Message: "El túnel SSH se cerró.",
		Hint:    "La base puede estar perfectamente: lo que se cortó es el camino hasta ella. Reconectá para abrir el túnel de nuevo.",
		// Se conserva lo que dijo el motor: es la frase que alguien va a
		// reconocer si busca el problema en otro lado.
		Detail: f.Detail,
	}
}
