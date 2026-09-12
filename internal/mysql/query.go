package mysql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/dml"
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
	// Una conexión DEDICADA, como en SQLite: ROW_COUNT() habla de la última
	// sentencia de ESTA conexión, y CONNECTION_ID() es lo que hace falta para
	// poder matar la consulta si la cancelan.
	cn, err := db.Conn(ctx)
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	defer cn.Close()
	return runEn(ctx, db, cn, sql_, d, opts)
}

// runEn corre la sentencia en una conexión ya tomada: la de run, que la
// devuelve enseguida, o la de una sesión del editor, que la retiene.
func runEn(
	ctx context.Context, db *sql.DB, cn *sql.Conn, sql_ string, d query.Dialect, opts engine.RunOptions,
) (*query.Batch, *engine.Failure) {
	limite := opts.RowLimit
	if limite == 0 {
		limite = DefaultRowLimit
	}

	// «Cancelar» en el driver cierra el socket y nada más: el servidor solo se
	// entera cuando intenta escribir, así que un UPDATE grande cancelado desde
	// el editor se reportaba cancelado y terminaba y confirmaba en autocommit
	// (C-14 de la auditoría del 2026-09-11). Con el id de la conexión, al
	// cancelar se manda KILL QUERY por otra conexión, que sí aborta la
	// sentencia en el servidor.
	var id int64
	if err := cn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
		return nil, ClassifyStatement(err, "")
	}
	defer vigilarCancelacion(ctx, db, id)()

	inicio := time.Now()
	rows, err := cn.QueryContext(ctx, sql_)
	if err != nil {
		// No se reintenta con Exec, y no es una omisión. El reintento venía de
		// una premisa falsa —que un INSERT por QueryContext fallaba— y lo que
		// hacía era mandar DOS veces una sentencia que falló de verdad: si la
		// conexión se cortaba después de que el servidor aplicó la escritura
		// y antes de leer el OK, `UPDATE t SET n = n + 1` sumaba dos (C-03).
		return nil, ClassifyStatement(err, "")
	}
	defer rows.Close()

	columnas, err := rows.Columns()
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	cmd := query.Command(sql_, d)
	if len(columnas) == 0 {
		// El servidor respondió con un paquete OK: es un INSERT, un UPDATE, un
		// DELETE o un DDL. database/sql lo entrega como un resultado sin
		// columnas, no como un error, así que antes seguía a leerFilas y salía
		// como «0 filas devueltas»: un DELETE que borró diez mil filas se veía
		// igual que uno que no encontró ninguna (C-03).
		_ = rows.Close()
		var afectadas int64
		if modificaFilas(cmd) {
			if err := cn.QueryRowContext(ctx, "SELECT ROW_COUNT()").Scan(&afectadas); err != nil {
				return nil, ClassifyStatement(err, "")
			}
			if afectadas < 0 {
				afectadas = 0
			}
		}
		return &query.Batch{
			Results: []query.Result{{
				ReturnsRows:  false,
				AffectedRows: afectadas,
				Command:      cmd,
			}},
			ElapsedMs: time.Since(inicio).Milliseconds(),
		}, nil
	}

	r, fail := leerFilas(rows, limite)
	if fail != nil {
		return nil, fail
	}
	r.Command = cmd
	return &query.Batch{Results: []query.Result{*r}, ElapsedMs: time.Since(inicio).Milliseconds()}, nil
}

// modificaFilas dice si una sentencia puede haber cambiado filas, que es
// cuando ROW_COUNT() habla de ella.
func modificaFilas(cmd string) bool {
	switch cmd {
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "WITH", "LOAD", "CALL":
		return true
	}
	return false
}

// vigilarCancelacion manda KILL QUERY a la conexión `id` si el contexto se
// cancela antes de que la sentencia termine. Devuelve la función que apaga la
// vigilancia; llamarla es obligatorio, o el KILL podría alcanzar a la próxima
// sentencia de esa conexión.
//
// Va por OTRA conexión del pool, con su propio plazo: la cancelada ya no
// sirve —el driver la cierra— y el contexto que se canceló tampoco. El id es
// un número que dio el servidor, no texto de nadie: KILL no acepta
// parámetros.
func vigilarCancelacion(ctx context.Context, db *sql.DB, id int64) func() {
	listo := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// El contexto también se cancela al TERMINAR una corrida normal
			// —registrar lo cancela al soltar—, y si las dos señales llegan
			// juntas el select elige al azar. Se vuelve a mirar `listo` antes
			// de matar: un KILL a una conexión que ya volvió al pool alcanza a
			// la sentencia siguiente, y nada explicaría por qué murió.
			select {
			case <-listo:
				return
			default:
			}
			plazo, cancelar := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelar()
			_, _ = db.ExecContext(plazo, fmt.Sprintf("KILL QUERY %d", id))
		case <-listo:
		}
	}()
	return func() { close(listo) }
}

// textoDe convierte los bytes que dio el servidor en el texto de la celda.
//
// Los binarios —BINARY, VARBINARY, BLOB y variantes— salen en hexadecimal:
// venían como bytes crudos adentro de un string, que hacia la interfaz se
// corrompían en U+FFFD y en el archivo SQL iban literales. Un UUID en
// BINARY(16) —patrón común— llegaba roto y la tabla no se podía editar
// (C-13 de la auditoría del 2026-09-11). El literal de vuelta es X'…'
// (Quoting.Binary). BIT sale como el número que es: `BIT(8) = 65` llegaba
// como «A».
func textoDe(b []byte, tipo string) string {
	switch tipo {
	case "BINARY", "VARBINARY", "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "VECTOR":
		// VECTOR va acá porque claseDe lo clasifica binario: lo que la clase
		// diga binario tiene que salir en hexadecimal, o el escritor SQL lo
		// envuelve crudo en X'…'.
		return strings.ToUpper(hex.EncodeToString(b))
	case "BIT":
		var n uint64
		for _, x := range b {
			n = n<<8 | uint64(x)
		}
		return strconv.FormatUint(n, 10)
	}
	return string(b)
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
			s := textoDe(b, tipos[i].DatabaseTypeName())
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
	filtro, args, err := dml.Where(opts.Where, dialectoDML(QuoteString), 0)
	if err != nil {
		return nil, &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}
	}
	if filtro != "" {
		b.WriteString(" WHERE " + filtro)
	}
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

	rows, err := db.QueryContext(ctx, b.String(), args...)
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
func count(
	ctx context.Context, db *sql.DB, base, tabla string, where []query.Condition,
) (int64, *engine.Failure) {
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", QualifiedName(base, tabla))
	filtro, args, err := dml.Where(where, dialectoDML(QuoteString), 0)
	if err != nil {
		return 0, &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}
	}
	if filtro != "" {
		q += " WHERE " + filtro
	}
	var n int64
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, ClassifyStatement(err, "")
	}
	return n, nil
}
