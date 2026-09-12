package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// DefaultRowLimit es el corte cuando la conexión no fija uno.
//
// Mil filas entran en la grilla sin que se note y no llenan la memoria con el
// resultado de un `select * from` contra una tabla grande escrito de apuro. Es
// un corte de lectura, no un LIMIT agregado a la consulta: la consulta que el
// usuario escribió se manda tal cual, y lo que se corta es cuánto traemos.
const DefaultRowLimit = 1000

// RunOptions ajusta una ejecución.
type RunOptions struct {
	// RowLimit corta la lectura. Cero usa DefaultRowLimit; negativo lee todo.
	//
	// Coincide con la convención de connection.Safety, donde cero significa
	// "usá el default" y -1 significa "sin límite", de modo que el valor se
	// pasa derecho desde la conexión sin traducir.
	RowLimit int
}

// Run ejecuta una sentencia y devuelve el resultado listo para la grilla.
//
// La SQL se manda tal como la escribió el usuario. No se envuelve, no se le
// agrega LIMIT y no se parsea: un editor de SQL que reescribe lo que ejecutás
// es un editor en el que no se puede confiar, y cualquier reescritura nuestra
// se equivocaría con CTEs, UNION o funciones que devuelven conjuntos.
//
// Se usa el protocolo simple a propósito. Es el que usa psql, no crea sentencias
// preparadas —que ensucian el servidor con SQL que se escribe una vez— y, sobre
// todo, garantiza que el servidor devuelva todo en formato texto. Ver leerFilas.
func Run(ctx context.Context, pool *pgxpool.Pool, sql string, opts RunOptions) (*query.Batch, *Failure) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, Classify(err, "la consulta")
	}
	defer conn.Release()
	return runEn(ctx, conn, sql, opts)
}

// runEn corre la sentencia en una conexión ya tomada: la de Run, que la
// devuelve enseguida, o la de una Session, que la retiene.
func runEn(ctx context.Context, conn *pgxpool.Conn, sql string, opts RunOptions) (*query.Batch, *Failure) {
	limite := opts.RowLimit
	if limite == 0 {
		limite = DefaultRowLimit
	}
	arranque := time.Now()

	// De acá en adelante el error es de la SENTENCIA, no de la conexión, y se
	// clasifica con ClassifyStatement: con Classify, un error de sintaxis, una
	// tabla inexistente o una división por cero se mostraban todos como «El
	// servidor rechazó la conexión con la consulta» (comprobado el 2026-09-12
	// al atender la auditoría; no estaba en ella).

	// Exec del protocolo simple y no pool.Query: Query devuelve UN resultado y
	// descarta los demás en silencio. Con `select 1; drop table x;` mostraría la
	// fila del select como si el drop no hubiera existido —y el drop se ejecuta
	// igual, porque el servidor recibe el lote entero—. Exec expone todos.
	mrr := conn.Conn().PgConn().Exec(ctx, sql)

	lote := &query.Batch{Results: []query.Result{}}
	// Los OID de cada resultado, en paralelo, para resolver después los tipos
	// que pgx no conoce. No van en Result porque a la interfaz no le sirven.
	var oidsPorResultado [][]uint32

	for mrr.NextResult() {
		rr := mrr.ResultReader()
		campos := rr.FieldDescriptions()

		res := query.Result{Rows: [][]*string{}, RowLimit: limite}
		oids := make([]uint32, len(campos))
		if len(campos) > 0 {
			res.Columns = make([]query.Column, len(campos))
			res.ReturnsRows = true
			tm := conn.Conn().TypeMap()
			for i, f := range campos {
				res.Columns[i] = columnaDe(f, tm)
				oids[i] = f.DataTypeOID
			}
		}

		for rr.NextRow() {
			if limite >= 0 && len(res.Rows) >= limite {
				res.Truncated = true
				// Se sigue leyendo hasta el final del resultado igual: cortar acá
				// dejaría la conexión con datos pendientes y la próxima consulta
				// leería los de esta.
				continue
			}
			crudas := rr.Values()
			fila := make([]*string, len(crudas))
			for i, b := range crudas {
				// NULL viaja con longitud -1 y la cadena vacía con longitud 0.
				// El protocolo las distingue y acá se preserva: un
				// `if len(b) == 0` las colapsaría y la grilla mostraría las dos
				// como celda en blanco.
				if b == nil {
					continue
				}
				// Se copia porque los bytes solo valen hasta el próximo NextRow.
				s := string(b)
				fila[i] = &s
			}
			res.Rows = append(res.Rows, fila)
		}

		tag, err := rr.Close()
		if err != nil {
			lote.ElapsedMs = time.Since(arranque).Milliseconds()
			return lote, conAvisoDeLote(ClassifyStatement(err, "la consulta"), lote.Results)
		}
		res.Command = tag.String()
		res.AffectedRows = tag.RowsAffected()
		lote.Results = append(lote.Results, res)
		oidsPorResultado = append(oidsPorResultado, oids)
	}

	if err := mrr.Close(); err != nil {
		lote.ElapsedMs = time.Since(arranque).Milliseconds()
		return lote, conAvisoDeLote(ClassifyStatement(err, "la consulta"), lote.Results)
	}
	lote.ElapsedMs = time.Since(arranque).Milliseconds()

	// Los tipos que pgx no conoce —enums, dominios, tipos del usuario— se
	// resuelven contra el catálogo, y recién cuando aparecen. La consulta normal
	// devuelve tipos incorporados y no paga ningún viaje extra.
	//
	// Con `conn`, la que ya se tiene, y NO con `pool`: acá la conexión sigue
	// tomada (el Release es diferido), así que pedir otra al pool con
	// PoolSize 1 se colgaba hasta cancelar, y con dos pestañas sobre un pool
	// de 2 las dos esperaban a la otra (C-15 de la auditoría del 2026-09-11).
	// `columnasDe` en scan.go ya lo hacía bien por el mismo motivo.
	for i := range lote.Results {
		if err := resolverTiposDesconocidos(ctx, conn, &lote.Results[i], oidsPorResultado[i]); err != nil {
			return nil, Classify(err, "los tipos del resultado")
		}
	}
	return lote, nil
}

// consultador es lo mínimo que hace falta para preguntarle algo al catálogo:
// un pool o UNA conexión ya tomada del pool.
//
// La distinción no es cosmética. Un recorrido largo (Scan) se queda con una
// conexión durante minutos; si además pidiera otra al pool para resolver los
// tipos, con un pool de dos —que es lo que se abre en una conexión de solo
// lectura— dos exportaciones a la vez se quedarían esperando una a la otra.
// Recibiendo la conexión que ya se tomó, no hay una segunda.
type consultador interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// columnaDe traduce la descripción del campo a lo que necesita la interfaz.
func columnaDe(f pgconn.FieldDescription, tm *pgtype.Map) query.Column {
	col := query.Column{Name: f.Name, Class: claseDeOID(f.DataTypeOID)}
	if t, ok := tm.TypeForOID(f.DataTypeOID); ok {
		col.DataType = t.Name
		// Postgres nombra los arrays de un tipo incorporado con guion bajo
		// adelante: text[] es _text. Es la única señal que da el nombre, y
		// enumerar los OID de array de cada tipo se desactualiza solo.
		if strings.HasPrefix(t.Name, "_") {
			col.Class = query.ClassArray
		}
	}
	return col
}

// claseDeOID clasifica los tipos incorporados.
//
// Solo los que cambian cómo se muestra la columna. Todo lo demás cae en
// ClassOther y se muestra como texto, que es lo correcto: inventar una
// categoría para un tipo que no conocemos es peor que admitir que no lo
// conocemos.
func claseDeOID(oid uint32) query.Class {
	switch oid {
	case pgtype.BoolOID:
		return query.ClassBool
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID,
		pgtype.Float4OID, pgtype.Float8OID, pgtype.NumericOID,
		pgtype.OIDOID, pgtype.XIDOID, pgtype.CIDOID:
		return query.ClassNumber
	case pgtype.TextOID, pgtype.VarcharOID, pgtype.BPCharOID, pgtype.NameOID,
		pgtype.UUIDOID, pgtype.InetOID, pgtype.CIDROID, pgtype.MacaddrOID:
		return query.ClassText
	case pgtype.DateOID, pgtype.TimeOID, pgtype.TimestampOID,
		pgtype.TimestamptzOID, pgtype.IntervalOID, pgtype.TimetzOID:
		return query.ClassTemporal
	case pgtype.JSONOID, pgtype.JSONBOID:
		return query.ClassJSON
	case pgtype.ByteaOID:
		return query.ClassBinary
	}
	return query.ClassOther
}

// resolverTiposDesconocidos completa el nombre del tipo de las columnas que pgx
// no reconoció, y las clasifica por la categoría que declara el catálogo.
//
// Una columna de un enum llegaba acá con DataType vacío, y "sin nombre de tipo"
// en el encabezado de la grilla no le sirve a nadie.
func resolverTiposDesconocidos(ctx context.Context, q consultador, res *query.Result, oids []uint32) error {
	var faltantes []int64
	pendientes := map[uint32][]int{}
	for i, c := range res.Columns {
		if c.DataType != "" {
			continue
		}
		oid := oids[i]
		if _, visto := pendientes[oid]; !visto {
			faltantes = append(faltantes, int64(oid))
		}
		pendientes[oid] = append(pendientes[oid], i)
	}
	if len(faltantes) == 0 {
		return nil
	}

	rows, err := q.Query(ctx,
		`select oid, typname, typcategory from pg_type where oid = any($1::oid[])`, faltantes)
	if err != nil {
		return fmt.Errorf("leer pg_type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var oid uint32
		var nombre, categoria string
		if err := rows.Scan(&oid, &nombre, &categoria); err != nil {
			return fmt.Errorf("leer pg_type: %w", err)
		}
		for _, i := range pendientes[oid] {
			res.Columns[i].DataType = nombre
			res.Columns[i].Class = clasePorCategoria(categoria)
		}
	}
	return rows.Err()
}

// clasePorCategoria traduce typcategory de pg_type.
//
// Es una letra sola que Postgres ya mantiene por nosotros, así que un enum del
// usuario se alinea y se muestra bien sin que tengamos que conocerlo.
func clasePorCategoria(c string) query.Class {
	switch c {
	case "N":
		return query.ClassNumber
	case "S":
		return query.ClassText
	case "D":
		return query.ClassTemporal
	case "B":
		return query.ClassBool
	case "E":
		return query.ClassEnum
	case "A":
		return query.ClassArray
	}
	return query.ClassOther
}

// conAvisoDeLote explica qué pasó con las sentencias anteriores del lote.
//
// Verificado contra el motor: Postgres ejecuta un string con varias sentencias
// dentro de una transacción implícita, así que si una falla se revierten todas
// — incluidas las que el servidor ya había confirmado con su tag. Sin este
// aviso, la lista de sentencias ejecutadas se lee como "esto quedó aplicado",
// que es exactamente al revés y en la dirección peligrosa: alguien creería que
// una tabla existe cuando no se creó.
//
// La excepción es un COMMIT explícito en el medio del lote, que cierra la
// transacción implícita y hace durable lo anterior. No se puede saber desde acá
// sin parsear la SQL, así que el aviso lo dice en vez de afirmar de más.
func conAvisoDeLote(f *Failure, previas []query.Result) *Failure {
	if f == nil || len(previas) == 0 {
		return f
	}
	tags := make([]string, len(previas))
	for i, r := range previas {
		tags[i] = r.Command
	}
	aviso := fmt.Sprintf(
		"Antes del error, el servidor procesó %d sentencia(s): %s. "+
			"Postgres corre el lote en una transacción implícita, así que se revirtieron todas, "+
			"salvo que la consulta traiga un COMMIT explícito.",
		len(previas), strings.Join(tags, ", "))
	if f.Hint == "" {
		f.Hint = aviso
	} else {
		f.Hint += " " + aviso
	}
	return f
}
