// Package postgres conecta con PostgreSQL y traduce sus fallos a algo que la
// interfaz pueda mostrar.
//
// Nada de lo que sale de este paquete lleva credenciales: el DSN se arma, se
// usa y se descarta. Ver CLAUDE.md.
package postgres

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// DefaultConnectTimeout es cuánto se espera a que el servidor responda antes de
// darlo por inalcanzable. Suficiente para una VPN lenta, corto como para no
// dejar la interfaz colgada esperando a un host que no existe.
const DefaultConnectTimeout = 10 * time.Second

// MinServerVersion es la versión más vieja de PostgreSQL que la app soporta.
// 14 es la más antigua con soporte oficial de PostgreSQL a septiembre de 2026.
// La lista de mínimos de los cuatro motores vive en engine.MinVersion.
var MinServerVersion = engine.MinVersion(engine.Postgres)

// ServerInfo vive en `internal/engine`: es la misma forma para los cuatro
// motores, aunque cada uno la complete a su manera. Acá queda el alias.
type ServerInfo = engine.ServerInfo

// Probe abre una conexión, lee los datos del servidor y la cierra.
//
// Es lo que hay detrás del botón de probar conexión: no deja nada abierto y
// devuelve un Failure ya interpretado en vez del error crudo de la red.
//
// `desc` es la descripción segura de la conexión, para los mensajes de error.
func Probe(ctx context.Context, dsn, desc string) (*ServerInfo, *Failure) {
	return ProbeThrough(ctx, dsn, desc, nil)
}

// ProbeThrough prueba la conexión discando por la función que se le pase.
//
// Existe para que "probar conexión" pase por el túnel cuando la conexión usa
// uno. Sin esto, probar una conexión con bastión intentaba llegar directo a la
// base y fallaba con un error de red que no mencionaba el túnel — o peor,
// funcionaba desde una red donde la base sí era alcanzable y daba por buena una
// configuración que en otra máquina no iba a andar.
func ProbeThrough(ctx context.Context, dsn, desc string, dial pgconn.DialFunc) (*ServerInfo, *Failure) {
	ctx, cancel := context.WithTimeout(ctx, DefaultConnectTimeout)
	defer cancel()

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fallaDeParseo(err, desc)
	}
	if dial != nil {
		cfg.DialFunc = dial
		// Y la resolución del nombre pasa al otro lado del túnel, por lo mismo
		// que en Connect: el host de la base suele resolver solo desde el
		// bastión.
		cfg.LookupFunc = func(_ context.Context, host string) ([]string, error) {
			return []string{host}, nil
		}
	}

	inicio := time.Now()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, Classify(err, desc)
	}
	defer func() {
		// El cierre usa su propio contexto: el de arriba puede estar vencido y
		// entonces la conexión quedaría colgada del lado del servidor.
		cerrar, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelar()
		_ = conn.Close(cerrar)
	}()

	info, err := readServerInfo(ctx, conn)
	if err != nil {
		return nil, Classify(err, desc)
	}
	if f := checkVersion(info); f != nil {
		return nil, f
	}
	info.Latency = time.Since(inicio)
	info.LatencyMS = info.Latency.Milliseconds()
	return info, nil
}

// checkVersion rechaza servidores por debajo del mínimo soportado.
//
// No es formalismo: en PostgreSQL 13 y anteriores, pg_class.reltuples vale 0
// para una tabla nunca analizada en vez de -1, así que el árbol mostraría
// "0 filas" para una tabla con millones. La distinción entre "no sé" y "cero"
// se pierde en silencio, que es la peor forma de perderla.
func checkVersion(info *ServerInfo) *Failure {
	if info.Supported() {
		return nil
	}
	return &Failure{
		Kind:    FailureOther,
		Message: info.Display + " es más antigua de lo que Kaname soporta.",
		Hint:    "El mínimo es PostgreSQL 14, la versión más vieja con soporte oficial.",
	}
}

// Connect abre un pool listo para usar y verifica que responde.
//
// El pool existe porque cancelar una query necesita una segunda conexión, y
// porque la interfaz puede tener varias pestañas consultando a la vez.
// ConnectOptions son las decisiones de la conexión que el pool tiene que
// hacer cumplir.
//
// Van acá y no en cada consulta a propósito. Solo lectura y statement_timeout
// se mandan como parámetros del paquete de arranque, así que toda conexión que
// el pool abra nace con ellos y no hay forma de olvidarse de aplicarlos en un
// camino nuevo. Un guard que hay que acordarse de invocar termina siendo un
// guard que alguien no invoca.
type ConnectOptions struct {
	MaxConns int32

	// ReadOnly pone default_transaction_read_only. El servidor rechaza toda
	// escritura con 25006, incluidas las que no pasen por nuestro código.
	ReadOnly bool

	// StatementTimeout corta del lado del servidor. Cancelar desde el cliente
	// depende de que el cliente siga vivo; esto no.
	StatementTimeout time.Duration

	// DialFunc reemplaza cómo se abre el socket hacia la base.
	//
	// Es lo que hace posible el túnel SSH sin abrir ningún puerto local. Un
	// túnel se implementa habitualmente escuchando en 127.0.0.1 y reenviando,
	// pero un puerto en loopback es alcanzable desde cualquier pestaña del
	// navegador — la misma razón por la que esta aplicación no tiene servidor
	// HTTP. Con esto, la conexión existe solo dentro del proceso.
	//
	// Nil usa el discado normal de pgx.
	DialFunc pgconn.DialFunc

	// SessionSQL corre en cada conexión que el pool abre, antes que nada.
	SessionSQL string
}

func Connect(ctx context.Context, dsn, desc string, opts ConnectOptions) (*pgxpool.Pool, *ServerInfo, *Failure) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, fallaDeParseo(err, desc)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if opts.ReadOnly {
		cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	if opts.StatementTimeout > 0 {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] =
			strconv.FormatInt(opts.StatementTimeout.Milliseconds(), 10)
	}
	// La SQL de sesión va en AfterConnect, que corre en cada conexión que el
	// pool abre. Las protecciones ya viajaron en el paquete de arranque; se
	// vuelven a pedir DESPUÉS de la SQL de sesión, por si esta las tocó: un
	// `SET default_transaction_read_only = off` escrito ahí no puede ganarle
	// a la casilla de solo lectura. Lo último que se dice es lo que queda.
	if sesion := engine.SessionStatements(opts.SessionSQL, engine.Postgres); len(sesion) > 0 {
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			for _, st := range sesion {
				if _, err := conn.Exec(ctx, st.SQL); err != nil {
					return &engine.SessionSQLError{Line: st.Line, Err: err}
				}
			}
			for k, v := range cfg.ConnConfig.RuntimeParams {
				if k != "default_transaction_read_only" && k != "statement_timeout" {
					continue
				}
				// Como literal, que vale para el booleano y para el número. El
				// valor lo escribió Connect, no la persona, pero se escapa igual.
				if _, err := conn.Exec(ctx, "SET "+k+" = '"+strings.ReplaceAll(v, "'", "''")+"'"); err != nil {
					return fmt.Errorf("reponer %s después de la SQL de sesión: %w", k, err)
				}
			}
			return nil
		}
	}
	if opts.DialFunc != nil {
		cfg.ConnConfig.DialFunc = opts.DialFunc
		// Y la resolución de nombres pasa a hacerse del otro lado del túnel.
		//
		// pgx resuelve el host ANTES de llamar a DialFunc y le pasa una IP. Con
		// un túnel eso es al revés de lo que hace falta: el nombre de la base
		// —`db.interna`, o el nombre de un servicio— suele resolver solo desde
		// el bastión, no desde esta máquina. Sin esto, conectar por túnel falla
		// con "no such host" aunque el túnel esté perfecto.
		//
		// Va acá y no como opción aparte a propósito: quien ponga DialFunc sin
		// esto se come ese error, y el error no se parece en nada a la causa.
		cfg.ConnConfig.LookupFunc = func(_ context.Context, host string) ([]string, error) {
			return []string{host}, nil
		}
	}
	// Solo si el DSN no trajo el suyo: un connect_timeout explícito en la cadena
	// de conexión es una decisión del usuario, y pisarla hacía que Connect y
	// Probe se comportaran distinto ante el mismo DSN.
	if cfg.ConnConfig.ConnectTimeout == 0 {
		cfg.ConnConfig.ConnectTimeout = DefaultConnectTimeout
	}
	// Una conexión ociosa que el servidor cerró por su cuenta reaparece como un
	// error raro en la próxima query. Reciclarlas antes evita ese misterio.
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = time.Hour

	abrir, cancel := context.WithTimeout(ctx, DefaultConnectTimeout)
	defer cancel()

	inicio := time.Now()
	pool, err := pgxpool.NewWithConfig(abrir, cfg)
	if err != nil {
		return nil, nil, Classify(err, desc)
	}

	// NewWithConfig es perezoso: sin esto el primer error aparecería recién en
	// la primera query, cuando el usuario ya cree que está conectado.
	conn, err := pool.Acquire(abrir)
	if err != nil {
		pool.Close()
		// Una sentencia de la SQL de sesión que el servidor rechazó no es «no
		// se pudo conectar»: es un error de sentencia, con su línea.
		var es *engine.SessionSQLError
		if errors.As(err, &es) {
			return nil, nil, engine.SessionSQLFailure(es, ClassifyStatement(es.Err, desc))
		}
		return nil, nil, Classify(err, desc)
	}
	info, err := readServerInfo(abrir, conn.Conn())
	conn.Release()
	if err != nil {
		pool.Close()
		return nil, nil, Classify(err, desc)
	}
	if f := checkVersion(info); f != nil {
		pool.Close()
		return nil, nil, f
	}
	info.Latency = time.Since(inicio)
	info.LatencyMS = info.Latency.Milliseconds()

	return pool, info, nil
}

// fallaDeParseo interpreta un error de ParseConfig.
//
// El error entero puede citar el DSN, que lleva la contraseña, así que no se
// propaga tal cual. Pero adentro hay un caso que no es «la cadena no es
// válida»: pgx abre los certificados —sslrootcert, sslcert, sslkey— DURANTE el
// parseo, y una ruta con un error de tipeo, un archivo que no está en esta
// máquina o uno que no es PEM llegaban acá como una cadena inválida sin
// ningún detalle. El error interior de un ParseConfigError no lleva la
// cadena, y dice qué archivo fue.
func fallaDeParseo(err error, desc string) *Failure {
	var pce *pgconn.ParseConfigError
	if errors.As(err, &pce) {
		if interior := pce.Unwrap(); interior != nil && esTLS(strings.ToLower(pce.Error())) {
			return &Failure{
				Kind:    FailureTLS,
				Message: "No se pudo usar uno de los certificados de " + desc + ".",
				Hint:    "Tiene que ser un archivo PEM legible desde esta máquina. La ruta se guarda, no el archivo.",
				Detail:  engine.Redact(interior.Error()),
			}
		}
	}
	return &Failure{
		Kind:    FailureOther,
		Message: "La cadena de conexión de " + desc + " no es válida.",
	}
}

// canalDe describe el canal TLS de una conexión, o nil si va en claro.
//
// pgx envuelve el socket en un *tls.Conn cuando negoció TLS y lo expone tal
// cual; no se lee ni se escribe por él, solo se mira el estado de la
// negociación, así que no hace falta sincronizar nada.
func canalDe(conn *pgx.Conn) *engine.TLSInfo {
	if tc, ok := conn.PgConn().Conn().(*tls.Conn); ok {
		return engine.TLSInfoOf(tc.ConnectionState())
	}
	return nil
}

// readServerInfo junta todo lo que interesa del servidor en un solo ida y
// vuelta. Son datos del servidor, no del usuario: nada de esto es sensible.
func readServerInfo(ctx context.Context, conn *pgx.Conn) (*ServerInfo, error) {
	const q = `
		SELECT version(),
		       current_setting('server_version_num')::int,
		       current_setting('server_version'),
		       current_user,
		       current_database(),
		       current_setting('server_encoding'),
		       current_setting('TimeZone'),
		       (SELECT rolsuper FROM pg_roles WHERE rolname = current_user),
		       pg_is_in_recovery(),
		       current_setting('default_transaction_read_only')::bool`

	// Kind se pone acá y no lo deduce quien lee: la interfaz decide qué
	// ofrecer según el motor, y un ServerInfo sin motor la obligaría a
	// adivinarlo del texto de la versión.
	info := ServerInfo{Kind: engine.Postgres}
	var short string
	err := conn.QueryRow(ctx, q).Scan(
		&info.Version,
		&info.VersionNum,
		&short,
		&info.CurrentUser,
		&info.CurrentDB,
		&info.Encoding,
		&info.TimeZone,
		&info.IsSuperuser,
		&info.InRecovery,
		&info.DefaultReadOnly,
	)
	if err != nil {
		return nil, fmt.Errorf("leer la información del servidor: %w", err)
	}
	info.Display = "PostgreSQL " + short
	info.TLS = canalDe(conn)

	// Conteo aparte porque puede fallar por permisos sin que eso invalide la
	// conexión: si no se puede contar, queda en cero y la UI lo muestra como
	// "sin tablas visibles", que es exactamente lo que pasa.
	if err := conn.QueryRow(ctx, visibleTablesQuery).Scan(&info.VisibleTables); err != nil {
		info.VisibleTables = 0
	}
	return &info, nil
}

// visibleTablesQuery cuenta las tablas que el usuario puede ver, con los mismos
// filtros que usa la introspección para que los dos números coincidan.
const visibleTablesQuery = `
	SELECT count(*)
	FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind IN ('r', 'p')
	  AND NOT c.relispartition
	  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	  AND n.nspname NOT LIKE 'pg\_toast%'
	  AND n.nspname NOT LIKE 'pg\_temp%'
	  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')`
