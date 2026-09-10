package mysql

import (
	"context"
	"database/sql"
	"sync"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Conn es la conexión a MySQL o MariaDB detrás de la costura de engine.
type Conn struct {
	db     *sql.DB
	server *engine.ServerInfo
	desc   string
	// dialer es el nombre con el que se registró el dialer del túnel, si lo
	// hay. Se guarda para poder describirlo, no se desregistra: el driver no
	// ofrece cómo, y son unas pocas entradas por sesión.
	dialer string

	// sinEscapes recuerda si el servidor tiene NO_BACKSLASH_ESCAPES, que cambia
	// cómo hay que citar un literal de texto en el DDL. Ver quoteString.
	sinEscapes bool

	unaVez sync.Once
}

var _ engine.Conn = (*Conn)(nil)

func (c *Conn) Kind() engine.Kind          { return c.server.Kind }
func (c *Conn) Caps() engine.Caps          { return engine.CapsOf(c.server.Kind) }
func (c *Conn) Server() *engine.ServerInfo { return c.server }
func (c *Conn) Close()                     { c.unaVez.Do(func() { _ = c.db.Close() }) }

// DB expone el *sql.DB para los tests del propio paquete. No lo usa el servicio.
func (c *Conn) DB() *sql.DB { return c.db }

// base es la base contra la que está posicionada la conexión.
//
// En MySQL «esquema» y «base» son la misma cosa, así que cuando el servicio
// pasa un esquema vacío hay que completarlo: las consultas al catálogo filtran
// por table_schema y sin valor traerían el catálogo entero del servidor.
func (c *Conn) base(esquema string) string {
	if esquema != "" {
		return esquema
	}
	return c.server.CurrentDB
}

// Dialect: acento invertido para los identificadores, `#` abre comentario, y el
// cuerpo de un trigger o un procedimiento va entre BEGIN y END.
//
// La barra invertida sale del modo del SERVIDOR y no del motor: con
// NO_BACKSLASH_ESCAPES no escapa nada, y creerle al motor en vez de al servidor
// haría partir el texto adentro de una cadena.
func (c *Conn) Dialect() query.Dialect {
	return query.Dialect{
		Backtick:         true,
		HashComments:     true,
		BackslashEscapes: !c.sinEscapes,
		Compound:         true,
	}
}

func (c *Conn) Exec(ctx context.Context, sql string) error {
	_, err := c.db.ExecContext(ctx, sql)
	return err
}

// Modify manda los valores como parámetros de una sentencia preparada del
// lado del servidor —go-sql-driver no interpola salvo que se le pida— y
// devuelve las filas ALCANZADAS, que es lo que clientFoundRows cambia. Ver
// engine.Tx.Modify y Open.
func (c *Conn) Modify(ctx context.Context, sql string, args []any) (int64, error) {
	return filasDe(c.db.ExecContext(ctx, sql, args...))
}

// filasDe saca el conteo del resultado de database/sql.
func filasDe(r sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// Begin ignora las opciones: MySQL y MariaDB hacen ALTER de verdad y no
// reconstruyen la tabla, así que no tienen nada que apagar antes del BEGIN.
func (c *Conn) Begin(ctx context.Context, _ engine.TxOptions) (engine.Tx, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &txMy{tx: tx}, nil
}

type txMy struct {
	tx    *sql.Tx
	hecha bool
}

func (t *txMy) Exec(ctx context.Context, sql string) error {
	_, err := t.tx.ExecContext(ctx, sql)
	return err
}

func (t *txMy) Modify(ctx context.Context, sql string, args []any) (int64, error) {
	return filasDe(t.tx.ExecContext(ctx, sql, args...))
}

func (t *txMy) Commit(ctx context.Context) error {
	t.hecha = true
	return t.tx.Commit()
}

// Verify no tiene nada que adelantar: ni MySQL ni MariaDB tienen restricciones
// diferidas —`SET CONSTRAINTS` no existe y una clave foránea se comprueba fila
// por fila—, así que un COMMIT no puede fallar por algo que las sentencias no
// hayan fallado ya.
//
// De todos modos estos dos motores nunca llegan acá: el ensayo de S15 exige DDL
// transaccional y ellos no lo tienen.
func (t *txMy) Verify(context.Context) error { return nil }

// Rollback después de un Commit exitoso es un no-op, para que quien la abrió
// pueda hacer `defer tx.Rollback()` sin pensar. database/sql devolvería
// ErrTxDone, que no es un error real en ese caso.
func (t *txMy) Rollback(ctx context.Context) error {
	if t.hecha {
		return nil
	}
	err := t.tx.Rollback()
	if err == sql.ErrTxDone {
		return nil
	}
	return err
}

// RenderDDL ignora el contexto: acá es una función pura. Ver engine.Conn.
func (c *Conn) RenderDDL(_ context.Context, ch change.Change) (change.Statement, error) {
	return renderDDL(ch, c.server.Kind, c.sinEscapes)
}

func (c *Conn) ClassifyStatement(err error, desc string) *engine.Failure {
	return ClassifyStatement(err, desc)
}

func (c *Conn) Introspect(ctx context.Context) (*schema.Snapshot, error) {
	return introspect(ctx, c.db, c.server.CurrentDB)
}

func (c *Conn) Detail(ctx context.Context, esquema, tabla string) (*schema.TableDetail, error) {
	return detail(ctx, c.db, c.base(esquema), tabla, c.server.Kind, c.sinEscapes)
}

func (c *Conn) ColumnTypes(ctx context.Context) ([]schema.TypeOption, error) {
	return columnTypes(c.server.Kind), nil
}

func (c *Conn) PrimaryKeyColumns(ctx context.Context, esquema, tabla string) ([]string, error) {
	return primaryKeyColumns(ctx, c.db, c.base(esquema), tabla)
}

func (c *Conn) Run(ctx context.Context, sql string, opts engine.RunOptions) (*query.Batch, *engine.Failure) {
	return run(ctx, c.db, sql, c.Dialect(), opts)
}

func (c *Conn) Page(
	ctx context.Context, esquema, tabla string, opts engine.PageOptions,
) (*query.Result, *engine.Failure) {
	return page(ctx, c.db, c.base(esquema), tabla, opts)
}

func (c *Conn) Count(ctx context.Context, esquema, tabla string) (int64, *engine.Failure) {
	return count(ctx, c.db, c.base(esquema), tabla)
}
