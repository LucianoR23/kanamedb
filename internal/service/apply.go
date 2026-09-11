package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// ErrReadOnly lo devuelve cualquier escritura sobre una conexión de solo lectura.
var ErrReadOnly = errors.New("la conexión es de solo lectura")

// ErrNeedsConfirmation lo devuelve Apply contra producción sin confirmación.
var ErrNeedsConfirmation = errors.New("hace falta confirmar el nombre de la conexión")

// ChangeView es un cambio pendiente con su sentencia ya escrita.
//
// La SQL viaja resuelta y no se arma en el frontend: citar identificadores es
// específico del motor y la interfaz no tiene forma de saber cómo. Ver CLAUDE.md.
type ChangeView struct {
	Change    change.Change    `json:"change"`
	Statement change.Statement `json:"statement"`

	// RowEstimate es cuántas filas tiene la tabla que toca, cuando la sentencia
	// las va a leer o reescribir todas. Es -1 cuando no aplica o no se sabe.
	//
	// Es la diferencia entre «bloquea la tabla» y «bloquea la tabla y son doce
	// mil filas»: lo primero no ayuda a decidir, lo segundo sí.
	RowEstimate int64 `json:"rowEstimate"`

	// Error explica por qué esta operación no se puede escribir, si es el caso.
	// Nunca debería aparecer —la interfaz solo ofrece lo que se sabe renderizar—
	// pero se muestra en vez de esconderse: un cambio que no se puede aplicar y
	// no lo dice es peor que uno que falla.
	Error string `json:"error,omitempty"`
}

// ChangesetView es el changeset completo, listo para la pantalla de pendientes.
type ChangesetView struct {
	Changes []ChangeView   `json:"changes"`
	Summary change.Summary `json:"summary"`
	Tables  []string       `json:"tables"`

	// Order es la secuencia de ejecución, que NO es el orden de edición.
	Order []ChangeView `json:"order"`

	// Warnings son avisos sobre el conjunto, no sobre una sentencia suelta.
	Warnings []string `json:"warnings,omitempty"`

	// NeedsConfirmation avisa que la conexión es de producción y Apply va a
	// pedir el nombre de la base escrito a mano.
	NeedsConfirmation bool `json:"needsConfirmation"`

	// ConfirmWord es exactamente lo que hay que escribir. Viaja para que la
	// pantalla no tenga que deducirlo: si lo dedujera mal, el botón quedaría
	// bloqueado sin explicación.
	ConfirmWord string `json:"confirmWord,omitempty"`

	// Script es todo el changeset como una sola pieza de SQL, numerada y con
	// sus avisos. Es lo que se muestra, se copia y se guarda como .sql.
	Script string `json:"script"`

	// ReadOnly avisa que no se va a poder aplicar nada.
	ReadOnly bool `json:"readOnly"`

	// Engine es el motor de la conexión. La pantalla lo nombra en los avisos:
	// «MariaDB no puede revertir esto» se entiende y «el motor» no.
	Engine engine.Kind `json:"engine"`

	// TransactionalDDL dice si la casilla «Una sola transacción» puede cumplir
	// lo que promete.
	//
	// Es el campo por el que existe todo esto. La casilla decía «todo o nada»
	// SIEMPRE, y contra MySQL y MariaDB eso es falso: un DDL en el medio de una
	// transacción commitea todo lo anterior y el ROLLBACK final no revierte
	// nada. Una interfaz que promete lo que la base no cumple es peor que una
	// que no promete nada.
	TransactionalDDL bool `json:"transactionalDdl"`

	// Tramos es en cuántos pedazos se va a partir el apply CON la casilla
	// puesta. Uno significa que el «todo o nada» es real.
	Tramos int `json:"tramos"`

	// RebuildsTables dice que alguna sentencia no modifica la tabla sino que la
	// reconstruye entera: crea una nueva, copia las filas, tira la vieja y
	// renombra. Es SQLite, y cambia el costo de «solo metadatos» al tamaño de
	// la tabla.
	RebuildsTables bool `json:"rebuildsTables"`

	// CanDryRun dice si el botón de ensayo tiene sentido en esta conexión.
	//
	// Exige DDL transaccional, y no por prolijidad: sin él, «correr todo y
	// revertir» no revierte nada y el ensayo sería el apply. Es el mismo límite
	// que parte el apply en tramos, mirado desde el otro lado.
	CanDryRun bool `json:"canDryRun"`
}

// soloLecturaBool es soloLectura cuando solo interesa el sí o el no.
func soloLecturaBool(sesion *openSession) bool {
	ro, _ := soloLectura(sesion)
	return ro
}

// ApplyState es por dónde va un apply.
type ApplyState string

const (
	ApplyIdle    ApplyState = "idle"
	ApplyRunning ApplyState = "running"
)

// ApplyProgress es lo que la pantalla consulta mientras el apply corre.
//
// Se consulta en vez de recibirse: este proyecto no usa eventos de Wails y un
// apply que puede tardar minutos no puede ser una espera sin información. El
// costo de preguntar cada doscientos milisegundos es una llamada a una función
// que lee tres campos bajo un lock.
type ApplyProgress struct {
	State ApplyState `json:"state"`

	Total     int `json:"total"`
	Completed int `json:"completed"`

	// Current es la sentencia que está corriendo ahora, y lo que hace falta
	// saber para entender por qué tarda.
	Current     string        `json:"current,omitempty"`
	Impact      change.Impact `json:"impact,omitempty"`
	Destructive bool          `json:"destructive,omitempty"`

	ElapsedMs int64 `json:"elapsedMs"`
}

func (s *Session) iniciarApply(sts []change.Statement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progreso = ApplyProgress{State: ApplyRunning, Total: len(sts)}
	s.progresoDesde = time.Now()
}

func (s *Session) avanzarApply(i int, st change.Statement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progreso.Completed = i
	s.progreso.Current = st.SQL
	s.progreso.Impact = st.Impact
	s.progreso.Destructive = st.Destructive
}

func (s *Session) terminarApply() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progreso = ApplyProgress{State: ApplyIdle}
}

// ApplyStatus dice por dónde va el apply en curso, si hay uno.
func (s *Session) ApplyStatus() ApplyProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.progreso
	if p.State == ApplyRunning {
		p.ElapsedMs = time.Since(s.progresoDesde).Milliseconds()
	}
	return p
}

// Stage agrega un cambio al changeset de la sesión y devuelve cómo quedó escrito.
//
// Devolver la sentencia en el acto no es un lujo: es lo que permite que quien
// edita vea la SQL apenas hace el cambio, en vez de descubrirla al final.
func (s *Session) Stage(ctx context.Context, c change.Change, confirm string) (ChangeView, error) {
	vs, err := s.StageMany(ctx, []change.Change{c}, confirm)
	if err != nil {
		return ChangeView{}, err
	}
	return vs[0], nil
}

// StageMany agrega varios cambios de una vez, todos o ninguno.
//
// Es lo que manda la grilla: una sesión de edición son diez filas tocadas y
// tres borradas, y prepararlas de a una obligaría a contestar diez veces la
// confirmación de producción o a dejar la mitad adentro si la sexta no se
// sabe escribir. Acá se comprueba todo primero y se guarda después.
func (s *Session) StageMany(ctx context.Context, cs []change.Change, confirm string) ([]ChangeView, error) {
	sesion, err := s.abierta()
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, errors.New("no hay ningún cambio que preparar")
	}
	// Contra producción, un cambio destructivo no entra al changeset «sin
	// querer». La confirmación va acá y no en cada pantalla que puede prepararlo:
	// una regla escrita en cinco lugares es una regla que alguna pantalla nueva
	// se va a olvidar de aplicar.
	//
	// Se pide al PREPARAR y no solo al aplicar porque son dos preguntas
	// distintas: «¿de verdad querés borrar esta columna?» se contesta mirando la
	// columna, y «¿de verdad querés correr estas ocho sentencias?» mirando la
	// lista. Contestar la segunda no contesta la primera.
	if sesion.conn.Environment.NeedsWriteConfirmation() &&
		strings.TrimSpace(confirm) != nombreDeLaBase(sesion) {
		for _, c := range cs {
			if c.Destructive() {
				return nil, fmt.Errorf(
					"%w: %s sobre %s es destructivo y la conexión es de producción; para "+
						"prepararlo hay que escribir %q",
					ErrNeedsConfirmation, c.Type, c.Target(), nombreDeLaBase(sesion))
			}
		}
	}
	// Se renderiza ANTES de guardar, y todos antes de guardar el primero. Un
	// cambio que no se sabe escribir no entra al changeset: dejarlo entrar
	// sería prometer un apply que va a fallar.
	for _, c := range cs {
		if _, err := sesion.db.RenderDDL(ctx, c); err != nil {
			return nil, err
		}
	}
	out := make([]ChangeView, 0, len(cs))
	for _, c := range cs {
		id, err := sesion.cambios.Add(c)
		if err != nil {
			// No puede pasar: Add valida lo mismo que RenderDDL acaba de
			// validar. Si pasa, lo que ya entró se saca, para que «todo o
			// nada» siga siendo verdad.
			for _, v := range out {
				sesion.cambios.Remove(v.Change.ID)
			}
			return nil, err
		}
		c.ID = id
		st, _ := sesion.db.RenderDDL(ctx, c)
		out = append(out, ChangeView{Change: c, Statement: st})
	}
	return out, nil
}

// Unstage saca un cambio del changeset.
func (s *Session) Unstage(id string) error {
	sesion, err := s.abierta()
	if err != nil {
		return err
	}
	if !sesion.cambios.Remove(id) {
		return fmt.Errorf("no hay ningún cambio pendiente con id %q", id)
	}
	return nil
}

// IncludeChange deja un cambio dentro o fuera de este apply, sin sacarlo del
// changeset.
func (s *Session) IncludeChange(id string, incluir bool) error {
	sesion, err := s.abierta()
	if err != nil {
		return err
	}
	if !sesion.cambios.SetExcluded(id, !incluir) {
		return fmt.Errorf("no hay ningún cambio pendiente con id %q", id)
	}
	return nil
}

// DiscardChanges vacía el changeset.
func (s *Session) DiscardChanges() error {
	sesion, err := s.abierta()
	if err != nil {
		return err
	}
	sesion.cambios.Clear()
	return nil
}

// Changeset devuelve todo lo pendiente con sus sentencias y sus avisos.
//
// Toma un contexto porque escribir la vista previa toca la base: en SQLite,
// renderizar un cambio de columna exige leer la definición actual de la tabla
// para poder reconstruirla.
func (s *Session) Changeset(ctx context.Context) (ChangesetView, error) {
	sesion, err := s.abierta()
	if err != nil {
		return ChangesetView{}, err
	}

	vista := ChangesetView{
		Summary:           sesion.cambios.Summarize(),
		Tables:            sesion.cambios.Tables(),
		NeedsConfirmation: sesion.conn.Environment.NeedsWriteConfirmation(),
		ReadOnly:          soloLecturaBool(sesion),
	}
	if vista.NeedsConfirmation {
		vista.ConfirmWord = nombreDeLaBase(sesion)
	}
	for _, c := range sesion.cambios.List() {
		vista.Changes = append(vista.Changes, renderizar(ctx, sesion.db, c, sesion.snapshot))
	}
	for _, c := range sesion.cambios.Ordered() {
		vista.Order = append(vista.Order, renderizar(ctx, sesion.db, c, sesion.snapshot))
	}
	// Lo que la pantalla necesita saber del motor. Se calcula acá y no en el
	// frontend porque depende de las capacidades de la conexión abierta, que el
	// frontend no tiene — y deducirlo del nombre del motor sería adivinar.
	caps := sesion.db.Caps()
	vista.Engine = sesion.db.Kind()
	vista.TransactionalDDL = caps.TransactionalDDL
	// El ensayo necesita que TODO el tramo se pueda revertir: DDL transaccional,
	// o ningún DDL incluido. Ver DryRun.
	vista.CanDryRun = (caps.TransactionalDDL || vista.Summary.Schema == 0) && !vista.ReadOnly
	for _, c := range vista.Order {
		if c.Statement.RebuildsTable {
			vista.RebuildsTables = true
		}
	}
	vista.Tramos = len(engine.TramosDe(len(vista.Order), caps,
		func(i int) bool { return vista.Order[i].Change.Kind() == change.KindData },
		func(i int) bool { return vista.Order[i].Statement.Aislada }))

	vista.Warnings = avisos(sesion, vista.Order)
	vista.Script = guion(vista.Order, true)
	return vista, nil
}

// nombreDeLaBase es contra qué se está trabajando de verdad.
func nombreDeLaBase(sesion *openSession) string {
	if sesion.server != nil && sesion.server.CurrentDB != "" {
		return sesion.server.CurrentDB
	}
	return sesion.conn.Database
}

// guion arma todo el changeset como una sola pieza de SQL.
//
// Cada sentencia va numerada y precedida por lo que cuesta, en un comentario.
// No es decoración: el guion se copia y se guarda, y termina en un ticket o en
// un repositorio, donde nadie va a tener la pantalla al lado para saber cuál
// bloqueaba la tabla.
func guion(orden []ChangeView, transaccion bool) string {
	if len(orden) == 0 {
		return ""
	}
	var b strings.Builder
	if transaccion {
		b.WriteString("BEGIN;\n\n")
	}
	for i, v := range orden {
		fmt.Fprintf(&b, "-- %d · %s", i+1, v.Change.Target())
		if nota := costo(v.Statement); nota != "" {
			b.WriteString(" · ")
			b.WriteString(nota)
		}
		b.WriteString("\n")
		if v.Statement.Note != "" {
			for _, linea := range envolver(v.Statement.Note, 76) {
				b.WriteString("--   ")
				b.WriteString(linea)
				b.WriteString("\n")
			}
		}
		b.WriteString(v.Statement.SQL)
		b.WriteString(";\n\n")
	}
	if transaccion {
		b.WriteString("COMMIT;\n")
	}
	return b.String()
}

// costo resume en pocas palabras qué le va a pasar a la tabla.
func costo(st change.Statement) string {
	partes := []string{}
	switch st.Impact {
	case change.ImpactMetadata:
		partes = append(partes, "solo metadatos")
	case change.ImpactScan:
		partes = append(partes, "lee la tabla entera")
	case change.ImpactRewrite:
		partes = append(partes, "reescribe la tabla")
	}
	switch st.Lock {
	case change.LockAll:
		partes = append(partes, "bloquea lecturas y escrituras")
	case change.LockWrites:
		partes = append(partes, "bloquea escrituras")
	}
	if st.Destructive {
		partes = append(partes, "DESTRUCTIVA")
	}
	return strings.Join(partes, ", ")
}

// envolver corta un texto en líneas para que quepa en un comentario.
func envolver(s string, ancho int) []string {
	palabras := strings.Fields(s)
	if len(palabras) == 0 {
		return nil
	}
	lineas := []string{palabras[0]}
	for _, p := range palabras[1:] {
		ultima := len(lineas) - 1
		if len(lineas[ultima])+1+len(p) <= ancho {
			lineas[ultima] += " " + p
		} else {
			lineas = append(lineas, p)
		}
	}
	return lineas
}

// renderizar arma la vista previa de un cambio.
//
// Recibe la conexión y no solo el cambio porque en SQLite escribir la sentencia
// NO es una función pura: casi todo cambio de columna se hace reconstruyendo la
// tabla, y para eso hay que leer la definición que hay. Ver engine.Conn.
func renderizar(ctx context.Context, db engine.Conn, c change.Change, snap *schema.Snapshot) ChangeView {
	st, err := db.RenderDDL(ctx, c)
	v := ChangeView{Change: c, Statement: st, RowEstimate: -1}
	if err != nil {
		v.Error = err.Error()
	}
	if st.Impact == change.ImpactScan || st.Impact == change.ImpactRewrite {
		v.RowEstimate = filasDe(snap, c.Schema, c.Table)
	}
	return v
}

// filasDe busca la estimación de filas de una tabla en el snapshot.
//
// Es la estimación del planificador, la misma que muestra el árbol: no se
// consulta la base para esto. Un conteo exacto por cada sentencia del changeset
// recorrería cada tabla entera cada vez que se abre la pantalla.
func filasDe(snap *schema.Snapshot, esquema, tabla string) int64 {
	if snap == nil {
		return -1
	}
	for _, sc := range snap.Schemas {
		if sc.Name != esquema {
			continue
		}
		for _, t := range sc.Tables {
			if t.Name == tabla {
				return t.RowEstimate
			}
		}
	}
	return -1
}

// avisos son las cosas que hay que decir del conjunto, no de una sentencia.
func avisos(sesion *openSession, orden []ChangeView) []string {
	var out []string

	caps := sesion.db.Caps()

	// El aviso que la interfaz NO daba y que cambia lo que hay que esperar de
	// un fallo. La casilla «Una sola transacción» promete todo o nada; contra
	// MySQL y MariaDB eso no se puede cumplir, y el usuario tiene que saberlo
	// ANTES de aplicar, no cuando la mitad quedó puesta.
	if !caps.TransactionalDDL {
		hayEsquema := false
		for _, v := range orden {
			if v.Change.Kind() == change.KindSchema {
				hayEsquema = true
				break
			}
		}
		if hayEsquema {
			aviso := fmt.Sprintf(
				"%s no puede revertir cambios de esquema: cada uno se confirma solo, "+
					"aunque la casilla de transacción esté puesta. Si algo falla a la "+
					"mitad, lo anterior queda aplicado.", sesion.db.Kind().Label())
			if caps.AtomicDDL {
				// La otra mitad de la verdad, que evita asustar de más: la
				// sentencia que falla no deja nada a medias.
				aviso += " La que falle no queda a medias: cada sentencia es todo o nada por su cuenta."
			}
			out = append(out, aviso)
		}
	}

	// Y el de SQLite, que es de costo y no de garantía.
	var reconstruyen int
	for _, v := range orden {
		if v.Statement.RebuildsTable {
			reconstruyen++
		}
	}
	if reconstruyen > 0 {
		out = append(out, fmt.Sprintf(
			"%d %s no modifican la tabla: la reconstruyen entera —tabla nueva, copiar las "+
				"filas, tirar la vieja y renombrar—. Tarda en proporción al tamaño de la "+
				"tabla y necesita lugar en disco para las dos copias mientras corre.",
			reconstruyen, plural(reconstruyen, "operación", "operaciones")))

		// Y lo que la reconstrucción le hace a los cambios de DATOS que van en
		// la misma transacción: corre con las claves foráneas apagadas —es la
		// única forma de reconstruir sin disparar los ON DELETE CASCADE de las
		// hijas, ver engine.TxOptions— así que un borrado de esta tanda no
		// arrastra ni pone en NULL a sus hijas. No es silencioso: el
		// foreign_key_check del cierre encuentra las huérfanas y rechaza el
		// apply entero. Pero un borrado que solo funcionaría gracias a la
		// cascada va a fallar acá y andar aplicado solo, y hay que decirlo.
		hayDatos := false
		for _, v := range orden {
			if v.Change.Kind() == change.KindData {
				hayDatos = true
				break
			}
		}
		if hayDatos {
			out = append(out, "Con «Una sola transacción» puesta, mientras corre la reconstrucción las "+
				"claves foráneas están "+
				"apagadas, así que un borrado de esta tanda no arrastra sus filas hijas "+
				"(ON DELETE CASCADE / SET NULL). Si las deja huérfanas, el apply entero se "+
				"rechaza al cerrar y no queda nada. Para borrar con cascada, aplicá primero "+
				"el esquema y después los datos.")
		}
	}

	// El statement_timeout de la conexión mata la sentencia a mitad de camino.
	// Un ALTER que reescribe una tabla grande tarda más que cualquier timeout
	// razonable para consultas, y quedarse a medias en un DDL es peor que no
	// empezar. NO se sube el timeout por dentro: eso sacaría una protección sin
	// que nadie lo pida. Se avisa, y quien aplica decide.
	if t := sesion.conn.Safety.StatementTimeout(); t > 0 {
		for _, v := range orden {
			if v.Statement.Impact == change.ImpactRewrite || v.Statement.Impact == change.ImpactScan {
				out = append(out, fmt.Sprintf(
					"Esta conexión corta las sentencias a los %s. Hay operaciones que leen o "+
						"reescriben la tabla entera y pueden tardar más: si el corte llega "+
						"primero, la sentencia se cancela.", t))
				break
			}
		}
	}

	var bloquean, destructivas int
	for _, v := range orden {
		if v.Statement.Lock == change.LockAll {
			bloquean++
		}
		if v.Statement.Destructive {
			destructivas++
		}
	}
	if bloquean > 0 {
		out = append(out, fmt.Sprintf(
			"%d %s su tabla por completo mientras %s: nadie puede leerla "+
				"ni escribirla.",
			bloquean,
			plural(bloquean, "sentencia bloquea", "sentencias bloquean"),
			plural(bloquean, "corre", "corren")))
	}
	if destructivas > 0 {
		out = append(out, fmt.Sprintf(
			"%d %s datos o garantías de forma irreversible.",
			destructivas, plural(destructivas, "cambio pierde", "cambios pierden")))
	}
	return out
}

// ApplyOptions son las decisiones que se toman en la pantalla de revisión.
type ApplyOptions struct {
	// SingleTransaction corre todo adentro de una transacción. PostgreSQL
	// soporta DDL transaccional, así que acá el «todo o nada» es real.
	SingleTransaction bool `json:"singleTransaction"`

	// Confirm es el nombre de LA BASE escrito a mano. Solo hace falta contra
	// producción, y tiene que coincidir exactamente.
	//
	// La base y no el nombre de la conexión: el de la conexión lo eligió quien
	// la configuró y puede ser «prod» en las dos máquinas, mientras que el de la
	// base es lo que de verdad se va a modificar.
	Confirm string `json:"confirm,omitempty"`
}

// StatementResult es cómo le fue a una sentencia.
type StatementResult struct {
	ChangeID string `json:"changeId"`
	SQL      string `json:"sql"`

	// Applied es true si la sentencia QUEDÓ aplicada en la base.
	//
	// No es «corrió sin error», que era lo que decía antes. La diferencia
	// aparece con los tramos: una sentencia de un tramo que después se revierte
	// corrió perfectamente y NO está en la base, y llamarla aplicada tiene una
	// consecuencia concreta — `olvidarAplicados` la sacaría del changeset, así
	// que el usuario perdería la edición sin que el cambio existiera.
	//
	// Con transacción única sobre un motor con DDL transaccional esto coincide
	// con RolledBack para todas. Con varios tramos no: los tramos que
	// commitearon quedan en true y el que falló, en false.
	Applied   bool   `json:"applied"`
	ElapsedMs int64  `json:"elapsedMs"`
	Error     string `json:"error,omitempty"`
	SQLState  string `json:"sqlState,omitempty"`
}

// ApplyResult es el resultado del apply completo.
type ApplyResult struct {
	OK        bool              `json:"ok"`
	Results   []StatementResult `json:"results"`
	ElapsedMs int64             `json:"elapsedMs"`

	// Tramos es en cuántos pedazos se partió el apply.
	//
	// Es 1 cuando el motor tiene DDL transaccional y se pidió transacción
	// única: ahí «todo o nada» es real. Contra MySQL y MariaDB son más, y la
	// pantalla tiene que decirlo — porque si falla el tercero, los dos
	// primeros quedaron aplicados y no hay forma de deshacerlos.
	Tramos int `json:"tramos"`

	// TramoFallido es cuál se cortó, empezando en 1. Cero si no falló ninguno.
	TramoFallido int `json:"tramoFallido,omitempty"`

	// RolledBack dice que se revirtió todo. Solo puede pasar con transacción
	// única, y es la diferencia entre «falló la tercera» y «no quedó nada».
	RolledBack bool `json:"rolledBack"`

	// SchemaChanged dice que algún cambio de ESQUEMA quedó aplicado, así que
	// el árbol y el diagrama hay que releerlos. Un apply de puros datos lo
	// deja en false: la grilla se recarga, el catálogo no se vuelve a
	// inspeccionar.
	SchemaChanged bool `json:"schemaChanged"`

	// Failure explica el fallo en el vocabulario del usuario.
	Failure *engine.Failure `json:"failure,omitempty"`
}

// Apply ejecuta los cambios incluidos.
//
// Bloquea hasta terminar. Mientras tanto, ApplyStatus devuelve por dónde va: no
// hay eventos en este proyecto y un apply que puede tardar minutos no puede ser
// una espera sin información.
func (s *Session) Apply(ctx context.Context, opts ApplyOptions) (ApplyResult, error) {
	sesion, err := s.abierta()
	if err != nil {
		return ApplyResult{}, err
	}
	if ro, motivo := soloLectura(sesion); ro {
		return ApplyResult{}, fmt.Errorf("%w: %s", ErrReadOnly, motivo)
	}
	// La confirmación se verifica ACÁ y no en el frontend. Una comprobación que
	// vive solo del lado de la interfaz no es una protección: es un cartel.
	if sesion.conn.Environment.NeedsWriteConfirmation() &&
		strings.TrimSpace(opts.Confirm) != nombreDeLaBase(sesion) {
		return ApplyResult{}, fmt.Errorf(
			"%w: esta conexión es de producción; para aplicar hay que escribir %q",
			ErrNeedsConfirmation, nombreDeLaBase(sesion))
	}

	pendientes, sentencias, err := s.preparar(ctx, sesion)
	if err != nil {
		return ApplyResult{}, err
	}

	s.iniciarApply(sentencias)
	defer s.terminarApply()

	inicio := time.Now()
	res := ApplyResult{Results: make([]StatementResult, 0, len(sentencias))}

	res = s.aplicarPorTramos(ctx, sesion, pendientes, sentencias, opts.SingleTransaction)
	res.ElapsedMs = time.Since(inicio).Milliseconds()

	// Lo que se aplicó deja de estar pendiente. Lo que no —porque falló, o
	// porque se revirtió— se queda: perder el changeset después de un error
	// obligaría a rehacer todas las ediciones.
	//
	// Se mira sentencia por sentencia y no «salió todo bien o no salió nada».
	// Sin transacción, un apply que falla en la tercera deja las dos primeras
	// aplicadas de verdad, y dejarlas en la lista es peor que un detalle
	// cosmético: al reintentar se vuelven a correr, y una columna que ya existe
	// hace fallar el apply entero por algo que ya estaba hecho.
	_, esquema := s.olvidarAplicados(sesion, res)
	res.SchemaChanged = esquema
	if esquema {
		// El esquema cambió, así que el snapshot que tiene el árbol quedó
		// viejo. Solo entonces: editar una celda no cambia el árbol, y volver a
		// leer el catálogo entero por eso sería justo lo que CLAUDE.md prohíbe.
		s.mu.Lock()
		if s.current == sesion {
			s.current.snapshot = nil
		}
		s.mu.Unlock()
	}
	return res, nil
}

// preparar ordena los cambios y escribe la SQL de todos, o no devuelve ninguna.
//
// Es el arranque común de Apply y DryRun. Que el ensayo pase por acá no es
// prolijidad: si escribiera la SQL por otro camino, estaría ensayando una SQL
// distinta de la que después se aplica.
func (s *Session) preparar(
	ctx context.Context, sesion *openSession,
) ([]change.Change, []change.Statement, error) {
	pendientes := sesion.cambios.Ordered()
	if len(pendientes) == 0 {
		return nil, nil, errors.New("no hay cambios para aplicar")
	}
	sentencias := make([]change.Statement, 0, len(pendientes))
	for _, c := range pendientes {
		st, err := sesion.db.RenderDDL(ctx, c)
		if err != nil {
			// No se ejecuta NADA si alguna no se puede escribir. Aplicar la
			// mitad de un changeset porque la otra mitad no compila es la peor
			// combinación posible.
			return nil, nil, fmt.Errorf("no se puede aplicar: %w", err)
		}
		sentencias = append(sentencias, st)
	}
	return pendientes, sentencias, nil
}

// DryRun corre el changeset entero adentro de una transacción y la revierte.
//
// Responde la pregunta que la vista previa NO puede responder. La vista previa
// dice qué SQL se va a mandar; el ensayo dice si el motor la va a aceptar contra
// los datos que hay ahora. Son cosas distintas: `SET NOT NULL` sobre una columna
// con nulos es SQL impecable que falla, y un tipo nuevo que no admite lo que hay
// adentro también.
//
// Exige DDL transaccional y por eso no existe contra MySQL ni MariaDB. Ahí
// «correr todo y revertir» no revierte nada: cada DDL se commitea solo. Un botón
// de ensayo que aplica sería la peor pieza de interfaz que este proyecto podría
// tener, así que no está.
//
// Lo que el ensayo NO promete, y por eso el resultado no dice «va a andar»:
//
//   - Hace el MISMO trabajo que el apply, incluidas las reescrituras de tabla
//     enteras, y toma los MISMOS candados mientras corre. Ensayar no es barato:
//     contra una tabla grande sale lo mismo que aplicar, y después hay que
//     aplicar igual.
//   - Entre el ensayo y el apply la base sigue viva. Una fila insertada en el
//     medio puede romper el NOT NULL que el ensayo vio pasar.
//
// Contra producción pide la MISMA confirmación que Apply, y esto es una
// corrección: al principio no la pedía, con el argumento de que escribir el
// nombre de la base es la puerta de «esto queda» y esto no queda.
//
// El argumento era falso porque miraba la consecuencia equivocada. Lo que
// protege esa puerta no es solo la persistencia: un ensayo hace el MISMO
// trabajo que el apply y toma los MISMOS candados, así que contra una tabla
// grande de producción el corte de servicio es idéntico. Lo único que cambia es
// lo que queda escrito después. Un botón que toma un ACCESS EXCLUSIVE en
// producción con un clic es exactamente lo que CLAUDE.md prohibe cuando dice
// «confirmación extra en cualquier escritura».
func (s *Session) DryRun(ctx context.Context, confirm string) (ApplyResult, error) {
	sesion, err := s.abierta()
	if err != nil {
		return ApplyResult{}, err
	}
	// Una conexión de solo lectura rechaza la escritura del lado del SERVIDOR,
	// así que el ensayo fallaría en la primera sentencia con un error de permisos
	// que no tiene nada que ver con el changeset. Mejor decirlo antes.
	if ro, motivo := soloLectura(sesion); ro {
		return ApplyResult{}, fmt.Errorf("%w: %s", ErrReadOnly, motivo)
	}
	// Sin DDL transaccional se puede ensayar igual, SIEMPRE QUE no haya ningún
	// cambio de esquema incluido: un changeset de puras filas es un solo tramo
	// transaccional también en MySQL y MariaDB, porque el DML de InnoDB se
	// revierte. Con un DDL adentro no: cada sentencia se confirmaría sola y el
	// «ensayo» sería un apply.
	if !sesion.db.Caps().TransactionalDDL && sesion.cambios.Summarize().Schema > 0 {
		return ApplyResult{}, fmt.Errorf(
			"%s no puede ensayar un cambio de esquema: cada sentencia se confirma "+
				"sola, así que correr el changeset y revertirlo dejaría todo aplicado",
			sesion.db.Kind().Label())
	}
	// Se verifica ACÁ y no en el frontend, igual que en Apply: una comprobación
	// que vive solo del lado de la interfaz no es una protección, es un cartel.
	if sesion.conn.Environment.NeedsWriteConfirmation() &&
		strings.TrimSpace(confirm) != nombreDeLaBase(sesion) {
		return ApplyResult{}, fmt.Errorf(
			"%w: esta conexión es de producción; ensayar toma los mismos candados que "+
				"aplicar, así que también hay que escribir %q",
			ErrNeedsConfirmation, nombreDeLaBase(sesion))
	}

	_, sentencias, err := s.preparar(ctx, sesion)
	if err != nil {
		return ApplyResult{}, err
	}

	s.iniciarApply(sentencias)
	defer s.terminarApply()

	inicio := time.Now()
	// RolledBack NO se pone acá. Es la afirmación entera del botón —«no quedó
	// nada»— y darla por hecha antes de revertir la convierte en una suposición:
	// la pone `correrTramo` cuando el ROLLBACK devuelve bien.
	res := ApplyResult{
		OK:      true,
		Results: make([]StatementResult, 0, len(sentencias)),
		Tramos:  1,
	}

	// Un solo tramo con todo adentro. Se llegó acá porque el motor tiene DDL
	// transaccional o porque no hay ningún DDL: en los dos casos el «todo o
	// nada» es real y no hay nada que partir, así que no se llama a TramosDe.
	t := engine.Tramo{Desde: 0, Hasta: len(sentencias), Transaccional: true}
	if fallo, err := s.correrTramo(ctx, sesion, sentencias, t, &res, true); err != nil {
		res.OK = false
		res.TramoFallido = 1
		res.Failure = fallo
	}
	res.ElapsedMs = time.Since(inicio).Milliseconds()

	// El changeset queda intacto: no se aplicó nada, así que no hay nada que
	// sacar. Es la única diferencia con Apply.
	return res, nil
}

// olvidarAplicados saca del changeset los cambios que quedaron en la base, y
// devuelve cuántos fueron.
//
// RolledBack es la palabra clave: con transacción única, una sentencia puede
// haber corrido bien y aun así no existir más. Ahí no se saca nada.
func (s *Session) olvidarAplicados(sesion *openSession, res ApplyResult) (n int, esquema bool) {
	if res.RolledBack {
		return 0, false
	}
	tipos := map[string]change.Kind{}
	for _, c := range sesion.cambios.List() {
		tipos[c.ID] = c.Kind()
	}
	for _, r := range res.Results {
		if r.Applied && sesion.cambios.Remove(r.ChangeID) {
			n++
			if tipos[r.ChangeID] == change.KindSchema {
				esquema = true
			}
		}
	}
	return n, esquema
}

// aplicarPorTramos ejecuta las sentencias partiéndolas donde el motor obliga.
//
// Un solo BEGIN con todo adentro sirve en Postgres y en SQLite. En MySQL y
// MariaDB **no**, y no por falta de soporte sino por algo peor: un DDL en el
// medio de una transacción hace commit implícito de todo lo anterior y deja la
// conexión fuera de la transacción, así que lo que venga después también se
// commitea solo y el ROLLBACK final no revierte nada. Prometer «todo o nada» y
// aplicar todo es la peor forma de fallar. Ver engine.TramosDe y § 6 del plan.
//
// Con la casilla de transacción única apagada, cada sentencia va sola: es lo
// que el usuario pidió y no hay nada que agrupar.
func (s *Session) aplicarPorTramos(
	ctx context.Context, sesion *openSession,
	cambios []change.Change, sts []change.Statement, unaSola bool,
) ApplyResult {
	tramos := engine.TramosDe(len(sts), sesion.db.Caps(),
		func(i int) bool { return cambios[i].Kind() == change.KindData },
		func(i int) bool { return sts[i].Aislada })
	if !unaSola {
		tramos = nil
		for i := range sts {
			// Cada sentencia sola, pero las de DATOS adentro de su propia
			// transacción igual. No es agrupar —sigue siendo una por tramo—:
			// es que la comprobación de filas corre DESPUÉS de la sentencia, y
			// sin transacción un UPDATE que alcanzó dos filas ya está
			// commiteado cuando se descubre. Con la transacción, se revierte;
			// y el DML de los cuatro motores la soporta.
			tramos = append(tramos, engine.Tramo{
				Desde: i, Hasta: i + 1,
				Transaccional: cambios[i].Kind() == change.KindData,
			})
		}
	}

	res := ApplyResult{OK: true, Tramos: len(tramos)}
	for n, t := range tramos {
		fallo, err := s.correrTramo(ctx, sesion, sts, t, &res, false)
		if err == nil {
			continue
		}
		res.OK = false
		res.TramoFallido = n + 1
		res.Failure = fallo
		// Lo anterior a este tramo YA quedó aplicado y no se revierte: cada
		// tramo se commitea por su cuenta. Se corta acá porque una sentencia
		// que falla casi siempre es de la que dependían las siguientes.
		res.RolledBack = t.Transaccional && len(tramos) == 1
		return res
	}
	return res
}

// correrTramo ejecuta un tramo. Si es transaccional, todo o nada.
//
// Con ensayo en true corre todo igual pero NO commitea: en vez del COMMIT llama
// a Verify —que dispara lo que el motor deja para el cierre— y deja que el
// Rollback diferido lo revierta. Es el mismo camino que el apply de verdad, con
// una sola cosa distinta al final, y eso es a propósito: un ensayo que corre
// por otro código prueba otro código.
func (s *Session) correrTramo(
	ctx context.Context, sesion *openSession,
	sts []change.Statement, t engine.Tramo, res *ApplyResult, ensayo bool,
) (*engine.Failure, error) {
	desde := len(res.Results)

	if !t.Transaccional {
		// Un ensayo sobre un tramo sin transacción APLICARÍA de verdad. No
		// debería poder llegar acá —DryRun exige DDL transaccional— pero el
		// costo de equivocarse es escribir en producción creyendo que se estaba
		// ensayando, así que se corta y no se ejecuta nada.
		if ensayo {
			err := errors.New(
				"un ensayo necesita una transacción y este tramo no la tiene")
			return &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}, err
		}
		for i := t.Desde; i < t.Hasta; i++ {
			r, err := s.correr(ctx, sesion, sesion.db, i, sts[i])
			res.Results = append(res.Results, r)
			if err != nil {
				return clasificar(sesion, err), err
			}
		}
		return nil, nil
	}

	// Si adentro va una reconstrucción de tabla hay que decirlo ANTES del
	// BEGIN: SQLite tiene que apagar las claves foráneas, y ese pragma no hace
	// nada adentro de una transacción. Sin esto, reconstruir una tabla borra en
	// silencio las filas de las tablas que la referencian.
	var opts engine.TxOptions
	for i := t.Desde; i < t.Hasta; i++ {
		if sts[i].RebuildsTable {
			opts.RebuildsTables = true
		}
	}

	tx, err := sesion.db.Begin(ctx, opts)
	if err != nil {
		return clasificar(sesion, err), err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i := t.Desde; i < t.Hasta; i++ {
		r, err := s.correr(ctx, sesion, tx, i, sts[i])
		res.Results = append(res.Results, r)
		if err != nil {
			marcarRevertidas(res.Results[desde:])
			// En un apply, RolledBack lo pone aplicarPorTramos mirando cuántos
			// tramos hubo. El ensayo no pasa por ahí —arma su tramo único— así
			// que revierte y lo anota acá: un ensayo que falla tampoco deja nada,
			// y eso hay que comprobarlo, no suponerlo.
			if ensayo {
				revertirEnsayo(ctx, tx, res)
			}
			return clasificar(sesion, err), err
		}
	}
	// El cierre también puede fallar, y con razones propias: las claves
	// DEFERRABLE de Postgres se comprueban recién acá, y en SQLite acá corre el
	// foreign_key_check que cierra la reconstrucción.
	//
	// El ensayo tiene que ver esos mismos errores sin quedarse con el cambio, y
	// para eso está Verify: los adelanta y deja la transacción abierta para que
	// el Rollback diferido la tire.
	cerrar := tx.Commit
	if ensayo {
		cerrar = tx.Verify
	}
	if err := cerrar(ctx); err != nil {
		marcarRevertidas(res.Results[desde:])
		if ensayo {
			revertirEnsayo(ctx, tx, res)
		}
		return clasificar(sesion, err), err
	}
	if ensayo {
		// Nada de esto queda: el ensayo termina donde el apply recién
		// empezaría a estar aplicado.
		marcarRevertidas(res.Results[desde:])
		// Y se revierte ACÁ, mirando el error, en vez de dejarlo al Rollback
		// diferido —que lo descarta—. Es la única afirmación que el botón
		// hace, así que no puede apoyarse en un error que nadie lee.
		if !revertirEnsayo(ctx, tx, res) {
			err := errors.New("el ensayo corrió pero no se pudo revertir la transacción")
			return clasificar(sesion, err), err
		}
	}
	return nil, nil
}

// revertirEnsayo revierte la transacción del ensayo y anota si de verdad se
// revirtió.
//
// Un ROLLBACK que falla casi siempre significa que la conexión se murió —y
// entonces el motor aborta la transacción igual—, así que esto no es que los
// cambios hayan quedado. Es que ya no se puede AFIRMAR que no quedaron, y esa
// afirmación es todo lo que el botón vende.
//
// El Rollback diferido de arriba corre igual después; las tres
// implementaciones de Tx tratan el segundo Rollback como un no-op.
func revertirEnsayo(ctx context.Context, tx engine.Tx, res *ApplyResult) bool {
	res.RolledBack = tx.Rollback(ctx) == nil
	return res.RolledBack
}

// marcarRevertidas apaga el Applied de las sentencias de un tramo que se
// revirtió.
//
// Sin esto, una sentencia que corrió bien y después volvió atrás quedaba
// marcada como aplicada, y `olvidarAplicados` la sacaba del changeset: el
// usuario perdía la edición sin que el cambio estuviera en la base.
func marcarRevertidas(rs []StatementResult) {
	for i := range rs {
		rs[i].Applied = false
	}
}

// ejecutor es lo mínimo que Apply necesita para correr una sentencia. Lo
// cumplen tanto la conexión como una transacción, que es lo que permite que el
// camino con y sin transacción compartan el mismo código.
type ejecutor interface {
	Exec(ctx context.Context, sql string) error
	Modify(ctx context.Context, sql string, args []any) (int64, error)
}

// ErrRowCount es que una sentencia de datos no tocó las filas que tenía que
// tocar. Se envuelve con el detalle de cuántas fueron.
var ErrRowCount = errors.New("la sentencia no tocó exactamente la fila que tenía que tocar")

// ejecutar corre UNA sentencia por el camino que le corresponde: un DDL tal
// cual está escrito, y un cambio de datos con sus valores como parámetros
// —nunca por la SQL legible, que es para leer—.
//
// Para los datos, además, comprueba que la sentencia haya tocado las filas
// que Bound dice. Es la red que hace que editar una celda sea seguro contra
// una base que otro también está tocando: si la fila ya no está, se ve acá y
// revierte, en vez de terminar en «aplicado» sobre nada. Y si una clave que
// se creía única alcanzó dos filas, la segunda no se pierde en silencio.
func ejecutar(ctx context.Context, ej ejecutor, st change.Statement) error {
	// Los pasos se mandan de a uno: hay motores donde dos sentencias en la
	// misma cadena son un error de sintaxis. Ver Statement.Steps.
	if len(st.Steps) > 0 {
		for _, paso := range st.Steps {
			if err := ej.Exec(ctx, paso); err != nil {
				return err
			}
		}
		return nil
	}
	if st.Bound == nil {
		return ej.Exec(ctx, st.SQL)
	}
	n, err := ej.Modify(ctx, st.Bound.SQL, st.Bound.Args)
	if err != nil {
		return err
	}
	if st.Bound.Rows > 0 && n != st.Bound.Rows {
		switch {
		case n == 0 && st.Bound.Op == change.OpInsert:
			// Un INSERT que no inserta existe: un trigger BEFORE que devuelve
			// NULL, o una regla DO INSTEAD NOTHING. La fila no «se fue»:
			// nunca entró.
			return fmt.Errorf("%w: El INSERT no insertó ninguna fila. Un trigger o una regla "+
				"de la tabla la descartó sin dar error.", ErrRowCount)
		case n == 0:
			return fmt.Errorf("%w: No alcanzó ninguna fila. La fila ya no está en la base "+
				"—otra sesión la borró o le cambió la clave desde que se leyó—, o un trigger "+
				"BEFORE la descartó sin dar error.", ErrRowCount)
		default:
			return fmt.Errorf("%w: Alcanzó %d filas y tenía que alcanzar %d: la clave con la "+
				"que se identificó la fila no es única en la base.", ErrRowCount, n, st.Bound.Rows)
		}
	}
	return nil
}

// correr ejecuta una sentencia y devuelve cómo le fue MÁS el error crudo.
//
// Los dos, y no solo el resultado: el error de pgx lleva adentro un
// *pgconn.PgError con el SQLSTATE y los nombres del objeto que falló, y eso es
// lo único que permite decir «la columna apodo tiene nulos» en vez de «falló».
// Antes se guardaba únicamente el texto y después se reconstruía con
// errors.New, que tira el tipo: el clasificador nunca veía un SQLSTATE y por
// eso todo terminaba en el cajón de «no se pudo conectar».
func (s *Session) correr(
	ctx context.Context, sesion *openSession, ej ejecutor, i int, st change.Statement,
) (StatementResult, error) {
	s.avanzarApply(i, st)

	inicio := time.Now()
	err := ejecutar(ctx, ej, st)
	r := StatementResult{
		ChangeID:  st.ChangeID,
		SQL:       st.SQL,
		ElapsedMs: time.Since(inicio).Milliseconds(),
		Applied:   err == nil,
	}
	if err != nil {
		r.Error = engine.Redact(err.Error())
		// El código sale del clasificador del motor y no de un tipo de pgx:
		// cada uno tiene el suyo —SQLSTATE en Postgres y MySQL, código
		// extendido en SQLite— y los tres viajan por el mismo campo.
		if f := clasificar(sesion, err); f != nil {
			r.SQLState = f.SQLState
		}
	}
	return r, err
}

// plural elige la forma según el número. Un aviso que dice «1 operaciones» se
// lee como un error de programa, y hace dudar del resto del mensaje.
func plural(n int, uno, varios string) string {
	if n == 1 {
		return uno
	}
	return varios
}

// clasificar interpreta el error de una sentencia.
//
// Los errores que pone el propio servicio se interpretan acá, ANTES de pasar
// por el clasificador del motor: ese solo entiende errores del driver, y a
// cualquier otro lo manda al cajón de «no se pudo conectar». Ya pasó una vez
// —ver § 6 del plan, iteración 5— y es exactamente lo que le pasaría a
// ErrRowCount.
func clasificar(sesion *openSession, err error) *engine.Failure {
	if errors.Is(err, ErrRowCount) {
		// El hint no dice «nada quedó aplicado»: eso lo sabe ApplyResult
		// —RolledBack y el tramo que falló— y no este error. Con varios
		// tramos, lo anterior a este sí quedó.
		return &engine.Failure{
			Kind: engine.FailureData,
			// Sin el centinela adelante: la persona lee la explicación, no el
			// nombre del error.
			Message: strings.TrimPrefix(err.Error(), ErrRowCount.Error()+": "),
			Hint:    "Volvé a leer la tabla y hacé la edición de nuevo sobre lo que hay ahora.",
		}
	}
	return sesion.db.ClassifyStatement(err, sesion.conn.Describe())
}
