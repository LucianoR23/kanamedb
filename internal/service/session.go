package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/postgres"
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

	mu      sync.RWMutex
	current *openSession
}

type openSession struct {
	conn connection.Connection
	// tunel es el salto SSH, si la conexión lo usa. Se cierra junto con el pool:
	// dejarlo abierto mantendría viva una sesión en el bastión que ya no sirve
	// para nada y que el administrador de ese host ve como conectada.
	tunel    *tunnel.Client
	pool     *pgxpool.Pool
	server   *postgres.ServerInfo
	openedAt time.Time
	snapshot *schema.Snapshot
}

// NewSession arma el servicio.
func NewSession(st *store.Store, kr Keyring, known *tunnel.KnownHosts) *Session {
	return &Session{store: st, keyring: kr, known: known}
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

	Server *postgres.ServerInfo `json:"server,omitempty"`

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
	OK      bool              `json:"ok"`
	Session SessionView       `json:"session"`
	Failure *postgres.Failure `json:"failure,omitempty"`
}

// ErrNotConnected lo devuelven las operaciones que necesitan una sesión abierta.
var ErrNotConnected = errors.New("no hay ninguna conexión abierta")

// Connect abre la conexión y deja el pool listo.
//
// Cerrar la anterior es parte de conectar: dos pools abiertos contra bases
// distintas sin que la interfaz lo muestre es la receta para aplicar un cambio
// donde no era.
func (s *Session) Connect(ctx context.Context, id string) ConnectResult {
	c, err := s.store.Get(id)
	if err != nil {
		return failed(&postgres.Failure{
			Kind:    postgres.FailureOther,
			Message: err.Error(),
		})
	}

	password, err := s.keyring.Get(id)
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return failed(&postgres.Failure{
			Kind:    postgres.FailureOther,
			Message: "No se pudo leer la contraseña del keychain.",
		})
	}

	dsn, err := c.DSN(password)
	if err != nil {
		return failed(&postgres.Failure{
			Kind:    postgres.FailureOther,
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
			return failed(&postgres.Failure{
				Kind:    postgres.FailureOther,
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

		cli, err := tunnel.Dial(ctx, c.SSH, s.known, sec, tunnel.DialOptions{})
		if err != nil {
			return failed(&postgres.Failure{
				Kind:    postgres.FailureOther,
				Message: "No se pudo abrir el túnel SSH.",
				Detail:  postgres.Redact(err.Error()),
				Hint:    "Revisá el bastión, el usuario y el método de autenticación en la pestaña SSH.",
			})
		}
		tunelAbierto = cli
		opciones.DialFunc = cli.DialContext
	}

	pool, info, failure := postgres.Connect(ctx, dsn, c.Describe(), opciones)
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
		pool:     pool,
		server:   info,
		openedAt: time.Now(),
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

func failed(f *postgres.Failure) ConnectResult {
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

// cerrar suelta el pool y el túnel, en ese orden.
//
// El orden importa: cerrar el túnel primero dejaría al pool intentando hablar
// por un canal muerto, y sus errores de cierre serían ruido que no explica nada.
func (o *openSession) cerrar() {
	if o == nil {
		return
	}
	if o.pool != nil {
		o.pool.Close()
	}
	if o.tunel != nil {
		o.tunel.Close()
	}
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

	snap, err := postgres.Introspect(ctx, sesion.pool)
	if err != nil {
		return nil, fmt.Errorf("leer el esquema de %s: %w", sesion.conn.Describe(), err)
	}

	s.mu.Lock()
	// Puede haberse desconectado o reconectado mientras se leía: solo se guarda
	// si la sesión sigue siendo la misma.
	if s.current == sesion {
		s.current.snapshot = snap
	}
	s.mu.Unlock()
	return snap, nil
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
		OpenedAt:     s.current.openedAt.Format(time.RFC3339),

		RowLimit:                c.Safety.EffectiveRowLimit(),
		StatementTimeoutSeconds: int(c.Safety.StatementTimeout() / time.Second),
	}

	// La conexión puede ser de solo lectura por tres motivos distintos, y el
	// usuario merece saber cuál: no es lo mismo haberlo elegido que descubrir
	// que estás apuntando a una réplica.
	switch {
	case c.Safety.ReadOnly:
		v.ReadOnly = true
		v.ReadOnlyReason = "La conexión está configurada como solo lectura."
	case s.current.server != nil && s.current.server.InRecovery:
		v.ReadOnly = true
		v.ReadOnlyReason = "El servidor es una réplica y no acepta escrituras."
	case s.current.server != nil && s.current.server.DefaultReadOnly:
		v.ReadOnly = true
		v.ReadOnlyReason = "El servidor fuerza transacciones de solo lectura."
	}
	return v
}

// connectOptions traduce las protecciones de la conexión a lo que el pool tiene
// que hacer cumplir.
//
// Solo lectura y statement_timeout viajan en el arranque de cada conexión, no
// se aplican por consulta. Así una escritura la rechaza el servidor con 25006
// aunque el camino que la mande sea uno que todavía no existe, y el corte por
// tiempo sigue vigente aunque la app se cuelgue o se cierre.
func connectOptions(c connection.Connection) postgres.ConnectOptions {
	return postgres.ConnectOptions{
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
