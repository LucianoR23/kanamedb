package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/dml"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// maxIdent es el límite que se impone. SQLite no tiene ninguno: acepta un
// nombre de cualquier largo. Se pone uno generoso igual, porque un nombre de
// mil caracteres no es un caso de uso sino un accidente, y porque el mismo
// esquema puede terminar en otro motor que sí tiene límite.
const maxIdent = 255

// tipoValido acota lo que puede entrar como tipo.
//
// El tipo va CRUDO en la sentencia —no hay forma de parametrizarlo— así que es
// la única parte del DDL donde algo escrito por el usuario se concatena.
//
// Y en SQLite es más importante que en los otros motores, no menos: SQLite
// ACEPTA CUALQUIER NOMBRE DE TIPO. `CREATE TABLE t (a noexistetipo)` no da
// error, porque el tipo declarado define una afinidad y no una restricción.
// Comprobado. Así que no hay ninguna validación del motor detrás de esta: lo
// que pase de acá, se escribe.
var tipoValido = regexp.MustCompile(`^[A-Za-z0-9_ ,()'\[\]]*$`)

// renderDDL arma la sentencia de un cambio de esquema para SQLite.
//
// A diferencia de los otros tres motores, no es una función pura: la mayoría de
// los cambios de columna se hacen reconstruyendo la tabla, y para escribir la
// definición nueva hay que leer la que hay. Ver rebuild.go.
func renderDDL(ctx context.Context, db *sql.DB, c change.Change) (change.Statement, error) {
	if err := c.Validate(); err != nil {
		return change.Statement{}, err
	}
	for nombre, que := range identificadoresDe(c) {
		if err := validarIdent(que, nombre); err != nil {
			return change.Statement{}, err
		}
	}
	if c.Kind() == change.KindData {
		return dml.Render(c, dialectoDML)
	}
	tabla := QuoteIdent(c.Table)
	st := change.Statement{ChangeID: c.ID, Destructive: c.Destructive()}

	switch c.Type {

	case change.CreateTable:
		partes := make([]string, 0, len(c.Columns)+1)
		for _, col := range c.Columns {
			def, err := columnaNueva(col)
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
		// SQLite no tiene DROP TABLE ... CASCADE. Lo que sí hace, y conviene
		// decirlo, es disparar los ON DELETE de las tablas que la referencian:
		// el DROP hace un DELETE implícito.
		st.SQL = "DROP TABLE " + tabla
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Se borran la tabla y todas sus filas. No se deshace. Si otras tablas la " +
			"referencian con ON DELETE CASCADE, sus filas también se van."

	case change.RenameTable:
		st.SQL = fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tabla, QuoteIdent(c.NewName))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "SQLite además reescribe el nombre adentro de las vistas, los triggers y " +
			"las claves foráneas que la nombran, así que esas no se rompen. Lo que sí deja " +
			"de encontrarla es todo lo que esté fuera de la base: consultas guardadas, código."

	case change.AddColumn:
		def, err := columnaNueva(*c.Column)
		if err != nil {
			return st, err
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tabla, def)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Agregar una columna en SQLite solo toca el esquema: las filas que ya " +
			"están no se reescriben."

	case change.DropColumn:
		st.SQL = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tabla, QuoteIdent(c.Column.Name))
		// A diferencia de agregar, borrar SÍ reescribe todas las filas.
		st.Impact = change.ImpactRewrite
		st.Lock = change.LockAll
		st.Note = "Se pierden los datos de la columna. No se deshace. SQLite reescribe la " +
			"tabla entera para sacarla, así que tarda en proporción a su tamaño."

	case change.RenameColumn:
		st.SQL = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
			tabla, QuoteIdent(c.Column.Name), QuoteIdent(c.NewName))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "SQLite reescribe el nombre adentro de los índices, triggers y vistas que " +
			"la usan. Lo que esté fuera de la base deja de encontrarla."

	case change.AddIndex:
		var b strings.Builder
		b.WriteString("CREATE ")
		if c.Unique {
			b.WriteString("UNIQUE ")
		}
		b.WriteString("INDEX ")
		nombre := c.Name
		if nombre == "" {
			nombre = nombreDeIndice(c.Table, c.Names)
		}
		fmt.Fprintf(&b, "%s ON %s (%s)", QuoteIdent(nombre), tabla, listaDeIdent(c.Names))
		if c.Where != "" {
			// SQLite sí tiene índices parciales, a diferencia de MySQL.
			fmt.Fprintf(&b, " WHERE %s", c.Where)
		}
		st.SQL = b.String()
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites

	case change.DropIndex:
		// El DROP INDEX de SQLite no lleva la tabla: los índices viven en el
		// esquema, como en Postgres y a diferencia de MySQL.
		st.SQL = "DROP INDEX " + QuoteIdent(c.Name)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Las consultas que lo usaban pasan a recorrer la tabla."

	case change.AddUnique:
		// Acá SQLite se parece a Postgres y no a MySQL: la forma de agregar una
		// restricción de unicidad a una tabla que ya existe es un índice único,
		// porque ALTER TABLE ADD CONSTRAINT no existe. No es un rodeo: es el
		// mismo mecanismo que usa el motor para una UNIQUE declarada.
		nombre := c.Name
		if nombre == "" {
			nombre = nombreDeIndice(c.Table, c.Names)
		}
		st.SQL = fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)",
			QuoteIdent(nombre), tabla, listaDeIdent(c.Names))
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites
		st.Note = "SQLite no tiene ALTER TABLE ADD CONSTRAINT: la unicidad se impone con un " +
			"índice único, que es el mismo mecanismo que usa una UNIQUE declarada en la tabla."

	case change.SetTableComment, change.SetColumnComment:
		return st, &engine.ErrUnsupported{
			Engine: engine.SQLite, Operation: "comentarios",
			Motivo: "no existen: SQLite no guarda una descripción de las tablas ni de las " +
				"columnas en ningún lado",
		}

	// ------------------------------------------------ y todo lo que reconstruye

	case change.SetColumnType, change.SetNotNull, change.DropNotNull,
		change.SetDefault, change.DropDefault,
		change.AddPrimaryKey, change.AddForeignKey, change.AddCheck,
		change.DropConstraint:
		return rebuildDDL(ctx, db, c)

	default:
		return st, &engine.ErrUnsupported{Engine: engine.SQLite, Operation: string(c.Type)}
	}

	return st, nil
}

// columnaNueva escribe la definición de una columna para un CREATE TABLE o un
// ADD COLUMN.
func columnaNueva(col change.Column) (string, error) {
	if !tipoValido.MatchString(col.DataType) {
		return "", fmt.Errorf("el tipo %q tiene caracteres que no se aceptan", col.DataType)
	}
	if col.Comment != "" {
		return "", &engine.ErrUnsupported{
			Engine: engine.SQLite, Operation: "comentarios de columna",
			Motivo: "no existen",
		}
	}
	var b strings.Builder
	b.WriteString(QuoteIdent(col.Name))
	if col.DataType != "" {
		b.WriteString(" " + col.DataType)
	}
	if !col.Nullable {
		b.WriteString(" NOT NULL")
	}
	if col.Default != "" {
		b.WriteString(" DEFAULT " + col.Default)
	}
	return b.String(), nil
}

// nombreDeIndice arma un nombre cuando no se dio uno.
//
// En SQLite el nombre es OBLIGATORIO en CREATE INDEX, igual que en MySQL: el
// motor no elige uno como hace Postgres.
func nombreDeIndice(tabla string, cols []string) string {
	partes := append([]string{tabla}, cols...)
	nombre := strings.Join(partes, "_") + "_idx"
	if len(nombre) > maxIdent {
		nombre = nombre[:maxIdent]
	}
	return nombre
}

func listaDeIdent(cols []string) string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, QuoteIdent(c))
	}
	return strings.Join(out, ", ")
}

// accionReferencial normaliza la acción de una clave foránea.
func accionReferencial(a string) string {
	s := strings.ToUpper(strings.TrimSpace(a))
	switch s {
	case "", "NO ACTION":
		// Es el default de SQLite; escribirlo no aporta.
		return ""
	case "RESTRICT", "CASCADE", "SET NULL", "SET DEFAULT":
		return s
	}
	return ""
}

// identificadoresDe junta todo lo que va citado, para validarlo antes de
// renderizar.
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
			"%s tiene %d caracteres y el máximo que Kaname acepta es %d", que, len(v), maxIdent)
	}
	if strings.ContainsAny(v, "\x00\n\r") {
		return fmt.Errorf("%s tiene caracteres de control", que)
	}
	return nil
}

// dialectoDML es lo que dml necesita saber de SQLite.
var dialectoDML = dml.Dialect{
	Table:        QualifiedName,
	QuoteIdent:   QuoteIdent,
	QuoteLiteral: QuoteString,
	Placeholder:  func(int) string { return "?" },
	EmptyInsert:  "DEFAULT VALUES",
	InsertPrefix: func(ignorar bool) string {
		if ignorar {
			return "INSERT OR IGNORE INTO "
		}
		return "INSERT INTO "
	},
}
