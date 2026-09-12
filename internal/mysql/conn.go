package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/dml"
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

// codigoClaveDuplicada es el único aviso que «saltear las que chocan» admite.
const codigoClaveDuplicada = 1062

// VerifySkipped revisa los avisos del último INSERT IGNORE de esta
// transacción: cualquiera que no sea un choque de clave —un valor inválido
// convertido, uno recortado, un NULL en NOT NULL reemplazado— es un error, y
// se devuelve con el texto del servidor para que se sepa qué fila y por qué.
//
// SHOW WARNINGS habla de la última sentencia de ESTA conexión, así que tiene
// que correr en la transacción, antes de cualquier otra cosa.
func (t *txMy) VerifySkipped(ctx context.Context) error {
	// SHOW WARNINGS primero y cualquier otra consulta después: en el driver,
	// un SELECT previo —aunque sea de @@warning_count— dejaba la lista vacía.
	rows, err := t.tx.QueryContext(ctx, "SHOW WARNINGS")
	if err != nil {
		return fmt.Errorf("leer los avisos del lote: %w", err)
	}
	defer rows.Close()
	vistos := 0
	for rows.Next() {
		var nivel, mensaje string
		var codigo int
		if err := rows.Scan(&nivel, &codigo, &mensaje); err != nil {
			return fmt.Errorf("leer un aviso del lote: %w", err)
		}
		vistos++
		if codigo != codigoClaveDuplicada {
			return &avisoDeImportacion{codigo: codigo, mensaje: mensaje}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_ = rows.Close()
	// Los que se produjeron contra los que se pudieron ver. El servidor guarda
	// hasta max_error_count avisos —y con max_error_count = 0, ninguno—, pero
	// @@warning_count cuenta todos y sobrevive al SHOW WARNINGS (comprobado
	// contra MySQL 9.7). Si hubo más de los que se vieron, los que faltan
	// pudieron ser cualquier cosa. Se lee DESPUÉS del SHOW: antes, lo vaciaba.
	var total int
	if err := t.tx.QueryRowContext(ctx, "SELECT @@warning_count").Scan(&total); err != nil {
		return fmt.Errorf("leer la cuenta de avisos: %w", err)
	}
	if total > vistos {
		return fmt.Errorf("el lote produjo %d avisos y el servidor mostró %d: no se puede "+
			"comprobar que todos sean choques de clave. Importá en lotes más chicos o subí "+
			"max_error_count", total, vistos)
	}
	return nil
}

// avisoDeImportacion es un aviso que IGNORE tapó y que no era un choque.
type avisoDeImportacion struct {
	codigo  int
	mensaje string
}

func (a *avisoDeImportacion) Error() string {
	return fmt.Sprintf("«saltear las que chocan» saltea solo los choques de clave, y una fila "+
		"produjo otra cosa (código %d): %s", a.codigo, a.mensaje)
}

// Verify no tiene nada que adelantar: ni MySQL ni MariaDB tienen restricciones
// diferidas —`SET CONSTRAINTS` no existe y una clave foránea se comprueba fila
// por fila—, así que un COMMIT no puede fallar por algo que las sentencias no
// hayan fallado ya.
//
// Sí se llega acá: el ensayo de S15 corre contra estos dos motores cuando el
// changeset es de puros datos, que es un solo tramo transaccional.
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

// Quoting expone el citado de este motor para el formato SQL de la exportación.
//
// El citado de literales depende del SERVIDOR —con NO_BACKSLASH_ESCAPES la
// barra invertida no escapa nada—, así que sale de la conexión y no de una
// función suelta.
func (c *Conn) Quoting() engine.Quoting {
	d := dialectoDML(func(s string) string { return quoteString(s, c.sinEscapes) })
	return engine.Quoting{
		Table: d.Table, Ident: d.QuoteIdent, Literal: d.QuoteLiteral,
		// El recorrido entrega los binarios en hexadecimal (ver textoDe).
		Binary: func(hex string) string { return "X'" + hex + "'" },
	}
}

func (c *Conn) InsertBatch(
	esquema, tabla string, columnas []string, filas [][]*string, ignorar bool,
) (string, []any) {
	cita := func(s string) string { return quoteString(s, c.sinEscapes) }
	// Sigue siendo INSERT IGNORE, con una condición: IGNORE degrada a aviso
	// TODOS los errores de datos —'abc' en un INT entra como 0, un valor fuera
	// de rango se recorta— y la fila se contaba como insertada (C-04 de la
	// auditoría del 2026-09-11). Por eso la transacción de la importación
	// revisa los avisos después de cada lote (VerifySkipped) y falla con
	// cualquiera que no sea un choque de clave. ON DUPLICATE KEY UPDATE no
	// servía: con clientFoundRows, que esta conexión pone a propósito, las
	// filas salteadas cuentan como afectadas.
	return dml.InsertBatch(c.base(esquema), tabla, columnas, filas, dialectoDML(cita), ignorar)
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

func (c *Conn) Objects(ctx context.Context, esquemas []string) ([]schema.Object, error) {
	// Un esquema vacío es «la base abierta»: en MySQL las dos cosas son lo
	// mismo, y sin esto la lista saldría vacía y el archivo se vería como uno
	// que no deja nada afuera.
	pedidos := make([]string, 0, len(esquemas))
	for _, e := range esquemas {
		pedidos = append(pedidos, c.base(e))
	}
	if len(pedidos) == 0 {
		pedidos = append(pedidos, c.base(""))
	}
	return Objects(ctx, c.db, pedidos)
}

func (c *Conn) PrimaryKeyColumns(ctx context.Context, esquema, tabla string) ([]string, error) {
	return primaryKeyColumns(ctx, c.db, c.base(esquema), tabla)
}

func (c *Conn) Run(ctx context.Context, sql string, opts engine.RunOptions) (*query.Batch, *engine.Failure) {
	return run(ctx, c.db, sql, c.Dialect(), opts)
}

// Dedicated toma una conexión del pool para una pestaña del editor.
func (c *Conn) Dedicated(ctx context.Context) (engine.Session, error) {
	cn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	return &sesionMy{db: c.db, cn: cn, d: c.Dialect()}, nil
}

// sesionMy es una conexión retenida. MySQL no dice si hay una transacción
// abierta sin una consulta más, así que se sigue por las sentencias que
// pasaron; un DDL la cierra con commit implícito.
type sesionMy struct {
	db     *sql.DB
	cn     *sql.Conn
	d      query.Dialect
	enTx   bool
	muerta bool
}

func (s *sesionMy) Run(ctx context.Context, sql string, opts engine.RunOptions) (*query.Batch, *engine.Failure) {
	lote, f := runEn(ctx, s.db, s.cn, sql, s.d, opts)
	cmd := query.Command(sql, s.d)
	switch {
	case f == nil:
		s.enTx = engine.TransactionAfter(cmd, query.Trim(sql, s.d), s.enTx, true)
	case f.Kind == engine.FailureCanceled || strings.Contains(f.Detail, driver.ErrBadConn.Error()):
		// El driver cierra la conexión al cancelar: lo que hubiera abierto
		// ya no existe, y la conexión tampoco.
		s.enTx, s.muerta = false, true
	case cmd == "CREATE" || cmd == "ALTER" || cmd == "DROP" || cmd == "TRUNCATE" || cmd == "RENAME":
		// El commit implícito de un DDL pasa ANTES de ejecutarlo: un CREATE
		// que falla ya confirmó lo anterior.
		s.enTx = false
	}
	return lote, f
}

func (s *sesionMy) InTransaction() bool { return s.enTx }
func (s *sesionMy) Alive() bool         { return !s.muerta }

func (s *sesionMy) Close(ctx context.Context) error {
	var err error
	if s.enTx && !s.muerta {
		plazo, cancelar := context.WithTimeout(context.WithoutCancel(ctx), engine.CierreDeSesion)
		_, err = s.cn.ExecContext(plazo, "ROLLBACK")
		cancelar()
	}
	if e := s.cn.Close(); err == nil && !s.muerta {
		err = e
	}
	return err
}

func (c *Conn) Page(
	ctx context.Context, esquema, tabla string, opts engine.PageOptions,
) (*query.Result, *engine.Failure) {
	return page(ctx, c.db, c.base(esquema), tabla, opts)
}

func (c *Conn) Count(
	ctx context.Context, esquema, tabla string, where []query.Condition,
) (int64, *engine.Failure) {
	return count(ctx, c.db, c.base(esquema), tabla, where)
}

func (c *Conn) Scan(
	ctx context.Context, esquema, tabla string, opts engine.ScanOptions,
) (engine.RowStream, error) {
	return scan(ctx, c.db, c.base(esquema), tabla, opts)
}

func (c *Conn) CountWhere(ctx context.Context, esquema, tabla string, where []change.Cell) (int64, error) {
	cita := func(s string) string { return quoteString(s, c.sinEscapes) }
	sql, args := dml.CountWhere(c.base(esquema), tabla, where, dialectoDML(cita))
	var n int64
	if err := c.db.QueryRowContext(ctx, sql, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (c *Conn) AutoIncrement(col schema.DetailColumn) (string, bool) {
	return AutoIncrement(col)
}

// DumpHints no necesita nada: InnoDB ajusta el contador de AUTO_INCREMENT con
// los INSERT explícitos.
func (c *Conn) DumpHints(schema.TableDetail) engine.DumpHints { return engine.DumpHints{} }

func (c *Conn) ObjectDefinition(ctx context.Context, o schema.Object) (schema.ObjectDefinition, error) {
	return Definition(ctx, c.db, c.conEsquema(o))
}

// conEsquema completa el esquema del objeto con la base en curso.
//
// `Objects` ya lo hace con `c.base("")` y esto es la otra mitad: sin él, un
// objeto que llegue sin esquema —porque la conexión ya está parada en su base—
// armaba un `SHOW CREATE VIEW .`v“, con el punto suelto adelante.
func (c *Conn) conEsquema(o schema.Object) schema.Object {
	o.Schema = c.base(o.Schema)
	return o
}

func (c *Conn) Dependents(ctx context.Context, o schema.Object) (schema.Dependents, error) {
	return Dependents(ctx, c.db, c.conEsquema(o))
}
