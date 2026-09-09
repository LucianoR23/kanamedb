package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/postgres"
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
func (s *Session) Stage(c change.Change, confirm string) (ChangeView, error) {
	sesion, err := s.abierta()
	if err != nil {
		return ChangeView{}, err
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
	if c.Destructive() && sesion.conn.Environment.NeedsWriteConfirmation() &&
		strings.TrimSpace(confirm) != nombreDeLaBase(sesion) {
		return ChangeView{}, fmt.Errorf(
			"%w: %s sobre %s es destructivo y la conexión es de producción; para prepararlo hay "+
				"que escribir %q",
			ErrNeedsConfirmation, c.Type, c.Target(), nombreDeLaBase(sesion))
	}
	// Se renderiza ANTES de guardar. Un cambio que no se sabe escribir no entra
	// al changeset: dejarlo entrar sería prometer un apply que va a fallar.
	if _, err := postgres.RenderDDL(c); err != nil {
		return ChangeView{}, err
	}
	id, err := sesion.cambios.Add(c)
	if err != nil {
		return ChangeView{}, err
	}
	c.ID = id
	st, _ := postgres.RenderDDL(c)
	return ChangeView{Change: c, Statement: st}, nil
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
func (s *Session) Changeset() (ChangesetView, error) {
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
		vista.Changes = append(vista.Changes, renderizar(c, sesion.snapshot))
	}
	for _, c := range sesion.cambios.Ordered() {
		vista.Order = append(vista.Order, renderizar(c, sesion.snapshot))
	}
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

func renderizar(c change.Change, snap *schema.Snapshot) ChangeView {
	st, err := postgres.RenderDDL(c)
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
			"%d sentencias bloquean su tabla por completo mientras corren: nadie puede leerla "+
				"ni escribirla.", bloquean))
	}
	if destructivas > 0 {
		out = append(out, fmt.Sprintf(
			"%d cambios pierden datos o garantías de forma irreversible.", destructivas))
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

	// Applied es true si corrió sin error. En una transacción única, una
	// sentencia aplicada puede terminar revertida: eso lo dice RolledBack.
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

	// RolledBack dice que se revirtió todo. Solo puede pasar con transacción
	// única, y es la diferencia entre «falló la tercera» y «no quedó nada».
	RolledBack bool `json:"rolledBack"`

	// Failure explica el fallo en el vocabulario del usuario.
	Failure *postgres.Failure `json:"failure,omitempty"`
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

	pendientes := sesion.cambios.Ordered()
	if len(pendientes) == 0 {
		return ApplyResult{}, errors.New("no hay cambios para aplicar")
	}

	sentencias := make([]change.Statement, 0, len(pendientes))
	for _, c := range pendientes {
		st, err := postgres.RenderDDL(c)
		if err != nil {
			// No se ejecuta NADA si alguna no se puede escribir. Aplicar la
			// mitad de un changeset porque la otra mitad no compila es la peor
			// combinación posible.
			return ApplyResult{}, fmt.Errorf("no se puede aplicar: %w", err)
		}
		sentencias = append(sentencias, st)
	}

	s.iniciarApply(sentencias)
	defer s.terminarApply()

	inicio := time.Now()
	res := ApplyResult{Results: make([]StatementResult, 0, len(sentencias))}

	if opts.SingleTransaction {
		res = s.aplicarEnTransaccion(ctx, sesion, sentencias)
	} else {
		res = s.aplicarSuelto(ctx, sesion, sentencias)
	}
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
	if n := s.olvidarAplicados(sesion, res); n > 0 {
		// El esquema cambió, así que el snapshot que tiene el árbol quedó viejo.
		s.mu.Lock()
		if s.current == sesion {
			s.current.snapshot = nil
		}
		s.mu.Unlock()
	}
	return res, nil
}

// olvidarAplicados saca del changeset los cambios que quedaron en la base, y
// devuelve cuántos fueron.
//
// RolledBack es la palabra clave: con transacción única, una sentencia puede
// haber corrido bien y aun así no existir más. Ahí no se saca nada.
func (s *Session) olvidarAplicados(sesion *openSession, res ApplyResult) int {
	if res.RolledBack {
		return 0
	}
	n := 0
	for _, r := range res.Results {
		if r.Applied && sesion.cambios.Remove(r.ChangeID) {
			n++
		}
	}
	return n
}

func (s *Session) aplicarEnTransaccion(
	ctx context.Context, sesion *openSession, sts []change.Statement,
) ApplyResult {
	res := ApplyResult{OK: true}

	tx, err := sesion.pool.Begin(ctx)
	if err != nil {
		f := postgres.Classify(err, sesion.conn.Describe())
		return ApplyResult{Failure: f}
	}
	// Rollback después de un commit exitoso es un no-op en pgx, así que este
	// defer cubre el camino de error sin estorbar el feliz.
	defer func() { _ = tx.Rollback(ctx) }()

	for i, st := range sts {
		r, err := s.correr(ctx, tx, i, st)
		res.Results = append(res.Results, r)
		if err != nil {
			res.OK = false
			res.RolledBack = true
			res.Failure = postgres.ClassifyStatement(err, sesion.conn.Describe())
			return res
		}
	}
	// El commit también puede fallar, y con una razón propia: las claves
	// DEFERRABLE se comprueban recién acá.
	if err := tx.Commit(ctx); err != nil {
		res.OK = false
		res.RolledBack = true
		res.Failure = postgres.ClassifyStatement(err, sesion.conn.Describe())
	}
	return res
}

func (s *Session) aplicarSuelto(
	ctx context.Context, sesion *openSession, sts []change.Statement,
) ApplyResult {
	res := ApplyResult{OK: true}
	for i, st := range sts {
		r, err := s.correr(ctx, sesion.pool, i, st)
		res.Results = append(res.Results, r)
		if err != nil {
			res.OK = false
			res.Failure = postgres.ClassifyStatement(err, sesion.conn.Describe())
			// Sin transacción, lo anterior YA quedó aplicado. Se corta acá: si
			// una sentencia falló, las que venían después casi siempre dependían
			// de ella.
			return res
		}
	}
	return res
}

// ejecutor es lo mínimo que Apply necesita para correr una sentencia. Lo
// cumplen tanto el pool como una transacción, que es lo que permite que el
// camino con y sin transacción compartan el mismo código.
type ejecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
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
	ctx context.Context, ej ejecutor, i int, st change.Statement,
) (StatementResult, error) {
	s.avanzarApply(i, st)

	inicio := time.Now()
	_, err := ej.Exec(ctx, st.SQL)
	r := StatementResult{
		ChangeID:  st.ChangeID,
		SQL:       st.SQL,
		ElapsedMs: time.Since(inicio).Milliseconds(),
		Applied:   err == nil,
	}
	if err != nil {
		r.Error = postgres.Redact(err.Error())
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			r.SQLState = pgErr.Code
		}
	}
	return r, err
}
