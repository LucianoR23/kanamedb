package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	sqldriver "github.com/go-sql-driver/mysql"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// DefaultConnectTimeout es cuánto se espera a que el servidor responda antes de
// darlo por inalcanzable. El mismo criterio que en Postgres: suficiente para una
// VPN lenta, corto como para no dejar la interfaz colgada.
const DefaultConnectTimeout = 8 * time.Second

// DefaultRowLimit es cuántas filas trae una consulta del editor si nadie dijo
// otra cosa.
const DefaultRowLimit = 1000

// dialers registrados, para el túnel SSH.
//
// go-sql-driver no acepta un dialer en el DSN: hay que registrarlo en un mapa
// global con un nombre y usar ese nombre como «red». El contador hace que cada
// conexión tenga el suyo, porque dos conexiones por túneles distintos no pueden
// compartir el dialer.
var contadorDialer atomic.Uint64

// Open conecta y devuelve la conexión con la forma de la costura.
func Open(
	ctx context.Context, dsn, desc string, opts engine.OpenOptions,
) (*Conn, *engine.Failure) {
	cfg, err := sqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "La cadena de conexión no es válida.",
			Detail:  engine.Redact(err.Error()),
		}
	}
	cfg.Timeout = DefaultConnectTimeout
	// Sin esto el driver deja la conexión colgada si el servidor se muere en
	// medio de una consulta larga, que es justo lo que pasa aplicando un ALTER.
	cfg.CheckConnLiveness = true

	var red string
	if opts.DialFunc != nil {
		red = fmt.Sprintf("kaname-tunnel-%d", contadorDialer.Add(1))
		dial := opts.DialFunc
		sqldriver.RegisterDialContext(red, func(ctx context.Context, addr string) (net.Conn, error) {
			return dial(ctx, "tcp", addr)
		})
		cfg.Net = red
	}

	base, err := sqldriver.NewConnector(cfg)
	if err != nil {
		return nil, Classify(err, desc)
	}
	// La configuración de sesión va en el CONECTOR y no en un Exec después de
	// abrir. Un `SET SESSION` vale para una sola conexión, y el pool abre
	// varias: puesto después, la conexión siguiente no lo tiene. Ver sesion.go.
	init, exigeUna := sesionDe(opts.ReadOnly, opts.StatementTimeout)
	db := sql.OpenDB(&conector{base: base, init: init, alMenosUna: exigeUna})

	max := int(opts.MaxConns)
	if max <= 0 {
		max = 4
	}
	db.SetMaxOpenConns(max)
	db.SetMaxIdleConns(max)
	// Las conexiones viejas se cierran solas: MySQL tiene wait_timeout y una
	// conexión que el servidor ya cerró falla en la consulta siguiente con un
	// error que no explica nada.
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, Classify(err, desc)
	}

	info, sinEscapes, err := leerServerInfo(ctx, db)
	if err != nil {
		db.Close()
		return nil, Classify(err, desc)
	}
	if f := comprobarVersion(info); f != nil {
		db.Close()
		return nil, f
	}

	return &Conn{db: db, server: info, desc: desc, dialer: red, sinEscapes: sinEscapes}, nil
}

// Probe abre una conexión, lee los datos del servidor y la cierra.
func Probe(ctx context.Context, dsn, desc string, dial engine.DialFunc) (*engine.ServerInfo, *engine.Failure) {
	c, f := Open(ctx, dsn, desc, engine.OpenOptions{MaxConns: 1, DialFunc: dial})
	if f != nil {
		return nil, f
	}
	defer c.Close()
	return c.server, nil
}

func comprobarVersion(info *engine.ServerInfo) *engine.Failure {
	if info.Supported() {
		return nil
	}
	return &engine.Failure{
		Kind:    engine.FailureOther,
		Message: fmt.Sprintf("%s es más viejo que lo que esta versión de Kaname maneja.", info.Display),
		Hint: fmt.Sprintf(
			"El mínimo es %s. Las versiones anteriores ya no tienen soporte de su propio "+
				"fabricante, así que un bug del motor no tendría dónde reportarse.",
			versionLegible(engine.MinVersion(info.Kind))),
	}
}

// encendido interpreta el valor de una variable booleana del servidor.
//
// MySQL contesta 0/1 y MariaDB OFF/ON para las mismas variables, así que hay
// que aceptar las dos formas.
func encendido(v string) bool {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "1", "ON", "TRUE", "YES":
		return true
	}
	return false
}

// transaccionSoloLectura lee si el servidor abre las transacciones en modo solo
// lectura, con el nombre de variable que entienda este motor.
//
// No hay uno que sirva para los dos: MySQL 8.0 eliminó @@tx_read_only y solo
// tiene @@transaction_read_only; MariaDB agregó @@transaction_read_only recién
// en 11.1.1 y antes solo tenía @@tx_read_only. Se prueba el que corresponde y
// si no está, se prueba el otro.
//
// Que no se pueda leer NO es motivo para fallar la conexión: es un dato de la
// barra de estado. Antes lo era, y dejaba afuera a toda la línea 10.x de
// MariaDB.
func transaccionSoloLectura(ctx context.Context, db *sql.DB, k engine.Kind) bool {
	nombres := []string{"@@transaction_read_only", "@@tx_read_only"}
	if k == engine.MariaDB {
		nombres = []string{"@@tx_read_only", "@@transaction_read_only"}
	}
	for _, n := range nombres {
		var v string
		if err := db.QueryRowContext(ctx, "SELECT "+n).Scan(&v); err == nil {
			return encendido(v)
		}
	}
	return false
}

func versionLegible(num int) string {
	return fmt.Sprintf("%d.%d", num/10000, (num%10000)/100)
}

// leerServerInfo junta todo lo que interesa del servidor en un solo viaje.
//
// Son datos del servidor, no del usuario: nada de esto es sensible.
func leerServerInfo(ctx context.Context, db *sql.DB) (*engine.ServerInfo, bool, error) {
	// La consulta NO pide @@transaction_read_only, y eso es a propósito: esa
	// variable no existe en toda la matriz. MySQL la tiene desde 8.0 —donde
	// además ELIMINÓ la vieja @@tx_read_only— y MariaDB recién desde 11.1.1.
	// O sea que no hay un solo nombre que sirva en los dos, y pedirlo acá hacía
	// que la conexión entera fallara contra MariaDB 10.11 —la LTS más vieja que
	// esta aplicación declara soportar— con «Unknown system variable», que
	// después se mostraba como «el servidor rechazó la conexión» y mandaba a
	// revisar la contraseña. Comprobado contra 10.11.19.
	//
	// Se lee aparte, más abajo, donde ya se sabe qué motor hay del otro lado.
	const q = `
		SELECT VERSION(),
		       CURRENT_USER(),
		       DATABASE(),
		       @@character_set_database,
		       @@time_zone,
		       @@read_only,
		       @@sql_mode`

	var version, usuario, encoding, tz string
	var base sql.NullString
	// read_only se lee como TEXTO y no como bool, y esto no es cautela de más:
	// MySQL devuelve 0/1 y MariaDB devuelve OFF/ON. Escanear a bool funciona
	// contra uno y falla contra el otro con un error de conversión que después
	// se interpreta como «no se pudo conectar», que es falso y manda a buscar
	// el problema a la red.
	var readOnly, modo string
	if err := db.QueryRowContext(ctx, q).Scan(
		&version, &usuario, &base, &encoding, &tz, &readOnly, &modo,
	); err != nil {
		return nil, false, fmt.Errorf("leer los datos del servidor: %w", err)
	}
	// NO_BACKSLASH_ESCAPES cambia cómo hay que citar un literal de texto en el
	// DDL. Ver quoteString.
	sinEscapes := strings.Contains(strings.ToUpper(modo), "NO_BACKSLASH_ESCAPES")

	inicio := time.Now()
	var uno int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&uno); err != nil {
		return nil, false, fmt.Errorf("medir la latencia: %w", err)
	}
	latencia := time.Since(inicio)

	k := kindDe(version)
	info := &engine.ServerInfo{
		Kind:        k,
		Version:     version,
		VersionNum:  versionNum(version),
		Display:     display(k, version),
		CurrentUser: usuario,
		CurrentDB:   base.String,
		Encoding:    encoding,
		TimeZone:    tz,
		// InRecovery no existe como tal: el equivalente es un servidor con
		// read_only puesto, que es lo que tiene una réplica.
		InRecovery:      encendido(readOnly),
		DefaultReadOnly: transaccionSoloLectura(ctx, db, k),
		Latency:         latencia,
		LatencyMS:       latencia.Milliseconds(),
	}

	// Superusuario: en MySQL no hay uno solo, así que se pregunta por el
	// privilegio que de verdad importa para explicar por qué algo no se puede.
	var super sql.NullString
	_ = db.QueryRowContext(ctx, `
		SELECT 1 FROM information_schema.user_privileges
		 WHERE grantee = CONCAT("'", SUBSTRING_INDEX(CURRENT_USER(), '@', 1), "'@'",
		                        SUBSTRING_INDEX(CURRENT_USER(), '@', -1), "'")
		   AND privilege_type = 'SUPER' LIMIT 1`).Scan(&super)
	info.IsSuperuser = super.Valid

	var visibles int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'`).Scan(&visibles); err == nil {
		info.VisibleTables = visibles
	}

	return info, sinEscapes, nil
}
