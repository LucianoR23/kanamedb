package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/layout"
	"github.com/LucianoR23/kanamedb/internal/schema"
	"github.com/LucianoR23/kanamedb/internal/secrets"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// Session es la conexión abierta. La Iteración 1 sostiene una sola: el
// workspace de S05 trabaja contra una base por vez.
type Session struct {
	store   *store.Store
	keyring Keyring
	known   *tunnel.KnownHosts
	layouts *layout.Store

	mu      sync.RWMutex
	current *openSession

	// progreso es por dónde va el apply en curso. Va acá y no en la sesión
	// abierta porque lo consulta la interfaz mientras Apply está bloqueado, y
	// comparte el lock con el resto del estado.
	progreso      ApplyProgress
	progresoDesde time.Time
}

type openSession struct {
	conn connection.Connection
	// tunel es el salto SSH, si la conexión lo usa. Se cierra junto con el pool:
	// dejarlo abierto mantendría viva una sesión en el bastión que ya no sirve
	// para nada y que el administrador de ese host ve como conectada.
	tunel *tunnel.Client
	// db es la conexión abierta, del motor que sea. Acá estaba el
	// *pgxpool.Pool, que era lo que ataba el servicio entero a Postgres.
	db       engine.Conn
	server   *engine.ServerInfo
	openedAt time.Time
	snapshot *schema.Snapshot

	// cambios son las ediciones pendientes de aplicar.
	//
	// Vive acá y no en el Session porque muere con la conexión: un changeset
	// armado contra una base a la que ya no estás conectado no es útil, es
	// peligroso — la siguiente conexión podría ser otra base con las mismas
	// tablas.
	cambios *change.Set
}

// NewSession arma el servicio.
func NewSession(st *store.Store, kr Keyring, known *tunnel.KnownHosts, diagramas *layout.Store) *Session {
	return &Session{store: st, keyring: kr, known: known, layouts: diagramas}
}

// SessionView es el estado de la conexión tal como lo ve la interfaz.
type SessionView struct {
	Connected bool `json:"connected"`

	// ConnectionID es la conexión abierta, vacío si no hay ninguna.
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	// Describe es usuario@host:puerto/base, seguro para mostrar y loguear.
	Describe    string                 `json:"describe"`
	Environment connection.Environment `json:"environment"`

	// ReadOnly junta la configuración de la conexión con lo que dice el
	// servidor: una réplica es de solo lectura aunque la conexión no lo diga.
	ReadOnly bool `json:"readOnly"`
	// ReadOnlyReason explica por qué, para que la UI no tenga que adivinar.
	ReadOnlyReason string `json:"readOnlyReason,omitempty"`

	Server *engine.ServerInfo `json:"server,omitempty"`

	// Caps son las capacidades del motor abierto. Viaja entera y no campo por
	// campo: la pantalla ya necesitaba tres, y deducirlas del nombre del motor
	// del lado del frontend sería adivinar lo que acá está comprobado.
	Caps engine.Caps `json:"caps"`

	// RowLimit y StatementTimeoutSeconds son los efectivos de esta conexión, ya
	// resueltos: el cero de la configuración significa "usá el default", y la
	// interfaz no tiene por qué conocer esa convención para poder mostrar
	// "se traen hasta 1.000 filas" o "el servidor corta a los 30 s".
	RowLimit                int `json:"rowLimit"`
	StatementTimeoutSeconds int `json:"statementTimeoutSeconds"`

	// OpenedAt permite mostrar hace cuánto está abierta.
	OpenedAt string `json:"openedAt,omitempty"`
}

// ConnectResult es el resultado de intentar conectar.
//
// Es un struct con nombre y no un par (vista, fallo) por una razón concreta: un
// par se puede ignorar a medias. El frontend hacía `await Connect(id)` y seguía
// al workspace sin mirar el segundo valor, así que una conexión fallida
// terminaba en una pantalla vacía sin explicación. Con `ok` hay que mirarlo.
//
// Devolver un `error` de Go tampoco servía: Wails lo serializa como texto y se
// perderían la causa, la sugerencia y el SQLSTATE, que es justo lo que S24
// necesita para decir qué hay que arreglar.
type ConnectResult struct {
	OK      bool            `json:"ok"`
	Session SessionView     `json:"session"`
	Failure *engine.Failure `json:"failure,omitempty"`
}

// ErrNotConnected lo devuelven las operaciones que necesitan una sesión abierta.
var ErrNotConnected = errors.New("no hay ninguna conexión abierta")

// Connect abre la conexión y deja el pool listo.
//
// Cerrar la anterior es parte de conectar: dos pools abiertos contra bases
// distintas sin que la interfaz lo muestre es la receta para aplicar un cambio
// donde no era.
func (s *Session) Connect(ctx context.Context, id string) ConnectResult {
	return s.ConnectAccepting(ctx, id, "")
}

// ConnectAccepting conecta aceptando una clave de host solo para este intento.
//
// Es el botón "Conectar una vez" de S04: la huella vale para esta conexión y no
// se guarda en ningún lado. Va como parámetro explícito y no como estado del
// servicio para que no exista la posibilidad de que una aceptación temporal
// quede activa para la próxima conexión sin que nadie la haya pedido.
func (s *Session) ConnectAccepting(ctx context.Context, id, acceptOnce string) ConnectResult {
	c, err := s.store.Get(id)
	if err != nil {
		return failed(&engine.Failure{
			Kind:    engine.FailureOther,
			Message: err.Error(),
		})
	}

	password, err := s.keyring.Get(id)
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return failed(&engine.Failure{
			Kind:    engine.FailureOther,
			Message: "No se pudo leer la contraseña del keychain.",
		})
	}

	dsn, err := c.DSN(password)
	if err != nil {
		return failed(&engine.Failure{
			Kind:    engine.FailureOther,
			Message: err.Error(),
		})
	}

	opciones := connectOptions(c)

	// El túnel se abre ANTES que la base: si el salto no se puede establecer,
	// no tiene sentido intentar la conexión de abajo, y el error del túnel es
	// el que explica qué pasó.
	var tunelAbierto *tunnel.Client
	if c.SSH.Enabled {
		secreto, err := s.keyring.Get(SSHSecretID(id))
		if err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return failed(&engine.Failure{
				Kind:    engine.FailureOther,
				Message: "No se pudo leer el secreto del bastión del keychain.",
			})
		}
		sec := tunnel.Secrets{}
		switch c.SSH.Auth {
		case connection.SSHAuthPassword:
			sec.Password = secreto
		case connection.SSHAuthKeyFile:
			sec.Passphrase = secreto
		}

		cli, err := tunnel.Dial(ctx, c.SSH, s.known, sec, tunnel.DialOptions{AcceptOnce: acceptOnce})
		if err != nil {
			return failed(&engine.Failure{
				Kind:    engine.FailureOther,
				Message: "No se pudo abrir el túnel SSH.",
				Detail:  engine.Redact(err.Error()),
				Hint:    "Revisá el bastión, el usuario y el método de autenticación en la pestaña SSH.",
			})
		}
		tunelAbierto = cli
		opciones.DialFunc = cli.DialContext
	}

	db, failure := abrirMotor(ctx, c, dsn, opciones)
	if failure != nil {
		// El túnel quedó abierto y ya no sirve: cerrarlo acá evita dejar una
		// sesión colgada en el bastión por cada intento fallido.
		if tunelAbierto != nil {
			tunelAbierto.Close()
		}
		return failed(failure)
	}

	s.mu.Lock()
	anterior := s.current
	s.current = &openSession{
		conn:     c,
		tunel:    tunelAbierto,
		db:       db,
		server:   db.Server(),
		openedAt: time.Now(),
		cambios:  &change.Set{},
	}
	vista := s.viewLocked()
	s.mu.Unlock()

	// El cierre va fuera del lock: puede tardar y no hay razón para bloquear a
	// quien pregunte el estado mientras tanto.
	if anterior != nil {
		anterior.cerrar()
	}
	return ConnectResult{OK: true, Session: vista}
}

func failed(f *engine.Failure) ConnectResult {
	return ConnectResult{Failure: f}
}

// Disconnect cierra la conexión abierta. Sin sesión no es un error: el
// resultado buscado ya se cumple.
func (s *Session) Disconnect() {
	s.mu.Lock()
	anterior := s.current
	s.current = nil
	s.mu.Unlock()

	if anterior != nil {
		anterior.cerrar()
	}
}

// cerrar suelta la conexión y el túnel, en ese orden.
//
// El orden importa: cerrar el túnel primero dejaría a la conexión intentando
// hablar por un canal muerto, y sus errores de cierre serían ruido que no
// explica nada.
func (o *openSession) cerrar() {
	if o == nil {
		return
	}
	if o.db != nil {
		o.db.Close()
	}
	if o.tunel != nil {
		o.tunel.Close()
	}
}

// TunnelDown dice si esta sesión usa túnel y el túnel se cayó.
//
// Se pregunta DESPUÉS de que algo falla, no antes de cada operación: sondear
// el túnel en cada consulta agregaría trabajo a todas para atajar un caso raro.
// Cuando algo falla, en cambio, saber si el camino sigue en pie cambia el
// mensaje de "puede ser esto o aquello" a "fue esto".
func (s *Session) TunnelDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current != nil && s.current.tunel != nil && s.current.tunel.Closed()
}

// Current devuelve el estado de la sesión.
func (s *Session) Current() SessionView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.viewLocked()
}

// Schema devuelve el esquema de la base conectada.
//
// El resultado se guarda: el árbol se arma una vez y se refresca cuando el
// usuario lo pide o cuando algo cambia. Re-inspeccionar en cada render sería
// una consulta al catálogo por cada clic.
func (s *Session) Schema(ctx context.Context, refresh bool) (*schema.Snapshot, error) {
	s.mu.RLock()
	sesion := s.current
	if sesion != nil && sesion.snapshot != nil && !refresh {
		snap := sesion.snapshot
		s.mu.RUnlock()
		return snap, nil
	}
	s.mu.RUnlock()

	if sesion == nil {
		return nil, ErrNotConnected
	}

	snap, err := sesion.db.Introspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("leer el esquema de %s: %w", sesion.conn.Describe(), err)
	}
	conObjetos(ctx, sesion.db, snap)

	s.mu.Lock()
	// Puede haberse desconectado o reconectado mientras se leía: solo se guarda
	// si la sesión sigue siendo la misma.
	if s.current == sesion {
		s.current.snapshot = snap
	}
	s.mu.Unlock()
	return snap, nil
}

// TableDetail devuelve todo lo que el catálogo sabe de una tabla: columnas con
// sus defaults y comentarios, índices, claves foráneas en las dos direcciones,
// restricciones y triggers.
//
// A diferencia de Schema, esto NO se guarda. El esquema entero se cachea porque
// el árbol se arma una vez y se mira todo el tiempo; la estructura de una tabla
// se mira cuando alguien quiere saber cómo está hecha, y casi siempre es porque
// está por cambiarla o acaba de hacerlo. Una estructura vieja en esa pantalla es
// exactamente el momento en que más cara sale. Son siete consultas en un solo
// viaje: no vale la pena mentir para ahorrarlo.
func (s *Session) TableDetail(ctx context.Context, esquema, tabla string) (*schema.TableDetail, error) {
	sesion, err := s.abierta()
	if err != nil {
		return nil, err
	}
	d, err := sesion.db.Detail(ctx, esquema, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer la estructura de %s.%s: %w", esquema, tabla, err)
	}
	return d, nil
}

// ErdLayout devuelve dónde quedó cada tabla del diagrama de un esquema.
//
// El identificador de conexión sale de la sesión abierta y no lo pasa la
// interfaz: es lo que termina siendo un nombre de archivo, así que cuanto menos
// viaje por el puente, menos superficie hay que validar. (El paquete lo valida
// igual.)
func (s *Session) ErdLayout(esquema string) (layout.Positions, error) {
	sesion, err := s.abierta()
	if err != nil {
		return nil, err
	}
	p, err := s.layouts.Get(sesion.conn.ID, esquema)
	if err != nil {
		return nil, fmt.Errorf("leer el diagrama de %s: %w", esquema, err)
	}
	return p, nil
}

// SaveErdLayout guarda las posiciones del diagrama de un esquema. Un mapa vacío
// las olvida, que es cómo se vuelve al acomodado automático.
func (s *Session) SaveErdLayout(esquema string, posiciones layout.Positions) error {
	sesion, err := s.abierta()
	if err != nil {
		return err
	}
	if err := s.layouts.Save(sesion.conn.ID, esquema, posiciones); err != nil {
		return fmt.Errorf("guardar el diagrama de %s: %w", esquema, err)
	}
	return nil
}

// ColumnTypes lee del catálogo los tipos que se pueden elegir para una columna.
//
// Se leen de LA BASE CONECTADA y no de una lista en el código: los enums y
// dominios definidos ahí son tipos válidos, y las extensiones instaladas
// agregan los suyos. Una lista fija no podría ofrecerlos.
func (s *Session) ColumnTypes(ctx context.Context) ([]schema.TypeOption, error) {
	sesion, err := s.abierta()
	if err != nil {
		return nil, err
	}
	ts, err := sesion.db.ColumnTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("leer los tipos de %s: %w", sesion.conn.Describe(), err)
	}
	return ts, nil
}

// viewLocked arma la vista. Quien llama tiene el lock.
func (s *Session) viewLocked() SessionView {
	if s.current == nil {
		return SessionView{}
	}
	c := s.current.conn
	v := SessionView{
		Connected:    true,
		ConnectionID: c.ID,
		Name:         c.Name,
		Describe:     c.Describe(),
		Environment:  c.Environment,
		Server:       s.current.server,
		Caps:         s.current.db.Caps(),
		OpenedAt:     s.current.openedAt.Format(time.RFC3339),

		RowLimit:                c.Safety.EffectiveRowLimit(),
		StatementTimeoutSeconds: int(c.Safety.StatementTimeout() / time.Second),
	}

	v.ReadOnly, v.ReadOnlyReason = soloLectura(s.current)
	return v
}

// soloLectura dice si la sesión puede escribir, y por qué no si no puede.
//
// La conexión puede ser de solo lectura por tres motivos distintos y el usuario
// merece saber cuál: no es lo mismo haberlo elegido que descubrir que estás
// apuntando a una réplica.
//
// Lo usan la vista Y el apply, que es la razón de que no esté escrito adentro
// de la vista: una comprobación que solo alimenta la interfaz no protege nada.
func soloLectura(sesion *openSession) (bool, string) {
	switch {
	case sesion.conn.Safety.ReadOnly:
		return true, "La conexión está configurada como solo lectura."
	case sesion.server != nil && sesion.server.InRecovery:
		return true, "El servidor es una réplica y no acepta escrituras."
	case sesion.server != nil && sesion.server.DefaultReadOnly:
		return true, "El servidor fuerza transacciones de solo lectura."
	}
	return false, ""
}

// connectOptions traduce las protecciones de la conexión a lo que el pool tiene
// que hacer cumplir.
//
// Solo lectura y statement_timeout viajan en el arranque de cada conexión, no
// se aplican por consulta. Así una escritura la rechaza el servidor con 25006
// aunque el camino que la mande sea uno que todavía no existe, y el corte por
// tiempo sigue vigente aunque la app se cuelgue o se cierre.
func connectOptions(c connection.Connection) engine.OpenOptions {
	return engine.OpenOptions{
		MaxConns:         poolSize(c),
		ReadOnly:         c.Safety.ReadOnly,
		StatementTimeout: c.Safety.StatementTimeout(),
	}
}

// abierta devuelve la sesión en curso.
func (s *Session) abierta() (*openSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil, ErrNotConnected
	}
	return s.current, nil
}

// poolSize decide cuántas conexiones abrir.
//
// Cancelar una consulta necesita una segunda conexión, así que el mínimo útil
// es dos. Cuatro deja margen para el árbol y una pestaña consultando a la vez
// sin sorprender a quien administra el servidor con una avalancha de sesiones.
func poolSize(c connection.Connection) int32 {
	if c.Safety.ReadOnly {
		return 2
	}
	return 4
}

// conObjetos le agrega al snapshot los objetos que no son tablas.
//
// Va acá y no adentro de cada `Introspect` por el mismo motivo por el que
// `Conn.Objects` existe: es UNA implementación en vez de tres, y es exactamente
// la misma llamada que usa la cobertura del volcado, así que el árbol y el
// archivo no pueden discrepar sobre qué hay en la base.
//
// Un fallo NO rompe la lectura del esquema. El árbol de tablas sirve igual, y
// hacer fallar un «Conectar» entero porque una consulta al catálogo no se pudo
// leer sería una regresión sobre lo que venía funcionando. Queda dicho en
// `ObjectsError`, que la pantalla muestra: callarlo haría que un esquema lleno
// de vistas se viera igual que uno sin ninguna, que es el silencio que este
// proyecto no acepta.
func conObjetos(ctx context.Context, db engine.Conn, snap *schema.Snapshot) {
	if snap == nil || len(snap.Schemas) == 0 {
		return
	}
	nombres := make([]string, 0, len(snap.Schemas))
	for _, sc := range snap.Schemas {
		nombres = append(nombres, sc.Name)
	}

	// Las dos mitades se usan: `Objects` devuelve lo que alcanzó a leer JUNTO
	// con el error, así que una consulta al catálogo que falla —la de los
	// eventos de MariaDB es la candidata— cuesta sus objetos y no todos los
	// demás. El volcado hace lo contrario con el mismo par: se niega a escribir
	// un archivo incompleto.
	objetos, err := db.Objects(ctx, nombres)
	if err != nil {
		snap.ObjectsError = err.Error()
	}
	if len(objetos) == 0 {
		return
	}

	porEsquema := make(map[string][]schema.Object, len(snap.Schemas))
	for _, o := range objetos {
		porEsquema[o.Schema] = append(porEsquema[o.Schema], o)
	}
	for i := range snap.Schemas {
		// SQLite no tiene esquemas: sus objetos vienen con «main» y el único
		// esquema del snapshot se llama de otra forma. Con una sola entrada no
		// hay ambigüedad posible, así que van todos ahí.
		if len(snap.Schemas) == 1 {
			snap.Schemas[i].Objects = objetos
			break
		}
		snap.Schemas[i].Objects = porEsquema[snap.Schemas[i].Name]
	}
	snap.Normalize()
}

// ObjectDefinition devuelve la definición de un objeto del árbol.
//
// Se pide de a uno y a demanda, al revés que la lista: los nombres viajan con
// el snapshot porque el buscador los necesita todos, pero la definición de una
// vista puede ser de kilobytes y traerlas todas serían cientos de textos que
// nadie va a mirar.
func (s *Session) ObjectDefinition(ctx context.Context, o schema.Object) (schema.ObjectDefinition, error) {
	sesion, err := s.abierta()
	if err != nil {
		return schema.ObjectDefinition{}, err
	}
	// El error NO se vuelve a envolver: los mensajes de cada motor ya nombran al
	// objeto y dicen qué pasó. Un prefijo acá daba «leer la definición: sin
	// definición: kn_s05.positivo es un dominio…», con dos encabezados antes de
	// la frase que explica algo —probando a mano se ve enseguida—.
	return sesion.db.ObjectDefinition(ctx, o)
}

// ObjectDependents lista lo que se rompe si este objeto deja de existir.
//
// Va aparte de la definición y no pegado a ella porque son dos preguntas con
// dos costos y dos públicos: la definición se lee siempre que se abre un
// objeto, y esto solo importa cuando se está por reemplazarlo. Juntarlas
// haría pagar el recorrido de `pg_depend` a cada clic del árbol.
func (s *Session) ObjectDependents(ctx context.Context, o schema.Object) (schema.Dependents, error) {
	sesion, err := s.abierta()
	if err != nil {
		return schema.Dependents{}, err
	}
	return sesion.db.Dependents(ctx, o)
}
