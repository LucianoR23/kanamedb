package postgres

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/dml"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// tipoValido acota qué puede ser un nombre de tipo.
//
// El tipo llega como texto libre desde la interfaz —`numeric(10,2)`, `text[]`,
// `timestamp with time zone`, `demo.estado` son todos válidos y no hay forma de
// ofrecerlos con un desplegable cerrado—, así que va a la sentencia sin citar.
// Esta expresión no valida que el tipo exista: valida que no pueda ser otra
// cosa. Un `;` o el comienzo de un comentario no entran en ningún nombre de
// tipo real.
//
// No reemplaza al preview, que es la garantía de fondo: nada se ejecuta sin que
// alguien lea la SQL. Es defensa en profundidad para que el preview no tenga que
// ser el único filtro.
var tipoValido = regexp.MustCompile(`^[A-Za-z0-9_ ."\[\](),áéíóúñÁÉÍÓÚÑ]+$`)

// firmaValida acota qué puede ser la firma de una función.
//
// La firma va CONCATENADA al `DROP FUNCTION` y viene del frontend, así que
// necesita la misma disciplina que un tipo: no se valida que exista, se valida
// que no pueda ser otra cosa. Sin esto, un `Args` armado a mano con `) ; DROP
// … ; --` adentro se convierte en sentencias extra dentro del mismo envío,
// porque pgx manda un Exec sin parámetros por el protocolo simple.
//
// Es la misma forma que `tipoValido` —una firma es una lista de tipos con sus
// nombres y modos— y por eso comparte los caracteres, más nada.
var firmaValida = regexp.MustCompile(`^[A-Za-z0-9_ ."\[\](),áéíóúñÁÉÍÓÚÑ]*$`)

// validarFirma comprueba la firma antes de escribirla en un DROP.
func validarFirma(args string) error {
	if !firmaValida.MatchString(args) {
		return fmt.Errorf(
			"la firma %q tiene caracteres que no pueden estar en una lista de argumentos", args)
	}
	return nil
}

// accionesReferenciales traduce el vocabulario del modelo al de SQL.
//
// El modelo usa el mismo que devuelve la introspección —minúsculas, con espacio—
// para que lo que se lee y lo que se escribe sean la misma palabra.
var accionesReferenciales = map[string]string{
	"no action":   "NO ACTION",
	"restrict":    "RESTRICT",
	"cascade":     "CASCADE",
	"set null":    "SET NULL",
	"set default": "SET DEFAULT",
}

// maxIdent es lo que mide un identificador en PostgreSQL, en BYTES.
//
// Pasarse no da error: el servidor TRUNCA y sigue. Se comprobó — una columna de
// 70 caracteres queda con 63 y nadie avisa. Eso es un renombrado silencioso, y
// dos nombres largos distintos pueden terminar siendo el mismo.
//
// Es el valor de `max_identifier_length`, que se fija al compilar el servidor.
// 63 es el de cualquier build normal. Si alguna vez importa, el servidor lo
// informa y se puede leer por conexión; mientras tanto rechazar de más es el
// lado seguro: como mucho se rechaza un nombre absurdo que igual iba a cambiar.
const maxIdent = 63

// validarIdent rechaza un nombre que el servidor cambiaría por su cuenta.
func validarIdent(que, nombre string) error {
	if nombre == "" {
		return fmt.Errorf("el %s está vacío", que)
	}
	if len(nombre) > maxIdent {
		return fmt.Errorf(
			"el %s tiene %d bytes y PostgreSQL admite %d: lo truncaría sin avisar y el objeto "+
				"quedaría con otro nombre", que, len(nombre), maxIdent)
	}
	return nil
}

// identificadoresDe junta todos los nombres que la sentencia va a citar, para
// validarlos de una vez antes de armar nada.
func identificadoresDe(c change.Change) map[string]string {
	out := map[string]string{}
	poner := func(que, nombre string) {
		if nombre != "" {
			out[nombre] = que
		}
	}
	poner("nombre de la tabla", c.Table)
	poner("nombre del esquema", c.Schema)
	poner("nombre nuevo", c.NewName)
	poner("nombre de la restricción o del índice", c.Name)
	// Una etiqueta de enum también tiene el límite de 63 bytes, y sin esto se la
	// rechazaba el servidor en vez del renderizador —después de haberla dejado
	// preparar y revisar—.
	poner("valor del enum", c.Value)
	poner("valor del enum", c.Before)
	poner("nombre de la tabla referenciada", c.RefTable)
	poner("nombre del esquema referenciado", c.RefSchema)
	if c.Column != nil {
		poner("nombre de la columna", c.Column.Name)
	}
	for _, col := range c.Columns {
		poner("nombre de la columna", col.Name)
	}
	for _, lista := range [][]string{c.Names, c.RefNames, c.Included} {
		for _, n := range lista {
			poner("nombre de la columna", n)
		}
	}
	for _, lista := range [][]change.Cell{c.Values, c.Key} {
		for _, celda := range lista {
			poner("nombre de la columna", celda.Column)
		}
	}
	return out
}

// RenderDDL arma la sentencia de un cambio de esquema para PostgreSQL.
//
// Devuelve error para cualquier operación que no sepa escribir. Eso NO es un
// caso a manejar en la interfaz: es la red que garantiza que una operación sin
// renderizado nunca se ofrezca. Si aparece en producción, es un error de
// programación, no de uso.
func RenderDDL(c change.Change) (change.Statement, error) {
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
	tabla := QualifiedName(c.Schema, c.Table)
	st := change.Statement{ChangeID: c.ID, Destructive: c.Destructive()}

	switch c.Type {

	case change.CreateTable:
		partes := make([]string, 0, len(c.Columns)+1)
		for _, col := range c.Columns {
			def, err := columnaDDL(col)
			if err != nil {
				return st, err
			}
			partes = append(partes, "    "+def)
		}
		// La clave primaria va adentro del CREATE TABLE y no como un ALTER
		// aparte: es intrínseca a la tabla y no referencia nada de afuera, así
		// que no puede crear un problema de orden. Las claves foráneas sí, y por
		// eso van separadas.
		if len(c.Names) > 0 {
			partes = append(partes, "    PRIMARY KEY ("+listaDeIdent(c.Names)+")")
		}
		st.SQL = fmt.Sprintf("CREATE TABLE %s (\n%s\n)", tabla, strings.Join(partes, ",\n"))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone

	case change.DropTable:
		st.SQL = "DROP TABLE " + tabla + cascada(c)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Se borran la tabla y todas sus filas. No se deshace."
		if c.Cascade {
			st.Note = "Se borran la tabla, todas sus filas y todo lo que dependa de ella " +
				"—vistas, claves foráneas de otras tablas—, esté o no en pantalla. No se deshace."
		}

	case change.RenameTable:
		st.SQL = fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tabla, QuoteIdent(c.NewName))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Todo lo que nombre esta tabla —consultas guardadas, vistas, código— " +
			"deja de encontrarla."

	case change.SetTableComment:
		st.SQL = fmt.Sprintf("COMMENT ON TABLE %s IS %s", tabla, literal(c.Comment))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone

	case change.AddColumn:
		def, err := columnaDDL(*c.Column)
		if err != nil {
			return st, err
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tabla, def)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		// Desde PostgreSQL 11 agregar una columna con default NO reescribe la
		// tabla: el valor se guarda en el catálogo y se materializa al escribir
		// cada fila. Solo un default volátil obliga a reescribir, y eso no se
		// puede saber sin resolver la expresión.
		if c.Column.Default != "" {
			st.Note = "El valor por defecto se guarda en el catálogo y no reescribe la tabla, " +
				"salvo que la expresión dé un valor distinto en cada fila."
		}

	case change.DropColumn:
		st.SQL = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s%s",
			tabla, QuoteIdent(c.Column.Name), cascada(c))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Los valores de esa columna se pierden. El espacio no se libera hasta el " +
			"próximo VACUUM FULL, pero los datos no se recuperan."

	case change.RenameColumn:
		st.SQL = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
			tabla, QuoteIdent(c.Column.Name), QuoteIdent(c.NewName))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll

	case change.SetColumnType:
		if !tipoValido.MatchString(c.DataType) {
			return st, fmt.Errorf("tipo inválido: %q", c.DataType)
		}
		usando := ""
		if c.Expression != "" {
			usando = " USING " + c.Expression
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s%s",
			tabla, QuoteIdent(c.Column.Name), c.DataType, usando)
		st.Impact = change.ImpactRewrite
		st.Lock = change.LockAll
		st.Note = "Cambiar el tipo reescribe la tabla entera y la bloquea mientras tanto. " +
			"Si el tipo nuevo no entra —menos decimales, menos largo—, los valores se " +
			"truncan o la sentencia falla."

	case change.SetNotNull:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
			tabla, QuoteIdent(c.Column.Name))
		st.Impact = change.ImpactScan
		st.Lock = change.LockAll
		st.Note = "Se leen todas las filas para verificar que ninguna sea nula, con la tabla " +
			"bloqueada. Si alguna lo es, la sentencia falla y no se aplica nada."

	case change.DropNotNull:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL",
			tabla, QuoteIdent(c.Column.Name))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll

	case change.SetDefault:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s",
			tabla, QuoteIdent(c.Column.Name), c.Column.Default)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "El valor por defecto solo afecta a las filas que se inserten de ahora en " +
			"más. Las que ya están no cambian."

	case change.DropDefault:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
			tabla, QuoteIdent(c.Column.Name))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll

	case change.SetColumnComment:
		st.SQL = fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s",
			tabla, QuoteIdent(c.Column.Name), literal(c.Comment))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone

	case change.AddPrimaryKey:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD %sPRIMARY KEY (%s)",
			tabla, nombreDeRestriccion(c), listaDeIdent(c.Names))
		st.Impact = change.ImpactScan
		st.Lock = change.LockAll
		st.Note = "Se construye un índice único sobre esas columnas leyendo toda la tabla. " +
			"Falla si hay valores repetidos o nulos."

	case change.AddUnique:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD %sUNIQUE (%s)",
			tabla, nombreDeRestriccion(c), listaDeIdent(c.Names))
		st.Impact = change.ImpactScan
		st.Lock = change.LockAll
		st.Note = "Se construye un índice único leyendo toda la tabla. Falla si hay repetidos."

	case change.AddForeignKey:
		acciones, err := accionesDDL(c)
		if err != nil {
			return st, err
		}
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD %sFOREIGN KEY (%s) REFERENCES %s (%s)%s",
			tabla, nombreDeRestriccion(c), listaDeIdent(c.Names),
			QualifiedName(refEsquema(c), c.RefTable), listaDeIdent(c.RefNames), acciones)
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites
		st.Note = "Se verifica que cada fila existente tenga su fila en la otra tabla. " +
			"Mientras tanto no se puede escribir en ninguna de las dos."

	case change.AddCheck:
		st.SQL = fmt.Sprintf("ALTER TABLE %s ADD %sCHECK (%s)",
			tabla, nombreDeRestriccion(c), c.Expression)
		st.Impact = change.ImpactScan
		st.Lock = change.LockAll
		st.Note = "Se leen todas las filas para verificar la condición. Si alguna no la " +
			"cumple, la sentencia falla."

	case change.DropConstraint:
		st.SQL = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s%s",
			tabla, QuoteIdent(c.Name), cascada(c))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "La garantía deja de existir: a partir de acá los datos pueden violar lo " +
			"que esta restricción impedía."

	case change.AddIndex:
		st.SQL = fmt.Sprintf("CREATE %sINDEX %sON %s%s (%s)%s%s",
			unico(c), nombreDeIndice(c), tabla, metodo(c),
			listaDeIdent(c.Names), incluidas(c), predicado(c))
		st.Impact = change.ImpactScan
		st.Lock = change.LockWrites
		st.Note = "Construir el índice lee toda la tabla y bloquea las escrituras mientras " +
			"tanto. Las lecturas siguen."

	case change.DropIndex:
		st.SQL = "DROP INDEX " + QualifiedName(c.Schema, c.Name) + cascada(c)
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockAll
		st.Note = "Las consultas que lo usaban vuelven a recorrer la tabla. Reconstruirlo " +
			"puede tardar tanto como tardó la primera vez."

	case change.ReplaceObject:
		return objetoDDL(c)

	case change.AddEnumValue:
		// El valor va como LITERAL, no como identificador. Es la excepción
		// declarada del DDL —un valor no se puede parametrizar en un ALTER
		// TYPE— y pasa por la vista previa antes de correr.
		// `quoteString` y no `literal`: `literal` convierte la cadena vacía en
		// el keyword NULL —que es lo que Postgres quiere para sacar un
		// comentario— y acá eso no significa nada. La cadena vacía ya la rechaza
		// la validación; que el helper equivocado la hubiera convertido en NULL
		// es exactamente el tipo de trampa que conviene no dejar armada.
		st.SQL = fmt.Sprintf("ALTER TYPE %s ADD VALUE %s",
			QualifiedName(c.Schema, c.Name), quoteString(c.Value))
		if c.Before != "" {
			st.SQL += " BEFORE " + quoteString(c.Before)
		}
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		// Se confirma sola: el valor no se puede usar hasta que su transacción
		// cierre, en todas las versiones que soportamos. Ver Statement.Aislada.
		st.Aislada = true
		st.Note = "Agregar un valor no toca ninguna fila, y se aplica en su propia transacción: " +
			"PostgreSQL no deja usar un valor nuevo hasta que se confirma. Lo que NO se puede es sacarlo después " +
			"ni moverlo de lugar: PostgreSQL no tiene DROP VALUE, y el orden de un enum es el " +
			"orden en que sus valores comparan y ordenan."

	case change.RenameEnumValue:
		st.SQL = fmt.Sprintf("ALTER TYPE %s RENAME VALUE %s TO %s",
			QualifiedName(c.Schema, c.Name), quoteString(c.Value), quoteString(c.NewName))
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Las filas que tenían el valor viejo pasan a leerse con el nuevo, todas a la " +
			"vez. Lo que compare contra el texto anterior —una consulta guardada, el código de " +
			"la aplicación— deja de encontrarlo."

	default:
		return st, fmt.Errorf("PostgreSQL: no sé escribir la operación %q", c.Type)
	}

	return st, nil
}

// columnaDDL arma la definición de una columna.
func columnaDDL(col change.Column) (string, error) {
	if !tipoValido.MatchString(col.DataType) {
		return "", fmt.Errorf("tipo inválido para la columna %q: %q", col.Name, col.DataType)
	}
	var b strings.Builder
	b.WriteString(QuoteIdent(col.Name))
	b.WriteString(" ")
	b.WriteString(col.DataType)
	if !col.Nullable {
		b.WriteString(" NOT NULL")
	}
	if col.Default != "" {
		b.WriteString(" DEFAULT ")
		b.WriteString(col.Default)
	}
	return b.String(), nil
}

// nombreDeRestriccion devuelve "CONSTRAINT nombre " o "" para que Postgres elija.
//
// Dejar que lo elija el motor no es pereza: el nombre generado sigue la
// convención de Postgres —tabla_columna_fkey— que es la que espera cualquiera
// que después lea el esquema desde otra herramienta.
func nombreDeRestriccion(c change.Change) string {
	if c.Name == "" {
		return ""
	}
	return "CONSTRAINT " + QuoteIdent(c.Name) + " "
}

func nombreDeIndice(c change.Change) string {
	if c.Name == "" {
		return ""
	}
	return QuoteIdent(c.Name) + " "
}

func unico(c change.Change) string {
	if c.Unique {
		return "UNIQUE "
	}
	return ""
}

func metodo(c change.Change) string {
	if c.Method == "" {
		return ""
	}
	// El access method es un identificador del catálogo: btree, gin, gist…
	return " USING " + QuoteIdent(c.Method)
}

func incluidas(c change.Change) string {
	if len(c.Included) == 0 {
		return ""
	}
	return " INCLUDE (" + listaDeIdent(c.Included) + ")"
}

func predicado(c change.Change) string {
	if c.Where == "" {
		return ""
	}
	return " WHERE " + c.Where
}

func cascada(c change.Change) string {
	if c.Cascade {
		return " CASCADE"
	}
	return ""
}

func refEsquema(c change.Change) string {
	if c.RefSchema == "" {
		return c.Schema
	}
	return c.RefSchema
}

func accionesDDL(c change.Change) (string, error) {
	var b strings.Builder
	for _, par := range []struct{ clausula, valor string }{
		{"ON DELETE", c.OnDelete},
		{"ON UPDATE", c.OnUpdate},
	} {
		if par.valor == "" {
			continue
		}
		sql, ok := accionesReferenciales[par.valor]
		if !ok {
			return "", fmt.Errorf("acción referencial desconocida: %q", par.valor)
		}
		b.WriteString(" ")
		b.WriteString(par.clausula)
		b.WriteString(" ")
		b.WriteString(sql)
	}
	return b.String(), nil
}

// literal cita una cadena para que vaya como valor dentro de la sentencia.
//
// Solo se usa para comentarios, que es el único texto libre que termina como
// literal en el DDL. Duplicar la comilla simple es todo el escape que define el
// estándar, igual que con los identificadores.
//
// Un comentario vacío se escribe como NULL, que es como Postgres dice «sacale el
// comentario»: `IS ”` deja un comentario vacío, que no es lo mismo.
func literal(s string) string {
	if s == "" {
		return "NULL"
	}
	return quoteString(s)
}

// quoteString cita un texto como literal. Con standard_conforming_strings —el
// default desde 9.1— la barra invertida no escapa nada, así que duplicar la
// comilla es todo.
//
// Para los cambios de datos esto solo produce la SQL que se LEE; lo que se
// ejecuta lleva los valores como parámetros. Ver dml.
func quoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// dialectoDML es lo que dml necesita saber de Postgres.
var dialectoDML = dml.Dialect{
	Table:        QualifiedName,
	QuoteIdent:   QuoteIdent,
	QuoteLiteral: quoteString,
	Placeholder:  func(n int) string { return "$" + strconv.Itoa(n) },
	EmptyInsert:  "DEFAULT VALUES",
	InsertSuffix: func(ignorar bool) string {
		if ignorar {
			// Sin destino: cualquier restricción única o de exclusión que
			// choque saltea la fila. Elegir un destino exigiría saber cuál es
			// la clave que va a chocar, y puede ser más de una.
			return " ON CONFLICT DO NOTHING"
		}
		return ""
	},
}

func listaDeIdent(nombres []string) string {
	citados := make([]string, len(nombres))
	for i, n := range nombres {
		citados[i] = QuoteIdent(n)
	}
	return strings.Join(citados, ", ")
}

// objetoDDL escribe el reemplazo de la definición de un objeto.
//
// La definición se ejecuta TAL CUAL la escribió la persona: no se reescribe, no
// se le agrega un OR REPLACE, no se le corrige el nombre. Es la promesa del
// editor —lo que se ve es lo que se ejecuta— y también la única postura
// defendible: para agregarle un OR REPLACE al texto habría que parsearlo, y un
// cuerpo de PL/pgSQL con `$$` adentro no se parsea con una expresión regular.
//
// Lo que Kaname sí decide es si va precedida de un DROP, y eso lo pide la
// pantalla. Cuando lo pide, las dos sentencias van en la MISMA Statement: si
// fueran dos cambios del changeset, alguien podría excluir la segunda y dejar
// el objeto borrado.
func objetoDDL(c change.Change) (change.Statement, error) {
	st := change.Statement{ChangeID: c.ID, Destructive: c.Destructive()}

	nombre := QualifiedName(c.Schema, c.Name)
	// Una función sobrecargada necesita su firma para poder borrarse: sin ella
	// `DROP FUNCTION demo.calcular` falla con «is not unique» y, peor, con una
	// sola sobrecarga borraría la que no era.
	if c.Args != "" {
		if err := validarFirma(c.Args); err != nil {
			return st, err
		}
		nombre += "(" + c.Args + ")"
	}
	// El trigger es el único que NO se califica: la gramática de Postgres es
	// `DROP TRIGGER nombre ON tabla`, donde el nombre es un identificador
	// pelado. Con el esquema adelante —`DROP TRIGGER "s"."t" ON …`— el
	// servidor devuelve un error de sintaxis, comprobado. El esquema va del
	// lado de la TABLA, que es lo que de verdad lo lleva.
	var sufijo string
	if c.ObjectKind == schema.ObjTrigger {
		nombre = QuoteIdent(c.Name)
		sufijo = " ON " + QualifiedName(c.Schema, c.Table)
	}

	drop, definicion, err := change.ObjetoAReemplazar(c, "postgres", nombre, sufijo)
	if err != nil {
		return st, err
	}

	if drop == "" {
		st.SQL = definicion
		st.Impact = change.ImpactMetadata
		st.Lock = change.LockNone
		st.Note = "Se reemplaza en el lugar: si la definición nueva falla, el objeto queda como está."
		return st, nil
	}

	drop += cascada(c)
	// Las dos van como PASOS y no como una cadena con `;` en el medio: se
	// ejecutan por separado, que es lo único que funciona en los cuatro
	// motores. `SQL` sigue siendo lo que se lee en la vista previa.
	st.Steps = []string{drop, definicion}
	st.SQL = drop + ";\n" + definicion
	st.Impact = change.ImpactMetadata
	st.Lock = change.LockAll
	st.Note = "El objeto se borra y se vuelve a crear, así que lo que dependa de él se rompe. " +
		"Con «una sola transacción» puesto, un CREATE que falle revierte el DROP; sin ella, el " +
		"objeto queda borrado."
	return st, nil
}
