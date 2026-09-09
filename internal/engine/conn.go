package engine

import (
	"context"
	"net"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/query"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// DialFunc reemplaza cómo se abre el socket hacia la base.
//
// Es lo que hace posible el túnel SSH sin abrir ningún puerto local. Un túnel
// se implementa habitualmente escuchando en 127.0.0.1 y reenviando, pero un
// puerto en loopback es alcanzable desde cualquier pestaña del navegador — la
// misma razón por la que esta aplicación no tiene servidor HTTP. Con esto, la
// conexión existe solo dentro del proceso.
//
// Está acá y no en cada motor porque el túnel es uno solo: los tres motores de
// servidor lo atraviesan igual. La firma es la de pgconn.DialFunc, que es la
// que ya usaba el código, y el driver de MySQL acepta lo mismo registrando un
// dialer propio.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// OpenOptions son las decisiones de la conexión que valen para cualquier motor.
type OpenOptions struct {
	MaxConns int32

	// ReadOnly se lo pide AL SERVIDOR, no se comprueba acá. La diferencia
	// importa: el servidor rechaza toda escritura, incluidas las que no pasen
	// por nuestro código. Una comprobación nuestra sería un cartel.
	ReadOnly bool

	// StatementTimeout corta del lado del servidor. Cancelar desde el cliente
	// depende de que el cliente siga vivo; esto no.
	StatementTimeout time.Duration

	// DialFunc nil usa el discado normal del driver.
	DialFunc DialFunc
}

// RunOptions son las opciones de ejecutar SQL escrita por el usuario.
type RunOptions struct {
	// RowLimit corta la lectura. Cero usa el default del motor; negativo lee
	// todo.
	RowLimit int
}

// PageOptions son las opciones de leer una página de una tabla.
type PageOptions struct {
	// OrderBy son las columnas por las que ordenar. Vacío significa sin ORDER
	// BY, y entonces el paginado no es confiable.
	OrderBy    []string
	Descending bool

	Limit  int
	Offset int
}

// Conn es una conexión abierta a una base, del motor que sea.
//
// Es la superficie completa que `internal/service` necesita, y por eso está
// escrita en términos de los tipos propios del proyecto —schema, query,
// change— y no de los del driver. Que un *pgxpool.Pool asomara por acá
// obligaría a MySQL a fingir que es Postgres.
//
// Un motor que no sepa hacer algo devuelve *ErrUnsupported. No debería llegar
// a pasar: la interfaz solo ofrece lo que Caps declara.
type Conn interface {
	// Kind es qué motor es esta conexión.
	Kind() Kind
	// Caps son sus capacidades. Se pide a la conexión y no al Kind porque
	// algunas dependen de la VERSIÓN del servidor, no solo del motor.
	Caps() Caps
	// Server es lo que se leyó al conectar.
	Server() *ServerInfo
	// Close libera el pool. Idempotente.
	Close()

	// Introspect lee el esquema completo para el árbol y el diagrama.
	Introspect(ctx context.Context) (*schema.Snapshot, error)
	// Detail lee todo lo de UNA tabla: columnas, índices, claves,
	// restricciones y triggers.
	Detail(ctx context.Context, esquema, tabla string) (*schema.TableDetail, error)
	// ColumnTypes son los tipos que se pueden elegir para una columna, leídos
	// del catálogo de esta base y no de una lista en el código.
	ColumnTypes(ctx context.Context) ([]schema.TypeOption, error)
	// PrimaryKeyColumns es por dónde ordenar para que el paginado sea estable.
	PrimaryKeyColumns(ctx context.Context, esquema, tabla string) ([]string, error)

	// Run ejecuta SQL escrita por el usuario, tal como la escribió.
	Run(ctx context.Context, sql string, opts RunOptions) (*query.Batch, *Failure)
	// Page lee una página de una tabla.
	Page(ctx context.Context, esquema, tabla string, opts PageOptions) (*query.Result, *Failure)
	// Count cuenta las filas de una tabla, exacto.
	Count(ctx context.Context, esquema, tabla string) (int64, *Failure)

	// RenderDDL escribe una operación del changeset como SQL de este motor.
	// Es lo único que sabe citar identificadores, y por eso la SQL nunca se
	// arma en el frontend. Ver CLAUDE.md.
	RenderDDL(c change.Change) (change.Statement, error)
	// ClassifyStatement interpreta el error de una sentencia que se estaba
	// ejecutando. Cada motor tiene su vocabulario de códigos.
	ClassifyStatement(err error, desc string) *Failure

	// Exec corre una sentencia sin transacción.
	Exec(ctx context.Context, sql string) error
	// Begin abre una transacción. Los motores sin DDL transaccional la
	// soportan igual: lo que no soportan es meter DDL adentro, y de eso se
	// encarga TramosDe.
	Begin(ctx context.Context) (Tx, error)
}

// Tx es una transacción abierta.
type Tx interface {
	Exec(ctx context.Context, sql string) error
	Commit(ctx context.Context) error
	// Rollback después de un Commit exitoso no es un error: es un no-op, para
	// que quien la abrió pueda hacer `defer tx.Rollback()` sin pensar.
	Rollback(ctx context.Context) error
}
