package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// La reconstrucción de tabla.
//
// SQLite tiene exactamente cuatro ALTER TABLE: RENAME TO, RENAME COLUMN, ADD
// COLUMN y DROP COLUMN. Todo lo demás —cambiar un tipo, exigir que una columna
// no sea nula, sacarle el valor por defecto, agregar un CHECK, una clave
// primaria o una foránea— se hace de una sola forma: creando una tabla nueva
// con la definición que se quiere, copiando las filas, tirando la vieja y
// renombrando la nueva.
//
// El procedimiento está en el manual de SQLite y tiene doce pasos. Los que no
// se ven acá son los pragmas de alrededor, que están en Conn.Begin: apagar las
// claves foráneas ANTES del BEGIN y comprobarlas antes del COMMIT. Están allá
// porque un PRAGMA foreign_keys adentro de una transacción no hace nada, y
// porque sin apagarlas el DROP TABLE del paso 3 dispara los ON DELETE CASCADE
// de las tablas que referencian a esta y borra sus filas en silencio.
//
// El orden de los cuatro pasos que SÍ están acá tampoco es casual, y la
// alternativa que parece más natural es la que rompe. Renombrar primero la
// tabla vieja para dejar el nombre libre —«ALTER TABLE x RENAME TO x_viejo»—
// hace que SQLite reescriba el REFERENCES de todas las tablas que la apuntan
// para que sigan apuntándole. Comprobado, y no lo evita PRAGMA
// legacy_alter_table: con el pragma leyendo 1, el rename reescribió la
// referencia igual. Después el DROP de la vieja se lleva puestas las filas
// hijas. Por eso la tabla nueva se crea con un nombre temporal, y el nombre
// original nunca cambia de dueño: se libera con el DROP y se toma con el
// RENAME, sin que nadie tenga que seguirlo.

// prefijoTemporal es el nombre que lleva la tabla nueva mientras se copia.
const prefijoTemporal = "kn_rebuild_"

// rebuildDDL arma el guion de reconstrucción para los cambios que SQLite no
// sabe hacer con un ALTER.
func rebuildDDL(ctx context.Context, db *sql.DB, c change.Change) (change.Statement, error) {
	st := change.Statement{
		ChangeID:      c.ID,
		Destructive:   c.Destructive(),
		Impact:        change.ImpactRewrite,
		Lock:          change.LockAll,
		RebuildsTable: true,
	}

	ddl, err := sqlDeObjeto(ctx, db, c.Table)
	if err != nil {
		return st, err
	}
	if ddl == "" {
		return st, fmt.Errorf("la tabla %q no existe", c.Table)
	}
	// Una tabla VIRTUAL —FTS5, R-Tree, csv— también figura como type='table' en
	// el catálogo, y su definición es «CREATE VIRTUAL TABLE t USING fts5(...)».
	// Reconstruirla con el procedimiento normal escribiría un CREATE TABLE
	// común: la sentencia funcionaría, y lo que quedaría en su lugar sería una
	// tabla corriente con los mismos nombres de columna y sin el módulo — o
	// sea, un índice de texto completo convertido en texto suelto.
	if esVirtual(ddl) {
		return st, &engine.ErrUnsupported{
			Engine: engine.SQLite, Operation: "modificar una tabla virtual",
			Motivo: c.Table + " la creó un módulo (FTS, R-Tree u otro) y solo ese módulo " +
				"sabe cómo está hecha por dentro; hay que borrarla y volver a crearla",
		}
	}

	t, err := leerCreateTable(ddl)
	if err != nil {
		return st, fmt.Errorf(
			"no se pudo leer la definición de %q, así que no se puede reescribir sin "+
				"riesgo de perder algo: %w", c.Table, err)
	}

	nota, err := aplicarCambio(t, c)
	if err != nil {
		return st, err
	}

	temporal := prefijoTemporal + c.Table
	if err := libre(ctx, db, temporal); err != nil {
		return st, err
	}

	// Las columnas que se copian: las del esquema ACTUAL, en orden, salvo las
	// generadas —no se puede insertar en ellas, se recalculan solas—.
	cols, err := columnasCopiables(ctx, db, c.Table)
	if err != nil {
		return st, err
	}
	if len(cols) == 0 {
		return st, fmt.Errorf("la tabla %q no tiene columnas que copiar", c.Table)
	}
	lista := listaDeIdent(cols)

	// Los índices y los triggers se van con el DROP TABLE y hay que volver a
	// crearlos. Se guardan sus sentencias tal cual están en el catálogo.
	recrear, err := indicesYTriggers(ctx, db, c.Table)
	if err != nil {
		return st, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "-- %s no se puede hacer con un ALTER en SQLite: se reconstruye la tabla.\n",
		c.Type)
	fmt.Fprintf(&b, "%s;\n", t.CreateComoTabla(temporal))
	fmt.Fprintf(&b, "INSERT INTO %s (%s)\n     SELECT %s FROM %s;\n",
		QuoteIdent(temporal), lista, lista, QuoteIdent(c.Table))
	fmt.Fprintf(&b, "DROP TABLE %s;\n", QuoteIdent(c.Table))
	fmt.Fprintf(&b, "ALTER TABLE %s RENAME TO %s;", QuoteIdent(temporal), QuoteIdent(c.Table))
	if seq, hay := secuenciaDe(ctx, db, c.Table); hay {
		// El contador de AUTOINCREMENT vive en sqlite_sequence, y el DROP TABLE
		// se lleva su fila. La copia crea una nueva con el máximo de las filas
		// que quedaron, que NO es lo mismo: si se habían borrado las últimas, el
		// contador RETROCEDE y la próxima fila recibe un id que ya se usó.
		// AUTOINCREMENT existe justamente para garantizar que eso no pase, así
		// que se lo devuelve a donde estaba.
		// Se borra y se inserta, y no se usa INSERT OR REPLACE: sqlite_sequence
		// se declara como `CREATE TABLE sqlite_sequence(name,seq)`, sin clave
		// primaria y sin índice único, así que no hay conflicto que resolver y
		// el OR REPLACE agrega una fila más en vez de pisar la que está.
		fmt.Fprintf(&b, "\nDELETE FROM sqlite_sequence WHERE name = %s;", QuoteString(c.Table))
		fmt.Fprintf(&b, "\nINSERT INTO sqlite_sequence (name, seq) VALUES (%s, %d);",
			QuoteString(c.Table), seq)
	}
	for _, s := range recrear {
		fmt.Fprintf(&b, "\n%s;", s)
	}

	st.SQL = b.String()
	st.Note = nota + " La tabla se reconstruye entera: tarda en proporción a su tamaño y " +
		"necesita lugar en disco para las dos copias mientras corre."
	if len(recrear) > 0 {
		st.Note += fmt.Sprintf(" Se vuelven a crear %d índices o triggers.", len(recrear))
	}
	return st, nil
}

// CreateComoTabla vuelve a escribir el CREATE TABLE con otro nombre.
//
// Se reemplaza SOLO el nombre del encabezado. El resto del texto —incluidas
// las referencias a la propia tabla en una clave foránea a sí misma— queda con
// el nombre original, que es lo correcto: el nombre temporal dura tres
// sentencias y la tabla termina llamándose como se llamaba.
func (t *tablaDDL) CreateComoTabla(nombre string) string {
	partes := make([]string, 0, len(t.Partes))
	for _, p := range t.Partes {
		partes = append(partes, "    "+p.Texto)
	}
	cola := ""
	if t.Cola != "" {
		cola = " " + t.Cola
	}
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n)%s",
		QuoteIdent(nombre), strings.Join(partes, ",\n"), cola)
}

// aplicarCambio modifica la definición leída. Devuelve la nota para el preview.
func aplicarCambio(t *tablaDDL, c change.Change) (string, error) {
	switch c.Type {

	case change.SetColumnType:
		if !tipoValido.MatchString(c.DataType) {
			return "", fmt.Errorf("el tipo %q tiene caracteres que no se aceptan", c.DataType)
		}
		if err := t.enColumna(c.Column.Name, func(col *columnaDDL) error {
			col.Tipo = c.DataType
			return nil
		}); err != nil {
			return "", err
		}
		// Vale la pena decirlo: en SQLite el tipo declarado no obliga a nada.
		return "En SQLite el tipo de una columna es una AFINIDAD, no una restricción: los " +
			"valores que ya están se convierten si se puede y se dejan como están si no. " +
			"No hay error de conversión que avise.", nil

	case change.SetNotNull:
		if err := t.enColumna(c.Column.Name, func(col *columnaDDL) error {
			col.ponerClausula("NOT NULL", "NOT NULL")
			return nil
		}); err != nil {
			return "", err
		}
		return "Si alguna fila tiene NULL en esa columna, la copia falla y no queda nada " +
			"aplicado.", nil

	case change.DropNotNull:
		if err := t.enColumna(c.Column.Name, func(col *columnaDDL) error {
			if !col.sacarClausula("NOT NULL") {
				return fmt.Errorf("la columna %q ya admite nulos", c.Column.Name)
			}
			return nil
		}); err != nil {
			return "", err
		}
		return "", nil

	case change.SetDefault:
		if err := t.enColumna(c.Column.Name, func(col *columnaDDL) error {
			col.ponerClausula("DEFAULT", "DEFAULT "+c.Column.Default)
			return nil
		}); err != nil {
			return "", err
		}
		return "El valor por defecto solo se aplica a las filas nuevas: las que ya están no " +
			"cambian.", nil

	case change.DropDefault:
		if err := t.enColumna(c.Column.Name, func(col *columnaDDL) error {
			if !col.sacarClausula("DEFAULT") {
				return fmt.Errorf("la columna %q no tiene valor por defecto", c.Column.Name)
			}
			return nil
		}); err != nil {
			return "", err
		}
		return "", nil

	case change.AddPrimaryKey:
		for _, p := range t.Partes {
			if p.Clase == partPrimary {
				return "", fmt.Errorf("la tabla %q ya tiene clave primaria", c.Table)
			}
		}
		// Una PRIMARY KEY declarada en la columna cuenta igual, y no aparece
		// como parte de tabla.
		for _, p := range t.Partes {
			if p.Clase != partColumna {
				continue
			}
			col, err := leerColumnaDDL(p.Texto)
			if err == nil && col.tieneClausula("PRIMARY") {
				return "", fmt.Errorf(
					"la columna %q ya está declarada como clave primaria", col.Nombre)
			}
		}
		t.Partes = append(t.Partes, parte{
			Clase: partPrimary,
			Texto: restriccion(c.Name, "PRIMARY KEY ("+listaDeIdent(c.Names)+")"),
		})
		return "Se comprueba que no haya repetidos ni nulos en esas columnas.", nil

	case change.AddForeignKey:
		var b strings.Builder
		fmt.Fprintf(&b, "FOREIGN KEY (%s) REFERENCES %s (%s)",
			listaDeIdent(c.Names), QuoteIdent(c.RefTable), listaDeIdent(c.RefNames))
		if a := accionReferencial(c.OnDelete); a != "" {
			fmt.Fprintf(&b, " ON DELETE %s", a)
		}
		if a := accionReferencial(c.OnUpdate); a != "" {
			fmt.Fprintf(&b, " ON UPDATE %s", a)
		}
		t.Partes = append(t.Partes, parte{
			Clase: partForanea,
			Texto: restriccion(c.Name, b.String()),
		})
		return "Se comprueba que cada fila tenga su fila en la otra tabla, al final de la " +
			"transacción y no al copiar.", nil

	case change.AddCheck:
		t.Partes = append(t.Partes, parte{
			Clase: partCheck,
			Texto: restriccion(c.Name, "CHECK ("+c.Expression+")"),
		})
		return "Se comprueba contra todas las filas que ya están.", nil

	case change.DropConstraint:
		return "", t.sacarRestriccion(c.Name)
	}

	return "", &engine.ErrUnsupported{Engine: engine.SQLite, Operation: string(c.Type)}
}

// restriccion antepone el CONSTRAINT con su nombre, si se dio uno.
func restriccion(nombre, cuerpo string) string {
	if nombre == "" {
		return cuerpo
	}
	return "CONSTRAINT " + QuoteIdent(nombre) + " " + cuerpo
}

// enColumna busca una columna y deja que la modifiquen.
func (t *tablaDDL) enColumna(nombre string, f func(*columnaDDL) error) error {
	for i, p := range t.Partes {
		if p.Clase != partColumna || !strings.EqualFold(p.Nombre, nombre) {
			continue
		}
		col, err := leerColumnaDDL(p.Texto)
		if err != nil {
			return fmt.Errorf("leer la definición de la columna %q: %w", nombre, err)
		}
		if col.Generada {
			// Una columna generada no se reescribe. Su expresión puede tener
			// cualquier cosa adentro y cambiarle el tipo o la nulabilidad
			// significa algo distinto que en una columna normal.
			return &engine.ErrUnsupported{
				Engine: engine.SQLite, Operation: "modificar una columna generada",
				Motivo: "la columna " + nombre + " se calcula a partir de otras; " +
					"para cambiarla hay que borrarla y volver a agregarla",
			}
		}
		if err := f(col); err != nil {
			return err
		}
		t.Partes[i].Texto = col.String()
		return nil
	}
	return fmt.Errorf("la tabla no tiene una columna llamada %q", nombre)
}

// sacarRestriccion saca del CREATE TABLE la restricción con ese nombre.
func (t *tablaDDL) sacarRestriccion(nombre string) error {
	var quedan []parte
	sacó := false
	for _, p := range t.Partes {
		if p.Clase != partColumna && strings.EqualFold(p.Nombre, nombre) {
			sacó = true
			continue
		}
		quedan = append(quedan, p)
	}
	if !sacó {
		return fmt.Errorf(
			"la tabla no tiene una restricción llamada %q. En SQLite las restricciones sin "+
				"nombre no se pueden borrar por separado: hay que reescribir la tabla", nombre)
	}
	t.Partes = quedan
	return nil
}

// esVirtual dice si la definición es la de una tabla creada por un módulo.
func esVirtual(ddl string) bool {
	toks, err := tokenizar(ddl)
	if err != nil {
		// Si no se puede leer, tratarla como virtual es el lado seguro: se
		// niega en vez de reescribir algo que no se entendió.
		return true
	}
	for i, t := range toks {
		if i > 3 {
			break
		}
		if esPalabra(t, "VIRTUAL") {
			return true
		}
	}
	return false
}

// secuenciaDe lee el contador de AUTOINCREMENT de la tabla, si tiene.
func secuenciaDe(ctx context.Context, db *sql.DB, tabla string) (int64, bool) {
	var seq int64
	err := db.QueryRowContext(ctx,
		`SELECT seq FROM sqlite_sequence WHERE name = ?`, tabla).Scan(&seq)
	if err != nil {
		// sqlite_sequence solo existe si alguna tabla de la base usa
		// AUTOINCREMENT, y la fila solo si esta tuvo algún insert.
		return 0, false
	}
	return seq, true
}

/* --------------------------------------------------------- del catálogo */

// libre comprueba que el nombre temporal no esté ocupado.
//
// Es improbable y sería desastroso: el guion crearía la tabla sobre algo que ya
// existe —fallando— o, peor, la borraría al final.
func libre(ctx context.Context, db *sql.DB, nombre string) error {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_schema WHERE name = ?`, nombre).Scan(&n); err != nil {
		return fmt.Errorf("comprobar el nombre temporal: %w", err)
	}
	if n > 0 {
		return fmt.Errorf(
			"ya existe un objeto llamado %q y la reconstrucción necesita ese nombre; "+
				"borralo o renombralo antes", nombre)
	}
	return nil
}

// columnasCopiables son las columnas cuyos valores hay que pasar a la tabla
// nueva.
//
// Las generadas quedan afuera: SQLite rechaza un INSERT que las nombre, porque
// no se guardan sino que se calculan. Incluirlas haría fallar la copia entera.
func columnasCopiables(ctx context.Context, db *sql.DB, tabla string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name FROM pragma_table_xinfo(?) WHERE hidden IN (0, 1) ORDER BY cid`, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer las columnas de %q: %w", tabla, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// indicesYTriggers devuelve las sentencias que hay que volver a ejecutar
// después de reconstruir.
//
// El DROP TABLE se lleva los índices y los triggers de la tabla. Los índices
// implícitos —los de una UNIQUE o una PRIMARY KEY declaradas— tienen `sql` en
// NULL porque no los creó nadie con un CREATE INDEX: los recrea el propio
// CREATE TABLE, y por eso se filtran. Volver a crearlos a mano daría un error
// de nombre repetido.
func indicesYTriggers(ctx context.Context, db *sql.DB, tabla string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT sql FROM sqlite_schema
		 WHERE tbl_name = ? AND type IN ('index', 'trigger') AND sql IS NOT NULL
		 ORDER BY type DESC, name`, tabla)
	if err != nil {
		return nil, fmt.Errorf("leer los índices y triggers de %q: %w", tabla, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";")))
	}
	return out, rows.Err()
}
