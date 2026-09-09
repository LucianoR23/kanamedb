package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// col arma una columna para las fixtures.
func col(nombre, tipo string, nullable bool) change.Column {
	return change.Column{Name: nombre, DataType: tipo, Nullable: nullable}
}

// aplicar renderiza el cambio y lo ejecuta. Es el camino completo: si el
// renderizado produce SQL que Postgres no acepta, se ve acá y no en producción.
func aplicar(t *testing.T, pool *pgxpool.Pool, c change.Change) change.Statement {
	t.Helper()
	st, err := RenderDDL(c)
	if err != nil {
		t.Fatalf("RenderDDL(%s) falló: %v", c.Type, err)
	}
	if _, err := pool.Exec(context.Background(), st.SQL); err != nil {
		t.Fatalf("la SQL generada no se pudo ejecutar.\n  operación: %s\n  SQL: %s\n  error: %v",
			c.Type, st.SQL, err)
	}
	return st
}

// detalleDe lee la estructura de una tabla después de aplicar.
func detalleDe(t *testing.T, pool *pgxpool.Pool, esq, tabla string) *schema.TableDetail {
	t.Helper()
	d, err := Detail(context.Background(), pool, esq, tabla)
	if err != nil {
		t.Fatalf("Detail(%s.%s) falló: %v", esq, tabla, err)
	}
	return d
}

func buscarColumna(d *schema.TableDetail, nombre string) (schema.DetailColumn, bool) {
	for _, c := range d.Columns {
		if c.Name == nombre {
			return c, true
		}
	}
	return schema.DetailColumn{}, false
}

// El test que decide si este paquete sirve: cada operación se renderiza, se
// EJECUTA contra Postgres, y después se vuelve a leer el catálogo para
// comprobar que quedó lo que se pidió.
//
// Comparar la SQL contra una cadena esperada no prueba nada de esto: una
// sentencia puede verse perfecta, ejecutarse sin error y dejar otra cosa. Es
// exactamente lo que hace Atlas con las columnas VIRTUAL, y es lo que este test
// existe para no repetir.
func TestCadaOperacionDejaElEsquemaComoSePidio(t *testing.T) {
	pool, esq := conectar(t)

	// --- crear una tabla con clave primaria -------------------------------
	crear := base(t, esq, change.CreateTable, "pedidos")
	crear.Columns = []change.Column{
		col("id", "bigint", false),
		col("nota", "text", true),
		col("total", "numeric(10,2)", false),
	}
	crear.Names = []string{"id"}
	aplicar(t, pool, crear)

	d := detalleDe(t, pool, esq, "pedidos")
	if len(d.Columns) != 3 {
		t.Fatalf("la tabla quedó con %d columnas: %+v", len(d.Columns), d.Columns)
	}
	if c, _ := buscarColumna(d, "id"); !c.PrimaryKey || c.Nullable {
		t.Errorf("id: PrimaryKey = %v, Nullable = %v", c.PrimaryKey, c.Nullable)
	}
	if c, _ := buscarColumna(d, "total"); c.DataType != "numeric(10,2)" {
		t.Errorf("total: DataType = %q, se esperaba numeric(10,2)", c.DataType)
	}
	if c, _ := buscarColumna(d, "nota"); !c.Nullable {
		t.Error("nota: Nullable = false y se pidió que admitiera nulos")
	}

	// --- agregar una columna con default ----------------------------------
	agregar := base(t, esq, change.AddColumn, "pedidos")
	agregar.Column = &change.Column{
		Name: "moneda", DataType: "char(3)", Nullable: false, Default: "'EUR'",
	}
	aplicar(t, pool, agregar)
	if c, ok := buscarColumna(detalleDe(t, pool, esq, "pedidos"), "moneda"); !ok ||
		c.Nullable || !strings.Contains(c.Default, "EUR") {
		t.Errorf("moneda: %+v", c)
	}

	// --- comentarios -------------------------------------------------------
	comentarTabla := base(t, esq, change.SetTableComment, "pedidos")
	comentarTabla.Comment = "los pedidos"
	aplicar(t, pool, comentarTabla)

	comentarCol := base(t, esq, change.SetColumnComment, "pedidos")
	comentarCol.Column = &change.Column{Name: "total"}
	comentarCol.Comment = "sin IVA"
	aplicar(t, pool, comentarCol)

	d = detalleDe(t, pool, esq, "pedidos")
	if d.Comment != "los pedidos" {
		t.Errorf("comentario de la tabla = %q", d.Comment)
	}
	if c, _ := buscarColumna(d, "total"); c.Comment != "sin IVA" {
		t.Errorf("comentario de total = %q", c.Comment)
	}

	// Un comentario vacío BORRA el comentario. `IS ''` dejaría uno vacío, que
	// no es lo mismo y se ve distinto en cualquier herramienta.
	borrarComentario := base(t, esq, change.SetTableComment, "pedidos")
	aplicar(t, pool, borrarComentario)
	if d := detalleDe(t, pool, esq, "pedidos"); d.Comment != "" {
		t.Errorf("después de borrar, el comentario quedó en %q", d.Comment)
	}

	// --- nulabilidad y defaults -------------------------------------------
	quitarNotNull := base(t, esq, change.DropNotNull, "pedidos")
	quitarNotNull.Column = &change.Column{Name: "total"}
	aplicar(t, pool, quitarNotNull)
	if c, _ := buscarColumna(detalleDe(t, pool, esq, "pedidos"), "total"); !c.Nullable {
		t.Error("total: sigue siendo NOT NULL después de DropNotNull")
	}

	ponerNotNull := base(t, esq, change.SetNotNull, "pedidos")
	ponerNotNull.Column = &change.Column{Name: "total"}
	aplicar(t, pool, ponerNotNull)
	if c, _ := buscarColumna(detalleDe(t, pool, esq, "pedidos"), "total"); c.Nullable {
		t.Error("total: sigue admitiendo nulos después de SetNotNull")
	}

	ponerDefault := base(t, esq, change.SetDefault, "pedidos")
	ponerDefault.Column = &change.Column{Name: "total", Default: "0.00"}
	aplicar(t, pool, ponerDefault)
	if c, _ := buscarColumna(detalleDe(t, pool, esq, "pedidos"), "total"); c.Default == "" {
		t.Error("total: quedó sin valor por defecto")
	}

	sacarDefault := base(t, esq, change.DropDefault, "pedidos")
	sacarDefault.Column = &change.Column{Name: "total"}
	aplicar(t, pool, sacarDefault)
	if c, _ := buscarColumna(detalleDe(t, pool, esq, "pedidos"), "total"); c.Default != "" {
		t.Errorf("total: quedó con default %q después de DropDefault", c.Default)
	}

	// --- cambiar el tipo ---------------------------------------------------
	cambiarTipo := base(t, esq, change.SetColumnType, "pedidos")
	cambiarTipo.Column = &change.Column{Name: "nota"}
	cambiarTipo.DataType = "character varying(80)"
	aplicar(t, pool, cambiarTipo)
	if c, _ := buscarColumna(detalleDe(t, pool, esq, "pedidos"), "nota"); c.DataType != "character varying(80)" {
		t.Errorf("nota: DataType = %q", c.DataType)
	}

	// --- renombrar ---------------------------------------------------------
	renombrarCol := base(t, esq, change.RenameColumn, "pedidos")
	renombrarCol.Column = &change.Column{Name: "nota"}
	renombrarCol.NewName = "observacion"
	aplicar(t, pool, renombrarCol)
	d = detalleDe(t, pool, esq, "pedidos")
	if _, ok := buscarColumna(d, "observacion"); !ok {
		t.Error("no apareció la columna renombrada")
	}
	if _, ok := buscarColumna(d, "nota"); ok {
		t.Error("la columna vieja sigue ahí")
	}

	// --- restricciones -----------------------------------------------------
	check := base(t, esq, change.AddCheck, "pedidos")
	check.Name = "pedidos_total_no_negativo"
	check.Expression = "total >= 0"
	aplicar(t, pool, check)

	d = detalleDe(t, pool, esq, "pedidos")
	if len(d.Checks) != 1 || d.Checks[0].Name != "pedidos_total_no_negativo" {
		t.Fatalf("restricciones = %+v", d.Checks)
	}
	if !d.Checks[0].Validated {
		t.Error("la restricción quedó sin validar")
	}

	// --- índices -----------------------------------------------------------
	indice := base(t, esq, change.AddIndex, "pedidos")
	indice.Name = "pedidos_moneda_idx"
	indice.Names = []string{"moneda"}
	indice.Included = []string{"total"}
	indice.Where = "total > 0"
	aplicar(t, pool, indice)

	d = detalleDe(t, pool, esq, "pedidos")
	var ix *schema.Index
	for i := range d.Indexes {
		if d.Indexes[i].Name == "pedidos_moneda_idx" {
			ix = &d.Indexes[i]
		}
	}
	if ix == nil {
		t.Fatalf("no se creó el índice: %+v", d.Indexes)
	}
	if !reflect.DeepEqual(ix.Columns, []string{"moneda"}) {
		t.Errorf("columnas del índice = %v", ix.Columns)
	}
	if !reflect.DeepEqual(ix.Included, []string{"total"}) {
		t.Errorf("incluidas del índice = %v", ix.Included)
	}
	if ix.Predicate == "" {
		t.Error("el índice parcial quedó sin predicado")
	}

	// --- clave foránea -----------------------------------------------------
	crearPadre := base(t, esq, change.CreateTable, "clientes")
	crearPadre.Columns = []change.Column{col("id", "bigint", false)}
	crearPadre.Names = []string{"id"}
	aplicar(t, pool, crearPadre)

	agregarFK := base(t, esq, change.AddColumn, "pedidos")
	agregarFK.Column = &change.Column{Name: "cliente_id", DataType: "bigint", Nullable: true}
	aplicar(t, pool, agregarFK)

	fk := base(t, esq, change.AddForeignKey, "pedidos")
	fk.Name = "pedidos_cliente_fk"
	fk.Names = []string{"cliente_id"}
	fk.RefTable = "clientes"
	fk.RefNames = []string{"id"}
	fk.OnDelete = "cascade"
	fk.OnUpdate = "restrict"
	aplicar(t, pool, fk)

	d = detalleDe(t, pool, esq, "pedidos")
	if len(d.ForeignKeys) != 1 {
		t.Fatalf("claves foráneas = %+v", d.ForeignKeys)
	}
	got := d.ForeignKeys[0]
	if got.Name != "pedidos_cliente_fk" {
		t.Errorf("nombre = %q", got.Name)
	}
	if got.OnDelete != schema.Cascade || got.OnUpdate != schema.Restrict {
		t.Errorf("acciones = %q / %q", got.OnDelete, got.OnUpdate)
	}
	if got.RefTable != "clientes" || !reflect.DeepEqual(got.RefColumns, []string{"id"}) {
		t.Errorf("destino = %s.%v", got.RefTable, got.RefColumns)
	}
	if !got.Optional {
		t.Error("la clave tendría que ser opcional: la columna admite nulos")
	}

	// --- borrar ------------------------------------------------------------
	borrarFK := base(t, esq, change.DropConstraint, "pedidos")
	borrarFK.Name = "pedidos_cliente_fk"
	aplicar(t, pool, borrarFK)
	if d := detalleDe(t, pool, esq, "pedidos"); len(d.ForeignKeys) != 0 {
		t.Errorf("la clave sigue ahí: %+v", d.ForeignKeys)
	}

	borrarIndice := base(t, esq, change.DropIndex, "pedidos")
	borrarIndice.Name = "pedidos_moneda_idx"
	aplicar(t, pool, borrarIndice)
	for _, i := range detalleDe(t, pool, esq, "pedidos").Indexes {
		if i.Name == "pedidos_moneda_idx" {
			t.Error("el índice sigue ahí")
		}
	}

	borrarCol := base(t, esq, change.DropColumn, "pedidos")
	borrarCol.Column = &change.Column{Name: "observacion"}
	aplicar(t, pool, borrarCol)
	if _, ok := buscarColumna(detalleDe(t, pool, esq, "pedidos"), "observacion"); ok {
		t.Error("la columna sigue ahí")
	}

	renombrarTabla := base(t, esq, change.RenameTable, "pedidos")
	renombrarTabla.NewName = "ordenes"
	aplicar(t, pool, renombrarTabla)
	if _, err := Detail(context.Background(), pool, esq, "ordenes"); err != nil {
		t.Errorf("la tabla renombrada no aparece: %v", err)
	}

	borrarTabla := base(t, esq, change.DropTable, "ordenes")
	aplicar(t, pool, borrarTabla)
	if _, err := Detail(context.Background(), pool, esq, "ordenes"); err == nil {
		t.Error("la tabla sigue existiendo después del DROP")
	}
}

// Un nombre hostil tiene que salir como un identificador raro, no como SQL.
//
// El test crea la tabla con el nombre hostil de verdad y comprueba las dos
// cosas: que la sentencia inyectada NO se ejecutó, y que el objeto con ese
// nombre existe. Sin lo segundo, el test pasaría también si el renderizado
// hubiera fallado por cualquier otro motivo.
func TestUnNombreHostilNoSeConvierteEnSQL(t *testing.T) {
	pool, esq := conectar(t)

	// Una tabla que la inyección querría borrar. Existe de verdad, así que si
	// la inyección funciona, desaparece.
	testigo := base(t, esq, change.CreateTable, "testigo")
	testigo.Columns = []change.Column{col("id", "bigint", false)}
	aplicar(t, pool, testigo)

	// Corto a propósito: lo que se prueba es el citado, no el límite de largo.
	// Con el nombre del esquema adentro se pasaría de 63 bytes y el renderizado
	// lo rechazaría antes de llegar a lo interesante.
	const hostil = `raro"); DROP TABLE testigo; --`

	crear := base(t, esq, change.CreateTable, hostil)
	crear.Columns = []change.Column{col("id", "bigint", false)}
	aplicar(t, pool, crear)

	// El testigo sigue vivo: la inyección no se ejecutó.
	if _, err := Detail(context.Background(), pool, esq, "testigo"); err != nil {
		t.Fatalf("la tabla testigo desapareció: la inyección se ejecutó. %v", err)
	}
	// Y la tabla con el nombre hostil existe con ese nombre exacto: el
	// identificador se citó, no se escapó a medias.
	if _, err := Detail(context.Background(), pool, esq, hostil); err != nil {
		t.Errorf("no existe una tabla llamada %q: %v", hostil, err)
	}

	// Lo mismo con una columna.
	const colHostil = `c"); DROP TABLE testigo; --`
	agregar := base(t, esq, change.AddColumn, "testigo")
	agregar.Column = &change.Column{Name: colHostil, DataType: "text", Nullable: true}
	aplicar(t, pool, agregar)

	d := detalleDe(t, pool, esq, "testigo")
	if _, ok := buscarColumna(d, colHostil); !ok {
		t.Errorf("no existe la columna %q: %+v", colHostil, d.Columns)
	}
}

func base(t *testing.T, esq string, tipo change.Type, tabla string) change.Change {
	t.Helper()
	return change.Change{Type: tipo, Schema: esq, Table: tabla, Source: "test"}
}

// Un tipo no se puede citar —`numeric(10,2)` no es un identificador— así que va
// crudo a la sentencia. La validación no comprueba que el tipo exista: comprueba
// que no pueda ser otra cosa.
// PostgreSQL trunca los identificadores de más de 63 bytes SIN AVISAR: una
// columna de 70 caracteres queda con 63 y el objeto termina con otro nombre.
// Dos nombres largos distintos pueden colisionar. Se rechaza antes.
func TestUnIdentificadorDemasiadoLargoSeRechaza(t *testing.T) {
	largo := strings.Repeat("a", 64)
	corto := strings.Repeat("a", 63)

	casos := []struct {
		nombre string
		c      change.Change
	}{
		{"tabla", change.Change{Type: change.DropTable, Schema: "s", Table: largo}},
		{"columna nueva", change.Change{
			Type: change.AddColumn, Schema: "s", Table: "t",
			Column: &change.Column{Name: largo, DataType: "text", Nullable: true},
		}},
		{"restricción", change.Change{
			Type: change.AddCheck, Schema: "s", Table: "t", Name: largo, Expression: "true",
		}},
		{"columna de un índice", change.Change{
			Type: change.AddIndex, Schema: "s", Table: "t", Names: []string{largo},
		}},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := RenderDDL(c.c)
			if err == nil {
				t.Fatalf("RenderDDL aceptó un identificador de %d bytes", len(largo))
			}
			if !strings.Contains(err.Error(), "63") {
				t.Errorf("el error no dice cuál es el límite: %v", err)
			}
		})
	}

	// Y justo en el límite tiene que pasar, o la validación estaría corriendo el
	// borde un byte de más.
	enElLimite := change.Change{
		Type: change.AddColumn, Schema: "s", Table: "t",
		Column: &change.Column{Name: corto, DataType: "text", Nullable: true},
	}
	if _, err := RenderDDL(enElLimite); err != nil {
		t.Errorf("RenderDDL rechazó un nombre de exactamente %d bytes: %v", len(corto), err)
	}
}

func TestUnTipoNoPuedeTraerSQL(t *testing.T) {
	hostiles := []string{
		"text; DROP TABLE x",
		"text -- comentario",
		"text /* bloque */",
		"text'",
		"",
	}
	for _, tipo := range hostiles {
		c := change.Change{
			Type: change.AddColumn, Schema: "s", Table: "t",
			Column: &change.Column{Name: "c", DataType: tipo, Nullable: true},
		}
		if _, err := RenderDDL(c); err == nil {
			t.Errorf("RenderDDL aceptó el tipo %q", tipo)
		}
	}

	// Y los tipos de verdad tienen que pasar, o la validación sería inútil.
	validos := []string{
		"text", "bigint", "numeric(10,2)", "character varying(255)", "text[]",
		"timestamp with time zone", "demo.estado", `"MiTipo"`, "double precision",
	}
	for _, tipo := range validos {
		c := change.Change{
			Type: change.AddColumn, Schema: "s", Table: "t",
			Column: &change.Column{Name: "c", DataType: tipo, Nullable: true},
		}
		if _, err := RenderDDL(c); err != nil {
			t.Errorf("RenderDDL rechazó el tipo válido %q: %v", tipo, err)
		}
	}
}
