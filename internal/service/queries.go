package service

import (
	"context"
	"sync"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/history"
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

	// historial es dónde se anota cada corrida. Nil lo apaga, que es lo que
	// pasa en los tests que no lo necesitan.
	historial *history.Store
}

// UsarHistorial le da a las consultas dónde anotarse.
//
// Va por un setter y no por el constructor porque el historial necesita al
// Session y las Queries también: pasarlo por el constructor obligaba a armar
// los tres en un orden que no existe.
func (q *Queries) UsarHistorial(h *history.Store) { q.historial = h }

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

	// Where son las condiciones del filtro de la grilla. Los valores viajan
	// como parámetros: en la consulta que se ejecuta solo entra el nombre de
	// la columna, citado por el motor.
	Where []query.Condition `json:"where"`

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

	// El texto se parte ACÁ y cada sentencia se manda sola.
	//
	// Antes iba entero en una sola llamada y cada driver hacía una cosa
	// distinta: Postgres corría todo y devolvía un resultado por sentencia,
	// MySQL contestaba un error de sintaxis sin correr nada, y SQLite corría
	// TODO y devolvía UN resultado —así que tres INSERT escribían tres filas y
	// la pantalla mostraba una—. Ver query.Split y § 6 del plan.
	sentencias := query.Split(sql, sesion.db.Dialect())
	if len(sentencias) == 0 {
		// Un texto que es solo comentarios o espacio. No es un error: no hay
		// nada que ejecutar, y mandarlo al motor haría que conteste uno.
		return RunResult{OK: true, Batch: &query.Batch{}}
	}

	limite := sesion.conn.Safety.EffectiveRowLimit()
	inicio := time.Now()
	lote := &query.Batch{Results: make([]query.Result, 0, len(sentencias))}

	for i, st := range sentencias {
		uno, f := sesion.db.Run(ctx, st.SQL, engine.RunOptions{RowLimit: limite})
		if uno != nil {
			for _, r := range uno.Results {
				r.Line = st.Line
				lote.Results = append(lote.Results, r)
			}
		}
		if f != nil {
			lote.ElapsedMs = time.Since(inicio).Milliseconds()
			q.anotar(sesion.conn.ID, sql, lote, f)
			// Se corta en la primera que falla. Seguir sería peor: casi siempre
			// las que vienen dependen de la que rompió, y el resultado sería
			// una lista de errores en cascada donde el primero es el único que
			// importa. Lo que YA corrió viaja en el lote, que es distinto de
			// «no pasó nada».
			f.Statement = i + 1
			f.Line = st.Line
			f.TotalStatements = len(sentencias)
			return RunResult{Batch: lote, Failure: q.porElTunel(f)}
		}
	}
	lote.ElapsedMs = time.Since(inicio).Milliseconds()
	q.anotar(sesion.conn.ID, sql, lote, nil)
	return RunResult{OK: true, Batch: lote}
}

// anotar deja la corrida en el historial.
//
// Se guarda el TEXTO ENTERO tal como se escribió, no cada sentencia por
// separado: lo que uno quiere recuperar del historial es lo que tenía en el
// editor, y partirlo obligaría a volver a juntarlo a mano.
//
// Un fallo al escribir el historial NO afecta a la consulta: ya corrió, el
// resultado está, y no poder anotar dónde estuviste no es motivo para esconder
// lo que la base contestó. Es lo mismo que hace el árbol con el catálogo de
// objetos, por la misma razón.
//
// Lo que NO se guarda es el resultado. El historial es lo que escribiste vos,
// no lo que contestó el servidor: las filas en un archivo local serían una
// copia de los datos de la base sin su control de acceso.
func (q *Queries) anotar(conn, sql string, lote *query.Batch, f *engine.Failure) {
	if q.historial == nil {
		return
	}
	e := history.Entry{ConnectionID: conn, SQL: sql}
	if lote != nil {
		e.ElapsedMs = lote.ElapsedMs
		for _, r := range lote.Results {
			e.Rows += int64(len(r.Rows))
		}
	}
	if f != nil {
		// Se anota QUE falló y no QUÉ dijo el motor.
		//
		// El mensaje de un fallo de datos lleva valores de fila adentro: un
		// `unique_violation` de Postgres se traduce a «Ya hay filas con (email)
		// = (ana@example.com) repetido». Guardarlo dejaba un pedazo de los datos
		// del servidor en un archivo de texto de esta máquina, sin su control de
		// acceso. El detalle del fallo ya está en la pestaña Mensajes del
		// editor, que es donde hace falta.
		e.Failed = true
	}
	_, _ = q.historial.Add(e)
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
		Where:      req.Where,
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
func (q *Queries) TableCount(
	ctx context.Context, runID, schema, table string, where []query.Condition,
) CountResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return CountResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: err.Error(),
		}}
	}

	ctx, listo := q.registrar(ctx, runID)
	defer listo()

	n, f := sesion.db.Count(ctx, schema, table, where)
	if f != nil {
		return CountResult{Failure: q.porElTunel(f)}
	}
	return CountResult{OK: true, Count: n}
}

// Operators son los operadores de filtro que la interfaz puede ofrecer, con su
// nombre y cuántos valores pide cada uno.
//
// Sale de Go y no de una lista escrita en el frontend porque «qué operadores
// hay» y «cuántos valores pide cada uno» ya están decididos acá: una segunda
// copia se desincroniza en cuanto se agregue uno, y la que se olvide va a ser
// la que alguien use.
func (q *Queries) Operators() []query.OperatorInfo {
	return append([]query.OperatorInfo(nil), query.Operators...)
}

// ArrayItems parte el valor de una celda de array en sus elementos.
//
// Despacha por motor porque el literal no es el mismo: Postgres escribe
// `{a,"b,c"}` con comillas y escapes, y una columna SET de MySQL es una lista
// separada por comas y nada más. SQLite no tiene arrays, así que devuelve que
// no lo es y el visor lo muestra como texto.
//
// Va en Go y no en el frontend porque el literal de Postgres tiene casos borde
// —comas adentro de comillas, NULL contra la palabra "NULL", escapes— y lógica
// con casos borde sin tests es lógica rota que nadie ve.
func (q *Queries) ArrayItems(valor string) ItemsResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return ItemsResult{}
	}
	var (
		items query.Items
		ok    bool
	)
	switch sesion.db.Kind() {
	case engine.Postgres:
		items, ok = query.ParsePostgresArray(valor)
	case engine.MySQL, engine.MariaDB:
		items, ok = query.ParseMySQLSet(valor)
	}
	return ItemsResult{OK: ok, Items: items}
}

// ItemsResult es el array partido, o el aviso de que no era uno.
type ItemsResult struct {
	// OK en false significa «esto no es un array en este motor»: el visor lo
	// muestra como texto en vez de inventar elementos.
	OK    bool        `json:"ok"`
	Items query.Items `json:"items"`
}

// RowJSON devuelve una fila entera como JSON.
//
// Se arma acá y no con `row_to_json` del servidor, que era lo que decía el
// plan, por tres motivos que aparecieron al escribirlo: la fila YA está leída
// —pedirla de nuevo es un viaje por nada—, `row_to_json` es de Postgres y
// habría que escribir la consulta equivalente en los otros tres, y sobre todo
// el resultado tiene que coincidir con lo que sale al exportar en JSON. Se usa
// el MISMO escritor, así que coincide por construcción y no por cuidado.
func (q *Queries) RowJSON(columns []query.Column, row []*string) (string, error) {
	// JSONL y no JSON: una fila es UN objeto, y envolverla en un array de uno
	// sería contar una fila como si fueran varias. Sale en una línea y no
	// indentada a propósito: indentar exigiría volver a parsear los números, y
	// ahí un numeric como 12.50 se convertiría en 12.5. El valor exacto vale
	// más que la sangría.
	return export.Render(export.JSONL, export.Options{}, columns, [][]*string{row}, 0)
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
	// Una ejecución ANIDADA con el mismo identificador no se registra encima de
	// la de afuera.
	//
	// Pasa de verdad: el volcado se registra una vez y después llama a `volcar`
	// por cada tabla, que se registra otra vez con el mismo runID. Registrarse
	// encima y borrar la clave al terminar dejaba el volcado ENTERO sin
	// registrar en cuanto la primera tabla terminaba, y «Cancelar» se volvía
	// silenciosamente inútil entre una tabla y la siguiente.
	//
	// No hace falta registrarla: el context de adentro deriva del de afuera, así
	// que cancelar el de afuera la corta igual. Lo único que hace este cierre es
	// liberar sus propios recursos.
	if _, anidada := q.enCurso[runID]; anidada {
		q.mu.Unlock()
		return ctx, cancel
	}
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
