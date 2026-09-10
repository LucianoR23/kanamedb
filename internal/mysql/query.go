package mysql

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
//
// El DSN tiene multiStatements en false a propósito, así que acá siempre hay
// una sentencia. Con varias, el driver no podría asociar cada error a la suya,
// que es justo lo que la pantalla de apply necesita para decir cuál falló.
func run(
	ctx context.Context, db *sql.DB, sql_ string, d query.Dialect, opts engine.RunOptions,
) (*query.Batch, *engine.Failure) {
	limite := opts.RowLimit
	if limite == 0 {
		limite = DefaultRowLimit
	}

	inicio := time.Now()
	rows, err := db.QueryContext(ctx, sql_)
	if err != nil {
		// Una sentencia que no devuelve filas —INSERT, ALTER— llega acá igual
		// con database/sql, así que se reintenta como Exec antes de darla por
		// fallada.
		res, err2 := db.ExecContext(ctx, sql_)
		if err2 != nil {
			return nil, ClassifyStatement(err, "")
		}
		afectadas, _ := res.RowsAffected()
		return &query.Batch{
			Results: []query.Result{{
				ReturnsRows:  false,
				AffectedRows: afectadas,
				Command:      query.Command(sql_, d),
			}},
			ElapsedMs: time.Since(inicio).Milliseconds(),
		}, nil
	}
	defer rows.Close()

	r, fail := leerFilas(rows, limite)
	if fail != nil {
		return nil, fail
	}
	r.Command = query.Command(sql_, d)
	return &query.Batch{Results: []query.Result{*r}, ElapsedMs: time.Since(inicio).Milliseconds()}, nil
}

// leerFilas convierte un *sql.Rows en el resultado de la grilla.
//
// Todo se lee como *string y no con los tipos de Go a propósito, igual que en
// Postgres: la grilla muestra texto, y convertir a float64 o time.Time para
// volver a formatear pierde precisión y zona horaria por el camino. NULL se
// distingue de la cadena vacía por el puntero nil, que es la diferencia que la
// interfaz tiene que mostrar.
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
		crudo := make([]sql.RawBytes, n)
		punteros := make([]any, n)
		for i := range crudo {
			punteros[i] = &crudo[i]
		}
		if err := rows.Scan(punteros...); err != nil {
			return nil, ClassifyStatement(err, "")
		}
		fila := make([]*string, n)
		for i, b := range crudo {
			if b == nil {
				continue
			}
			// RawBytes apunta al buffer del driver y se invalida en el Next
			// siguiente: hay que copiar. Sin la copia, todas las filas
			// terminarían con el valor de la última.
			s := string(b)
			fila[i] = &s
		}
		r.Rows = append(r.Rows, fila)
	}
	if err := rows.Err(); err != nil {
		return nil, ClassifyStatement(err, "")
	}
	return r, nil
}

// claseDe agrupa el tipo del motor en las clases que la grilla sabe alinear y
// colorear.
func claseDe(tipo string) query.Class {
	switch strings.ToUpper(tipo) {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT",
		"DECIMAL", "FLOAT", "DOUBLE", "NEWDECIMAL", "YEAR", "BIT":
		return query.ClassNumber
	case "BOOL", "BOOLEAN":
		return query.ClassBool
	case "DATE", "DATETIME", "TIMESTAMP", "TIME":
		return query.ClassTemporal
	case "JSON":
		return query.ClassJSON
	case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY", "VECTOR":
		return query.ClassBinary
	case "ENUM":
		return query.ClassEnum
	case "SET":
		return query.ClassArray
	case "CHAR", "VARCHAR", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT", "UUID", "INET6":
		return query.ClassText
	}
	return query.ClassOther
}

// page lee una página de una tabla.
//
// Sin ORDER BY, LIMIT/OFFSET no define qué filas devuelve: el motor puede
// entregar la misma fila en dos páginas y saltearse otra. Por eso no se inventa
// un orden acá — quien llama resuelve la clave primaria y la pasa.
func page(
	ctx context.Context, db *sql.DB, base, tabla string, opts engine.PageOptions,
) (*query.Result, *engine.Failure) {
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT * FROM %s", QualifiedName(base, tabla))
	if len(opts.OrderBy) > 0 {
		// El DESC va pegado a CADA columna, no una sola vez al final. `ORDER BY
		// a, b DESC` ordena por `a` ASCENDENTE y solo desempata por `b` al
		// revés, que no es «la página anterior»: con una clave primaria
		// compuesta el orden deja de ser total, y el paginado por LIMIT/OFFSET
		// empieza a repetir y a saltear filas.
		cols := make([]string, 0, len(opts.OrderBy))
		for _, c := range opts.OrderBy {
			q := QuoteIdent(c)
			if opts.Descending {
				q += " DESC"
			}
			cols = append(cols, q)
		}
		b.WriteString(" ORDER BY " + strings.Join(cols, ", "))
	}
	if opts.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", opts.Limit)
	}
	if opts.Offset > 0 {
		// MySQL exige LIMIT para poder usar OFFSET. El número grande es el
		// idiom que recomienda la propia documentación.
		if opts.Limit <= 0 {
			b.WriteString(" LIMIT 18446744073709551615")
		}
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

// count cuenta las filas, exacto. Recorre la tabla entera: es a propósito, y
// por eso va aparte de la estimación que muestra el árbol.
func count(ctx context.Context, db *sql.DB, base, tabla string) (int64, *engine.Failure) {
	var n int64
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", QualifiedName(base, tabla))
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, ClassifyStatement(err, "")
	}
	return n, nil
}
