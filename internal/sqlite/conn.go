package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Conn es la conexión al archivo detrás de la costura de engine.
type Conn struct {
	db     *sql.DB
	server *engine.ServerInfo
	desc   string

	unaVez sync.Once
}

var _ engine.Conn = (*Conn)(nil)

func (c *Conn) Kind() engine.Kind          { return engine.SQLite }
func (c *Conn) Caps() engine.Caps          { return engine.CapsOf(engine.SQLite) }
func (c *Conn) Server() *engine.ServerInfo { return c.server }
func (c *Conn) Close()                     { c.unaVez.Do(func() { _ = c.db.Close() }) }

// DB expone el *sql.DB para los tests del propio paquete. No lo usa el servicio.
func (c *Conn) DB() *sql.DB { return c.db }

// Exec corre una sentencia sin transacción.
//
// **No sirve para una sentencia con Statement.RebuildsTable puesto.** Una
// reconstrucción necesita las claves foráneas apagadas mientras corre, y eso
// hay que pedirlo antes de abrir la transacción: Begin con
// engine.TxOptions{RebuildsTables: true}. Mandada por acá funcionaría —sin dar
// ningún error— y borraría las filas de las tablas hijas. Ver
// TestElRebuildNoDisparaElCascade.
// Dialect: SQLite acepta corchetes para los identificadores —herencia de Access—
// y el cuerpo de un trigger va entre BEGIN y END. La barra invertida no escapa
// nada adentro de una cadena.
func (c *Conn) Dialect() query.Dialect {
	return query.Dialect{Brackets: true, Compound: true}
}

func (c *Conn) Exec(ctx context.Context, sql string) error {
	_, err := c.db.ExecContext(ctx, sql)
	return err
}

// Modify manda los valores como parámetros. SQLite les aplica la afinidad de
// la columna igual que a un literal: un "7" en una columna INTEGER se guarda
// como entero.
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

func (c *Conn) ClassifyStatement(err error, desc string) *engine.Failure {
	return ClassifyStatement(err, desc)
}

func (c *Conn) Introspect(ctx context.Context) (*schema.Snapshot, error) {
	return introspect(ctx, c.db, c.server.CurrentDB)
}

func (c *Conn) Detail(ctx context.Context, _, tabla string) (*schema.TableDetail, error) {
	return detail(ctx, c.db, tabla)
}

func (c *Conn) ColumnTypes(ctx context.Context) ([]schema.TypeOption, error) {
	return columnTypes(), nil
}

func (c *Conn) PrimaryKeyColumns(ctx context.Context, _, tabla string) ([]string, error) {
	return primaryKeyColumns(ctx, c.db, tabla)
}

func (c *Conn) Run(ctx context.Context, sql string, opts engine.RunOptions) (*query.Batch, *engine.Failure) {
	return run(ctx, c.db, sql, c.Dialect(), opts)
}

func (c *Conn) Page(
	ctx context.Context, _, tabla string, opts engine.PageOptions,
) (*query.Result, *engine.Failure) {
	return page(ctx, c.db, tabla, opts)
}

func (c *Conn) Count(ctx context.Context, _, tabla string) (int64, *engine.Failure) {
	return count(ctx, c.db, tabla)
}

func (c *Conn) RenderDDL(ctx context.Context, ch change.Change) (change.Statement, error) {
	return renderDDL(ctx, c.db, ch)
}

/* --------------------------------------------------------- transacciones */

// Begin abre una transacción.
//
// Toma una conexión DEDICADA del pool en vez de dejar que database/sql elija.
// Con una transacción normal daría igual —el Tx ya se queda con una— pero acá
// hace falta ejecutar un PRAGMA ANTES del BEGIN, y un PRAGMA es estado de
// conexión: mandado al pool podría aplicarse a otra.
func (c *Conn) Begin(ctx context.Context, opts engine.TxOptions) (engine.Tx, error) {
	cn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, err
	}

	t := &txLite{cn: cn}

	if opts.RebuildsTables {
		// El procedimiento que describe el manual de SQLite para cambiar una
		// tabla. El orden es lo que importa: foreign_keys tiene que apagarse
		// ANTES del BEGIN porque adentro de una transacción el pragma es un
		// no-op silencioso —no falla, no avisa, no hace nada—.
		//
		// Sin esto, el DROP TABLE del rebuild hace un DELETE implícito que
		// dispara los ON DELETE CASCADE de las tablas que la referencian, y las
		// filas hijas desaparecen sin un solo error. Ver engine.TxOptions.
		var antes string
		if err := cn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&antes); err != nil {
			cn.Close()
			return nil, fmt.Errorf("leer el estado de las claves foráneas: %w", err)
		}
		t.fkEstaban = encendido(antes)
		if t.fkEstaban {
			if _, err := cn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
				cn.Close()
				return nil, fmt.Errorf("apagar las claves foráneas: %w", err)
			}
			// Comprobar que se apagó de verdad. Si por lo que sea quedó
			// encendida, seguir sería reconstruir la tabla con el cuchillo
			// puesto: mejor no empezar.
			var ahora string
			if err := cn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&ahora); err != nil || encendido(ahora) {
				cn.Close()
				return nil, fmt.Errorf(
					"no se pudieron apagar las claves foráneas, y reconstruir una tabla " +
						"con ellas encendidas borra las filas de las tablas hijas")
			}
		}

		// Y la otra mitad, que no tiene nada que ver con las claves foráneas.
		//
		// Desde 3.25 un ALTER TABLE ... RENAME TO vuelve a analizar el esquema
		// ENTERO para arreglar las referencias al nombre viejo. En el medio del
		// rebuild la tabla original no existe —la acaba de tirar el paso
		// anterior— así que cualquier VISTA que la nombre hace fallar ese
		// análisis con «error in view vt: no such table». Sin esto, ningún
		// cambio que reconstruya se puede aplicar sobre una tabla que tenga una
		// vista encima, que no es un caso raro.
		//
		// legacy_alter_table apaga ese análisis. Acá es justo lo que se quiere:
		// las vistas siguen nombrando a la tabla por su nombre de siempre, y la
		// tabla vuelve a llamarse así tres sentencias después.
		if _, err := cn.ExecContext(ctx, "PRAGMA legacy_alter_table = ON"); err != nil {
			t.restaurar(ctx)
			cn.Close()
			return nil, fmt.Errorf("preparar el renombrado: %w", err)
		}
		t.legacy = true
		t.verifica = true
	}

	tx, err := cn.BeginTx(ctx, nil)
	if err != nil {
		t.restaurar(ctx)
		cn.Close()
		return nil, err
	}
	t.tx = tx
	return t, nil
}

type txLite struct {
	cn *sql.Conn
	tx *sql.Tx

	// verifica pide correr foreign_key_check antes del commit. Va de la mano
	// con haber apagado las claves: lo que se apaga hay que comprobarlo.
	verifica bool
	// fkEstaban recuerda si había que volver a encenderlas.
	fkEstaban bool
	// legacy recuerda que hay que devolver legacy_alter_table a su lugar.
	legacy bool

	hecha   bool
	cerrada bool
}

func (t *txLite) Exec(ctx context.Context, sql string) error {
	_, err := t.tx.ExecContext(ctx, sql)
	return err
}

func (t *txLite) Modify(ctx context.Context, sql string, args []any) (int64, error) {
	return filasDe(t.tx.ExecContext(ctx, sql, args...))
}

// Commit cierra la transacción, comprobando antes lo que se apagó para poder
// abrirla.
//
// El foreign_key_check va DENTRO de la transacción y su resultado decide si se
// commitea. Correrlo después sería inútil: para entonces ya estaría aplicado.
func (t *txLite) Commit(ctx context.Context) error {
	if t.verifica {
		if err := t.comprobarForaneas(ctx); err != nil {
			_ = t.tx.Rollback()
			t.terminar(ctx)
			return err
		}
	}
	err := t.tx.Commit()
	t.hecha = err == nil
	t.terminar(ctx)
	return err
}

// Verify corre el foreign_key_check sin commitear.
//
// Es exactamente la comprobación que Commit hace al final, adelantada: lo que
// una reconstrucción de tabla puede romper no se ve en ninguna sentencia
// —mientras la transacción corre las claves están apagadas— sino recién en este
// chequeo. Sin esto, el ensayo de S15 abriría, correría todo, revertiría y
// diría que salió bien.
//
// La transacción queda abierta y usable, que es lo que el ensayo necesita para
// después revertirla.
func (t *txLite) Verify(ctx context.Context) error {
	if !t.verifica {
		return nil
	}
	return t.comprobarForaneas(ctx)
}

// Rollback después de un Commit exitoso es un no-op, para que quien la abrió
// pueda hacer `defer tx.Rollback()` sin pensar.
func (t *txLite) Rollback(ctx context.Context) error {
	if t.hecha {
		t.terminar(ctx)
		return nil
	}
	err := t.tx.Rollback()
	t.terminar(ctx)
	if err == sql.ErrTxDone {
		return nil
	}
	return err
}

// comprobarForaneas corre PRAGMA foreign_key_check y arma un error legible con
// lo que encuentre.
//
// Es la contraparte de haber apagado las claves: mientras la transacción corre
// no se comprueba nada, así que se comprueba todo junto al final. Es la misma
// semántica que un DEFERRABLE INITIALLY DEFERRED de Postgres.
func (t *txLite) comprobarForaneas(ctx context.Context) error {
	rows, err := t.tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("comprobar las claves foráneas: %w", err)
	}
	defer rows.Close()

	// Las cuatro columnas son: tabla hija, rowid, tabla padre, índice de la
	// clave. El rowid puede venir NULL en tablas WITHOUT ROWID.
	var partes []string
	n := 0
	for rows.Next() {
		var hija, padre string
		var rowid sql.NullInt64
		var idx sql.NullInt64
		if err := rows.Scan(&hija, &rowid, &padre, &idx); err != nil {
			return fmt.Errorf("leer el resultado de foreign_key_check: %w", err)
		}
		n++
		if len(partes) < 5 {
			partes = append(partes, fmt.Sprintf("%s → %s", hija, padre))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	msg := fmt.Sprintf(
		"quedarían %d filas apuntando a algo que no existe (%s)", n, strings.Join(partes, ", "))
	if n > len(partes) {
		msg += " y otras"
	}
	return fmt.Errorf("%s; no se aplicó nada", msg)
}

func (t *txLite) terminar(ctx context.Context) {
	if t.cerrada {
		return
	}
	t.cerrada = true
	t.restaurar(ctx)
	_ = t.cn.Close()
}

// restaurar vuelve a encender las claves foráneas si estaban encendidas.
//
// Corre siempre, pase lo que pase, porque la conexión vuelve al pool: dejarla
// con las claves apagadas convertiría en escrituras sin control de integridad
// todas las siguientes que salieran por ella — y cuál sale por cuál no se
// puede saber desde afuera.
//
// El contexto se desengancha de la cancelación a propósito, y esto es lo que
// se comprobó al respecto: con el ctx cancelado, HOY el pragma corre igual,
// porque el driver de modernc no mira la cancelación para una sentencia
// trivial. O sea que no hay un test que distinga las dos versiones, y por eso
// no hay uno: sería un test que no puede fallar.
//
// Se desengancha igual porque de lo que depende no puede ser eso. Una limpieza
// que solo funciona mientras el driver ignore la cancelación es una limpieza
// que se rompe con una actualización del driver, y lo que quedaría del otro
// lado es una conexión en el pool sin control de integridad.
func (t *txLite) restaurar(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	if t.legacy {
		_, _ = t.cn.ExecContext(ctx, "PRAGMA legacy_alter_table = OFF")
	}
	if t.fkEstaban {
		_, _ = t.cn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	}
}
