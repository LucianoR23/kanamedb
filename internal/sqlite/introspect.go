package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// esquemaPrincipal es como SQLite llama a la base abierta. No es un esquema en
// el sentido de Postgres —no se pueden crear más— pero es el nombre real y el
// que hay que usar para calificar cuando hay bases adjuntas con ATTACH.
const esquemaPrincipal = "main"

// noInternas descarta las tablas que crea el propio motor: sqlite_sequence,
// sqlite_stat1, sqlite_autoindex_*. Mostrarlas en el árbol sería mostrar
// plomería.
const noInternas = `name NOT LIKE 'sqlite\_%' ESCAPE '\'`

// introspect lee el esquema completo para el árbol y el diagrama.
func introspect(ctx context.Context, db *sql.DB, base string) (*schema.Snapshot, error) {
	snap := &schema.Snapshot{Database: base, CapturedAt: time.Now()}
	esq := schema.Schema{Name: esquemaPrincipal}

	tablas, orden, err := leerTablas(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := leerColumnas(ctx, db, tablas); err != nil {
		return nil, err
	}
	if err := leerClavesDelEsquema(ctx, db, tablas); err != nil {
		return nil, err
	}
	for _, nombre := range orden {
		esq.Tables = append(esq.Tables, *tablas[nombre])
	}
	snap.Schemas = []schema.Schema{esq}
	return snap, nil
}

func leerTablas(ctx context.Context, db *sql.DB) (map[string]*schema.Table, []string, error) {
	// Las estimaciones salen de sqlite_stat1, que SOLO existe si alguien corrió
	// ANALYZE. Cuando no está, RowEstimate queda en -1 y la interfaz muestra
	// «sin estimación», que es la verdad: SQLite no lleva estadísticas solo.
	estim := estimaciones(ctx, db)

	q := `SELECT name FROM sqlite_schema WHERE type = 'table' AND ` + noInternas + ` ORDER BY name`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, nil, fmt.Errorf("leer las tablas: %w", err)
	}
	defer rows.Close()

	tablas := map[string]*schema.Table{}
	var orden []string
	for rows.Next() {
		t := schema.Table{RowEstimate: -1, Readable: true}
		if err := rows.Scan(&t.Name); err != nil {
			return nil, nil, fmt.Errorf("leer una tabla: %w", err)
		}
		if n, ok := estim[t.Name]; ok {
			t.RowEstimate = n
		}
		// SQLite no tiene comentarios de tabla ni particiones. El cero es la
		// respuesta correcta, no una omisión.
		tablas[t.Name] = &t
		orden = append(orden, t.Name)
	}
	return tablas, orden, rows.Err()
}

// estimaciones lee sqlite_stat1. La primera palabra de `stat` es la cantidad
// estimada de filas de la tabla.
func estimaciones(ctx context.Context, db *sql.DB) map[string]int64 {
	out := map[string]int64{}
	rows, err := db.QueryContext(ctx, `SELECT tbl, stat FROM sqlite_stat1`)
	if err != nil {
		// La tabla no existe porque nunca se corrió ANALYZE. No es un error.
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var tabla, stat string
		if err := rows.Scan(&tabla, &stat); err != nil {
			return out
		}
		campos := strings.Fields(stat)
		if len(campos) == 0 {
			continue
		}
		n, err := strconv.ParseInt(campos[0], 10, 64)
		if err != nil {
			continue
		}
		// Hay una fila por índice y todas empiezan con el mismo número, así
		// que la primera que llegue sirve.
		if _, ya := out[tabla]; !ya {
			out[tabla] = n
		}
	}
	return out
}

func leerColumnas(ctx context.Context, db *sql.DB, tablas map[string]*schema.Table) error {
	// pragma_table_xinfo es una función de tabla, así que se puede unir con
	// sqlite_schema y traer las columnas de TODAS las tablas en una consulta.
	// La alternativa —un pragma por tabla— serían doscientas consultas para
	// abrir el árbol.
	//
	// xinfo y no info: la versión corta esconde las columnas generadas, y una
	// columna que no aparece en el árbol pero sí en la grilla es peor que no
	// mostrarla en ningún lado.
	q := `
		SELECT m.name, p.name, p.type, p."notnull", p.dflt_value, p.pk, p.cid, p.hidden
		  FROM sqlite_schema m
		  JOIN pragma_table_xinfo(m.name) p
		 WHERE m.type = 'table' AND m.` + noInternas + `
		 ORDER BY m.name, p.cid`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("leer las columnas: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tabla string
		var c schema.Column
		var noNulo, pk, cid, oculta int
		var porDefecto sql.NullString
		if err := rows.Scan(&tabla, &c.Name, &c.DataType, &noNulo, &porDefecto,
			&pk, &cid, &oculta); err != nil {
			return fmt.Errorf("leer una columna: %w", err)
		}
		c.Nullable = noNulo == 0
		c.HasDefault = porDefecto.Valid
		// pk es la POSICIÓN dentro de la clave primaria, empezando en 1. Cero
		// significa que no participa.
		c.PrimaryKey = pk > 0
		c.Position = cid + 1
		if t, ok := tablas[tabla]; ok {
			t.Columns = append(t.Columns, c)
			if c.PrimaryKey {
				t.HasPrimaryKey = true
			}
		}
	}
	return rows.Err()
}

// leerClavesDelEsquema trae todas las claves foráneas de una vez: son las
// aristas del ERD y el diagrama las necesita juntas.
func leerClavesDelEsquema(ctx context.Context, db *sql.DB, tablas map[string]*schema.Table) error {
	fks, err := leerClaves(ctx, db, "")
	if err != nil {
		return err
	}
	for _, fk := range fks {
		t, ok := tablas[fk.Table]
		if !ok {
			continue
		}
		t.ForeignKeys = append(t.ForeignKeys, fk)
		for _, col := range fk.Columns {
			for i := range t.Columns {
				if t.Columns[i].Name == col {
					t.Columns[i].ForeignKey = true
				}
			}
		}
	}
	return nil
}

// leerClaves lee las claves foráneas SALIENTES. Con `tabla` vacío trae las de
// toda la base.
//
// El ORDER BY por seq no es cosmético: en una clave compuesta las columnas
// emparejan POSICIONALMENTE con las del otro lado.
func leerClaves(ctx context.Context, db *sql.DB, tabla string) ([]schema.ForeignKey, error) {
	// Se une con table_xinfo para saber si la columna local admite nulos, que
	// es lo que decide si la arista del diagrama va punteada.
	q := `
		SELECT m.name, f.id, f."table", f."from", f."to", f.on_delete, f.on_update,
		       COALESCE(c."notnull", 0)
		  FROM sqlite_schema m
		  JOIN pragma_foreign_key_list(m.name) f
		  LEFT JOIN pragma_table_xinfo(m.name) c ON c.name = f."from"
		 WHERE m.type = 'table' AND m.` + noInternas
	args := []any{}
	if tabla != "" {
		q += ` AND m.name = ?`
		args = append(args, tabla)
	}
	q += ` ORDER BY m.name, f.id, f.seq`

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("leer las claves foráneas: %w", err)
	}
	defer rows.Close()

	var out []schema.ForeignKey
	indice := map[string]int{}
	// Las claves que no dicen a qué columna apuntan: hay que resolverlas contra
	// la clave primaria de la tabla padre. Se juntan para no consultar dos
	// veces la misma.
	sinDestino := map[int][]int{}

	for rows.Next() {
		var tab, refTab, col, alBorrar, alActualizar string
		var id, noNulo int
		var refCol sql.NullString
		if err := rows.Scan(&tab, &id, &refTab, &col, &refCol,
			&alBorrar, &alActualizar, &noNulo); err != nil {
			return nil, fmt.Errorf("leer una clave foránea: %w", err)
		}
		clave := fmt.Sprintf("%s.%d", tab, id)
		i, ok := indice[clave]
		if !ok {
			out = append(out, schema.ForeignKey{
				// SQLite no le pone nombre a la clave salvo que se lo declaren
				// con CONSTRAINT, y el pragma no devuelve ese nombre ni cuando
				// lo tiene. Se arma uno estable con el número que sí da, para
				// que la interfaz tenga algo con qué identificarla.
				Name:      fmt.Sprintf("fk_%s_%d", tab, id),
				Schema:    esquemaPrincipal,
				Table:     tab,
				RefSchema: esquemaPrincipal,
				RefTable:  refTab,
				OnDelete:  accionDe(alBorrar),
				OnUpdate:  accionDe(alActualizar),
			})
			i = len(out) - 1
			indice[clave] = i
		}
		out[i].Columns = append(out[i].Columns, col)
		if refCol.Valid {
			out[i].RefColumns = append(out[i].RefColumns, refCol.String)
		} else {
			// `to` viene NULL cuando la clave se declaró sin decir a qué
			// columna apunta: REFERENCES padre, sin paréntesis. SQLite lo
			// resuelve contra la clave primaria del padre, y el diagrama tiene
			// que dibujar la misma línea que el motor hace cumplir.
			sinDestino[i] = append(sinDestino[i], len(out[i].Columns)-1)
		}
		if noNulo == 0 {
			out[i].Optional = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, posiciones := range sinDestino {
		pks, err := primaryKeyColumns(ctx, db, out[i].RefTable)
		if err != nil {
			return nil, err
		}
		for _, pos := range posiciones {
			for len(out[i].RefColumns) <= pos {
				out[i].RefColumns = append(out[i].RefColumns, "")
			}
			if pos < len(pks) {
				out[i].RefColumns[pos] = pks[pos]
			}
		}
	}
	return out, nil
}

// accionDe normaliza la acción referencial al vocabulario del proyecto, que es
// el de Postgres en minúsculas.
func accionDe(a string) schema.ReferenceAction {
	return schema.ReferenceAction(strings.ToLower(strings.TrimSpace(a)))
}

// detail lee todo lo de UNA tabla.
func detail(ctx context.Context, db *sql.DB, tabla string) (*schema.TableDetail, error) {
	d := &schema.TableDetail{
		Schema: esquemaPrincipal, Name: tabla, RowEstimate: -1, CapturedAt: time.Now(),
	}

	ddl, err := sqlDeObjeto(ctx, db, tabla)
	if err != nil {
		return nil, err
	}
	if ddl == "" {
		return nil, fmt.Errorf("la tabla %q no existe", tabla)
	}
	if e := estimaciones(ctx, db); e != nil {
		if n, ok := e[tabla]; ok {
			d.RowEstimate = n
		}
	}
	d.TotalBytes, d.TableBytes = tamaño(ctx, db, tabla)

	if d.Columns, err = detalleColumnas(ctx, db, tabla, ddl); err != nil {
		return nil, err
	}
	if d.Indexes, err = detalleIndices(ctx, db, tabla); err != nil {
		return nil, err
	}
	if d.ForeignKeys, err = leerClaves(ctx, db, tabla); err != nil {
		return nil, err
	}
	if d.ReferencedBy, err = clavesEntrantes(ctx, db, tabla); err != nil {
		return nil, err
	}
	if d.Checks, err = detalleChecks(ddl); err != nil {
		return nil, err
	}
	if d.Triggers, err = detalleTriggers(ctx, db, tabla); err != nil {
		return nil, err
	}

	esFK := map[string]bool{}
	for _, fk := range d.ForeignKeys {
		for _, c := range fk.Columns {
			esFK[c] = true
		}
	}
	for i := range d.Columns {
		d.Columns[i].ForeignKey = esFK[d.Columns[i].Name]
	}
	return d, nil
}

// sqlDeObjeto devuelve el texto con el que se creó un objeto.
func sqlDeObjeto(ctx context.Context, db *sql.DB, nombre string) (string, error) {
	var s sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_schema WHERE name = ?`, nombre).Scan(&s)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("leer la definición de %q: %w", nombre, err)
	}
	return s.String, nil
}

// tamaño estima cuánto ocupa la tabla, usando dbstat.
//
// dbstat es un módulo OPCIONAL: está en el SQLite de modernc pero puede no
// estar en otro build. Cuando no está, se devuelve cero y la interfaz no
// muestra el tamaño — que es mejor que mostrar un número inventado.
func tamaño(ctx context.Context, db *sql.DB, tabla string) (total, propio int64) {
	// El nombre de los índices de la tabla se necesita para sumar lo suyo al
	// total, igual que en Postgres.
	var t, p sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(pgsize), 0),
		       COALESCE(SUM(CASE WHEN name = ? THEN pgsize ELSE 0 END), 0)
		  FROM dbstat
		 WHERE name = ?
		    OR name IN (SELECT name FROM sqlite_schema WHERE type='index' AND tbl_name = ?)`,
		tabla, tabla, tabla).Scan(&t, &p)
	if err != nil {
		return 0, 0
	}
	return t.Int64, p.Int64
}

func detalleColumnas(ctx context.Context, db *sql.DB, tabla, ddl string) ([]schema.DetailColumn, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, type, "notnull", dflt_value, pk, cid, hidden
		  FROM pragma_table_xinfo(?) ORDER BY cid`, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer las columnas: %w", err)
	}
	defer rows.Close()

	var out []schema.DetailColumn
	for rows.Next() {
		var c schema.DetailColumn
		var noNulo, pk, cid, oculta int
		var porDefecto sql.NullString
		if err := rows.Scan(&c.Name, &c.DataType, &noNulo, &porDefecto,
			&pk, &cid, &oculta); err != nil {
			return nil, fmt.Errorf("leer una columna: %w", err)
		}
		c.Nullable = noNulo == 0
		c.PrimaryKey = pk > 0
		c.Position = cid + 1
		c.Default = porDefecto.String
		// hidden vale 2 para una columna GENERATED VIRTUAL y 3 para STORED. El
		// 1 es de las tablas virtuales, que no son columnas generadas.
		switch oculta {
		case 2:
			c.Generated = "virtual"
			c.Default = ""
		case 3:
			c.Generated = "stored"
			c.Default = ""
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Una columna `INTEGER PRIMARY KEY` sola es un alias del rowid: si el
	// INSERT no la trae, SQLite le pone el siguiente número. Eso es lo que en
	// los otros motores se llama identidad «by default» —se puede dar un valor,
	// y si no, lo pone la base—, y hay que decirlo: si no, la revisión de un
	// alta que la omite diría que la columna NOT NULL quedó sin valor y que el
	// alta va a fallar, cuando es el caso normal. Con WITHOUT ROWID no hay
	// rowid que aliasar, y con una clave compuesta tampoco.
	claves := 0
	for _, c := range out {
		if c.PrimaryKey {
			claves++
		}
	}
	if claves == 1 && !strings.Contains(strings.ToUpper(ddl), "WITHOUT ROWID") {
		for i := range out {
			if out[i].PrimaryKey && strings.EqualFold(strings.TrimSpace(out[i].DataType), "INTEGER") {
				out[i].Identity = "by default"
			}
		}
	}

	// SQLite no tiene comentarios de columna, y AUTOINCREMENT —lo más parecido
	// a una identidad— solo se ve en el texto del CREATE TABLE.
	if strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT") {
		t, err := leerCreateTable(ddl)
		if err == nil {
			for _, p := range t.Partes {
				if p.Clase != partColumna ||
					!strings.Contains(strings.ToUpper(p.Texto), "AUTOINCREMENT") {
					continue
				}
				for i := range out {
					if out[i].Name == p.Nombre {
						out[i].Identity = "always"
					}
				}
			}
		}
	}
	return out, nil
}

func detalleIndices(ctx context.Context, db *sql.DB, tabla string) ([]schema.Index, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, "unique", origin, partial
		  FROM pragma_index_list(?) ORDER BY seq`, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer los índices: %w", err)
	}
	defer rows.Close()

	type fila struct {
		nombre, origen string
		unico, parcial bool
	}
	var filas []fila
	for rows.Next() {
		var f fila
		if err := rows.Scan(&f.nombre, &f.unico, &f.origen, &f.parcial); err != nil {
			return nil, fmt.Errorf("leer un índice: %w", err)
		}
		filas = append(filas, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	var out []schema.Index
	for _, f := range filas {
		i := schema.Index{
			Name: f.nombre,
			// SQLite solo tiene un método: B-tree. Se escribe igual para que la
			// pantalla no tenga que preguntar de qué motor viene.
			Method: "btree",
			Unique: f.unico,
			// origin dice de dónde salió: 'c' de un CREATE INDEX, 'u' de una
			// restricción UNIQUE, 'pk' de la clave primaria.
			Primary: f.origen == "pk",
			Scans:   -1, // SQLite no lleva esa estadística.
			Valid:   true,
		}
		cols, err := columnasDeIndice(ctx, db, f.nombre)
		if err != nil {
			return nil, err
		}
		i.Columns = cols
		// El predicado de un índice parcial solo está en el texto del CREATE
		// INDEX: el pragma dice que es parcial pero no cuál es la condición.
		// Un índice parcial mostrado como total es una mentira que cuesta una
		// consulta lenta descubrir.
		def, err := sqlDeObjeto(ctx, db, f.nombre)
		if err != nil {
			return nil, err
		}
		i.Definition = def
		if f.parcial {
			i.Predicate = predicadoDe(def)
		}
		out = append(out, i)
	}
	return out, nil
}

// columnasDeIndice lee las columnas CLAVE de un índice.
//
// index_xinfo y no index_info: la versión corta no distingue las columnas
// clave de las que el índice arrastra —en SQLite, siempre las de la clave
// primaria— y mostrarlas como si fueran clave haría creer que un índice sirve
// para un WHERE que no cubre.
func columnasDeIndice(ctx context.Context, db *sql.DB, indice string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, "desc", key FROM pragma_index_xinfo(?) ORDER BY seqno`, indice)
	if err != nil {
		return nil, fmt.Errorf("leer las columnas del índice %q: %w", indice, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var nombre sql.NullString
		var desc, clave int
		if err := rows.Scan(&nombre, &desc, &clave); err != nil {
			return nil, err
		}
		if clave == 0 {
			continue
		}
		// El nombre viene NULL cuando la entrada es una expresión o el rowid.
		n := nombre.String
		if !nombre.Valid {
			n = "(expresión)"
		}
		if desc == 1 {
			n += " DESC"
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// predicadoDe saca el WHERE del texto de un CREATE INDEX parcial.
func predicadoDe(def string) string {
	toks, err := tokenizar(def)
	if err != nil {
		return ""
	}
	for i, t := range toks {
		if t.prof == 0 && esPalabra(t, "WHERE") {
			return strings.TrimSpace(def[toks[i].fin:])
		}
	}
	return ""
}

func clavesEntrantes(ctx context.Context, db *sql.DB, tabla string) ([]schema.ForeignKey, error) {
	todas, err := leerClaves(ctx, db, "")
	if err != nil {
		return nil, err
	}
	var out []schema.ForeignKey
	for _, fk := range todas {
		// La comparación es sin distinguir mayúsculas porque SQLite compara
		// los nombres de tabla así, y una clave escrita `REFERENCES Padre`
		// apunta a la tabla `padre`.
		if strings.EqualFold(fk.RefTable, tabla) {
			out = append(out, fk)
		}
	}
	return out, nil
}

// detalleChecks saca los CHECK del texto del CREATE TABLE.
//
// No hay otra forma: SQLite no expone las restricciones CHECK en ningún pragma
// ni en ninguna tabla del catálogo. Solo existen en la sentencia que creó la
// tabla.
func detalleChecks(ddl string) ([]schema.CheckConstraint, error) {
	t, err := leerCreateTable(ddl)
	if err != nil {
		// Una tabla que no se puede leer no es motivo para romper la pantalla
		// entera: se muestra sin los CHECK.
		return nil, nil
	}
	var out []schema.CheckConstraint
	for _, p := range t.Partes {
		switch p.Clase {
		case partCheck:
			out = append(out, schema.CheckConstraint{
				Name:       p.Nombre,
				Expression: expresionDelCheck(p.Texto),
				// SQLite no tiene NOT VALID: un CHECK que está, se cumple.
				Validated: true,
			})
		case partColumna:
			col, err := leerColumnaDDL(p.Texto)
			if err != nil {
				continue
			}
			for _, cl := range col.Clausulas {
				if claseDeClausula(cl) != "CHECK" {
					continue
				}
				out = append(out, schema.CheckConstraint{
					Name:       "",
					Expression: expresionDelCheck(cl),
					Validated:  true,
				})
			}
		}
	}
	return out, nil
}

func detalleTriggers(ctx context.Context, db *sql.DB, tabla string) ([]schema.Trigger, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, COALESCE(sql, '') FROM sqlite_schema
		 WHERE type = 'trigger' AND tbl_name = ? ORDER BY name`, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer los triggers: %w", err)
	}
	defer rows.Close()

	var out []schema.Trigger
	for rows.Next() {
		var t schema.Trigger
		if err := rows.Scan(&t.Name, &t.Definition); err != nil {
			return nil, fmt.Errorf("leer un trigger: %w", err)
		}
		t.Timing, t.Events = disparoDe(t.Definition)
		// En SQLite un trigger es SIEMPRE por fila y no se puede deshabilitar.
		t.Level = "row"
		t.Enabled = true
		t.Function = "(cuerpo propio)"
		out = append(out, t)
	}
	return out, rows.Err()
}

// disparoDe lee cuándo y con qué se dispara un trigger, del texto que lo creó.
// SQLite no lo guarda en columnas separadas.
func disparoDe(def string) (timing string, eventos []string) {
	toks, err := tokenizar(def)
	if err != nil {
		return "", nil
	}
	timing = "before" // el default de SQLite cuando no se dice nada
	for i, t := range toks {
		if t.kind != tokPalabra {
			continue
		}
		switch strings.ToUpper(t.texto) {
		case "BEFORE":
			timing = "before"
		case "AFTER":
			timing = "after"
		case "INSTEAD":
			timing = "instead of"
		case "INSERT", "DELETE":
			eventos = append(eventos, strings.ToLower(t.texto))
		case "UPDATE":
			eventos = append(eventos, "update")
		case "ON":
			// A partir de acá viene la tabla: lo que sigue ya no son eventos.
			_ = i
			return timing, eventos
		}
	}
	return timing, eventos
}

// primaryKeyColumns es por dónde ordenar para que el paginado sea estable.
func primaryKeyColumns(ctx context.Context, db *sql.DB, tabla string) ([]string, error) {
	// pk es la posición dentro de la clave, empezando en 1, así que ordenar por
	// ella da el orden en que se declaró — que es el que importa en una clave
	// compuesta.
	rows, err := db.QueryContext(ctx, `
		SELECT name FROM pragma_table_xinfo(?) WHERE pk > 0 ORDER BY pk`, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer la clave primaria: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
