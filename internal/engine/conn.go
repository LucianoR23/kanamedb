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

	// Where son las condiciones del filtro de la grilla. Vacío es la tabla
	// entera. Los valores viajan como parámetros: lo único que entra en el
	// texto de la consulta es el nombre de la columna, citado.
	Where []query.Condition
}

// ScanOptions es cómo recorrer una tabla entera.
//
// No tiene Limit ni Offset a propósito: recorrer es lo contrario de paginar.
// Paginar una tabla grande con LIMIT/OFFSET obliga al servidor a releer y
// descartar las filas anteriores en cada página —y sin un orden total, además,
// repite y saltea—; una sola consulta que se lee a medida que llega no tiene
// ninguno de los dos problemas.
type ScanOptions struct {
	// OrderBy son las columnas por las que ordenar. Vacío significa sin ORDER
	// BY, que para un recorrido completo está bien: salen todas igual. Se
	// ordena cuando el archivo tiene que ser comparable entre dos corridas.
	OrderBy    []string
	Descending bool

	// Where son las condiciones del filtro, igual que en PageOptions: exportar
	// «lo que se está mirando» es exportar la tabla con el mismo filtro puesto.
	Where []query.Condition
}

// RowStream entrega las filas de una lectura larga a medida que llegan.
//
// Existe porque una tabla de dos millones de filas no entra en un
// query.Result: exportar tiene que escribir mientras lee. El uso es el de
// database/sql —Next hasta que da false, y después Err— y Close es
// obligatorio: la consulta sigue abierta del lado del servidor hasta que se
// cierra.
//
// Row devuelve una rebanada NUEVA en cada fila, no una reusada: el que la
// recibe puede guardarla sin copiarla. Se probó al revés —reusar la rebanada,
// como hace sql.RawBytes— y ahorraba una asignación de las N+1 de cada fila,
// porque los valores hay que copiarlos igual; no vale la trampa que deja.
type RowStream interface {
	// Columns son las columnas del recorrido. Están desde antes de la primera
	// fila: el escritor de la exportación necesita el encabezado para empezar.
	Columns() []query.Column
	// Next avanza a la fila siguiente. False es fin o error; Err lo dice.
	Next() bool
	// Row son los valores de la fila actual, como texto del servidor. nil es
	// NULL, igual que en query.Result.
	Row() []*string
	// Err es el error que cortó el recorrido, o nil si terminó entero.
	Err() error
	// Close libera la consulta. Idempotente, y se puede llamar sin haber
	// terminado de leer.
	Close()
}

// Quoting es cómo este motor escribe un nombre y un valor en SQL para LEER.
//
// Existe para el formato de exportación «SQL inserts», que es el único lugar
// donde un valor de fila se escribe adentro de la SQL — y es legítimo porque
// esa SQL no la ejecuta Kaname: es un archivo. Todo lo que Kaname ejecuta va
// con parámetros.
//
// Se devuelven funciones y no un formato armado para que la costura no sepa
// qué formatos hay: lo único que el motor aporta es cómo se cita acá.
type Quoting struct {
	// Table arma el nombre calificado de la tabla, con la regla del motor
	// sobre qué hacer con el esquema.
	Table func(esquema, tabla string) string
	// Ident cita el nombre de una columna.
	Ident func(string) string
	// Literal cita un texto como literal.
	Literal func(string) string
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

	// Uncovered lista lo que hay en estos esquemas y el volcado de estructura
	// NO sabe escribir: vistas, funciones, triggers, políticas, tipos.
	//
	// Es la condición para que el volcado de estructura exista. El problema de
	// un export de esquema no es la dificultad —el DDL de una tabla ya se
	// renderiza— sino el silencio: uno que se olvida de una política de RLS se
	// ve idéntico a uno correcto. Cada motor sabe qué puede haber en él, así
	// que la lista sale de acá y no de una constante en el servicio.
	Uncovered(ctx context.Context, esquemas []string) ([]schema.Object, error)

	// Quoting es cómo este motor cita nombres y valores. Ver Quoting.
	Quoting() Quoting

	// InsertBatch escribe UN insert con varias filas, con los valores como
	// parámetros. Es lo que usa la importación de CSV.
	//
	// `ignorar` pide que una fila que choca con otra se saltee en vez de
	// abortar, y la cláusula que hace eso es distinta en cada motor: por eso
	// sale de acá y no de quien llama.
	InsertBatch(esquema, tabla string, columnas []string, filas [][]*string, ignorar bool) (string, []any)

	// Dialect es cómo se LEE el texto de este motor: qué delimita una cadena,
	// dónde empieza un comentario, si el cuerpo de un trigger va entre BEGIN y
	// END. Lo usa el editor para partir el texto en sentencias.
	//
	// Se pide a la conexión y no al Kind porque una de las banderas depende del
	// SERVIDOR: con NO_BACKSLASH_ESCAPES, la barra invertida no escapa nada.
	Dialect() query.Dialect

	// Run ejecuta UNA sentencia escrita por el usuario, tal como la escribió.
	//
	// Una y no varias: el editor parte el texto antes de llegar acá. Ver
	// query.Split y § 6 del plan.
	Run(ctx context.Context, sql string, opts RunOptions) (*query.Batch, *Failure)
	// Page lee una página de una tabla.
	Page(ctx context.Context, esquema, tabla string, opts PageOptions) (*query.Result, *Failure)
	// Count cuenta las filas de una tabla, exacto. Con condiciones cuenta las
	// que pasan el filtro, que es el número que la grilla muestra al lado de
	// «de N filas».
	Count(ctx context.Context, esquema, tabla string, where []query.Condition) (int64, *Failure)
	// Scan recorre una tabla ENTERA sin juntarla en memoria. Es lo que usa la
	// exportación; la grilla usa Page, que trae una página y para.
	Scan(ctx context.Context, esquema, tabla string, opts ScanOptions) (RowStream, error)
	// CountWhere cuenta las filas que coinciden con los valores dados, que
	// viajan como parámetros. Es lo que la revisión de una fila usa para
	// decir si su clave identifica una sola, si el padre de su clave foránea
	// existe y cuántas hijas arrastraría un borrado. Sin condiciones no
	// cuenta nada.
	CountWhere(ctx context.Context, esquema, tabla string, where []change.Cell) (int64, error)

	// RenderDDL escribe una operación del changeset como SQL de este motor.
	// Es lo único que sabe citar identificadores, y por eso la SQL nunca se
	// arma en el frontend. Ver CLAUDE.md.
	//
	// Se llama DDL por lo que era, pero también escribe los cambios de DATOS
	// —insertar, actualizar y borrar una fila—, que salen con Statement.Bound
	// puesto: la forma con parámetros, que es la que se ejecuta. Ver dml.
	//
	// Recibe un contexto porque en SQLite no es una función pura: casi
	// cualquier cambio de columna se hace reconstruyendo la tabla, y para
	// escribir la definición nueva hay que leer la que hay. Los otros tres
	// motores no lo usan.
	RenderDDL(ctx context.Context, c change.Change) (change.Statement, error)
	// ClassifyStatement interpreta el error de una sentencia que se estaba
	// ejecutando. Cada motor tiene su vocabulario de códigos.
	ClassifyStatement(err error, desc string) *Failure

	// Exec corre una sentencia de ESQUEMA sin transacción, tal cual está
	// escrita.
	Exec(ctx context.Context, sql string) error
	// Modify corre una sentencia de DATOS sin transacción, con sus valores
	// como parámetros, y devuelve cuántas filas tocó. Ver Tx.Modify.
	Modify(ctx context.Context, sql string, args []any) (int64, error)
	// Begin abre una transacción. Los motores sin DDL transaccional la
	// soportan igual: lo que no soportan es meter DDL adentro, y de eso se
	// encarga TramosDe.
	Begin(ctx context.Context, opts TxOptions) (Tx, error)
}

// TxOptions es lo que la transacción necesita saber de lo que va a ir adentro.
//
// Tiene un solo campo y no es un descuido: es la única cosa que un motor
// necesita preparar ANTES del BEGIN y que no se puede decidir después.
type TxOptions struct {
	// RebuildsTables avisa que adentro va a haber una reconstrucción de tabla
	// —crear una nueva, copiar, tirar la vieja y renombrar—.
	//
	// Solo SQLite lo mira, y sin esto pierde datos en silencio. El motivo es
	// una cadena de tres hechos que por separado parecen inofensivos:
	//
	//  1. Con foreign_keys encendido, un DROP TABLE hace un DELETE implícito,
	//     así que dispara los ON DELETE CASCADE de las tablas que la apuntan.
	//  2. PRAGMA foreign_keys es un NO-OP adentro de una transacción, así que
	//     no se puede apagar desde donde haría falta.
	//  3. PRAGMA foreign_key_check DESPUÉS del rebuild devuelve cero filas,
	//     porque las hijas no quedaron huérfanas: quedaron borradas.
	//
	// Comprobado: reconstruir una tabla con foreign_keys encendido borra las
	// filas de las tablas hijas sin decir nada. Ver rebuild_test.go.
	//
	// Con esto en true, SQLite apaga foreign_keys antes del BEGIN, corre
	// foreign_key_check antes del COMMIT —y se niega a commitear si encuentra
	// algo— y lo vuelve a encender al terminar. Es el procedimiento que el
	// propio manual de SQLite describe.
	//
	// Los otros tres motores hacen ALTER de verdad y no tienen nada que
	// apagar, así que lo ignoran.
	RebuildsTables bool
}

// Tx es una transacción abierta.
type Tx interface {
	// Exec corre una sentencia de esquema tal cual está escrita.
	Exec(ctx context.Context, sql string) error

	// Modify corre una sentencia de datos con sus valores como parámetros y
	// devuelve cuántas filas tocó.
	//
	// Son dos métodos y no uno con argumentos opcionales porque son dos
	// contratos: el DDL no tiene valores que parametrizar ni filas que contar,
	// y el DML no puede correr sin las dos cosas. El conteo es lo que permite
	// exigir que un UPDATE por clave toque exactamente una fila.
	//
	// Cuenta las filas que la sentencia ALCANZÓ, no las que cambiaron de
	// valor. En Postgres y SQLite es lo único que hay; en MySQL y MariaDB hay
	// que pedirlo —clientFoundRows— y sin eso un UPDATE que deja el mismo
	// valor cuenta cero, que acá se leería como «la fila ya no está».
	Modify(ctx context.Context, sql string, args []any) (int64, error)

	Commit(ctx context.Context) error
	// Rollback después de un Commit exitoso no es un error: es un no-op, para
	// que quien la abrió pueda hacer `defer tx.Rollback()` sin pensar.
	Rollback(ctx context.Context) error

	// Verify dispara ACÁ las comprobaciones que el motor deja para el COMMIT,
	// sin commitear.
	//
	// Existe por el ensayo de S15, y sin esto el ensayo mentiría en el peor
	// sentido posible: diría «va a andar» y el apply fallaría. Hay errores que
	// el motor NO levanta en la sentencia sino recién al cerrar —las claves
	// DEFERRABLE INITIALLY DEFERRED de Postgres, y en SQLite el
	// foreign_key_check que cierra una reconstrucción de tabla—, así que una
	// transacción que se abre, corre todo y se revierte nunca los ve.
	//
	// Un motor sin nada diferido devuelve nil. Después de Verify la
	// transacción sigue abierta y usable.
	Verify(ctx context.Context) error
}
