// Package postgres conecta con PostgreSQL y traduce sus fallos a algo que la
// interfaz pueda mostrar.
//
// Nada de lo que sale de este paquete lleva credenciales: el DSN se arma, se
// usa y se descarta. Ver CLAUDE.md.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultConnectTimeout es cuánto se espera a que el servidor responda antes de
// darlo por inalcanzable. Suficiente para una VPN lenta, corto como para no
// dejar la interfaz colgada esperando a un host que no existe.
const DefaultConnectTimeout = 10 * time.Second

// MinServerVersion es la versión más vieja de PostgreSQL que la app soporta.
// 14 es la más antigua con soporte oficial de PostgreSQL a septiembre de 2026.
const MinServerVersion = 140000

// ServerInfo es lo que se sabe del servidor una vez conectado. Alimenta la
// barra de estado y las decisiones sobre qué se puede hacer con la conexión.
type ServerInfo struct {
	// Version es el texto completo, como lo reporta el servidor.
	Version string `json:"version"`
	// VersionNum es server_version_num: 180006 para 18.6. Sirve para comparar.
	VersionNum int `json:"versionNum"`
	// Display es la versión corta para mostrar: "PostgreSQL 18.6".
	Display string `json:"display"`

	CurrentUser string `json:"currentUser"`
	CurrentDB   string `json:"currentDatabase"`
	Encoding    string `json:"encoding"`
	TimeZone    string `json:"timeZone"`

	// IsSuperuser importa para explicar por qué una operación no se puede hacer.
	IsSuperuser bool `json:"isSuperuser"`

	// InRecovery es true si el servidor es una réplica. Escribir contra una
	// réplica falla con un mensaje poco claro, así que conviene avisar antes.
	InRecovery bool `json:"inRecovery"`

	// DefaultReadOnly es true si el servidor fuerza transacciones de solo
	// lectura, otra causa de escrituras que fallan sin explicación obvia.
	DefaultReadOnly bool `json:"defaultReadOnly"`

	// Latency es lo que tardó el ida y vuelta de la verificación.
	Latency time.Duration `json:"-"`
	// LatencyMS es lo mismo, en milisegundos, para el frontend.
	LatencyMS int64 `json:"latencyMs"`
}

// Supported dice si la versión del servidor está dentro de lo que la app maneja.
func (s ServerInfo) Supported() bool { return s.VersionNum >= MinServerVersion }

// Probe abre una conexión, lee los datos del servidor y la cierra.
//
// Es lo que hay detrás del botón de probar conexión: no deja nada abierto y
// devuelve un Failure ya interpretado en vez del error crudo de la red.
//
// `desc` es la descripción segura de la conexión, para los mensajes de error.
func Probe(ctx context.Context, dsn, desc string) (*ServerInfo, *Failure) {
	ctx, cancel := context.WithTimeout(ctx, DefaultConnectTimeout)
	defer cancel()

	inicio := time.Now()
	conn, err := pgx.Connect(ctx, dsn)
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
	info.Latency = time.Since(inicio)
	info.LatencyMS = info.Latency.Milliseconds()
	return info, nil
}

// Connect abre un pool listo para usar y verifica que responde.
//
// El pool existe porque cancelar una query necesita una segunda conexión, y
// porque la interfaz puede tener varias pestañas consultando a la vez.
func Connect(ctx context.Context, dsn, desc string, maxConns int32) (*pgxpool.Pool, *ServerInfo, *Failure) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// El error de parseo puede citar el DSN, que lleva la contraseña.
		return nil, nil, &Failure{
			Kind:    FailureOther,
			Message: "La cadena de conexión de " + desc + " no es válida.",
		}
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.ConnConfig.ConnectTimeout = DefaultConnectTimeout
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
		return nil, nil, Classify(err, desc)
	}
	info, err := readServerInfo(abrir, conn.Conn())
	conn.Release()
	if err != nil {
		pool.Close()
		return nil, nil, Classify(err, desc)
	}
	info.Latency = time.Since(inicio)
	info.LatencyMS = info.Latency.Milliseconds()

	return pool, info, nil
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

	var info ServerInfo
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
	return &info, nil
}

// ErrUnsupportedVersion se devuelve cuando el servidor es más viejo de lo que
// la app soporta.
var ErrUnsupportedVersion = errors.New("versión de PostgreSQL no soportada")
