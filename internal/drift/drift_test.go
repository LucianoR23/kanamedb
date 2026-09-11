package drift

import (
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// destructivas son las operaciones que esta comparación NO puede producir
// nunca, pase lo que pase. Es la regla central del paquete.
var destructivas = map[change.Type]bool{
	change.DropTable:      true,
	change.DropColumn:     true,
	change.DropConstraint: true,
	change.DropIndex:      true,
	change.DeleteRow:      true,
}

func snap(esquemas ...schema.Schema) schema.Snapshot {
	s := schema.Snapshot{Database: "x", Schemas: esquemas}
	s.Normalize()
	return s
}

func tabla(nombre string, cols ...schema.Column) schema.Table {
	return schema.Table{Name: nombre, Columns: cols, RowEstimate: -1}
}

func col(nombre, tipo string, nullable bool) schema.Column {
	return schema.Column{Name: nombre, DataType: tipo, Nullable: nullable}
}

// buscar devuelve la diferencia de un objeto, o falla.
func buscar(t *testing.T, r Resultado, objeto string) Diferencia {
	t.Helper()
	var encontradas []Diferencia
	for _, d := range r.Diferencias {
		if d.Objeto == objeto {
			encontradas = append(encontradas, d)
		}
	}
	if len(encontradas) != 1 {
		t.Fatalf("se esperaba una diferencia sobre %q y hay %d: %+v", objeto, len(encontradas), r.Diferencias)
	}
	return encontradas[0]
}

/* ------------------------------------------------------- la regla central */

// Nada de lo que sale de una comparación puede borrar.
//
// Es la regla que justifica el paquete entero: lo que está solo en el destino
// puede tener datos, y desde acá no hay forma de saber cuántos. El caso recorre
// una comparación con TODAS las formas de «esto sobra del otro lado» a la vez y
// exige dos cosas: que ninguna traiga operación, y que ninguna operación de la
// comparación entera sea destructiva.
func TestLaComparacionNoGeneraNingunBorrado(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("orders", col("id", "bigint", false)),
	}})
	destino := snap(schema.Schema{
		Name: "public",
		Tables: []schema.Table{
			tabla("orders",
				col("id", "bigint", false),
				col("legacy_ref", "text", true)),
			tabla("tabla_vieja", col("id", "bigint", false)),
		},
		Objects: []schema.Object{
			{Kind: schema.ObjView, Schema: "public", Name: "v_vieja"},
		},
	}, schema.Schema{Name: "solo_en_destino", Tables: []schema.Table{tabla("t", col("id", "int", true))}})

	r := Comparar(origen, destino, Opciones{MismoMotor: true})

	soloDestino := 0
	for _, d := range r.Diferencias {
		if d.Cambio != nil && destructivas[d.Cambio.Type] {
			t.Errorf("%s generó %s: la comparación no borra nunca", d.Objeto, d.Cambio.Type)
		}
		if d.Lado != SoloEnDestino {
			continue
		}
		soloDestino++
		if d.Cambio != nil {
			t.Errorf("%s está solo en el destino y trae una operación (%s)", d.Objeto, d.Cambio.Type)
		}
		if d.SinSentencia == "" {
			t.Errorf("%s no trae operación y tampoco explica por qué", d.Objeto)
		}
	}
	// Cuatro formas distintas de sobrar: esquema, tabla, columna y objeto. Sin
	// esta cuenta, un bug que dejara de reportarlas haría pasar el caso solo.
	if soloDestino != 4 {
		t.Errorf("se encontraron %d diferencias del lado del destino, se esperaban 4: %+v",
			soloDestino, r.Diferencias)
	}
}

// Toda operación que sale de acá tiene que PODER entrar al changeset.
//
// Es el test que faltaba, y su ausencia costó una promesa falsa: el `addColumn`
// de una columna NOT NULL se generaba «con el aviso», y `change.Validate` lo
// rechaza —una columna NOT NULL nueva necesita un valor por defecto, que el
// catálogo no trae—. O sea que la pantalla ofrecía una operación y el changeset
// la devolvía con un error de validación, sin ningún camino hacia adelante.
//
// El caso anterior no lo agarraba porque comprobaba que el campo no fuera nil,
// no que sirviera. Una operación que el resto del sistema va a rechazar no es
// una operación.
func TestTodaOperacionGeneradaEsValida(t *testing.T) {
	conDefault := col("con_default", "text", true)
	conDefault.HasDefault = true
	pk := col("id", "bigint", false)
	pk.PrimaryKey = true

	fk := schema.ForeignKey{
		Name: "libros_autor_fk", Schema: "public", Table: "libros",
		Columns: []string{"autor_id"}, RefSchema: "public", RefTable: "autores",
		RefColumns: []string{"id"}, OnDelete: schema.Cascade,
	}

	// Una comparación con todas las formas que generan algo a la vez.
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		{Name: "libros",
			Columns: []schema.Column{
				pk,
				col("autor_id", "bigint", false),
				col("nota", "text", true),
				// Una columna NOT NULL que falta del otro lado: es el caso que
				// generaba un addColumn que el changeset rechaza, y sin él en
				// el fixture este test no cubría lo que decía cubrir. Lo
				// descubrió la inyección, no la lectura.
				col("moneda", "char(3)", false),
				conDefault,
			},
			ForeignKeys: []schema.ForeignKey{fk}, RowEstimate: -1},
		tabla("tabla_nueva", pk, col("texto", "text", true)),
		tabla("tipos", col("precio", "numeric(10,2)", false), col("nulable", "text", false)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("libros", col("id", "bigint", false), col("autor_id", "bigint", false), col("con_default", "text", true)),
		tabla("tipos", col("precio", "numeric(8,2)", false), col("nulable", "text", true)),
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: true})

	generadas := 0
	for _, d := range r.Diferencias {
		if d.Cambio == nil {
			continue
		}
		generadas++
		if err := d.Cambio.Validate(); err != nil {
			t.Errorf("%s generó un %s que el changeset rechaza: %v", d.Objeto, d.Cambio.Type, err)
		}
	}
	// Sin esta cuenta, una comparación que dejara de generar cualquier cosa
	// pasaría el caso en verde sin haber validado nada.
	if generadas < 5 {
		t.Errorf("solo se generaron %d operaciones: el caso dejó de cubrir lo que decía cubrir", generadas)
	}
}

// Una operación y un motivo son excluyentes: o se sabe escribir o se dice por
// qué no. Tener las dos cosas, o ninguna, deja a la pantalla eligiendo a cuál
// creerle.
func TestCadaDiferenciaTraeUnaOperacionOUnMotivo(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("a", col("id", "bigint", false), col("nueva", "text", true)),
		tabla("nueva_tabla", col("id", "bigint", false)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("a", col("id", "text", false)),
		tabla("sobra", col("id", "bigint", false)),
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: true})
	if len(r.Diferencias) == 0 {
		t.Fatal("no se encontró ninguna diferencia")
	}
	for _, d := range r.Diferencias {
		tieneOperacion := d.Cambio != nil
		tieneMotivo := d.SinSentencia != ""
		if tieneOperacion == tieneMotivo {
			t.Errorf("%s: operación=%v motivo=%q — tiene que ser una cosa o la otra",
				d.Objeto, tieneOperacion, d.SinSentencia)
		}
	}
}

// Un «no hay diferencias» sobre algo que no se miró es una mentira, así que lo
// que no se comparó se dice SIEMPRE — incluso cuando los dos esquemas son
// idénticos, que es justo cuando más se lo lee.
func TestSiempreSeDiceQueQuedoSinComparar(t *testing.T) {
	uno := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("a", col("id", "bigint", false)),
	}})

	r := Comparar(uno, uno, Opciones{MismoMotor: true})
	if len(r.Diferencias) != 0 {
		t.Errorf("dos esquemas idénticos dieron %d diferencias: %+v", len(r.Diferencias), r.Diferencias)
	}
	if len(r.NoComparado) == 0 {
		t.Fatal("no se dijo nada sobre lo que quedó sin comparar")
	}
	junto := strings.ToLower(strings.Join(r.NoComparado, " "))
	for _, tiene := range []string{"definición", "índices", "datos"} {
		if !strings.Contains(junto, tiene) {
			t.Errorf("lo que quedó sin comparar no menciona %q: %q", tiene, junto)
		}
	}
}

/* ------------------------------------------------------- lo que sí genera */

func TestUnaTablaQueFaltaSeCreaConTodasSusColumnas(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("reviews",
			col("id", "bigint", false),
			col("rating", "smallint", false),
			col("body", "text", true)),
	}})
	destino := snap(schema.Schema{Name: "public"})

	d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "reviews")
	if d.Lado != SoloEnOrigen || d.Clase != ClaseTabla {
		t.Fatalf("lado=%q clase=%q", d.Lado, d.Clase)
	}
	if d.Cambio == nil || d.Cambio.Type != change.CreateTable {
		t.Fatalf("no se generó un createTable: %+v", d.Cambio)
	}
	if len(d.Cambio.Columns) != 3 {
		t.Fatalf("el createTable lleva %d columnas, se esperaban 3", len(d.Cambio.Columns))
	}
	// La nulabilidad tiene que viajar: una tabla creada con todo nullable no es
	// la misma tabla, y el error aparecería recién al insertar.
	for _, c := range d.Cambio.Columns {
		quiero := c.Name == "body"
		if c.Nullable != quiero {
			t.Errorf("la columna %s quedó Nullable=%v, se esperaba %v", c.Name, c.Nullable, quiero)
		}
	}
	if d.Riesgo != RiesgoBajo {
		t.Errorf("Riesgo = %q: crear una tabla que no existía no puede romper nada", d.Riesgo)
	}
}

// El snapshot dice QUÉ columnas tienen default, no CUÁL es. Crear la tabla sin
// ellos es una diferencia nueva escondida adentro de la corrección, así que hay
// que avisarlo.
func TestUnaTablaConDefaultsAvisaQueSeCreaSinEllos(t *testing.T) {
	conDefault := col("estado", "text", false)
	conDefault.HasDefault = true

	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("pedidos", col("id", "bigint", false), conDefault),
	}})
	d := buscar(t, Comparar(origen, snap(schema.Schema{Name: "public"}), Opciones{MismoMotor: true}), "pedidos")

	if !strings.Contains(strings.ToLower(d.Nota), "defecto") {
		t.Errorf("la nota no avisa de los defaults: %q", d.Nota)
	}
	if d.Riesgo == RiesgoBajo {
		t.Error("una tabla que se crea sin sus defaults no es riesgo bajo")
	}
}

func TestUnaColumnaQueFaltaSeAgrega(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("orders", col("id", "bigint", false), col("note", "text", true)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("orders", col("id", "bigint", false)),
	}})

	d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "orders.note")
	if d.Cambio == nil || d.Cambio.Type != change.AddColumn {
		t.Fatalf("no se generó un addColumn: %+v", d.Cambio)
	}
	if d.Cambio.Column.DataType != "text" || !d.Cambio.Column.Nullable {
		t.Errorf("la columna se agregaría como %+v", d.Cambio.Column)
	}
	if d.Riesgo != RiesgoBajo {
		t.Errorf("Riesgo = %q: una columna nullable es solo metadatos", d.Riesgo)
	}
}

// Una columna NOT NULL nueva necesita un valor por defecto para las filas que
// ya están, y el catálogo no dice cuál es: solo dice que el origen tiene uno.
//
// Antes se generaba el `addColumn` igual, «con el aviso». Era una promesa falsa:
// `change.Validate` lo rechaza, así que mandarlo al changeset devolvía un error
// de validación sin camino hacia adelante. Se reporta la diferencia y se dice
// dónde sí se puede hacer.
func TestUnaColumnaNotNullQueFaltaNoSeGeneraSinSuDefault(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("orders", col("id", "bigint", false), col("moneda", "char(3)", false)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("orders", col("id", "bigint", false)),
	}})

	d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "orders.moneda")
	if d.Riesgo != RiesgoMedio {
		t.Errorf("Riesgo = %q, se esperaba medio", d.Riesgo)
	}
	if d.Cambio != nil {
		t.Fatalf("se generó una operación que el changeset rechaza: %+v", d.Cambio)
	}
	if !strings.Contains(strings.ToLower(d.SinSentencia), "editor") {
		t.Errorf("no se dice dónde sí se puede hacer: %q", d.SinSentencia)
	}
}

func TestUnTipoDistintoSeAlineaHaciaElOrigen(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("items", col("precio", "numeric(10,2)", false)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("items", col("precio", "numeric(8,2)", false)),
	}})

	d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "items.precio")
	if d.Lado != Distinto {
		t.Fatalf("lado = %q", d.Lado)
	}
	if d.Cambio == nil || d.Cambio.Type != change.SetColumnType {
		t.Fatalf("no se generó un setColumnType: %+v", d.Cambio)
	}
	// Hacia el ORIGEN: la dirección es lo único que hace útil la comparación, y
	// al revés dejaría el destino como estaba y el origen roto.
	if d.Cambio.DataType != "numeric(10,2)" {
		t.Errorf("se alinearía a %q, y el origen es numeric(10,2)", d.Cambio.DataType)
	}
	if d.Riesgo != RiesgoMedio {
		t.Errorf("Riesgo = %q: cambiar un tipo puede reescribir la tabla", d.Riesgo)
	}
}

func TestLaNulabilidadSeAlineaEnLosDosSentidos(t *testing.T) {
	casos := []struct {
		nombre                   string
		origenNullable           bool
		destinoNullable          bool
		quieroTipo               change.Type
		quieroRiesgoPorLoMenosMe bool
	}{
		{"el origen exige NOT NULL", false, true, change.SetNotNull, true},
		{"el origen lo relaja", true, false, change.DropNotNull, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
				tabla("t", col("c", "text", c.origenNullable)),
			}})
			destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
				tabla("t", col("c", "text", c.destinoNullable)),
			}})

			d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "t.c")
			if d.Cambio == nil || d.Cambio.Type != c.quieroTipo {
				t.Fatalf("se generó %+v, se esperaba %s", d.Cambio, c.quieroTipo)
			}
			if c.quieroRiesgoPorLoMenosMe && d.Riesgo == RiesgoBajo {
				t.Error("exigir NOT NULL recorre la tabla y puede fallar: no es riesgo bajo")
			}
		})
	}
}

// El snapshot dice que hay un default, no cuál. Inventar uno sería escribir un
// valor que nadie pidió en una columna de producción.
func TestUnDefaultQueFaltaNoSeInventa(t *testing.T) {
	conDefault := col("estado", "text", true)
	conDefault.HasDefault = true

	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{tabla("t", conDefault)}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("t", col("estado", "text", true)),
	}})

	d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "t.estado")
	if d.Cambio != nil {
		t.Fatalf("se generó una sentencia con un default que no se conoce: %+v", d.Cambio)
	}
	if d.SinSentencia == "" {
		t.Error("no se explicó por qué no hay sentencia")
	}
}

func TestUnaForaneaQueFaltaSeCreaConSusAcciones(t *testing.T) {
	fk := schema.ForeignKey{
		Name: "libros_autor_fk", Schema: "public", Table: "libros",
		Columns: []string{"autor_id"}, RefSchema: "public", RefTable: "autores",
		RefColumns: []string{"id"}, OnDelete: schema.Cascade,
	}
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		{Name: "libros", Columns: []schema.Column{col("autor_id", "bigint", false)},
			ForeignKeys: []schema.ForeignKey{fk}, RowEstimate: -1},
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("libros", col("autor_id", "bigint", false)),
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: true})
	var d Diferencia
	for _, x := range r.Diferencias {
		if x.Clase == ClaseForanea {
			d = x
		}
	}
	if d.Cambio == nil || d.Cambio.Type != change.AddForeignKey {
		t.Fatalf("no se generó un addForeignKey: %+v", r.Diferencias)
	}
	if d.Cambio.RefTable != "autores" || len(d.Cambio.RefNames) != 1 || d.Cambio.RefNames[0] != "id" {
		t.Errorf("el destino de la clave quedó %+v", d.Cambio)
	}
	// La acción referencial tiene que viajar: una clave con ON DELETE CASCADE y
	// otra sin él se comportan distinto ante un borrado, y eso es datos.
	if d.Cambio.OnDelete != string(schema.Cascade) {
		t.Errorf("OnDelete = %q, se esperaba cascade", d.Cambio.OnDelete)
	}
}

// Una tabla que se recrea SIN su clave primaria no es la misma tabla.
//
// Y el daño no se ve enseguida: `HasPrimaryKey` es lo que habilita editar la
// grilla, así que la tabla nueva queda de solo lectura en Kaname — y la
// comparación siguiente diría que están alineadas, porque la clave tampoco se
// comparaba. Dos errores que se tapaban entre ellos.
func TestUnaTablaQueFaltaSeCreaConSuClavePrimaria(t *testing.T) {
	pk := col("id", "bigint", false)
	pk.PrimaryKey = true

	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("reviews", pk, col("body", "text", true)),
	}})
	d := buscar(t, Comparar(origen, snap(schema.Schema{Name: "public"}), Opciones{MismoMotor: true}), "reviews")

	if d.Cambio == nil {
		t.Fatal("no se generó el createTable")
	}
	if len(d.Cambio.Names) != 1 || d.Cambio.Names[0] != "id" {
		t.Errorf("la clave primaria quedó en %q: la tabla se crearía sin clave", d.Cambio.Names)
	}
}

// Y la clave primaria se compara, que es la otra mitad del mismo problema.
func TestUnaClavePrimariaQueFaltaSeReporta(t *testing.T) {
	pk := col("id", "bigint", false)
	pk.PrimaryKey = true

	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{tabla("orders", pk)}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("orders", col("id", "bigint", false)),
	}})

	d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "orders.id")
	if d.Lado != Distinto {
		t.Fatalf("lado = %q", d.Lado)
	}
	if d.Cambio == nil || d.Cambio.Type != change.AddPrimaryKey {
		t.Fatalf("no se generó un addPrimaryKey: %+v", d.Cambio)
	}

	// Al revés NO se genera: soltar una clave primaria es borrar.
	alReves := buscar(t, Comparar(destino, origen, Opciones{MismoMotor: true}), "orders.id")
	if alReves.Cambio != nil {
		t.Errorf("se generó el borrado de una clave primaria: %+v", alReves.Cambio)
	}
}

// El NOMBRE de la clave foránea viaja, y sin él la comparación NUNCA converge.
//
// Las claves se comparan por nombre. Si se crea con el nombre que le pone el
// motor, la comparación siguiente ve la del origen como «falta en el destino» y
// la recién creada como «sobra en el destino», las dos a la vez y para siempre —
// y aplicar otra vez deja una clave duplicada.
func TestLaForaneaSeCreaConSuNombre(t *testing.T) {
	fk := schema.ForeignKey{
		Name: "fk_elegido_a_mano", Schema: "public", Table: "libros",
		Columns: []string{"autor_id"}, RefSchema: "public", RefTable: "autores",
		RefColumns: []string{"id"},
	}
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		{Name: "libros", Columns: []schema.Column{col("autor_id", "bigint", false)},
			ForeignKeys: []schema.ForeignKey{fk}, RowEstimate: -1},
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("libros", col("autor_id", "bigint", false)),
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: true})
	for _, d := range r.Diferencias {
		if d.Clase != ClaseForanea || d.Cambio == nil {
			continue
		}
		if d.Cambio.Name != "fk_elegido_a_mano" {
			t.Errorf("la clave se crearía con el nombre %q en vez del del origen", d.Cambio.Name)
		}
		return
	}
	t.Fatal("no se generó ninguna clave foránea")
}

// Entre motores distintos tampoco se copian los tipos DENTRO de un CREATE TABLE
// o de un ADD COLUMN.
//
// Suprimir la comparación de tipos sin suprimir esto dejaba la mitad del
// problema, que es peor que ninguna: un `jsonb` o un `timestamp with time zone`
// de Postgres adentro de un CREATE TABLE de MySQL es una sentencia que no corre,
// y la pantalla la ofrecía como si sí.
func TestEntreMotoresDistintosNoSeCopiaNingunTipo(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("nueva", col("payload", "jsonb", true)),
		tabla("vieja", col("id", "bigint", false), col("creado", "timestamp with time zone", true)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("vieja", col("id", "bigint", false)),
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: false})
	vistas := 0
	for _, d := range r.Diferencias {
		if d.Lado != SoloEnOrigen || d.Clase == ClaseObjeto {
			continue
		}
		vistas++
		if d.Cambio != nil {
			t.Errorf("%s llevaría el tipo del otro motor adentro: %+v", d.Objeto, d.Cambio)
		}
		if d.SinSentencia == "" {
			t.Errorf("%s no explica por qué no hay sentencia", d.Objeto)
		}
	}
	if vistas != 2 {
		t.Errorf("se miraron %d diferencias, se esperaban 2 (la tabla y la columna)", vistas)
	}

	// Con el mismo motor esas dos SÍ se generan. Sin este medio caso, dejar de
	// generarlas del todo pasaría el test de arriba.
	conMismo := Comparar(origen, destino, Opciones{MismoMotor: true})
	generadas := 0
	for _, d := range conMismo.Diferencias {
		if d.Cambio != nil {
			generadas++
		}
	}
	if generadas != 2 {
		t.Errorf("con el mismo motor se generaron %d operaciones, se esperaban 2", generadas)
	}
}

// Las vistas y las funciones se comparan por NOMBRE, así que dos homónimas con
// cuerpos distintos NO se reportan — y por eso el paquete lo dice en
// NoComparado. Lo que sí se reporta es la que está de un solo lado.
func TestUnObjetoSeReportaPorNombreYSinSentencia(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Objects: []schema.Object{
		{Kind: schema.ObjView, Schema: "public", Name: "v_ventas"},
	}})
	destino := snap(schema.Schema{Name: "public"})

	d := buscar(t, Comparar(origen, destino, Opciones{MismoMotor: true}), "v_ventas")
	if d.Lado != SoloEnOrigen || d.Clase != ClaseObjeto {
		t.Fatalf("lado=%q clase=%q", d.Lado, d.Clase)
	}
	if d.Cambio != nil {
		t.Fatalf("se generó una sentencia sin tener la definición: %+v", d.Cambio)
	}
	if d.SinSentencia == "" {
		t.Error("no se explicó por qué no hay sentencia")
	}
}

// Dos objetos con el mismo nombre y distinta clase son cosas distintas y no se
// comparan entre sí.
func TestUnaVistaYUnaFuncionHomonimasNoSeConfunden(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Objects: []schema.Object{
		{Kind: schema.ObjView, Schema: "public", Name: "ventas"},
	}})
	destino := snap(schema.Schema{Name: "public", Objects: []schema.Object{
		{Kind: schema.ObjFunction, Schema: "public", Name: "ventas"},
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: true})
	if len(r.Diferencias) != 2 {
		t.Fatalf("se esperaban dos diferencias —una de cada lado— y hay %d: %+v",
			len(r.Diferencias), r.Diferencias)
	}
}

/* ------------------------------------------------------------ el orden */

// Dos corridas de la misma comparación tienen que dar exactamente lo mismo, en
// el mismo orden: el recorrido va por mapas, y sin ordenar, dos comparaciones
// idénticas se verían distintas cada vez que se aprieta «Volver a escanear».
func TestElOrdenDeLaSalidaEsEstable(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("zeta", col("id", "bigint", false)),
		tabla("alfa", col("id", "bigint", false)),
		tabla("media", col("id", "bigint", false)),
	}})
	destino := snap(schema.Schema{Name: "public"})

	primera := Comparar(origen, destino, Opciones{MismoMotor: true})
	for i := 0; i < 20; i++ {
		otra := Comparar(origen, destino, Opciones{MismoMotor: true})
		if len(otra.Diferencias) != len(primera.Diferencias) {
			t.Fatalf("la corrida %d dio %d diferencias y la primera %d",
				i, len(otra.Diferencias), len(primera.Diferencias))
		}
		for j := range otra.Diferencias {
			if otra.Diferencias[j].ID != primera.Diferencias[j].ID {
				t.Fatalf("la corrida %d cambió el orden: %q contra %q",
					i, otra.Diferencias[j].ID, primera.Diferencias[j].ID)
			}
		}
	}
}

// Entre motores distintos, los tipos NO se comparan.
//
// `character varying(255)` y `varchar(255)` son el mismo tipo escrito por dos
// motores, y hay uno así por columna: compararlos daría cientos de diferencias
// que no lo son, cada una con una sentencia equivocada. Ruido con esa forma no
// molesta, esconde las diferencias de verdad. Lo que sí se compara igual es qué
// tablas y qué columnas hay de cada lado, que es lo que uno mira migrando.
func TestEntreMotoresDistintosLosTiposNoSeComparan(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("t",
			col("nombre", "character varying(255)", true),
			col("solo_en_origen", "text", true)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("t", col("nombre", "varchar(255)", true)),
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: false})
	for _, d := range r.Diferencias {
		if d.Objeto == "t.nombre" {
			t.Errorf("se reportó un tipo entre motores distintos: %+v", d)
		}
	}
	// Y la columna que de verdad falta sigue apareciendo.
	if _, ok := buscarOpcional(r, "t.solo_en_origen"); !ok {
		t.Error("la columna que falta no se reportó: solo los TIPOS quedan afuera")
	}
	junto := strings.ToLower(strings.Join(r.NoComparado, " "))
	if !strings.Contains(junto, "tipos") {
		t.Errorf("no se dijo que los tipos quedaron sin comparar: %q", junto)
	}

	// Con el mismo motor, esa misma diferencia SÍ se reporta. Sin este medio
	// caso, apagar la comparación de tipos del todo pasaría el test de arriba.
	conMismoMotor := Comparar(origen, destino, Opciones{MismoMotor: true})
	if _, ok := buscarOpcional(conMismoMotor, "t.nombre"); !ok {
		t.Error("con el mismo motor el tipo distinto tiene que reportarse")
	}
}

func buscarOpcional(r Resultado, objeto string) (Diferencia, bool) {
	for _, d := range r.Diferencias {
		if d.Objeto == objeto {
			return d, true
		}
	}
	return Diferencia{}, false
}

func TestLaCuentaSeparaLosTresLados(t *testing.T) {
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("a", col("id", "bigint", false)),
		tabla("b", col("id", "text", false)),
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("b", col("id", "bigint", false)),
		tabla("c", col("id", "bigint", false)),
	}})

	soloOrigen, distintas, soloDestino := Comparar(origen, destino, Opciones{MismoMotor: true}).Cuenta()
	if soloOrigen != 1 || distintas != 1 || soloDestino != 1 {
		t.Errorf("cuenta = (%d, %d, %d), se esperaba (1, 1, 1)", soloOrigen, distintas, soloDestino)
	}
}

// Las claves foráneas de una tabla nueva no viajan en su CREATE TABLE, y eso
// se dice. No se pierden —la comparación siguiente las reporta— pero una tabla
// «igual» creada sin sus claves es una diferencia escondida adentro de la
// corrección, y callarla es lo que el paquete promete no hacer.
func TestUnaTablaNuevaConForaneasAvisaQueSeCreaSinEllas(t *testing.T) {
	fk := schema.ForeignKey{
		Name: "libros_autor_fk", Schema: "public", Table: "libros",
		Columns: []string{"autor_id"}, RefSchema: "public", RefTable: "autores",
		RefColumns: []string{"id"},
	}
	origen := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("autores", col("id", "bigint", false)),
		{Name: "libros", Columns: []schema.Column{col("autor_id", "bigint", false)},
			ForeignKeys: []schema.ForeignKey{fk}, RowEstimate: -1},
	}})
	destino := snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("autores", col("id", "bigint", false)),
	}})

	r := Comparar(origen, destino, Opciones{MismoMotor: true})
	libros := buscar(t, r, "libros")
	if libros.Cambio == nil || libros.Cambio.Type != change.CreateTable {
		t.Fatalf("la tabla nueva no salió como createTable: %+v", libros)
	}
	if !strings.Contains(libros.Nota, "clave foránea no va") {
		t.Errorf("la nota no avisa que la tabla se crea sin sus claves foráneas: %q", libros.Nota)
	}
	// Y la clave en sí NO se reporta aparte: no hay tabla del otro lado contra
	// la que compararla todavía. Reportarla con un addForeignKey sobre una
	// tabla que no existe sería una operación que el destino rechaza.
	for _, d := range r.Diferencias {
		if d.Clase == ClaseForanea {
			t.Errorf("se reportó una clave foránea de una tabla que no existe en el destino: %+v", d)
		}
	}

	// Una tabla nueva sin claves no lleva el aviso: sería ruido.
	sin := Comparar(snap(schema.Schema{Name: "public", Tables: []schema.Table{
		tabla("autores", col("id", "bigint", false)),
	}}), snap(schema.Schema{Name: "public"}), Opciones{MismoMotor: true})
	if strings.Contains(buscar(t, sin, "autores").Nota, "foránea") {
		t.Error("una tabla sin claves foráneas lleva el aviso igual")
	}
}
