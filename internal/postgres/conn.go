package postgres

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/dml"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Conn es la conexión a Postgres detrás de la costura de engine.
//
// Envuelve lo que ya existía en vez de reescribirlo: las funciones sueltas del
// paquete siguen siendo la implementación, y esto solo les pone la forma que
// pide la interfaz. Un motor que se agrega no obliga a tocar Postgres.
type Conn struct {
	pool   *pgxpool.Pool
	server *engine.ServerInfo
	desc   string

	// cerrar una sola vez: Close se llama desde el servicio y desde el defer
	// de quien abrió, y pgxpool.Close entra en pánico si se llama dos veces.
	unaVez sync.Once
}

// Verificación en tiempo de compilación. Sin esto, un cambio en la interfaz se
// descubre recién donde se usa, con un error mucho menos claro.
var _ engine.Conn = (*Conn)(nil)

// Open conecta y devuelve la conexión ya con la forma de la costura.
func Open(
	ctx context.Context, dsn, desc string, opts engine.OpenOptions,
) (*Conn, *engine.Failure) {
	var dial pgconn.DialFunc
	if opts.DialFunc != nil {
		dial = pgconn.DialFunc(opts.DialFunc)
	}
	pool, info, f := Connect(ctx, dsn, desc, ConnectOptions{
		MaxConns:         opts.MaxConns,
		ReadOnly:         opts.ReadOnly,
		StatementTimeout: opts.StatementTimeout,
		DialFunc:         dial,
		SessionSQL:       opts.SessionSQL,
	})
	if f != nil {
		return nil, f
	}
	return &Conn{pool: pool, server: info, desc: desc}, nil
}

// Pool expone el pool para lo que todavía no pasa por la costura.
//
// Es una fuga deliberada y acotada: el túnel SSH y algún test necesitan el pool
// crudo. No la usa el servicio.
func (c *Conn) Pool() *pgxpool.Pool { return c.pool }

func (c *Conn) Kind() Kind                 { return engine.Postgres }
func (c *Conn) Caps() engine.Caps          { return engine.CapsOf(engine.Postgres) }
func (c *Conn) Server() *engine.ServerInfo { return c.server }
func (c *Conn) Close()                     { c.unaVez.Do(func() { c.pool.Close() }) }

// Kind es el alias local, para no importar engine en cada firma.
type Kind = engine.Kind

func (c *Conn) Introspect(ctx context.Context) (*schema.Snapshot, error) {
	return Introspect(ctx, c.pool)
}

func (c *Conn) Detail(ctx context.Context, esquema, tabla string) (*schema.TableDetail, error) {
	return Detail(ctx, c.pool, esquema, tabla)
}

func (c *Conn) ColumnTypes(ctx context.Context) ([]schema.TypeOption, error) {
	return ColumnTypes(ctx, c.pool)
}

func (c *Conn) PrimaryKeyColumns(ctx context.Context, esquema, tabla string) ([]string, error) {
	return PrimaryKeyColumns(ctx, c.pool, esquema, tabla)
}

func (c *Conn) Objects(ctx context.Context, esquemas []string) ([]schema.Object, error) {
	return Objects(ctx, c.pool, esquemas)
}

func (c *Conn) AutoIncrement(col schema.DetailColumn) (string, bool) {
	return AutoIncrement(col)
}

func (c *Conn) Run(ctx context.Context, sql string, opts engine.RunOptions) (*query.Batch, *engine.Failure) {
	return Run(ctx, c.pool, sql, RunOptions{RowLimit: opts.RowLimit})
}

func (c *Conn) Page(
	ctx context.Context, esquema, tabla string, opts engine.PageOptions,
) (*query.Result, *engine.Failure) {
	return TableData(ctx, c.pool, esquema, tabla, TableDataOptions{
		OrderBy:    opts.OrderBy,
		Descending: opts.Descending,
		Limit:      opts.Limit,
		Offset:     opts.Offset,
		Where:      opts.Where,
	})
}

func (c *Conn) Count(
	ctx context.Context, esquema, tabla string, where []query.Condition,
) (int64, *engine.Failure) {
	return TableCount(ctx, c.pool, esquema, tabla, where)
}

func (c *Conn) Scan(
	ctx context.Context, esquema, tabla string, opts engine.ScanOptions,
) (engine.RowStream, error) {
	return Scan(ctx, c.pool, esquema, tabla, opts)
}

// RenderDDL ignora el contexto: en Postgres es una función pura. El parámetro
// está en la interfaz por SQLite, que necesita leer la definición actual de la
// tabla para reconstruirla.
func (c *Conn) RenderDDL(_ context.Context, ch change.Change) (change.Statement, error) {
	return RenderDDL(ch)
}

// Quoting expone el citado de este motor para el formato SQL de la exportación.
func (c *Conn) Quoting() engine.Quoting {
	return engine.Quoting{
		Table:   dialectoDML.Table,
		Ident:   dialectoDML.QuoteIdent,
		Literal: dialectoDML.QuoteLiteral,
	}
}

func (c *Conn) InsertBatch(
	esquema, tabla string, columnas []string, filas [][]*string, ignorar bool,
) (string, []any) {
	return dml.InsertBatch(esquema, tabla, columnas, filas, dialectoDML, ignorar)
}

func (c *Conn) ClassifyStatement(err error, desc string) *engine.Failure {
	return ClassifyStatement(err, desc)
}

// Dialect: Postgres no tiene acento invertido ni corchetes, la barra invertida
// no escapa nada adentro de una cadena —standard_conforming_strings—, y el
// cuerpo de una función va entre $$ en vez de entre BEGIN y END sueltos.
func (c *Conn) Dialect() query.Dialect {
	return query.Dialect{DollarQuotes: true}
}

func (c *Conn) Exec(ctx context.Context, sql string) error {
	_, err := c.pool.Exec(ctx, sql)
	return err
}

// Modify manda los valores como parámetros de texto.
//
// pgx envía un *string en formato texto sea cual sea el tipo del parámetro, y
// el servidor lo interpreta según la columna. Comprobado con numeric, boolean,
// timestamptz, jsonb, bytea e int[]: es la misma conversión que hace con un
// literal, sin que el literal exista.
func (c *Conn) Modify(ctx context.Context, sql string, args []any) (int64, error) {
	tag, err := c.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Begin ignora las opciones: Postgres hace ALTER de verdad, así que nunca
// reconstruye una tabla y no tiene nada que apagar antes del BEGIN.
func (c *Conn) Begin(ctx context.Context, _ engine.TxOptions) (engine.Tx, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &txPG{tx: tx}, nil
}

type txPG struct{ tx pgx.Tx }

func (t *txPG) Exec(ctx context.Context, sql string) error {
	_, err := t.tx.Exec(ctx, sql)
	return err
}
func (t *txPG) Modify(ctx context.Context, sql string, args []any) (int64, error) {
	tag, err := t.tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
func (t *txPG) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *txPG) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// Verify adelanta las restricciones diferidas.
//
// `SET CONSTRAINTS ALL IMMEDIATE` obliga a comprobar en el acto todas las que
// están DEFERRABLE INITIALLY DEFERRED, que si no se verificarían recién en el
// COMMIT. Es lo que hace que el ensayo de S15 no diga «va a andar» sobre una
// transacción que iba a fallar al cerrarse.
//
// Vale para la transacción entera y no se deshace, pero eso no molesta: en el
// ensayo lo que sigue es el ROLLBACK, y en un apply de verdad esto no se llama.
func (t *txPG) Verify(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	return err
}

func (c *Conn) CountWhere(ctx context.Context, esquema, tabla string, where []change.Cell) (int64, error) {
	sql, args := dml.CountWhere(esquema, tabla, where, dialectoDML)
	var n int64
	if err := c.pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (c *Conn) ObjectDefinition(ctx context.Context, o schema.Object) (schema.ObjectDefinition, error) {
	return Definition(ctx, c.pool, o)
}

func (c *Conn) Dependents(ctx context.Context, o schema.Object) (schema.Dependents, error) {
	return Dependents(ctx, c.pool, o)
}
