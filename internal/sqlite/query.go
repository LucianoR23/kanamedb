package sqlite

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
func run(
	ctx context.Context, db *sql.DB, sql_ string, d query.Dialect, opts engine.RunOptions,
) (*query.Batch, *engine.Failure) {
	// Una conexión dedicada, no la del pool. changes() cuenta lo que hizo la
	// última sentencia DE ESA CONEXIÓN, así que pedido al pool podría contestar
	// lo de otra — o cero.
	cn, err := db.Conn(ctx)
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	defer cn.Close()
	return runEn(ctx, cn, sql_, d, opts)
}

// runEn corre la sentencia en una conexión ya tomada: la de run, que la
// devuelve enseguida, o la de una sesión del editor, que la retiene.
func runEn(
	ctx context.Context, cn *sql.Conn, sql_ string, d query.Dialect, opts engine.RunOptions,
) (*query.Batch, *engine.Failure) {
	limite := opts.RowLimit
	if limite == 0 {
		limite = DefaultRowLimit
	}

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
		cmd := query.Command(sql_, d)
		var afectadas int64
		// changes() cuenta el último INSERT/UPDATE/DELETE DE ESTA CONEXIÓN,
		// aunque la última sentencia haya sido otra cosa: después de un CREATE
		// TABLE o un PRAGMA devolvía el conteo de un UPDATE anterior que pasó
		// por la misma conexión del pool (C-29 de la auditoría del
		// 2026-09-11). Solo se pregunta cuando la sentencia pudo cambiar filas.
		if modificaFilas(cmd) {
			if err := cn.QueryRowContext(ctx, "SELECT changes()").Scan(&afectadas); err != nil {
				return nil, ClassifyStatement(err, "")
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
			s, ok := aTexto(v, tipos[i].DatabaseTypeName())
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

// modificaFilas dice si una sentencia puede haber cambiado filas, que es
// cuando changes() habla de ella.
func modificaFilas(cmd string) bool {
	switch cmd {
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "WITH":
		return true
	}
	return false
}

// aTexto pasa a texto lo que el driver haya entregado, sin perder el NULL.
// Es la forma de la GRILLA: un BLOB se muestra por su tamaño.
func aTexto(v any, tipoDeclarado string) (string, bool) {
	return valorATexto(v, false, esSoloFecha(tipoDeclarado))
}

// esSoloFecha dice si el tipo declarado es una fecha sin hora: DATE, y no
// DATETIME ni TIMESTAMP.
func esSoloFecha(tipo string) bool {
	t := strings.ToUpper(tipo)
	return strings.Contains(t, "DATE") && !strings.Contains(t, "TIME")
}

// texto pasa a texto lo que el driver haya entregado, sin perder el NULL.
//
// Se escanea a `any` y no a *string porque el driver de SQLite devuelve el tipo
// del VALOR —int64, float64, string, []byte o nil— y un *string obligaría al
// driver a convertir, que es donde se pierden los blobs.
//
// Con blobHex, un BLOB sale en hexadecimal (mayúsculas, sin prefijo): es la
// forma de la EXPORTACIÓN, que tiene que ser fiel, y el escritor SQL la
// convierte en X'…'. Sin él sale como `[N bytes]`, para la grilla: volcar
// bytes crudos en una celda llena la pantalla de basura. El volcado escribía
// `'[12 bytes]'` en el lugar de cada BLOB y decía que no había dejado nada
// afuera (C-02 de la auditoría del 2026-09-11).
func valorATexto(v any, blobHex, soloFecha bool) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case []byte:
		if blobHex {
			return strings.ToUpper(hex.EncodeToString(x)), true
		}
		return fmt.Sprintf("[%d bytes]", len(x)), true
	case int64:
		return fmt.Sprintf("%d", x), true
	case float64:
		return textoDeReal(x), true
	case bool:
		if x {
			return "1", true
		}
		return "0", true
	case time.Time:
		return textoDeFecha(x, soloFecha), true
	}
	return fmt.Sprintf("%v", v), true
}

// textoDeReal escribe un REAL como lo escribiría SQLite: la representación
// más corta que vuelve a leer igual —0.1 y no 0.10000000000000001— y con el
// `.0` cuando es entero. `%v` de 3.0 daba `3`, que al volver a cargar en una
// columna sin afinidad quedaba INTEGER (C-26).
func textoDeReal(x float64) string {
	s := strconv.FormatFloat(x, 'g', -1, 64)
	if strings.ContainsAny(s, ".eIN") {
		return s
	}
	return s + ".0"
}

// textoDeFecha deshace lo que el driver hizo con una columna declarada DATE,
// DATETIME o TIMESTAMP: parsea el texto a time.Time sin que se le pueda pedir
// que no lo haga. El texto original no se puede recuperar, pero sí escribir el
// formato más probable de los que el propio driver reconoce, en vez de un
// RFC 3339 con una `Z` que nadie escribió (C-12). La grilla y la exportación
// no pasan por acá —leen esas columnas con CAST(… AS TEXT), ver lecturaFiel—;
// esto es para el editor SQL, donde la consulta la escribe la persona.
//
// soloFecha es para una columna declarada DATE: ahí una medianoche es una
// fecha. En una DATETIME se escribe la hora aunque sea 00:00:00, porque
// `2021-01-02` no encuentra la fila guardada como `2021-01-02 00:00:00`.
func textoDeFecha(t time.Time, soloFecha bool) string {
	formato := "2006-01-02 15:04:05"
	if t.Nanosecond() != 0 {
		formato = "2006-01-02 15:04:05.999999999"
	} else if soloFecha && t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Location() == time.UTC {
		formato = "2006-01-02"
	}
	if t.Location() != time.UTC {
		formato += "-07:00"
	}
	return t.Format(formato)
}

// declarada es una columna de la tabla según pragma_table_xinfo.
type declarada struct {
	nombre string
	tipo   string
}

// columnasDeclaradas lee la definición de la tabla: nombre y tipo declarado de
// cada columna, en orden, con las generadas (que `SELECT *` incluye) y sin las
// ocultas de las tablas virtuales (que no).
func columnasDeclaradas(ctx context.Context, db *sql.DB, tabla string) ([]declarada, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT name, type FROM pragma_table_xinfo(?) WHERE hidden <> 1 ORDER BY cid", tabla)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []declarada
	for rows.Next() {
		var d declarada
		if err := rows.Scan(&d.nombre, &d.tipo); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// lecturaFiel arma la lista del SELECT para leer una tabla tal como está
// guardada.
//
// El driver parsea a time.Time todo TEXT de una columna declarada DATE,
// DATETIME o TIMESTAMP, y no se le puede pedir que no lo haga: un
// `2021-01-02 16:39` guardado se veía como `2021-01-02T16:39:00Z`, y si esa
// columna era parte de la clave, el UPDATE de la grilla buscaba la fila con el
// texto reescrito y no la encontraba (C-12). Un `CAST(col AS TEXT)` no tiene
// tipo declarado, así que el driver entrega el texto tal cual; el tipo
// declarado se repone en el encabezado con `reponerDeclarados`, para que la
// grilla lo siga mostrando y alineando como fecha.
func lecturaFiel(declaradas []declarada, pedidas []string) string {
	tipoDe := make(map[string]string, len(declaradas))
	for _, d := range declaradas {
		tipoDe[d.nombre] = d.tipo
	}
	nombres := pedidas
	if len(nombres) == 0 {
		nombres = make([]string, 0, len(declaradas))
		for _, d := range declaradas {
			nombres = append(nombres, d.nombre)
		}
	}
	partes := make([]string, 0, len(nombres))
	for _, n := range nombres {
		if claseDe(tipoDe[n]) == query.ClassTemporal {
			partes = append(partes, "CAST("+QuoteIdent(n)+" AS TEXT) AS "+QuoteIdent(n))
		} else {
			partes = append(partes, QuoteIdent(n))
		}
	}
	return strings.Join(partes, ", ")
}

// reponerDeclarados devuelve al encabezado el tipo declarado de las columnas
// que lecturaFiel leyó con CAST, que llegan sin tipo.
func reponerDeclarados(cols []query.Column, declaradas []declarada) {
	tipoDe := make(map[string]string, len(declaradas))
	for _, d := range declaradas {
		tipoDe[d.nombre] = d.tipo
	}
	for i := range cols {
		if cols[i].DataType == "" {
			if t, ok := tipoDe[cols[i].Name]; ok && t != "" {
				cols[i].DataType = strings.ToLower(t)
				cols[i].Class = claseDe(t)
			}
		}
	}
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
	declaradas, err := columnasDeclaradas(ctx, db, tabla)
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT %s FROM %s", lecturaFiel(declaradas, nil), QuoteIdent(tabla))
	filtro, args, err := dml.Where(opts.Where, dialectoDML, 0)
	if err != nil {
		return nil, &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}
	}
	if filtro != "" {
		b.WriteString(" WHERE " + filtro)
	}
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

	rows, err := db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, ClassifyStatement(err, "")
	}
	defer rows.Close()

	limite := opts.Limit
	if limite <= 0 {
		limite = DefaultRowLimit
	}
	r, fail := leerFilas(rows, limite)
	if fail != nil {
		return nil, fail
	}
	reponerDeclarados(r.Columns, declaradas)
	return r, nil
}

// count cuenta las filas, exacto. Recorre la tabla entera: es a propósito.
func count(
	ctx context.Context, db *sql.DB, tabla string, where []query.Condition,
) (int64, *engine.Failure) {
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", QuoteIdent(tabla))
	filtro, args, err := dml.Where(where, dialectoDML, 0)
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
