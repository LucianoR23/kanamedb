package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// Scan recorre una tabla entera con UNA consulta que se lee a medida que llega.
//
// Va por el protocolo SIMPLE, igual que Run, y no por pool.Query. No es un
// detalle de estilo: con el protocolo extendido pgx pide muchos tipos en
// formato BINARIO y los decodifica a tipos de Go, así que un timestamptz
// volvería como time.Time y habría que volver a darle formato acá. Eso es
// exactamente lo que el resto del proyecto evita —el texto lo genera el
// servidor— y haría que la misma fila saliera distinta en la grilla y en el
// archivo. Con el protocolo simple el servidor manda texto y se copia tal cual.
//
// Tampoco usa `COPY … TO STDOUT`, que existe y es más rápido: COPY entrega
// bytes ya formateados en CSV o en texto, así que serviría para uno de los
// cuatro formatos y obligaría a traducir cada opción del diálogo —delimitador,
// marca de NULL, citado— a las de COPY, con dos formatos saliendo por caminos
// distintos. Si algún día se mide que el CSV lo justifica, el atajo entra acá
// adentro sin que nadie más se entere.
func Scan(
	ctx context.Context, pool *pgxpool.Pool, esquema, tabla string, opts engine.ScanOptions,
) (engine.RowStream, error) {
	var b strings.Builder
	b.WriteString("select * from ")
	b.WriteString(QualifiedName(esquema, tabla))
	if len(opts.OrderBy) > 0 {
		// El `desc` va pegado a CADA columna, como en TableData: `order by a, b
		// desc` ordena por `a` ascendente y solo desempata al revés.
		b.WriteString(" order by ")
		for i, col := range opts.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(QuoteIdent(col))
			if opts.Descending {
				b.WriteString(" desc")
			}
		}
	}

	// La conexión queda tomada mientras dure el recorrido y se devuelve en
	// Close. Un recorrido ES una conexión ocupada: no hay forma de leer dos
	// millones de filas sin tenerla.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("tomar una conexión para leer %s.%s: %w", esquema, tabla, err)
	}

	// Las columnas se resuelven ANTES de abrir el recorrido, con la misma
	// consulta y `limit 0`. El orden importa por dos motivos:
	//
	//  1. Mientras un recorrido está abierto, la conexión está en medio de un
	//     result set: mandarle otra consulta ahí corrompe el protocolo.
	//  2. Pedirle una segunda conexión al pool tampoco sirve: el recorrido se
	//     queda con la suya durante todo el volcado, y el pool de una conexión
	//     de solo lectura tiene DOS, así que dos exportaciones a la vez se
	//     esperarían una a la otra.
	//
	// Con `limit 0` la consulta vuelve enseguida, se cierra, y recién entonces
	// arranca el recorrido sobre la misma conexión.
	columnas, err := columnasDe(ctx, conn, b.String())
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("leer las columnas de %s.%s: %w", esquema, tabla, err)
	}

	mrr := conn.Conn().PgConn().Exec(ctx, b.String())
	if !mrr.NextResult() {
		// Sin resultado hay error: es una sola sentencia y siempre devuelve uno.
		err := mrr.Close()
		conn.Release()
		if err == nil {
			err = fmt.Errorf("la lectura de %s.%s no devolvió ningún resultado", esquema, tabla)
		}
		return nil, fmt.Errorf("leer %s.%s: %w", esquema, tabla, err)
	}

	return &flujo{mrr: mrr, rr: mrr.ResultReader(), soltar: conn.Release, columnas: columnas}, nil
}

// columnasDe averigua las columnas del recorrido sin traer ninguna fila.
//
// El escritor necesita el encabezado para empezar, así que las columnas tienen
// que estar antes de la primera fila. Un enum llega sin nombre de tipo y se
// resuelve contra el catálogo, igual que en una consulta del editor —y sobre
// esta misma conexión, que en este momento está libre—.
func columnasDe(ctx context.Context, conn *pgxpool.Conn, sql string) ([]query.Column, error) {
	rows, err := conn.Query(ctx, sql+" limit 0")
	if err != nil {
		return nil, err
	}
	campos := rows.FieldDescriptions()
	res := &query.Result{Columns: make([]query.Column, len(campos))}
	oids := make([]uint32, len(campos))
	tm := conn.Conn().TypeMap()
	for i, f := range campos {
		res.Columns[i] = columnaDe(f, tm)
		oids[i] = f.DataTypeOID
	}
	// Close antes de cualquier otra consulta: deja la conexión libre.
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := resolverTiposDesconocidos(ctx, conn, res, oids); err != nil {
		return nil, err
	}
	return res.Columns, nil
}

// flujo adapta el lector del protocolo simple a engine.RowStream.
type flujo struct {
	mrr      *pgconn.MultiResultReader
	rr       *pgconn.ResultReader
	soltar   func()
	columnas []query.Column
	fila     []*string
	cerrado  bool
	err      error
}

func (f *flujo) Columns() []query.Column { return f.columnas }

func (f *flujo) Next() bool {
	if f.cerrado || f.err != nil {
		return false
	}
	if !f.rr.NextRow() {
		// Se cierra ACÁ y no en Close: NextRow devuelve false tanto al terminar
		// como al fallar, y cuál de las dos fue lo dice el cierre. Sin esto,
		// quien hace `for Next() {}` y después mira Err() vería nil aunque el
		// servidor hubiera cortado a la mitad, y un archivo cortado se vería
		// igual que uno entero.
		f.terminar()
		return false
	}
	crudas := f.rr.Values()
	fila := make([]*string, len(crudas))
	for i, b := range crudas {
		// NULL viaja con longitud -1 y la cadena vacía con longitud 0. El
		// protocolo las distingue y acá se preserva: un `len(b) == 0` las
		// colapsaría y las dos saldrían iguales en el archivo.
		if b == nil {
			continue
		}
		// Se copia porque los bytes solo valen hasta el próximo NextRow.
		s := string(b)
		fila[i] = &s
	}
	f.fila = fila
	return true
}

func (f *flujo) Row() []*string { return f.fila }

func (f *flujo) Err() error { return f.err }

func (f *flujo) Close() { f.terminar() }

// terminar cierra el lector y devuelve la conexión al pool. Idempotente.
func (f *flujo) terminar() {
	if f.cerrado {
		return
	}
	f.cerrado = true
	if _, err := f.rr.Close(); err != nil && f.err == nil {
		f.err = err
	}
	if err := f.mrr.Close(); err != nil && f.err == nil {
		f.err = err
	}
	f.soltar()
}
