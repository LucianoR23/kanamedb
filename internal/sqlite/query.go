package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// run ejecuta la SQL tal como la escribió el usuario.
//
// No se envuelve, no se le agrega LIMIT y no se parsea: un editor de SQL que
// reescribe lo que ejecutás es un editor en el que no se puede confiar.
func run(
	ctx context.Context, db *sql.DB, sql_ string, d query.Dialect, opts engine.RunOptions,
) (*query.Batch, *engine.Failure) {
	limite := opts.RowLimit
	if limite == 0 {
		limite = DefaultRowLimit
	}

	// Una conexión dedicada, no la del pool. changes() cuenta lo que hizo la
	// última sentencia DE ESA CONEXIÓN, así que pedido al pool podría contestar
	// lo de otra — o cero.
	cn, err := db.Conn(ctx)
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	defer cn.Close()

	inicio := time.Now()
	rows, err := cn.QueryContext(ctx, sql_)
	if err != nil {
		// No se reintenta con Exec, y no es una omisión.
		//
		// El driver de SQLite ejecuta TODAS las sentencias de la cadena, así
		// que reintentar vuelve a correr las que ya corrieron. Con
		// «INSERT INTO t VALUES (1); SELECT * FROM no_existe;» el reintento
		// dejaba DOS filas y reportaba el error igual. Comprobado.
		//
		// El fallback tampoco hace falta: QueryContext acepta un UPDATE o un
		// CREATE sin chistar y devuelve un resultado de cero columnas. Ese es
		// el caso de abajo.
		return nil, ClassifyStatement(err, "")
	}
	defer rows.Close()

	columnas, err := rows.Columns()
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	if len(columnas) == 0 {
		// No devuelve filas: es un INSERT, un UPDATE, un DELETE o un DDL.
		_ = rows.Close()
		var afectadas int64
		if err := cn.QueryRowContext(ctx, "SELECT changes()").Scan(&afectadas); err != nil {
			return nil, ClassifyStatement(err, "")
		}
		return &query.Batch{
			Results: []query.Result{{
				ReturnsRows:  false,
				AffectedRows: afectadas,
				Command:      query.Command(sql_, d),
			}},
			ElapsedMs: time.Since(inicio).Milliseconds(),
		}, nil
	}

	r, fail := leerFilas(rows, limite)
	if fail != nil {
		return nil, fail
	}
	r.Command = query.Command(sql_, d)
	return &query.Batch{Results: []query.Result{*r}, ElapsedMs: time.Since(inicio).Milliseconds()}, nil
}

// leerFilas convierte un *sql.Rows en el resultado de la grilla.
//
// Todo se lee como *string igual que en los otros motores: la grilla muestra
// texto, y convertir a float64 para volver a formatear pierde precisión. NULL
// se distingue de la cadena vacía por el puntero nil.
//
// Acá hay una razón de más para leer texto, y es propia de SQLite: los tipos no
// son de la COLUMNA sino del VALOR. Una columna declarada `integer` puede tener
// un texto en una fila y un blob en la otra, porque salvo en las tablas STRICT
// el tipo declarado es solo una afinidad. Escanear a un tipo de Go obligaría a
// elegir uno para toda la columna y fallaría en la fila que no lo cumple.
func leerFilas(rows *sql.Rows, limite int) (*query.Result, *engine.Failure) {
	tipos, err := rows.ColumnTypes()
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	r := &query.Result{ReturnsRows: true, RowLimit: limite}
	for _, t := range tipos {
		r.Columns = append(r.Columns, query.Column{
			Name:     t.Name(),
			DataType: strings.ToLower(t.DatabaseTypeName()),
			Class:    claseDe(t.DatabaseTypeName()),
		})
	}

	n := len(tipos)
	for rows.Next() {
		if limite > 0 && len(r.Rows) >= limite {
			r.Truncated = true
			break
		}
		crudo := make([]any, n)
		punteros := make([]any, n)
		for i := range crudo {
			punteros[i] = &crudo[i]
		}
		if err := rows.Scan(punteros...); err != nil {
			return nil, ClassifyStatement(err, "")
		}
		fila := make([]*string, n)
		for i, v := range crudo {
			s, ok := aTexto(v)
			if !ok {
				continue
			}
			fila[i] = &s
		}
		r.Rows = append(r.Rows, fila)
	}
	if err := rows.Err(); err != nil {
		return nil, ClassifyStatement(err, "")
	}
	return r, nil
}

// aTexto pasa a texto lo que el driver haya entregado, sin perder el NULL.
//
// Se escanea a `any` y no a *string porque el driver de SQLite devuelve el tipo
// del VALOR —int64, float64, string, []byte o nil— y un *string obligaría al
// driver a convertir, que es donde se pierden los blobs.
func aTexto(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case []byte:
		// Es un BLOB. Se muestra su tamaño y no el contenido: volcar bytes
		// crudos en una celda llena la pantalla de basura, y si son UTF-8 el
		// driver ya los habría entregado como string.
		return fmt.Sprintf("[%d bytes]", len(x)), true
	case int64:
		return fmt.Sprintf("%d", x), true
	case float64:
		// %v da la representación más corta que vuelve a leer igual, que es lo
		// que hay que mostrar: 0.1 y no 0.10000000000000001.
		return fmt.Sprintf("%v", x), true
	case bool:
		if x {
			return "1", true
		}
		return "0", true
	case time.Time:
		return x.Format(time.RFC3339Nano), true
	}
	return fmt.Sprintf("%v", v), true
}

// claseDe agrupa el tipo declarado en las clases que la grilla sabe alinear.
//
// Se usan las reglas de AFINIDAD de SQLite, que son las del propio motor y no
// una lista de nombres: cualquier tipo que contenga INT es entero, cualquiera
// que contenga CHAR, CLOB o TEXT es texto, y así. Es lo que permite que
// `UNSIGNED BIG INT` y `VARYING CHARACTER(255)` —los dos, tipos válidos en
// SQLite— caigan donde corresponde sin enumerarlos.
func claseDe(tipo string) query.Class {
	t := strings.ToUpper(tipo)
	switch {
	case t == "":
		// Una expresión no tiene tipo declarado. Es lo normal en `SELECT 1+1`.
		return query.ClassOther
	case strings.Contains(t, "INT"):
		return query.ClassNumber
	case strings.Contains(t, "CHAR"), strings.Contains(t, "CLOB"), strings.Contains(t, "TEXT"):
		return query.ClassText
	case strings.Contains(t, "BLOB"):
		return query.ClassBinary
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"),
		strings.Contains(t, "NUM"), strings.Contains(t, "DEC"):
		return query.ClassNumber
	case strings.Contains(t, "BOOL"):
		return query.ClassBool
	case strings.Contains(t, "DATE"), strings.Contains(t, "TIME"):
		// SQLite no tiene tipo de fecha: se guardan como texto, número o juliano.
		// La columna igual se declara así y la grilla la alinea como fecha.
		return query.ClassTemporal
	case strings.Contains(t, "JSON"):
		return query.ClassJSON
	}
	return query.ClassOther
}

// page lee una página de una tabla.
//
// Sin ORDER BY, LIMIT/OFFSET no define qué filas devuelve. Quien llama resuelve
// la clave primaria y la pasa.
func page(
	ctx context.Context, db *sql.DB, tabla string, opts engine.PageOptions,
) (*query.Result, *engine.Failure) {
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT * FROM %s", QuoteIdent(tabla))
	if len(opts.OrderBy) > 0 {
		b.WriteString(" ORDER BY " + ordenDe(opts))
	}
	if opts.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", opts.Limit)
	} else if opts.Offset > 0 {
		// SQLite exige LIMIT para poder usar OFFSET. -1 es «sin límite», y es
		// la forma que documenta el propio motor.
		b.WriteString(" LIMIT -1")
	}
	if opts.Offset > 0 {
		fmt.Fprintf(&b, " OFFSET %d", opts.Offset)
	}

	rows, err := db.QueryContext(ctx, b.String())
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	defer rows.Close()

	limite := opts.Limit
	if limite <= 0 {
		limite = DefaultRowLimit
	}
	return leerFilas(rows, limite)
}

// count cuenta las filas, exacto. Recorre la tabla entera: es a propósito.
func count(ctx context.Context, db *sql.DB, tabla string) (int64, *engine.Failure) {
	var n int64
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", QuoteIdent(tabla))
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, ClassifyStatement(err, "")
	}
	return n, nil
}

// ordenDe arma la lista del ORDER BY.
//
// El DESC va pegado a CADA columna, no una sola vez al final. `ORDER BY a, b
// DESC` ordena por `a` ASCENDENTE y solo desempata por `b` al revés, que no es
// «la página anterior»: con una clave primaria compuesta el orden deja de ser
// total y el paginado por LIMIT/OFFSET empieza a repetir y saltear filas.
func ordenDe(opts engine.PageOptions) string {
	cols := make([]string, 0, len(opts.OrderBy))
	for _, c := range opts.OrderBy {
		q := QuoteIdent(c)
		if opts.Descending {
			q += " DESC"
		}
		cols = append(cols, q)
	}
	return strings.Join(cols, ", ")
}
