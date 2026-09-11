package mysql

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/dml"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// maxIdent es el límite de MySQL y MariaDB. A diferencia de Postgres, que
// trunca en silencio a 63, acá el motor rechaza el nombre — pero se valida
// igual antes de mandarlo, para que el error diga cuál es el límite.
const maxIdent = 64

// tipoValido acota lo que puede entrar como tipo.
//
// El tipo va CRUDO en la sentencia —no hay forma de parametrizarlo— así que es
// la única parte del DDL donde algo escrito por el usuario se concatena. Se
// permite lo que un tipo necesita y nada más: letras, dígitos, espacios,
// paréntesis, coma, comilla simple para los valores de un ENUM, y poco más.
// Queda afuera el punto y coma y los marcadores de comentario.
var tipoValido = regexp.MustCompile(`^[A-Za-z0-9_ ,()'\[\]]+$`)

// RenderDDL arma la sentencia de un cambio de esquema para MySQL o MariaDB.
//
// Devuelve error para cualquier operación que no sepa escribir. Eso NO es un
// caso a manejar en la interfaz: es la red que garantiza que una operación sin
// renderizado nunca se ofrezca.
func RenderDDL(c change.Change, k engine.Kind) (change.Statement, error) {
	return renderDDL(c, k, false)
}

// renderDDL es lo mismo, sabiendo cómo cita este servidor los literales de
// texto. Ver quoteString.
func renderDDL(c change.Change, k engine.Kind, sinEscapes bool) (change.Statement, error) {
	cita := func(s string) string { return quoteString(s, sinEscapes) }
	if err := c.Validate(); err != nil {
		return change.Statement{}, err
	}
	for nombre, que := range identificadoresDe(c) {
		if err := validarIdent(que, nombre); err != nil {
			return change.Statement{}, err
		}
	}
	if c.Kind() == change.KindData {
		return dml.Render(c, dialectoDML(cita))
	}
	tabla := QualifiedName(c.Schema, c.Table)
	st := change.Statement{ChangeID: c.ID, Destructive: c.Destructive()}

	switch c.Type {

	case change.CreateTable:
		partes := make([]string, 0, len(c.Columns)+1)
		for _, col := range c.Columns {
			def, err := columnaDDL(col, cita)
			if err != nil {
				return st, err
			}
			partes = append(partes, "    "+def)
		}
		if len(c.Names) > 0 {
			partes = append(partes, "    PRIMARY KEY ("+listaDeIdent(c.Names)+")")
		}
		st.SQL = fmt.Sprintf("CREATE TABLE %s (\n%s\n)", tabla, strings.Join(partes, ",\n"))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone

	case change.DropTable:
		// MySQL no tiene DROP TABLE ... CASCADE: la palabra se acepta y se
		// ignora, que es peor que no tenerla. Se omite a propósito para que el
		// preview no prometa un borrado en cascada que no va a pasar.
		st.SQL = "DROP TABLE " + tabla
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Se borran la tabla y todas sus filas. No se deshace."

	case change.RenameTable:
		st.SQL = fmt.Sprintf("RENAME TABLE %s TO %s", tabla, QualifiedName(c.Schema, c.NewName))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Todo lo que nombre la tabla —consultas guardadas, vistas, código— deja de encontrarla."

	case change.SetTableComment:
		st.SQL = fmt.Sprintf("ALTER TABLE %s COMMENT = %s", tabla, cita(c.Comment))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone

	case change.AddColumn:
		def, err := columnaDDL(*c.Column, cita)
		if err != nil {
			return st, err
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tabla, def)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Agregar una columna es instantáneo desde MySQL 8.0: se guarda en el " +
			"catálogo sin reescribir la tabla."

	case change.DropColumn:
		st.SQL = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tabla, QuoteIdent(c.Column.Name))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Se pierden los datos de la columna. No se deshace."

	case change.RenameColumn:
		// RENAME COLUMN existe desde MySQL 8.0 y MariaDB 10.5. La forma vieja
		// —CHANGE COLUMN— obliga a repetir el tipo entero, y repetirlo mal
		// convierte un renombrado en un cambio de tipo silencioso.
		st.SQL = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
			tabla, QuoteIdent(c.Column.Name), QuoteIdent(c.NewName))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Todo lo que nombre la columna deja de encontrarla."

	case change.SetColumnType:
		if !tipoValido.MatchString(c.DataType) {
			return st, fmt.Errorf("el tipo %q tiene caracteres que no se aceptan", c.DataType)
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s",
			tabla, QuoteIdent(c.Column.Name), c.DataType)
		st.Impact = change.ImpactRewrite
		st.Lock = change.LockWrites
		st.Note = "MODIFY COLUMN repite la definición entera de la columna: si tenía NOT NULL, " +
			"un valor por defecto o un comentario y no se vuelven a escribir, se pierden."

	case change.SetNotNull, change.DropNotNull:
		// Acá está la diferencia grande con Postgres. MySQL no tiene
		// SET/DROP NOT NULL: la nulabilidad es parte de la definición de la
		// columna, así que hay que reescribirla entera con MODIFY. Y para eso
		// hace falta el tipo, que por eso viaja en el cambio.
		if c.Column.DataType == "" {
			return st, fmt.Errorf(
				"para cambiar la nulabilidad en %s hace falta el tipo de la columna: "+
					"MODIFY reescribe la definición entera", c.Type)
		}
		if !tipoValido.MatchString(c.Column.DataType) {
			return st, fmt.Errorf("el tipo %q tiene caracteres que no se aceptan", c.Column.DataType)
		}
		nulo := "NULL"
		if c.Type == change.SetNotNull {
			nulo = "NOT NULL"
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s %s",
			tabla, QuoteIdent(c.Column.Name), c.Column.DataType, nulo)
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites
		if c.Type == change.SetNotNull {
			st.Note = "Se comprueba fila por fila que ninguna tenga NULL. " +
				"MODIFY reescribe la definición: lo que no se vuelva a escribir se pierde."
		}

	case change.SetDefault:
		// El valor sale de Column.Default y no de Expression: es lo que exige
		// Validate y lo que usan los otros motores. Con Expression, un cambio
		// perfectamente válido renderizaba «SET DEFAULT » y fallaba recién al
		// aplicar, con un error de sintaxis.
		st.SQL = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s",
			tabla, QuoteIdent(c.Column.Name), c.Column.Default)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone

	case change.DropDefault:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
			tabla, QuoteIdent(c.Column.Name))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone

	case change.SetColumnComment:
		// MySQL no tiene COMMENT ON: el comentario es parte de la definición,
		// así que hace falta el tipo por el mismo motivo que la nulabilidad.
		if c.Column.DataType == "" {
			return st, fmt.Errorf(
				"para comentar una columna en %s hace falta su tipo: el comentario es "+
					"parte de la definición", c.Type)
		}
		if !tipoValido.MatchString(c.Column.DataType) {
			return st, fmt.Errorf("el tipo %q tiene caracteres que no se aceptan", c.Column.DataType)
		}
		nulo := "NULL"
		if !c.Column.Nullable {
			nulo = "NOT NULL"
		}
		// El valor por defecto se vuelve a escribir. MODIFY reemplaza la
		// definición ENTERA de la columna, así que lo que no se repita se
		// pierde: agregarle un comentario a `estado varchar(20) NOT NULL
		// DEFAULT 'nuevo'` le borraba el default, en silencio y sin nada en la
		// vista previa que lo dijera.
		def := ""
		if c.Column.Default != "" {
			def = " DEFAULT " + c.Column.Default
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s %s%s COMMENT %s",
			tabla, QuoteIdent(c.Column.Name), c.Column.DataType, nulo, def,
			cita(c.Comment))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "MySQL no tiene COMMENT ON: el comentario es parte de la definición de la " +
			"columna, así que hay que reescribirla entera. Lo que no aparezca en esta " +
			"sentencia se pierde."

	case change.AddPrimaryKey:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", tabla, listaDeIdent(c.Names))
		st.Impact = change.ImpactRewrite
		st.Lock = change.LockWrites

	case change.AddForeignKey:
		refTabla := QualifiedName(primero(c.RefSchema, c.Schema), c.RefTable)
		var b strings.Builder
		fmt.Fprintf(&b, "ALTER TABLE %s ADD ", tabla)
		if c.Name != "" {
			fmt.Fprintf(&b, "CONSTRAINT %s ", QuoteIdent(c.Name))
		}
		fmt.Fprintf(&b, "FOREIGN KEY (%s) REFERENCES %s (%s)",
			listaDeIdent(c.Names), refTabla, listaDeIdent(c.RefNames))
		if a := accion(c.OnDelete); a != "" {
			fmt.Fprintf(&b, " ON DELETE %s", a)
		}
		if a := accion(c.OnUpdate); a != "" {
			fmt.Fprintf(&b, " ON UPDATE %s", a)
		}
		st.SQL = b.String()
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites
		st.Note = "Se verifica que cada fila tenga su fila en la otra tabla. MySQL además " +
			"exige que las columnas de los dos lados tengan el mismo tipo y la misma colación."

	case change.AddCheck:
		var b strings.Builder
		fmt.Fprintf(&b, "ALTER TABLE %s ADD ", tabla)
		if c.Name != "" {
			fmt.Fprintf(&b, "CONSTRAINT %s ", QuoteIdent(c.Name))
		}
		fmt.Fprintf(&b, "CHECK (%s)", c.Expression)
		st.SQL = b.String()
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites
		st.Note = "Se comprueba contra todas las filas que ya están."

	case change.AddUnique:
		var b strings.Builder
		fmt.Fprintf(&b, "ALTER TABLE %s ADD ", tabla)
		if c.Name != "" {
			fmt.Fprintf(&b, "CONSTRAINT %s ", QuoteIdent(c.Name))
		}
		fmt.Fprintf(&b, "UNIQUE (%s)", listaDeIdent(c.Names))
		st.SQL = b.String()
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites

	case change.DropConstraint:
		// Y acá otra divergencia entre los dos: MySQL 8.0.19+ y MariaDB 10.2+
		// aceptan DROP CONSTRAINT genérico, que es lo que se usa. Una clave
		// foránea también responde a DROP FOREIGN KEY, pero DROP CONSTRAINT
		// sirve para las tres clases y no obliga a saber cuál es.
		st.SQL = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tabla, QuoteIdent(c.Name))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Se pierde la garantía que daba la restricción. Los datos quedan."

	case change.AddIndex:
		if c.Where != "" {
			return st, &engine.ErrUnsupported{
				Engine: k, Operation: "índices parciales",
				Motivo: "no existe el WHERE en CREATE INDEX; se puede lograr algo parecido " +
					"con una columna generada indexada",
			}
		}
		var b strings.Builder
		b.WriteString("CREATE ")
		if c.Unique {
			b.WriteString("UNIQUE ")
		}
		b.WriteString("INDEX ")
		if c.Name != "" {
			b.WriteString(QuoteIdent(c.Name) + " ")
		} else {
			// MySQL no elige el nombre en CREATE INDEX: es obligatorio. Se arma
			// con la convención habitual para no obligar a inventarlo.
			b.WriteString(QuoteIdent(nombreDeIndice(c.Table, c.Names)) + " ")
		}
		fmt.Fprintf(&b, "ON %s (%s)", tabla, listaDeIdent(c.Names))
		st.SQL = b.String()
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites

	case change.DropIndex:
		// El DROP INDEX de MySQL necesita la tabla; el de Postgres no. Es la
		// consecuencia de que acá los índices vivan dentro de la tabla y no en
		// el esquema.
		st.SQL = fmt.Sprintf("DROP INDEX %s ON %s", QuoteIdent(c.Name), tabla)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Las consultas que lo usaban pasan a recorrer la tabla."

	case change.ReplaceObject:
		return objetoDDL(c, k)

	default:
		return st, &engine.ErrUnsupported{Engine: k, Operation: string(c.Type)}
	}

	return st, nil
}

// columnaDDL escribe la definición de una columna.
func columnaDDL(col change.Column, cita func(string) string) (string, error) {
	if !tipoValido.MatchString(col.DataType) {
		return "", fmt.Errorf("el tipo %q tiene caracteres que no se aceptan", col.DataType)
	}
	var b strings.Builder
	b.WriteString(QuoteIdent(col.Name) + " " + col.DataType)
	if !col.Nullable {
		b.WriteString(" NOT NULL")
	}
	if col.Default != "" {
		b.WriteString(" DEFAULT " + col.Default)
	}
	if col.Comment != "" {
		b.WriteString(" COMMENT " + cita(col.Comment))
	}
	return b.String(), nil
}

// nombreDeIndice arma un nombre cuando no se dio uno, con la convención que usa
// todo el mundo: tabla_col1_col2_idx.
func nombreDeIndice(tabla string, cols []string) string {
	partes := append([]string{tabla}, cols...)
	nombre := strings.Join(partes, "_") + "_idx"
	if len(nombre) > maxIdent {
		nombre = nombre[:maxIdent]
	}
	return nombre
}

func accion(a string) string {
	s := strings.ToUpper(strings.TrimSpace(a))
	switch s {
	case "", "NO ACTION":
		// MySQL trata NO ACTION igual que RESTRICT y es el default: escribirlo
		// no aporta.
		return ""
	case "RESTRICT", "CASCADE", "SET NULL", "SET DEFAULT":
		return s
	}
	return ""
}

func listaDeIdent(cols []string) string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, QuoteIdent(c))
	}
	return strings.Join(out, ", ")
}

func primero(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// identificadoresDe junta todo lo que va citado en la sentencia, para validarlo
// antes de renderizar.
func identificadoresDe(c change.Change) map[string]string {
	out := map[string]string{}
	// La clave es el IDENTIFICADOR y el valor es cómo se llama, no al revés.
	// Estaba dado vuelta, y con el bucle de quien llama —que pasa la clave
	// como nombre y el valor como etiqueta— el resultado era que se validaba
	// el largo de la DESCRIPCIÓN: un nombre de tabla de 300 caracteres, o con
	// un salto de línea adentro, pasaba entero.
	poner := func(que, nombre string) {
		if nombre != "" {
			out[nombre] = que
		}
	}
	poner("el nombre de la tabla", c.Table)
	poner("el nombre nuevo", c.NewName)
	poner("el nombre de la restricción", c.Name)
	poner("el nombre de la tabla referenciada", c.RefTable)
	if c.Column != nil {
		poner("el nombre de la columna", c.Column.Name)
	}
	for _, col := range c.Columns {
		poner("el nombre de la columna "+col.Name, col.Name)
	}
	for _, n := range c.Names {
		poner("el nombre de la columna "+n, n)
	}
	for _, n := range c.RefNames {
		poner("el nombre de la columna referenciada "+n, n)
	}
	for _, lista := range [][]change.Cell{c.Values, c.Key} {
		for _, celda := range lista {
			poner("el nombre de la columna "+celda.Column, celda.Column)
		}
	}
	return out
}

func validarIdent(que, v string) error {
	if len(v) > maxIdent {
		return fmt.Errorf(
			"%s tiene %d caracteres y el máximo de MySQL es %d", que, len(v), maxIdent)
	}
	if strings.ContainsAny(v, "\x00\n\r") {
		return fmt.Errorf("%s tiene caracteres de control", que)
	}
	return nil
}

// dialectoDML es lo que dml necesita saber de MySQL y MariaDB. El citado de
// literales depende del SERVIDOR —NO_BACKSLASH_ESCAPES— y por eso se recibe.
//
// `() VALUES ()` es la única forma que MySQL acepta para insertar una fila con
// todos sus defaults: `DEFAULT VALUES` no existe acá.
func dialectoDML(cita func(string) string) dml.Dialect {
	return dml.Dialect{
		Table:        QualifiedName,
		QuoteIdent:   QuoteIdent,
		QuoteLiteral: cita,
		Placeholder:  func(int) string { return "?" },
		EmptyInsert:  "() VALUES ()",
		InsertPrefix: func(ignorar bool) string {
			if ignorar {
				// MySQL lo pone adelante y no al final. IGNORE degrada a aviso
				// MÁS cosas que un choque de clave —un valor fuera de rango, por
				// ejemplo— así que solo se usa cuando se pidió saltear.
				return "INSERT IGNORE INTO "
			}
			return "INSERT INTO "
		},
	}
}

// objetoDDL escribe el reemplazo de la definición de un objeto.
//
// La definición se ejecuta TAL CUAL, sin reescribirla: es la promesa del editor.
//
// La nota del camino con DROP dice algo que NO vale para Postgres ni para
// SQLite, y es la diferencia que más caro sale de esta familia: **acá cada
// sentencia de DDL hace commit sola**. No hay transacción que envuelva al DROP
// y al CREATE, así que si el CREATE falla —un error de sintaxis, un permiso— el
// DROP ya está aplicado y el objeto se PERDIÓ. Kaname tiene su definición
// anterior en la pantalla de donde salió, que es el único respaldo que existe;
// decirlo antes es lo mínimo.
func objetoDDL(c change.Change, k engine.Kind) (change.Statement, error) {
	st := change.Statement{ChangeID: c.ID, Destructive: c.Destructive()}

	nombre := QualifiedName(c.Schema, c.Name)
	drop, definicion, err := change.ObjetoAReemplazar(c, string(k), nombre, "")
	if err != nil {
		return st, err
	}

	st.Impact = change.ImpactMetadata
	if drop == "" {
		st.SQL = definicion
		st.Lock = change.LockNone
		st.Note = "Se reemplaza en el lugar: si la definición nueva falla, el objeto queda como está."
		return st, nil
	}

	// Los dos pasos se mandan POR SEPARADO. El DSN de esta familia lleva
	// `multiStatements=false` a propósito —ver internal/query/split.go— así que
	// `DROP …; CREATE …` en una sola cadena no son dos sentencias: es un error
	// de sintaxis. Sin esto, reemplazar una rutina o un trigger no se podía
	// ejecutar NUNCA en MySQL ni en MariaDB, que son justamente los objetos que
	// este motor no sabe reemplazar en el lugar.
	st.Steps = []string{drop, definicion}
	st.SQL = drop + ";\n" + definicion
	st.Lock = change.LockAll
	st.Note = fmt.Sprintf(
		"El objeto se borra y se vuelve a crear. En %s el DDL confirma solo, así que NO hay "+
			"transacción que lo revierta: si el CREATE falla, el objeto queda borrado y su "+
			"definición anterior solo existe en esta pantalla. Copiala antes de aplicar.",
		k.Label())
	return st, nil
}
