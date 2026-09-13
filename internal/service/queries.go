package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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
// Es una FUNCIÓN del paquete y no un método a propósito: Wails expone como
// binding todo método exportado de un servicio, y `UsarHistorial(null)` desde
// el webview apagaba el historial (K-14 de la auditoría del 2026-09-11). Una
// función del paquete no se bindea. Va aparte del constructor porque los tests
// que arman Queries sin historial son la mayoría.
func UsarHistorial(q *Queries, h *history.Store) { q.historial = h }

func NewQueries(s *Session) *Queries {
	q := &Queries{session: s, enCurso: map[string]context.CancelFunc{}}
	// Una consulta o un volcado largos no son inactividad: la sesión pregunta
	// acá antes de cerrarse sola. Ver inactividad.go.
	s.vigilarActividad(func() bool { return q.running() > 0 })
	return q
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

	// Transaction es el estado de la pestaña después de correr, cuando la
	// pestaña tiene auto-commit sacado. Nil en el modo normal. Ver
	// transacciones.go.
	Transaction *TransactionState `json:"transaction,omitempty"`
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

// Run ejecuta la SQL que escribió el usuario, en el modo normal: cada
// sentencia por su cuenta y en autocommit.
func (q *Queries) Run(ctx context.Context, runID, sql string) RunResult {
	return q.RunIn(ctx, "", runID, sql)
}

// RunIn ejecuta la SQL de una pestaña. Con tabID, y si la pestaña tiene
// auto-commit sacado, corre en su conexión dedicada; si no, es Run.
func (q *Queries) RunIn(ctx context.Context, tabID, runID, sql string) RunResult {
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

	// Lo que se rechaza se mira sobre el lote ENTERO antes de correr la
	// primera: si la sentencia prohibida es la tercera, las dos anteriores
	// tampoco corren. Igual que preparar no ejecuta nada si una sentencia no
	// se sabe escribir.
	manual := sesion.pestanaManual(tabID)
	for i, st := range sentencias {
		var f *engine.Failure
		// Con auto-commit sacado, BEGIN/COMMIT/ROLLBACK son justamente lo que
		// la pestaña puede cumplir; las demás protecciones valen igual. `SET
		// autocommit` se rechaza en los dos modos: en el manual haría por
		// debajo lo que la conexión dedicada ya hace, sin que el seguimiento
		// del estado lo vea, y la conexión podría volver al pool con una
		// transacción implícita abierta (C-01 otra vez).
		if manual {
			f = rechazarSetAutocommit(st.SQL, sesion.db.Dialect())
		} else {
			f = controlDeTransaccion(st.SQL, sesion.db.Dialect())
		}
		if f == nil && q.session.soloDatos {
			f = esquemaEnElTelefono(st.SQL, sesion.db.Dialect())
		}
		if f == nil && sesion.conn.Safety.BlockDropTruncate {
			f = bloqueadaPorPolitica(st.SQL, sesion.db.Dialect())
		}
		if f == nil && sesion.conn.Safety.ReadOnly {
			f = revierteSoloLectura(st.SQL, sesion.db.Dialect())
		}
		if f != nil {
			f.Statement = i + 1
			f.Line = st.Line
			f.TotalStatements = len(sentencias)
			return RunResult{Failure: f}
		}
	}
	if res := q.correrEnPestana(ctx, sesion, tabID, sql, sentencias); res != nil {
		return *res
	}
	// En modo normal el estado también viaja cuando hay pestaña: si la
	// conexión cambió por debajo —reconexión, otra base— la pestaña se entera
	// con la próxima ejecución en vez de seguir mostrando lo de antes.
	var estadoDePestana *TransactionState
	if tabID != "" {
		estado := q.estadoDe(sesion, nil)
		estadoDePestana = &estado
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
			return RunResult{Batch: lote, Failure: q.porElTunel(f), Transaction: estadoDePestana}
		}
	}
	lote.ElapsedMs = time.Since(inicio).Milliseconds()
	q.anotar(sesion.conn.ID, sql, lote, nil)
	return RunResult{OK: true, Batch: lote, Transaction: estadoDePestana}
}

// setAutocommit reconoce `SET [SESSION|GLOBAL|@@…] autocommit = …` de MySQL,
// que deja la conexión fuera de autocommit igual que un BEGIN.
var setAutocommit = regexp.MustCompile(`(?i)^\s*set\s+(session\s+|global\s+|local\s+|@@(session|global|local)\.|@@)?autocommit\b`)

// controlDeTransaccion rechaza BEGIN, COMMIT y compañía escritos en el editor.
//
// No es una limitación cosmética: el editor manda cada sentencia por su
// cuenta y cada una toma su propia conexión del pool. Contra Postgres,
// pgxpool DESTRUYE la conexión que vuelve en transacción, así que `BEGIN;
// UPDATE …; ROLLBACK;` corría el UPDATE en otra conexión, en autocommit, y el
// ROLLBACK respondía bien sobre una tercera: la persona creía haber
// revertido y el UPDATE estaba confirmado (K-03 de la auditoría del
// 2026-09-11, comprobado). Contra MySQL y SQLite «funcionaba» por accidente
// —database/sql devuelve la última conexión liberada— y un BEGIN sin cerrar
// dejaba la conexión en transacción adentro del pool: lock del archivo en
// SQLite, commit implícito de lo pendiente en el próximo Apply de MySQL
// (C-01). Un `SET autocommit = 0` hace lo mismo por otro camino.
//
// Hasta que exista el control manual por pestaña —con una conexión dedicada
// que sobreviva entre ejecuciones— el editor corre en autocommit, y decirlo
// antes de correr nada es mejor que hacer lo contrario de lo pedido.
func controlDeTransaccion(sql string, d query.Dialect) *engine.Failure {
	cmd := query.Command(sql, d)
	switch cmd {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "END":
	case "START":
		// Solo START TRANSACTION: `START REPLICA` y `START GROUP_REPLICATION`
		// de MySQL no tienen nada que ver.
		if !startTransaction.MatchString(query.Trim(sql, d)) {
			return nil
		}
		cmd = "START TRANSACTION"
	case "SET":
		if !setAutocommit.MatchString(query.Trim(sql, d)) {
			return nil
		}
		cmd = "SET autocommit"
	default:
		return nil
	}
	return &engine.Failure{
		Kind: engine.FailureOther,
		Message: fmt.Sprintf(
			"El editor corre en autocommit: cada sentencia se confirma sola, y %s no "+
				"tendría efecto sobre las siguientes.", cmd),
		Detail: "Cada sentencia del lote toma su propia conexión del pool. Un BEGIN acá " +
			"no abre una transacción para el UPDATE que viene después: el UPDATE se " +
			"confirmaría igual y el ROLLBACK respondería bien sin revertir nada.",
		Hint: "No se ejecutó ninguna sentencia del lote. Para revertir, editá desde la " +
			"grilla o el changeset, que sí van en transacción. El control manual de " +
			"transacciones en el editor está agendado.",
	}
}

// rechazarSetAutocommit es la parte de controlDeTransaccion que vale en los
// dos modos.
func rechazarSetAutocommit(sql string, d query.Dialect) *engine.Failure {
	if query.Command(sql, d) != "SET" || !setAutocommit.MatchString(query.Trim(sql, d)) {
		return nil
	}
	return &engine.Failure{
		Kind: engine.FailureOther,
		Message: "SET autocommit no se ejecuta desde el editor: la casilla «Auto-commit» de la " +
			"pestaña hace eso, y de una forma que la pestaña puede seguir.",
		Hint: "No se ejecutó ninguna sentencia del lote.",
	}
}

// startTransaction distingue `START TRANSACTION` de los otros START de MySQL.
var startTransaction = regexp.MustCompile(`(?i)^start\s+transaction\b`)

// apagaSoloLectura reconoce las sentencias que revierten el modo solo lectura
// de la sesión, en los tres motores de servidor y en SQLite:
//
//	SET [SESSION|LOCAL] default_transaction_read_only = off      Postgres
//	SET [SESSION|LOCAL] transaction_read_only = off              Postgres
//	SET [SESSION CHARACTERISTICS AS] TRANSACTION … READ WRITE    Postgres, MySQL
//	SET [SESSION|GLOBAL|@@…] transaction_read_only / tx_read_only  MySQL
//	RESET default_transaction_read_only / RESET ALL              Postgres
//	PRAGMA query_only = 0                                        SQLite
var apagaSoloLectura = regexp.MustCompile(
	`(?is)^\s*(` +
		`set\s+(session\s+|local\s+|global\s+|@@(session|global|local)\.|@@)?(default_)?(transaction_read_only|tx_read_only)\b` +
		`|set\s+(session\s+characteristics\s+as\s+|session\s+|global\s+)?transaction\b.*\bread\s+write\b` +
		`|reset\s+(default_transaction_read_only|transaction_read_only|all)\b` +
		`|pragma\s+query_only\b` +
		`)`)

// revierteSoloLectura rechaza, en una conexión marcada solo lectura, lo que
// la apaga desde el editor.
//
// El modo se pone como parámetro de sesión —`default_transaction_read_only`
// en Postgres, `SET SESSION TRANSACTION READ ONLY` en MySQL, `PRAGMA
// query_only` en SQLite— y un `SET … = off` escrito en el editor lo revertía
// en la conexión que tomó del pool; la sentencia siguiente, por el orden LIFO
// del pool, volvía a tomar esa misma y escribía (K-06 de la auditoría del
// 2026-09-11, comprobado contra Postgres). Quien escribe eso es la persona,
// no un atacante; pero la casilla se presenta como «bloquea toda escritura»
// y el SessionSQL de la pestaña Advanced ya se protegía de esto. Es la misma
// invariante en el otro camino.
func revierteSoloLectura(sql string, d query.Dialect) *engine.Failure {
	if !apagaSoloLectura.MatchString(query.Trim(sql, d)) {
		return nil
	}
	return &engine.Failure{
		Kind:    engine.FailurePermission,
		Message: "Esta conexión está en solo lectura, y esta sentencia lo apagaría.",
		Hint: "No se ejecutó ninguna sentencia del lote. El interruptor está en el gestor " +
			"de conexiones, en «Abrir en solo lectura».",
	}
}

// dropDentroDeAlter encuentra cada DROP adentro de un ALTER con la palabra
// que le sigue. Un ALTER tira algo con `DROP COLUMN c`, `DROP c` (Postgres y
// MySQL aceptan el atajo), `DROP IF EXISTS c`, `DROP CONSTRAINT`, `DROP INDEX`,
// `DROP PARTITION`… Lo que NO tira nada es `DROP NOT NULL`, `DROP DEFAULT`,
// `DROP IDENTITY` y `DROP EXPRESSION`, que quitan una propiedad de la columna
// y en el changeset son OpAlter, no OpDrop. Es lo que `preparar` bloquea del
// lado del changeset, y las dos puertas tienen que decir lo mismo.
var dropDentroDeAlter = regexp.MustCompile(`(?i)\bdrop\s+(\w+)`)

// alterQueTira dice si un ALTER tiene un DROP que borra algo.
func alterQueTira(sql string) bool {
	for _, m := range dropDentroDeAlter.FindAllStringSubmatch(sql, -1) {
		switch strings.ToLower(m[1]) {
		case "not", "default", "identity", "expression":
			continue
		}
		return true
	}
	return false
}

// bloqueadaPorPolitica dice si «Bloquear DROP y TRUNCATE» rechaza esta
// sentencia. Mira el comando y no el texto: un `SELECT 'drop'` pasa, y un
// `/* … */ DROP` no. Un `ALTER TABLE … DROP COLUMN` también se bloquea, y la
// búsqueda es sobre el texto: un `ALTER` con la palabra adentro de una cadena
// se bloquea de más, que es el lado tolerable del error. Lo que NO ve es un
// DROP adentro del cuerpo de un `DO $$…$$` o de una rutina; la ayuda de la
// casilla lo dice.
func bloqueadaPorPolitica(sql string, d query.Dialect) *engine.Failure {
	cmd := query.Command(sql, d)
	switch {
	case cmd == "DROP", cmd == "TRUNCATE":
	case cmd == "ALTER" && alterQueTira(sql):
		cmd = "ALTER … DROP"
	default:
		return nil
	}
	return &engine.Failure{
		Kind: engine.FailurePermission,
		Message: fmt.Sprintf(
			"«Bloquear DROP y TRUNCATE» está puesta en esta conexión y esta sentencia es un %s.",
			cmd),
		Hint: "No se ejecutó ninguna sentencia del lote. Para correrla, apagá la casilla " +
			"en la pestaña Safety de la conexión.",
	}
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

// running dice cuántas ejecuciones hay en curso. Para los tests y para la
// desconexión por inactividad. No se exporta: un método exportado del servicio
// es un binding (K-14).
func (q *Queries) running() int {
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
		// Terminar es actividad para la desconexión por inactividad: ver
		// Session.tocarActual.
		q.session.tocarActual()
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
	if f == nil || !q.session.tunnelDown() {
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
