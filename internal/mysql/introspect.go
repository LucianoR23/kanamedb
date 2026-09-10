package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// introspect lee el esquema completo para el árbol y el diagrama.
//
// En MySQL «esquema» y «base» son la misma cosa, así que el snapshot trae un
// solo Schema con el nombre de la base conectada. No se listan las otras bases
// del servidor: la conexión está posicionada en una, y mostrar las demás daría
// un árbol donde la mitad de los objetos no se pueden abrir.
func introspect(ctx context.Context, db *sql.DB, base string) (*schema.Snapshot, error) {
	snap := &schema.Snapshot{Database: base, CapturedAt: time.Now()}
	esq := schema.Schema{Name: base}

	tablas, orden, err := leerTablas(ctx, db, base)
	if err != nil {
		return nil, err
	}
	if err := leerColumnas(ctx, db, base, tablas); err != nil {
		return nil, err
	}
	if err := leerClavesDelEsquema(ctx, db, base, tablas); err != nil {
		return nil, err
	}
	for _, nombre := range orden {
		esq.Tables = append(esq.Tables, *tablas[nombre])
	}
	snap.Schemas = []schema.Schema{esq}
	return snap, nil
}

func leerTablas(ctx context.Context, db *sql.DB, base string) (map[string]*schema.Table, []string, error) {
	const q = `
		SELECT table_name,
		       COALESCE(table_comment, ''),
		       -- table_rows es una ESTIMACIÓN de InnoDB, no un conteo. Puede
		       -- errar por mucho, y por eso la interfaz la muestra como
		       -- aproximada igual que la de Postgres.
		       COALESCE(table_rows, -1),
		       COALESCE(create_options, '')
		  FROM information_schema.tables
		 WHERE table_schema = ? AND table_type = 'BASE TABLE'
		 ORDER BY table_name`

	rows, err := db.QueryContext(ctx, q, base)
	if err != nil {
		return nil, nil, fmt.Errorf("leer las tablas: %w", err)
	}
	defer rows.Close()

	tablas := map[string]*schema.Table{}
	var orden []string
	for rows.Next() {
		var t schema.Table
		var opciones string
		if err := rows.Scan(&t.Name, &t.Comment, &t.RowEstimate, &opciones); err != nil {
			return nil, nil, fmt.Errorf("leer una tabla: %w", err)
		}
		// Una tabla particionada lo dice en create_options. Sus particiones no
		// son tablas propias en MySQL, así que no hay nada que esconder del
		// árbol; el dato se usa para el aviso de la pantalla de estructura.
		t.Partitioned = strings.Contains(opciones, "partitioned")
		// Si el usuario ve la tabla en information_schema es porque tiene algún
		// privilegio sobre ella: MySQL filtra el catálogo por permisos.
		t.Readable = true
		tablas[t.Name] = &t
		orden = append(orden, t.Name)
	}
	return tablas, orden, rows.Err()
}

func leerColumnas(ctx context.Context, db *sql.DB, base string, tablas map[string]*schema.Table) error {
	const q = `
		SELECT table_name, column_name,
		       -- column_type trae el tipo CON sus modificadores
		       -- —varchar(255), decimal(10,2), enum('a','b')— que es lo que hay
		       -- que mostrar. data_type solo trae "varchar", que no alcanza.
		       column_type,
		       is_nullable = 'YES',
		       column_default IS NOT NULL OR extra LIKE '%%DEFAULT_GENERATED%%',
		       column_key = 'PRI',
		       ordinal_position
		  FROM information_schema.columns
		 WHERE table_schema = ?
		 ORDER BY table_name, ordinal_position`

	rows, err := db.QueryContext(ctx, q, base)
	if err != nil {
		return fmt.Errorf("leer las columnas: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tabla string
		var c schema.Column
		if err := rows.Scan(&tabla, &c.Name, &c.DataType, &c.Nullable,
			&c.HasDefault, &c.PrimaryKey, &c.Position); err != nil {
			return fmt.Errorf("leer una columna: %w", err)
		}
		if t, ok := tablas[tabla]; ok {
			t.Columns = append(t.Columns, c)
			if c.PrimaryKey {
				t.HasPrimaryKey = true
			}
		}
	}
	return rows.Err()
}

// leerClavesDelEsquema trae todas las claves foráneas de la base de una vez.
//
// Son las aristas del ERD, y el diagrama las necesita juntas: pedirlas tabla por
// tabla serían tantos viajes como tablas antes de dibujar la primera línea.
func leerClavesDelEsquema(ctx context.Context, db *sql.DB, base string, tablas map[string]*schema.Table) error {
	fks, err := leerClaves(ctx, db, base, "")
	if err != nil {
		return err
	}
	// Marcar las columnas que participan, para las etiquetas FK de la grilla.
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
// El ORDER BY por ordinal_position no es cosmético: en una clave compuesta las
// columnas emparejan POSICIONALMENTE con las del otro lado, así que leerlas
// desordenadas daría una relación que apunta a la columna equivocada.
func leerClaves(ctx context.Context, db *sql.DB, base, tabla string) ([]schema.ForeignKey, error) {
	q := `
		SELECT k.constraint_name, k.table_schema, k.table_name, k.column_name,
		       k.referenced_table_schema, k.referenced_table_name, k.referenced_column_name,
		       r.delete_rule, r.update_rule,
		       c.is_nullable = 'YES'
		  FROM information_schema.key_column_usage k
		  JOIN information_schema.referential_constraints r
		    ON r.constraint_schema = k.constraint_schema
		   AND r.constraint_name = k.constraint_name
		   AND r.table_name = k.table_name
		  JOIN information_schema.columns c
		    ON c.table_schema = k.table_schema
		   AND c.table_name = k.table_name
		   AND c.column_name = k.column_name
		 WHERE k.table_schema = ? AND k.referenced_table_name IS NOT NULL`
	args := []any{base}
	if tabla != "" {
		q += " AND k.table_name = ?"
		args = append(args, tabla)
	}
	q += " ORDER BY k.table_name, k.constraint_name, k.ordinal_position"

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("leer las claves foráneas: %w", err)
	}
	defer rows.Close()

	// Una clave compuesta llega como varias filas: se juntan por nombre y tabla.
	var out []schema.ForeignKey
	indice := map[string]int{}
	for rows.Next() {
		var nombre, esq, tab, col, refEsq, refTab, refCol, alBorrar, alActualizar string
		var nulable bool
		if err := rows.Scan(&nombre, &esq, &tab, &col, &refEsq, &refTab, &refCol,
			&alBorrar, &alActualizar, &nulable); err != nil {
			return nil, fmt.Errorf("leer una clave foránea: %w", err)
		}
		clave := esq + "." + tab + "." + nombre
		i, ok := indice[clave]
		if !ok {
			out = append(out, schema.ForeignKey{
				Name: nombre, Schema: esq, Table: tab,
				RefSchema: refEsq, RefTable: refTab,
				OnDelete: schema.ReferenceAction(strings.ToLower(alBorrar)),
				OnUpdate: schema.ReferenceAction(strings.ToLower(alActualizar)),
			})
			i = len(out) - 1
			indice[clave] = i
		}
		out[i].Columns = append(out[i].Columns, col)
		out[i].RefColumns = append(out[i].RefColumns, refCol)
		// Basta una columna que admita nulos para que la relación sea opcional,
		// igual que con MATCH SIMPLE en Postgres: la arista va punteada.
		if nulable {
			out[i].Optional = true
		}
	}
	return out, rows.Err()
}

// detail lee todo lo de UNA tabla.
func detail(
	ctx context.Context, db *sql.DB, base, tabla string, kind engine.Kind, sinEscapes bool,
) (*schema.TableDetail, error) {
	d := &schema.TableDetail{
		Schema: base, Name: tabla, RowEstimate: -1, CapturedAt: time.Now(),
	}

	const qTabla = `
		SELECT COALESCE(table_comment, ''), COALESCE(table_rows, -1),
		       COALESCE(data_length, 0), COALESCE(data_length + index_length, 0)
		  FROM information_schema.tables
		 WHERE table_schema = ? AND table_name = ?`
	if err := db.QueryRowContext(ctx, qTabla, base, tabla).Scan(
		&d.Comment, &d.RowEstimate, &d.TableBytes, &d.TotalBytes); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("la tabla %s.%s no existe", base, tabla)
		}
		return nil, fmt.Errorf("leer la tabla: %w", err)
	}

	var err error
	if d.Columns, err = detalleColumnas(ctx, db, base, tabla, kind, sinEscapes); err != nil {
		return nil, err
	}
	if d.Indexes, err = detalleIndices(ctx, db, base, tabla, kind); err != nil {
		return nil, err
	}
	if d.ForeignKeys, err = leerClaves(ctx, db, base, tabla); err != nil {
		return nil, err
	}
	if d.ReferencedBy, err = clavesEntrantes(ctx, db, base, tabla); err != nil {
		return nil, err
	}
	if d.Checks, err = detalleChecks(ctx, db, base, tabla, kind); err != nil {
		return nil, err
	}
	if d.Triggers, err = detalleTriggers(ctx, db, base, tabla); err != nil {
		return nil, err
	}

	// Marcar qué columnas son clave foránea, para el ícono de la grilla.
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

func detalleColumnas(
	ctx context.Context, db *sql.DB, base, tabla string, kind engine.Kind, sinEscapes bool,
) ([]schema.DetailColumn, error) {
	const q = `
		SELECT column_name, column_type, is_nullable = 'YES', ordinal_position,
		       COALESCE(column_default, ''), COALESCE(column_comment, ''),
		       column_key = 'PRI',
		       COALESCE(generation_expression, ''),
		       COALESCE(extra, '')
		  FROM information_schema.columns
		 WHERE table_schema = ? AND table_name = ?
		 ORDER BY ordinal_position`

	rows, err := db.QueryContext(ctx, q, base, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer las columnas: %w", err)
	}
	defer rows.Close()

	var out []schema.DetailColumn
	for rows.Next() {
		var c schema.DetailColumn
		var generacion, extra string
		if err := rows.Scan(&c.Name, &c.DataType, &c.Nullable, &c.Position,
			&c.Default, &c.Comment, &c.PrimaryKey, &generacion, &extra); err != nil {
			return nil, fmt.Errorf("leer una columna: %w", err)
		}
		if generacion != "" {
			// MySQL escribe VIRTUAL GENERATED o STORED GENERATED en `extra`.
			// La distinción importa igual que en Postgres: una virtual no
			// ocupa lugar y se recalcula al leer.
			if strings.Contains(extra, "STORED") {
				c.Generated = "stored"
			} else {
				c.Generated = "virtual"
			}
			// La expresión vive acá y no en Default, para no repetir el error
			// que ya se cometió con Postgres: ofrecer «sacar el valor por
			// defecto» sobre una columna generada.
			c.Default = ""
		} else {
			c.Default = normalizarDefault(c.Default, c.DataType, extra, kind, sinEscapes)
		}
		if strings.Contains(extra, "auto_increment") {
			// El equivalente más cercano a una columna de identidad.
			c.Identity = "by default"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func detalleIndices(ctx context.Context, db *sql.DB, base, tabla string, kind engine.Kind) ([]schema.Index, error) {
	// `expression` existe en MySQL 8.0+, donde guarda la fórmula de un índice
	// funcional, y NO existe en MariaDB — que directamente no tiene índices
	// funcionales, se hacen con una columna generada indexada. Pedirla contra
	// MariaDB da «Unknown column» y se cae la pantalla de estructura entera.
	q := `
		SELECT index_name, non_unique = 0, index_type, column_name, seq_in_index,
		       COALESCE(expression, '')
		  FROM information_schema.statistics
		 WHERE table_schema = ? AND table_name = ?
		 ORDER BY index_name, seq_in_index`
	if kind == engine.MariaDB {
		q = `
			SELECT index_name, non_unique = 0, index_type, column_name, seq_in_index, ''
			  FROM information_schema.statistics
			 WHERE table_schema = ? AND table_name = ?
			 ORDER BY index_name, seq_in_index`
	}

	rows, err := db.QueryContext(ctx, q, base, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer los índices: %w", err)
	}
	defer rows.Close()

	var out []schema.Index
	indice := map[string]int{}
	for rows.Next() {
		var nombre, tipo, expresion string
		var unico bool
		var seq int
		var columna sql.NullString
		if err := rows.Scan(&nombre, &unico, &tipo, &columna, &seq, &expresion); err != nil {
			return nil, fmt.Errorf("leer un índice: %w", err)
		}
		i, ok := indice[nombre]
		if !ok {
			out = append(out, schema.Index{
				Name:   nombre,
				Method: strings.ToLower(tipo),
				Unique: unico,
				// En MySQL la clave primaria SIEMPRE se llama PRIMARY. No es
				// una convención: el motor no deja llamarla de otra forma.
				Primary: nombre == "PRIMARY",
				Scans:   -1, // MySQL no lleva esa estadística por índice.
				Valid:   true,
			})
			i = len(out) - 1
			indice[nombre] = i
		}
		// Un índice funcional tiene column_name en NULL y la expresión aparte.
		if columna.Valid {
			out[i].Columns = append(out[i].Columns, columna.String)
		} else if expresion != "" {
			out[i].Columns = append(out[i].Columns, "("+expresion+")")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Definition = definicionDeIndice(base, tabla, out[i])
	}
	return out, nil
}

// definicionDeIndice arma el CREATE INDEX para poder copiarlo.
//
// MySQL no guarda la definición como texto —a diferencia de
// pg_get_indexdef— así que se reconstruye. La primaria no se puede crear con
// CREATE INDEX, así que se muestra como ALTER.
func definicionDeIndice(base, tabla string, i schema.Index) string {
	cols := make([]string, 0, len(i.Columns))
	for _, c := range i.Columns {
		if strings.HasPrefix(c, "(") {
			cols = append(cols, c)
		} else {
			cols = append(cols, QuoteIdent(c))
		}
	}
	nombre := QualifiedName(base, tabla)
	if i.Primary {
		return fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", nombre, strings.Join(cols, ", "))
	}
	unico := ""
	if i.Unique {
		unico = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
		unico, QuoteIdent(i.Name), nombre, strings.Join(cols, ", "))
}

func clavesEntrantes(ctx context.Context, db *sql.DB, base, tabla string) ([]schema.ForeignKey, error) {
	const q = `
		SELECT k.constraint_name, k.table_schema, k.table_name, k.column_name,
		       k.referenced_table_schema, k.referenced_table_name, k.referenced_column_name,
		       r.delete_rule, r.update_rule
		  FROM information_schema.key_column_usage k
		  JOIN information_schema.referential_constraints r
		    ON r.constraint_schema = k.constraint_schema
		   AND r.constraint_name = k.constraint_name
		   AND r.table_name = k.table_name
		 WHERE k.referenced_table_schema = ? AND k.referenced_table_name = ?
		 ORDER BY k.table_name, k.constraint_name, k.ordinal_position`

	rows, err := db.QueryContext(ctx, q, base, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer las claves entrantes: %w", err)
	}
	defer rows.Close()

	var out []schema.ForeignKey
	indice := map[string]int{}
	for rows.Next() {
		var nombre, esq, tab, col, refEsq, refTab, refCol, alBorrar, alActualizar string
		if err := rows.Scan(&nombre, &esq, &tab, &col, &refEsq, &refTab, &refCol,
			&alBorrar, &alActualizar); err != nil {
			return nil, fmt.Errorf("leer una clave entrante: %w", err)
		}
		clave := esq + "." + tab + "." + nombre
		i, ok := indice[clave]
		if !ok {
			out = append(out, schema.ForeignKey{
				Name: nombre, Schema: esq, Table: tab,
				RefSchema: refEsq, RefTable: refTab,
				OnDelete: schema.ReferenceAction(strings.ToLower(alBorrar)),
				OnUpdate: schema.ReferenceAction(strings.ToLower(alActualizar)),
			})
			i = len(out) - 1
			indice[clave] = i
		}
		out[i].Columns = append(out[i].Columns, col)
		out[i].RefColumns = append(out[i].RefColumns, refCol)
	}
	return out, rows.Err()
}

func detalleChecks(ctx context.Context, db *sql.DB, base, tabla string, kind engine.Kind) ([]schema.CheckConstraint, error) {
	// MySQL y MariaDB guardan los CHECK en lugares distintos, y esta es una de
	// las divergencias que obliga a preguntar cuál motor es. MySQL 8.0.16+ los
	// pone en check_constraints con el nombre de la restricción; MariaDB los
	// tiene en su propia vista con el nombre de la columna cuando son de
	// columna.
	q := `
		SELECT cc.constraint_name, cc.check_clause
		  FROM information_schema.check_constraints cc
		  JOIN information_schema.table_constraints tc
		    ON tc.constraint_schema = cc.constraint_schema
		   AND tc.constraint_name = cc.constraint_name
		 WHERE tc.table_schema = ? AND tc.table_name = ?`
	if kind == engine.MariaDB {
		q = `
			SELECT constraint_name, check_clause
			  FROM information_schema.check_constraints
			 WHERE constraint_schema = ? AND table_name = ?`
	}

	rows, err := db.QueryContext(ctx, q, base, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer las restricciones: %w", err)
	}
	defer rows.Close()

	var out []schema.CheckConstraint
	for rows.Next() {
		var c schema.CheckConstraint
		if err := rows.Scan(&c.Name, &c.Expression); err != nil {
			return nil, fmt.Errorf("leer una restricción: %w", err)
		}
		// En MySQL y MariaDB no existe NOT VALID: un CHECK que está, se cumple.
		c.Validated = true
		out = append(out, c)
	}
	return out, rows.Err()
}

func detalleTriggers(ctx context.Context, db *sql.DB, base, tabla string) ([]schema.Trigger, error) {
	const q = `
		SELECT trigger_name, action_timing, event_manipulation, action_statement
		  FROM information_schema.triggers
		 WHERE event_object_schema = ? AND event_object_table = ?
		 ORDER BY trigger_name`

	rows, err := db.QueryContext(ctx, q, base, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer los triggers: %w", err)
	}
	defer rows.Close()

	var out []schema.Trigger
	for rows.Next() {
		var t schema.Trigger
		var timing, evento, cuerpo string
		if err := rows.Scan(&t.Name, &timing, &evento, &cuerpo); err != nil {
			return nil, fmt.Errorf("leer un trigger: %w", err)
		}
		t.Timing = strings.ToLower(timing)
		t.Events = []string{strings.ToLower(evento)}
		// En MySQL un trigger es SIEMPRE por fila y no se puede deshabilitar.
		// Se dice acá en vez de dejar los campos en cero, para que la pantalla
		// no invente que puede estar apagado.
		t.Level = "row"
		t.Enabled = true
		t.Function = "(cuerpo propio)"
		t.Definition = fmt.Sprintf("CREATE TRIGGER %s %s %s ON %s FOR EACH ROW %s",
			QuoteIdent(t.Name), strings.ToUpper(timing), strings.ToUpper(evento),
			QualifiedName(base, tabla), cuerpo)
		out = append(out, t)
	}
	return out, rows.Err()
}

func primaryKeyColumns(ctx context.Context, db *sql.DB, base, tabla string) ([]string, error) {
	const q = `
		SELECT column_name
		  FROM information_schema.statistics
		 WHERE table_schema = ? AND table_name = ? AND index_name = 'PRIMARY'
		 ORDER BY seq_in_index`
	rows, err := db.QueryContext(ctx, q, base, tabla)
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

// normalizarDefault deja el valor por defecto escrito como SQL, listo para
// volver a ponerlo en un DDL.
//
// Hace falta porque los dos motores lo devuelven de forma DISTINTA, y el que
// más se usa es el que lo devuelve mal:
//
//	varchar DEFAULT 'nuevo'    MySQL: nuevo              MariaDB: 'nuevo'
//	int     DEFAULT 5          MySQL: 5                  MariaDB: 5
//	CURRENT_TIMESTAMP          MySQL: CURRENT_TIMESTAMP  MariaDB: current_timestamp()
//	                                  + extra=DEFAULT_GENERATED
//
// O sea que en MySQL un literal de texto llega SIN comillas. Y como MySQL no
// tiene COMMENT ON, comentar una columna la reescribe entera con MODIFY —hay
// que repetirle el tipo, la nulabilidad y el default o los pierde—, así que ese
// valor vuelve a salir en un DDL. Sin comillas, sale
// `DEFAULT nuevo` y el motor contesta un error de sintaxis. Comprobado contra
// MySQL 9.7 desde la aplicación.
//
// La regla es por motor porque la ambigüedad es por motor:
//
//   - MariaDB ya lo entrega como expresión: un literal de texto viene con
//     comillas y una función viene sin ellas. No hay nada que decidir.
//   - MySQL entrega el VALOR pelado, y marca las expresiones en `extra`. Lo que
//     no esté marcado es un literal, y hay que citarlo salvo que la columna sea
//     numérica —donde `b'1'` de un bit y el `5` de un int ya están escritos como
//     hay que escribirlos—.
func normalizarDefault(valor, tipo, extra string, kind engine.Kind, sinEscapes bool) string {
	if valor == "" {
		return ""
	}
	if kind == engine.MariaDB {
		return valor
	}
	// MySQL 8.0.13+ marca así los defaults que son expresión.
	if strings.Contains(extra, "DEFAULT_GENERATED") {
		return valor
	}
	if tipoNumerico(tipo) {
		return valor
	}
	// Se cita en el modo del SERVIDOR: con NO_BACKSLASH_ESCAPES la barra no
	// escapa nada, y duplicarla guardaría dos. Es el mismo error que ya se
	// cometió una vez con los comentarios.
	return quoteString(valor, sinEscapes)
}

// tipoNumerico dice si el tipo escribe su default sin comillas.
//
// Se mira el TIPO y no si el valor parece un número: un `varchar` con default
// `5` guarda el texto "5", y dejarlo pelado ahí sería suerte, no criterio.
func tipoNumerico(tipo string) bool {
	t := strings.ToLower(tipo)
	if i := strings.IndexAny(t, "( "); i > 0 {
		t = t[:i]
	}
	switch t {
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint",
		"decimal", "dec", "numeric", "fixed", "float", "double", "real", "bit", "bool", "boolean":
		return true
	}
	return false
}
