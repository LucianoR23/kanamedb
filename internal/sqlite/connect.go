package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	modernc "modernc.org/sqlite"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// DefaultRowLimit es cuántas filas trae una consulta del editor si nadie dijo
// otra cosa. El mismo que en los otros motores.
const DefaultRowLimit = 1000

// Open abre el archivo y devuelve la conexión con la forma de la costura.
//
// `dsn` tiene la forma `file:RUTA?_pragma=...`, que es lo que arma
// connection.dsnSQLite. Las opciones de sesión van ahí y no por Exec después,
// y el motivo es una trampa que ya costó una tarde: database/sql tiene un POOL,
// así que un `PRAGMA foreign_keys=ON` ejecutado con db.Exec se aplica a UNA
// conexión y la consulta siguiente puede salir por otra que no lo tiene.
// Comprobado: con el pragma puesto así, seis inserts de filas huérfanas pasaron
// sin error. En el DSN, en cambio, el driver lo aplica a cada conexión que abre.
func Open(
	ctx context.Context, dsn, desc string, opts engine.OpenOptions,
) (*Conn, *engine.Failure) {
	if opts.DialFunc != nil {
		// No es una limitación: no hay socket que discar. Decirlo es mejor que
		// ignorarlo en silencio y dejar creyendo que el túnel está en uso.
		return nil, &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "SQLite es un archivo local: no se puede llegar por un túnel SSH.",
			Hint: "Si el archivo está en otra máquina, copialo primero. Abrirlo por " +
				"una unidad de red compartida es una forma conocida de corromperlo.",
		}
	}

	// El modo solo lectura se pide en el DSN por lo mismo que los otros
	// pragmas. query_only rechaza toda escritura del lado del motor, no con una
	// comprobación nuestra.
	if opts.ReadOnly {
		dsn = conPragma(dsn, "query_only(1)")
	}

	base, err := modernc.NewConnector(dsn)
	if err != nil {
		return nil, Classify(err, desc)
	}
	// La SQL de sesión va por el conector, como en MySQL y por lo mismo que
	// los pragmas van en el DSN: tiene que llegar a CADA conexión del pool.
	// Después de ella se vuelve a pedir query_only si la conexión es de solo
	// lectura: el DSN ya lo puso, pero una SQL de sesión que lo apague no
	// puede ganar. Lo último que se dice es lo que queda.
	sesion := engine.SessionStatements(opts.SessionSQL, engine.SQLite)
	db := sql.OpenDB(engine.WithInit(base, func(ctx context.Context, ex driver.ExecerContext) error {
		if err := engine.RunSessionSQL(ctx, ex, sesion); err != nil {
			return err
		}
		if opts.ReadOnly {
			if _, err := ex.ExecContext(ctx, "PRAGMA query_only = 1", nil); err != nil {
				return fmt.Errorf("configurar la sesión: %w", err)
			}
		}
		return nil
	}))

	max := int(opts.MaxConns)
	if max <= 0 {
		max = 4
	}
	db.SetMaxOpenConns(max)
	db.SetMaxIdleConns(max)
	// No se pone ConnMaxLifetime: no hay servidor que cierre la conexión por
	// inactividad, así que reciclarlas solo costaría reabrir el archivo.

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		var es *engine.SessionSQLError
		if errors.As(err, &es) {
			return nil, engine.SessionSQLFailure(es, ClassifyStatement(es.Err, desc))
		}
		return nil, Classify(err, desc)
	}

	info, err := leerServerInfo(ctx, db, dsn)
	if err != nil {
		db.Close()
		return nil, Classify(err, desc)
	}
	if f := comprobarVersion(info); f != nil {
		db.Close()
		return nil, f
	}

	return &Conn{db: db, server: info, desc: desc}, nil
}

// Probe abre el archivo, lee lo que hay que saber y lo cierra.
func Probe(ctx context.Context, dsn, desc string) (*engine.ServerInfo, *engine.Failure) {
	c, f := Open(ctx, dsn, desc, engine.OpenOptions{MaxConns: 1})
	if f != nil {
		return nil, f
	}
	defer c.Close()
	return c.server, nil
}

// conPragma agrega un pragma al DSN sin pisar los que ya tiene.
func conPragma(dsn, pragma string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		// Un DSN que no parsea va a fallar igual al abrir, con mejor mensaje.
		return dsn
	}
	q := u.Query()
	q.Add("_pragma", pragma)
	u.RawQuery = q.Encode()
	return u.String()
}

// rutaDe saca la ruta del archivo del DSN, para mostrarla.
//
// No es un secreto: no lleva credenciales, porque en SQLite no hay. Es además
// lo único que identifica a la base, así que sin esto la barra de estado no
// tendría qué decir.
func rutaDe(dsn string) string {
	s := strings.TrimPrefix(dsn, "file:")
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	if r, err := url.PathUnescape(s); err == nil {
		s = r
	}
	return s
}

func comprobarVersion(info *engine.ServerInfo) *engine.Failure {
	if info.Supported() {
		return nil
	}
	return &engine.Failure{
		Kind:    engine.FailureOther,
		Message: fmt.Sprintf("%s es más viejo que lo que esta versión de Kaname maneja.", info.Display),
		Hint: "El mínimo es 3.35, que es donde aparecen ALTER TABLE DROP COLUMN y " +
			"RETURNING. Sin eso, media pantalla de estructura no tendría cómo funcionar.",
	}
}

// leerServerInfo junta lo que se sabe del archivo.
//
// Varios campos quedan vacíos y eso NO es una omisión: SQLite no tiene usuario,
// no tiene zona horaria de servidor y no tiene superusuario. El cero es la
// respuesta correcta, y la interfaz ya sabe no mostrar lo que viene vacío.
func leerServerInfo(ctx context.Context, db *sql.DB, dsn string) (*engine.ServerInfo, error) {
	ruta := rutaDe(dsn)

	inicio := time.Now()
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return nil, fmt.Errorf("leer la versión: %w", err)
	}
	latencia := time.Since(inicio)

	info := &engine.ServerInfo{
		Kind:       engine.SQLite,
		Version:    version,
		VersionNum: versionNum(version),
		Display:    display(version),
		// La «base» es el archivo. Se muestra el nombre y no la ruta entera
		// porque la ruta entera no entra en la barra de estado; la completa
		// está en la pantalla de la conexión.
		CurrentDB: filepath.Base(ruta),
		Latency:   latencia,
		LatencyMS: latencia.Milliseconds(),
	}

	// encoding es una propiedad del ARCHIVO, fijada al crearlo y no cambiable
	// después. Interesa por lo mismo que en Postgres: explica por qué un texto
	// se ve mal.
	var enc string
	if err := db.QueryRowContext(ctx, "PRAGMA encoding").Scan(&enc); err == nil {
		info.Encoding = enc
	}

	// query_only es lo más cerca que hay de «el servidor no acepta escrituras».
	var soloLectura string
	if err := db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&soloLectura); err == nil {
		info.DefaultReadOnly = encendido(soloLectura)
	}

	var visibles int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_schema
		 WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`).Scan(&visibles); err == nil {
		info.VisibleTables = visibles
	}

	return info, nil
}

// encendido interpreta el valor de un pragma booleano. SQLite contesta 0/1,
// pero acepta ON/OFF al escribirlos y algunos builds los devuelven así.
func encendido(v string) bool {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "1", "ON", "TRUE", "YES":
		return true
	}
	return false
}
