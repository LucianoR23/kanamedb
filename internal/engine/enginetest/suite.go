// Package enginetest es la batería que TODO motor tiene que pasar.
//
// Existe para que agregar un motor no sea una promesa. Cada implementación de
// engine.Conn corre exactamente los mismos casos, así que lo que un motor no
// sepa hacer se ve acá y no en producción. Es también la definición ejecutable
// de qué significa «está implementado»: si la suite pasa, el motor sirve para
// lo que la aplicación hace.
//
// La suite crea sus propios objetos y los borra. No asume nada del contenido de
// la base, y por eso puede correr contra la misma instancia que los otros tests.
package enginetest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Fixture es lo que cada motor tiene que darle a la suite.
type Fixture struct {
	// Abrir devuelve una conexión lista. La suite la cierra.
	Abrir func(t *testing.T) engine.Conn

	// Esquema es dónde crear los objetos de prueba. En Postgres es un esquema
	// de verdad; en MySQL y MariaDB es la base; en SQLite es "".
	Esquema func(c engine.Conn) string

	// TipoTexto y TipoEntero son cómo se ESCRIBEN en un CREATE TABLE de este
	// motor, con modificador incluido. No se puede suponer «text» e «integer»:
	// MySQL rechaza `varchar` sin largo, y un TEXT no se indexa sin decirle
	// cuántos caracteres.
	TipoTexto  string
	TipoEntero string

	// TiposEsperados son los nombres que ColumnTypes tiene que ofrecer. Van
	// aparte de los de arriba porque el catálogo lista el tipo sin modificador:
	// se crea con `varchar(64)` y se ofrece `varchar`.
	TiposEsperados []string
}

// Correr ejecuta la batería completa.
func Correr(t *testing.T, f Fixture) {
	t.Helper()

	t.Run("identidad", func(t *testing.T) { identidad(t, f) })
	t.Run("introspeccion", func(t *testing.T) { introspeccion(t, f) })
	t.Run("detalle", func(t *testing.T) { detalle(t, f) })
	t.Run("tipos de columna", func(t *testing.T) { tipos(t, f) })
	t.Run("consultas", func(t *testing.T) { consultas(t, f) })
	t.Run("pagina y cuenta", func(t *testing.T) { pagina(t, f) })
	t.Run("recorre la tabla entera", func(t *testing.T) { recorrido(t, f) })
	t.Run("filtra por columna", func(t *testing.T) { filtros(t, f) })
	t.Run("dice qué no sabe volcar", func(t *testing.T) { cobertura(t, f) })
	t.Run("la definición de un objeto se puede volver a correr", func(t *testing.T) { definicion(t, f) })
	t.Run("una lista de dependientes vacía no es lo mismo que no saber", func(t *testing.T) { dependientes(t, f) })
	t.Run("reemplazar la definición de un objeto", func(t *testing.T) { reemplazarObjeto(t, f) })
	t.Run("transacciones de datos", func(t *testing.T) { transacciones(t, f) })
	t.Run("cambios de datos", func(t *testing.T) { cambiosDeDatos(t, f) })
	t.Run("errores de sentencia", func(t *testing.T) { errores(t, f) })
	t.Run("ciclo aplicar y releer", func(t *testing.T) { cicloDDL(t, f) })
	t.Run("las filas sobreviven al cambio de esquema", func(t *testing.T) { filasSobreviven(t, f) })
}

/* ------------------------------------------------------------ los casos */

func identidad(t *testing.T, f Fixture) {
	c := abrir(t, f)
	if c.Kind() == "" {
		t.Error("Kind() vacío")
	}
	srv := c.Server()
	if srv == nil {
		t.Fatal("Server() nil")
	}
	if srv.Kind != c.Kind() {
		t.Errorf("Server().Kind = %q y Kind() = %q: tienen que coincidir", srv.Kind, c.Kind())
	}
	if srv.Display == "" {
		t.Error("Server().Display vacío: la barra de estado no tendría qué mostrar")
	}
	if !srv.Supported() {
		t.Errorf("la versión %d quedó por debajo del mínimo %d",
			srv.VersionNum, engine.MinVersion(c.Kind()))
	}
	// Caps tiene que ser la del motor, no el conjunto vacío del desconocido.
	if c.Caps().MaxIdentifier <= 0 {
		t.Error("Caps().MaxIdentifier <= 0: no se podría validar ningún nombre")
	}
}

func introspeccion(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_intro")

	snap, err := c.Introspect(context.Background())
	if err != nil {
		t.Fatalf("Introspect(): %v", err)
	}
	tb := buscarTabla(snap, esq, tabla)
	if tb == nil {
		t.Fatalf("Introspect() no encontró %s.%s; esquemas: %v", esq, tabla, nombresDeEsquemas(snap))
	}
	if len(tb.Columns) != 3 {
		t.Errorf("la tabla tiene %d columnas y se crearon 3: %+v", len(tb.Columns), tb.Columns)
	}
	var pk, fk bool
	for _, col := range tb.Columns {
		if col.PrimaryKey {
			pk = true
		}
		if col.ForeignKey {
			fk = true
		}
	}
	if !pk {
		t.Error("ninguna columna quedó marcada como clave primaria")
	}
	if !fk {
		t.Error("ninguna columna quedó marcada como clave foránea; el diagrama no dibujaría la relación")
	}
}

func detalle(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_detalle")

	d, err := c.Detail(context.Background(), esq, tabla)
	if err != nil {
		t.Fatalf("Detail(): %v", err)
	}
	if d.Name != tabla {
		t.Errorf("Detail().Name = %q, se esperaba %q", d.Name, tabla)
	}
	if len(d.Columns) != 3 {
		t.Fatalf("Detail() dio %d columnas, se esperaban 3", len(d.Columns))
	}
	for _, col := range d.Columns {
		if col.DataType == "" {
			t.Errorf("la columna %q no trae tipo: la pantalla de estructura mostraría una celda vacía", col.Name)
		}
	}
	if len(d.ForeignKeys) != 1 {
		t.Errorf("Detail() dio %d claves foráneas, se esperaba 1: %+v", len(d.ForeignKeys), d.ForeignKeys)
	}
	// El índice que se creó a mano tiene que aparecer. Los implícitos de la
	// clave primaria no se cuentan porque cada motor los nombra distinto.
	var hallado bool
	for _, i := range d.Indexes {
		if strings.Contains(i.Name, "kn_idx") {
			hallado = true
		}
	}
	if !hallado {
		t.Errorf("Detail() no trajo el índice kn_idx; trajo: %+v", d.Indexes)
	}
}

func tipos(t *testing.T, f Fixture) {
	c := abrir(t, f)
	ts, err := c.ColumnTypes(context.Background())
	if err != nil {
		t.Fatalf("ColumnTypes(): %v", err)
	}
	if len(ts) == 0 {
		t.Fatal("ColumnTypes() vacío: el selector de tipos no tendría nada que ofrecer")
	}
	// Los tipos que cualquier motor tiene. Sin ellos, la lista está mal armada.
	for _, quiere := range f.TiposEsperados {
		var hay bool
		for _, ty := range ts {
			if strings.EqualFold(ty.Name, quiere) {
				hay = true
			}
		}
		if !hay {
			t.Errorf("ColumnTypes() no ofrece %q, que es el tipo con el que la propia "+
				"suite crea sus tablas", quiere)
		}
	}
}

func consultas(t *testing.T, f Fixture) {
	c := abrir(t, f)
	b, fail := c.Run(context.Background(), "SELECT 1", engine.RunOptions{})
	if fail != nil {
		t.Fatalf("Run(): %s", fail.Message)
	}
	if len(b.Results) == 0 {
		t.Fatalf("Run() no devolvió resultado: %+v", b)
	}
	r := b.Results[0]
	if len(r.Rows) != 1 || len(r.Columns) != 1 {
		t.Errorf("SELECT 1 dio %d filas y %d columnas", len(r.Rows), len(r.Columns))
	}

	// Una consulta rota tiene que dar un Failure interpretado, no un pánico ni
	// un error crudo.
	_, fail = c.Run(context.Background(), "SELECT * FROM tabla_que_no_existe_kn", engine.RunOptions{})
	if fail == nil {
		t.Fatal("Run() con una tabla inexistente no falló")
	}
	if fail.Message == "" {
		t.Error("el fallo no tiene mensaje")
	}
}

func pagina(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_pagina")
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (%d, 'f%d')",
			califica(c, esq, tabla), i, i))
	}

	n, fail := c.Count(ctx, esq, tabla, nil)
	if fail != nil {
		t.Fatalf("Count(): %s", fail.Message)
	}
	if n != 5 {
		t.Errorf("Count() = %d, se esperaba 5", n)
	}

	r, fail := c.Page(ctx, esq, tabla, engine.PageOptions{
		OrderBy: []string{"id"}, Limit: 2, Offset: 0,
	})
	if fail != nil {
		t.Fatalf("Page(): %s", fail.Message)
	}
	if len(r.Rows) != 2 {
		t.Errorf("Page() con Limit 2 dio %d filas", len(r.Rows))
	}

	// El offset tiene que moverse de verdad. Sin ORDER BY estable esto sería
	// una lotería, y por eso la suite ordena por la clave primaria.
	r2, fail := c.Page(ctx, esq, tabla, engine.PageOptions{
		OrderBy: []string{"id"}, Limit: 2, Offset: 2,
	})
	if fail != nil {
		t.Fatalf("Page() con offset: %s", fail.Message)
	}
	if len(r2.Rows) != 2 {
		t.Fatalf("la segunda página dio %d filas", len(r2.Rows))
	}
	// Se comparan los VALORES, no los punteros. Rows es [][]*string, así que
	// `r.Rows[0][0] == r2.Rows[0][0]` compara dos direcciones de dos lecturas
	// distintas: nunca son iguales, y la comprobación no podía fallar ni con un
	// Page que ignorara el Offset por completo.
	if celda(r, 0, 0) == celda(r2, 0, 0) {
		t.Errorf("la segunda página empieza en la misma fila que la primera (%q): "+
			"«cargar más» repetiría filas", celda(r, 0, 0))
	}

	// Y el orden descendente tiene que aplicarse a TODAS las columnas del
	// ORDER BY, no solo a la última. `ORDER BY nombre, id DESC` ordena por
	// nombre ASCENDENTE, que es lo contrario de lo que pidió quien llama: con
	// una clave compuesta el orden deja de ser total y el paginado repite y
	// saltea filas.
	rd, fail := c.Page(ctx, esq, tabla, engine.PageOptions{
		OrderBy: []string{"nombre", "id"}, Descending: true, Limit: 1,
	})
	if fail != nil {
		t.Fatalf("Page() descendente: %s", fail.Message)
	}
	if len(rd.Rows) != 1 {
		t.Fatalf("Page() descendente dio %d filas", len(rd.Rows))
	}
	if v := celda(rd, 0, 1); v != "f5" {
		t.Errorf("la primera fila del orden descendente por (nombre, id) es %q y "+
			"tendría que ser \"f5\": el DESC se está aplicando solo a la última "+
			"columna del ORDER BY", v)
	}

	pks, err := c.PrimaryKeyColumns(ctx, esq, tabla)
	if err != nil {
		t.Fatalf("PrimaryKeyColumns(): %v", err)
	}
	if len(pks) != 1 || pks[0] != "id" {
		t.Errorf("PrimaryKeyColumns() = %v, se esperaba [id]", pks)
	}
}

// filtros comprueba que las condiciones de la grilla den lo mismo en los cuatro
// motores, en las tres puertas por donde se leen datos: Page, Count y Scan.
//
// Lo que importa no es que «anden» sino tres cosas concretas: que un comodín
// tecleado a mano NO sea un comodín, que «no es igual a» incluya las filas sin
// valor —que es lo que quiso decir quien filtró— y que el conteo cuente lo
// filtrado y no la tabla.
func filtros(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_filtro")
	ctx := context.Background()
	nom := califica(c, esq, tabla)

	// Un nombre con un porcentaje y otro con un guion bajo, que son los dos
	// comodines de LIKE, y una fila con NULL.
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (1, 'ana')", nom))
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (2, 'banana')", nom))
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (3, '50%%')", nom))
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (4, '5000')", nom))
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (5, 'a_b')", nom))
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (6, 'axb')", nom))
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (7, NULL)", nom))

	casos := []struct {
		nombre string
		cond   query.Condition
		ids    []string
	}{
		{"igual", query.Condition{Column: "nombre", Operator: query.OpEq, Values: unos("ana")}, []string{"1"}},
		{
			// «no es igual a» tiene que traer también la fila sin valor: con un
			// `<> 'ana'` a secas, la 7 no aparece y quien filtró no entiende
			// por qué le falta una fila.
			"distinto trae los nulos",
			query.Condition{Column: "nombre", Operator: query.OpNe, Values: unos("ana")},
			[]string{"2", "3", "4", "5", "6", "7"},
		},
		{"contiene", query.Condition{Column: "nombre", Operator: query.OpContains, Values: unos("nan")}, []string{"2"}},
		{"empieza con", query.Condition{Column: "nombre", Operator: query.OpStartsWith, Values: unos("a")}, []string{"1", "5", "6"}},
		{"termina con", query.Condition{Column: "nombre", Operator: query.OpEndsWith, Values: unos("na")}, []string{"1", "2"}},
		{
			// Lo importante del caso: «50%» trae la fila que DICE 50%, no la
			// que empieza con 50. Sin escapar, traería las dos.
			"un porcentaje es un porcentaje",
			query.Condition{Column: "nombre", Operator: query.OpContains, Values: unos("50%")},
			[]string{"3"},
		},
		{
			"un guion bajo es un guion bajo",
			query.Condition{Column: "nombre", Operator: query.OpContains, Values: unos("a_b")},
			[]string{"5"},
		},
		{"es nulo", query.Condition{Column: "nombre", Operator: query.OpIsNull}, []string{"7"}},
		{"no es nulo", query.Condition{Column: "nombre", Operator: query.OpIsNotNull}, []string{"1", "2", "3", "4", "5", "6"}},
		{"en la lista", query.Condition{Column: "id", Operator: query.OpIn, Values: unos("1", "3")}, []string{"1", "3"}},
		{"entre", query.Condition{Column: "id", Operator: query.OpBetween, Values: unos("2", "4")}, []string{"2", "3", "4"}},
		{"mayor", query.Condition{Column: "id", Operator: query.OpGt, Values: unos("5")}, []string{"6", "7"}},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			where := []query.Condition{caso.cond}
			r, fail := c.Page(ctx, esq, tabla, engine.PageOptions{
				OrderBy: []string{"id"}, Where: where,
			})
			if fail != nil {
				t.Fatalf("Page() con filtro: %s", fail.Message)
			}
			if got := idsDe(r); !mismosIDs(got, caso.ids) {
				t.Errorf("Page() dio %v y se esperaba %v", got, caso.ids)
			}
			// El conteo cuenta lo FILTRADO: es el número que la grilla muestra
			// al lado de «de N filas», y si contara la tabla entera diría que
			// faltan filas por cargar que no existen.
			n, fail := c.Count(ctx, esq, tabla, where)
			if fail != nil {
				t.Fatalf("Count() con filtro: %s", fail.Message)
			}
			if int(n) != len(caso.ids) {
				t.Errorf("Count() con filtro dio %d y se esperaba %d", n, len(caso.ids))
			}
			// Y el recorrido de la exportación ve exactamente lo mismo: exportar
			// «lo que estoy mirando» tiene que dar lo que se está mirando.
			st, err := c.Scan(ctx, esq, tabla, engine.ScanOptions{OrderBy: []string{"id"}, Where: where})
			if err != nil {
				t.Fatalf("Scan() con filtro: %v", err)
			}
			defer st.Close()
			var delRecorrido []string
			for st.Next() {
				delRecorrido = append(delRecorrido, *st.Row()[0])
			}
			if err := st.Err(); err != nil {
				t.Fatalf("Err(): %v", err)
			}
			if !mismosIDs(delRecorrido, caso.ids) {
				t.Errorf("Scan() dio %v y se esperaba %v", delRecorrido, caso.ids)
			}
		})
	}

	// Varias condiciones se combinan con AND.
	r, fail := c.Page(ctx, esq, tabla, engine.PageOptions{
		OrderBy: []string{"id"},
		Where: []query.Condition{
			{Column: "nombre", Operator: query.OpStartsWith, Values: unos("a")},
			{Column: "id", Operator: query.OpGte, Values: unos("5")},
		},
	})
	if fail != nil {
		t.Fatalf("Page() con dos condiciones: %s", fail.Message)
	}
	if got := idsDe(r); !mismosIDs(got, []string{"5", "6"}) {
		t.Errorf("dos condiciones dieron %v y se esperaba [5 6]", got)
	}

	// Un filtro que no se puede aplicar no llega al motor: se rechaza antes.
	if _, fail := c.Page(ctx, esq, tabla, engine.PageOptions{
		Where: []query.Condition{{Column: "id", Operator: "regex", Values: unos("x")}},
	}); fail == nil {
		t.Error("se aceptó un operador de filtro que no existe")
	}
}

// unos arma la lista de valores de una condición.
func unos(vs ...string) []*string {
	out := make([]*string, len(vs))
	for i := range vs {
		v := vs[i]
		out[i] = &v
	}
	return out
}

// idsDe devuelve la primera columna de cada fila.
func idsDe(r *query.Result) []string {
	out := make([]string, 0, len(r.Rows))
	for i := range r.Rows {
		out = append(out, celda(r, i, 0))
	}
	return out
}

func mismosIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// recorrido comprueba Scan, que es lo que usa la exportación: la tabla entera,
// sin límite, leída a medida que llega.
//
// Lo que se mira no es solo «vinieron las filas» sino las tres cosas que un
// archivo exportado necesita y que un Page no garantiza: que estén TODAS —un
// límite por defecto que se colara dejaría un archivo corto que se ve igual que
// uno entero—, que el texto sea EL MISMO que muestra la grilla —si no, la
// misma fila sale distinta en la pantalla y en el archivo—, y que NULL siga
// siendo distinto de la cadena vacía.
func recorrido(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_recorrido")
	ctx := context.Background()

	// Más filas que cualquier límite por defecto que pudiera colarse.
	const filas = 1200
	for i := 1; i <= filas; i++ {
		exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (%d, 'f%d')",
			califica(c, esq, tabla), i, i))
	}
	// Una fila con NULL y otra con la cadena vacía, que son distintas.
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (%d, NULL)",
		califica(c, esq, tabla), filas+1))
	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (%d, '')",
		califica(c, esq, tabla), filas+2))

	st, err := c.Scan(ctx, esq, tabla, engine.ScanOptions{OrderBy: []string{"id"}})
	if err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	defer st.Close()

	if len(st.Columns()) != 3 {
		t.Fatalf("Scan() dio %d columnas y la tabla tiene 3", len(st.Columns()))
	}
	// Las columnas están ANTES de la primera fila: el escritor necesita el
	// encabezado para empezar, y pedirlo después obligaría a juntar todo.
	if st.Columns()[0].Name != "id" {
		t.Errorf("la primera columna es %q y se esperaba \"id\"", st.Columns()[0].Name)
	}

	var n int
	var nulo, vacia *string
	for st.Next() {
		fila := st.Row()
		if len(fila) != 3 {
			t.Fatalf("la fila %d trae %d valores y la tabla tiene 3 columnas", n, len(fila))
		}
		n++
		switch n {
		case filas + 1:
			nulo = fila[1]
		case filas + 2:
			vacia = fila[1]
		case 1:
			if fila[1] == nil || *fila[1] != "f1" {
				t.Errorf("la primera fila es %s y se esperaba \"f1\"", comoTexto(fila[1]))
			}
		}
	}
	if err := st.Err(); err != nil {
		t.Fatalf("Err() después del recorrido: %v", err)
	}
	if n != filas+2 {
		t.Errorf("Scan() dio %d filas y la tabla tiene %d: falta el resto, o hay un límite por defecto colado",
			n, filas+2)
	}
	if nulo != nil {
		t.Errorf("la fila con NULL vino como %q y NULL tiene que ser nil", *nulo)
	}
	if vacia == nil || *vacia != "" {
		t.Errorf("la fila con la cadena vacía vino como %s y tiene que ser una cadena vacía", comoTexto(vacia))
	}

	// El texto es el mismo que ve la grilla. Si Scan decodificara los valores
	// —por ejemplo pidiéndolos en binario y volviendo a darles formato en Go—,
	// la misma fila saldría distinta en la pantalla y en el archivo.
	pag, fail := c.Page(ctx, esq, tabla, engine.PageOptions{OrderBy: []string{"id"}, Limit: 1})
	if fail != nil {
		t.Fatalf("Page(): %s", fail.Message)
	}
	st2, err := c.Scan(ctx, esq, tabla, engine.ScanOptions{OrderBy: []string{"id"}})
	if err != nil {
		t.Fatalf("Scan() de nuevo: %v", err)
	}
	defer st2.Close()
	if !st2.Next() {
		t.Fatalf("el segundo Scan() no dio ninguna fila: %v", st2.Err())
	}
	for i, col := range st2.Columns() {
		if col.Name != pag.Columns[i].Name || col.Class != pag.Columns[i].Class {
			t.Errorf("la columna %d es %+v en Scan y %+v en Page", i, col, pag.Columns[i])
		}
		// Se comparan los *string y no celda(), que colapsa NULL a la cadena
		// vacía: acá justamente hay que ver que las dos lecturas coincidan
		// también en cuál de las dos es.
		a, b := st2.Row()[i], pag.Rows[0][i]
		if (a == nil) != (b == nil) || (a != nil && *a != *b) {
			t.Errorf("la columna %q vale %s en Scan y %s en Page: el texto tiene que ser el mismo",
				col.Name, comoTexto(a), comoTexto(b))
		}
	}

	// Cerrar sin haber terminado de leer no puede romper nada: es lo que pasa
	// cuando se cancela una exportación.
	st3, err := c.Scan(ctx, esq, tabla, engine.ScanOptions{})
	if err != nil {
		t.Fatalf("Scan() para cortar: %v", err)
	}
	st3.Next()
	st3.Close()
	st3.Close() // idempotente

	// Y la conexión sigue sirviendo después de cortar a la mitad: si el
	// recorrido dejara la conexión con filas pendientes, la lectura siguiente
	// leería las de la anterior.
	if _, fail := c.Count(ctx, esq, tabla, nil); fail != nil {
		t.Errorf("después de cortar un recorrido, Count() falla: %s", fail.Message)
	}
}

// comoTexto muestra un valor de celda distinguiendo NULL de la cadena vacía.
func comoTexto(v *string) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprintf("%q", *v)
}

// transacciones comprueba lo que la grilla editable de la Iteración 7 necesita:
// que un conjunto de cambios de DATOS sea todo o nada. Lo es en los cuatro
// motores, incluidos los que no tienen DDL transaccional.
func transacciones(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_tx")
	ctx := context.Background()
	nom := califica(c, esq, tabla)

	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (1, 'antes')", nom))

	tx, err := c.Begin(ctx, engine.TxOptions{})
	if err != nil {
		t.Fatalf("Begin(): %v", err)
	}
	if err := tx.Exec(ctx, fmt.Sprintf("UPDATE %s SET nombre='despues' WHERE id=1", nom)); err != nil {
		t.Fatalf("Exec en transacción: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback(): %v", err)
	}
	if v := unaCelda(t, c, fmt.Sprintf("SELECT nombre FROM %s WHERE id=1", nom)); v != "antes" {
		t.Errorf("después del rollback la fila dice %q: los cambios de datos "+
			"tienen que revertirse en TODOS los motores", v)
	}

	tx, err = c.Begin(ctx, engine.TxOptions{})
	if err != nil {
		t.Fatalf("Begin(): %v", err)
	}
	if err := tx.Exec(ctx, fmt.Sprintf("UPDATE %s SET nombre='commiteado' WHERE id=1", nom)); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit(): %v", err)
	}
	// Rollback después de un Commit exitoso tiene que ser un no-op, para que
	// quien la abrió pueda hacer `defer tx.Rollback()` sin pensar.
	_ = tx.Rollback(ctx)
	if v := unaCelda(t, c, fmt.Sprintf("SELECT nombre FROM %s WHERE id=1", nom)); v != "commiteado" {
		t.Errorf("después del commit la fila dice %q", v)
	}
}

// errores comprueba que cada motor traduzca sus códigos al mismo vocabulario.
// Sin esto, la pantalla de apply diría «falló» y nada más contra tres de los
// cuatro motores.
func errores(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_err")
	nom := califica(c, esq, tabla)
	ctx := context.Background()

	exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (1, 'a')", nom))

	casos := []struct {
		nombre string
		sql    string
		kind   engine.FailureKind
	}{
		{"clave primaria repetida",
			fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (1, 'b')", nom),
			engine.FailureData},
		{"tabla que no existe",
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN x %s", califica(c, esq, "kn_no_existe"), f.TipoEntero),
			engine.FailureMissing},
		{"columna que ya existe",
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN nombre %s", nom, f.TipoTexto),
			engine.FailureConflict},
	}

	for _, cs := range casos {
		t.Run(cs.nombre, func(t *testing.T) {
			err := c.Exec(ctx, cs.sql)
			if err == nil {
				t.Fatalf("la sentencia no falló, así que el caso no prueba nada: %s", cs.sql)
			}
			fail := c.ClassifyStatement(err, "usuario@host/base")
			if fail == nil {
				t.Fatal("ClassifyStatement() devolvió nil")
			}
			if fail.Kind != cs.kind {
				t.Errorf("Kind = %q, se esperaba %q (mensaje: %s)", fail.Kind, cs.kind, fail.Message)
			}
			if fail.Message == "" {
				t.Error("sin mensaje")
			}
			if fail.Detail == "" {
				t.Error("sin Detail: se pierde lo que dijo el motor, que es lo que otro reconoce")
			}
			// El vocabulario de conexión no puede aparecer: el servidor
			// contestó perfectamente.
			for _, prohibido := range []string{"conectar", "conexión", "usuario@host"} {
				if strings.Contains(fail.Message, prohibido) {
					t.Errorf("el mensaje habla de la conexión y esto es una sentencia: %s", fail.Message)
				}
			}
		})
	}
}

// cicloDDL es el test que CLAUDE.md pide: renderizar, EJECUTAR, y volver a leer
// el catálogo para comprobar que quedó lo que se pidió.
//
// No compara la SQL contra una cadena esperada a propósito. Una sentencia puede
// verse perfecta, correr sin error y dejar otra cosa — es literalmente lo que
// pasa con las columnas VIRTUAL en las herramientas que solo miran el texto.
func cicloDDL(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_ciclo")
	ctx := context.Background()

	pasos := []struct {
		nombre   string
		cambio   change.Change
		comprobá func(t *testing.T, d *schema.TableDetail)
	}{
		{
			nombre: "agregar una columna",
			cambio: change.Change{
				Type: change.AddColumn, Schema: esq, Table: tabla,
				Column: &change.Column{Name: "extra", DataType: f.TipoEntero, Nullable: true},
			},
			comprobá: func(t *testing.T, d *schema.TableDetail) {
				if !tieneColumna(d, "extra") {
					t.Error("la columna «extra» no quedó")
				}
			},
		},
		{
			nombre: "exigir que no sea nula",
			cambio: change.Change{
				Type: change.SetNotNull, Schema: esq, Table: tabla,
				Column: &change.Column{Name: "extra", DataType: f.TipoEntero},
			},
			comprobá: func(t *testing.T, d *schema.TableDetail) {
				for _, col := range d.Columns {
					if col.Name == "extra" && col.Nullable {
						t.Error("«extra» sigue admitiendo nulos")
					}
				}
			},
		},
		{
			nombre: "ponerle un valor por defecto",
			cambio: change.Change{
				Type: change.SetDefault, Schema: esq, Table: tabla,
				Column: &change.Column{Name: "extra", DataType: f.TipoEntero, Default: "7"},
			},
			comprobá: func(t *testing.T, d *schema.TableDetail) {
				for _, col := range d.Columns {
					if col.Name == "extra" && !strings.Contains(col.Default, "7") {
						t.Errorf("el valor por defecto de «extra» quedó en %q", col.Default)
					}
				}
			},
		},
		{
			nombre: "sacarle el valor por defecto",
			cambio: change.Change{
				Type: change.DropDefault, Schema: esq, Table: tabla,
				Column: &change.Column{Name: "extra", DataType: f.TipoEntero},
			},
			comprobá: func(t *testing.T, d *schema.TableDetail) {
				for _, col := range d.Columns {
					if col.Name == "extra" && col.Default != "" {
						t.Errorf("«extra» quedó con el valor por defecto %q", col.Default)
					}
				}
			},
		},
		{
			nombre: "renombrar la columna",
			cambio: change.Change{
				Type: change.RenameColumn, Schema: esq, Table: tabla,
				Column: &change.Column{Name: "extra", DataType: f.TipoEntero}, NewName: "renombrada",
			},
			comprobá: func(t *testing.T, d *schema.TableDetail) {
				if tieneColumna(d, "extra") {
					t.Error("quedó la columna vieja")
				}
				if !tieneColumna(d, "renombrada") {
					t.Error("no apareció la columna nueva")
				}
			},
		},
		{
			nombre: "borrar la columna",
			cambio: change.Change{
				Type: change.DropColumn, Schema: esq, Table: tabla,
				Column: &change.Column{Name: "renombrada", DataType: f.TipoEntero},
			},
			comprobá: func(t *testing.T, d *schema.TableDetail) {
				if tieneColumna(d, "renombrada") {
					t.Error("la columna no se borró")
				}
			},
		},
	}

	for _, p := range pasos {
		t.Run(p.nombre, func(t *testing.T) {
			st, err := c.RenderDDL(ctx, p.cambio)
			if err != nil {
				t.Fatalf("RenderDDL(): %v", err)
			}
			if strings.TrimSpace(st.SQL) == "" {
				t.Fatal("RenderDDL() devolvió SQL vacía")
			}
			if err := aplicar(ctx, c, st); err != nil {
				t.Fatalf("ejecutar %q: %v", st.SQL, err)
			}
			d, err := c.Detail(ctx, esq, tabla)
			if err != nil {
				t.Fatalf("Detail() después de aplicar: %v", err)
			}
			p.comprobá(t, d)
		})
	}
}

// filasSobreviven comprueba la invariante más básica que hay, y la que más
// caro sale romper: cambiar el ESQUEMA de una tabla no borra los DATOS de otra.
//
// Suena a que no hace falta probarlo. Hace falta, y este caso existe porque en
// SQLite pasa de verdad: como casi no hay ALTER TABLE, cambiar una columna se
// hace creando una tabla nueva, copiando y tirando la vieja — y ese DROP, con
// las claves foráneas encendidas, dispara los ON DELETE CASCADE de las tablas
// que la apuntan. Las filas hijas desaparecen sin un solo error, y hasta el
// PRAGMA foreign_key_check da limpio después, porque no quedaron huérfanas:
// quedaron borradas.
//
// Por eso el cambio va sobre la tabla PADRE y se cuentan las filas de la HIJA.
// El ciclo de cicloDDL toca la hija, que no es referenciada por nadie, así que
// no habría notado nada.
func filasSobreviven(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	hija := crearTabla(t, c, f, esq, "kn_sobrevive")
	padre := hija + "_padre"
	ctx := context.Background()

	exec(t, c, fmt.Sprintf("INSERT INTO %s (id) VALUES (1), (2)", califica(c, esq, padre)))
	exec(t, c, fmt.Sprintf(
		"INSERT INTO %s (id, nombre, padre_id) VALUES (10, 'a', 1), (11, 'b', 2)",
		califica(c, esq, hija)))

	// Los cambios que en SQLite obligan a reconstruir la tabla. Se aplican
	// sobre el padre, uno detrás del otro.
	pasos := []change.Change{
		{
			Type: change.AddColumn, Schema: esq, Table: padre,
			Column: &change.Column{Name: "etiqueta", DataType: f.TipoTexto, Nullable: true},
		},
		{
			Type: change.SetNotNull, Schema: esq, Table: padre,
			Column: &change.Column{Name: "etiqueta", DataType: f.TipoTexto, Default: "'x'"},
		},
	}
	for _, ch := range pasos {
		// SET NOT NULL sobre filas que ya tienen NULL falla en cualquier motor,
		// así que primero se llenan.
		if ch.Type == change.SetNotNull {
			exec(t, c, fmt.Sprintf("UPDATE %s SET %s = 'x'",
				califica(c, esq, padre), citar(c, "etiqueta")))
		}
		st, err := c.RenderDDL(ctx, ch)
		if err != nil {
			t.Fatalf("RenderDDL(%s): %v", ch.Type, err)
		}
		if err := aplicar(ctx, c, st); err != nil {
			t.Fatalf("aplicar %s:\n%s\n%v", ch.Type, st.SQL, err)
		}

		n, fail := c.Count(ctx, esq, hija, nil)
		if fail != nil {
			t.Fatalf("Count() sobre la hija: %s", fail.Message)
		}
		if n != 2 {
			t.Fatalf("después de %s sobre %q, la tabla que la referencia quedó con %d "+
				"filas de 2. Un cambio de esquema borró datos de otra tabla, y sin "+
				"un solo error.\nSQL:\n%s", ch.Type, padre, n, st.SQL)
		}
		if n, fail := c.Count(ctx, esq, padre, nil); fail != nil || n != 2 {
			t.Fatalf("la tabla que se modificó quedó con %d filas de 2", n)
		}
	}
}

// aplicar ejecuta una sentencia por el camino que ella misma pide.
//
// Una que reconstruye la tabla NO se puede correr suelta: necesita que las
// claves foráneas estén apagadas mientras corre, y eso hay que pedirlo antes
// de abrir la transacción. Ejecutarla con Exec daría verde igual en el caso
// fácil —cuando nadie referencia la tabla— y borraría filas en el caso real.
func aplicar(ctx context.Context, c engine.Conn, st change.Statement) error {
	if !st.RebuildsTable {
		return c.Exec(ctx, st.SQL)
	}
	tx, err := c.Begin(ctx, engine.TxOptions{RebuildsTables: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.Exec(ctx, st.SQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// cambiosDeDatos es lo que la grilla editable necesita de cada motor: que
// insertar, actualizar y borrar una fila se rendericen, que la forma con
// parámetros y la forma con literales dejen LA MISMA fila, y que el conteo de
// filas alcanzadas sirva para exigir «exactamente una».
func cambiosDeDatos(t *testing.T, f Fixture) {
	c := abrir(t, f)
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_datos")
	ctx := context.Background()
	nom := califica(c, esq, tabla)
	v := func(s string) *string { return &s }

	// El valor lleva lo que suele romper un citado: comilla simple, barra
	// invertida y un salto de línea. Es el mismo texto por los dos caminos.
	raro := "O'Brien " + `C:\datos` + "\nsegunda línea"

	render := func(ch change.Change) change.Statement {
		t.Helper()
		st, err := c.RenderDDL(ctx, ch)
		if err != nil {
			t.Fatalf("RenderDDL(%s): %v", ch.Type, err)
		}
		if st.Bound == nil {
			t.Fatalf("RenderDDL(%s) no trajo Bound: un cambio de datos se ejecuta con parámetros", ch.Type)
		}
		return st
	}
	leer := func(id string) string {
		t.Helper()
		return unaCelda(t, c, fmt.Sprintf("SELECT nombre FROM %s WHERE id = %s", nom, id))
	}

	// Insertar: la forma con parámetros para la fila 1 y la forma legible
	// —literales— para la fila 2. Tienen que quedar iguales.
	insertar := func(id string) change.Statement {
		return render(change.Change{Type: change.InsertRow, Schema: esq, Table: tabla,
			Values: []change.Cell{{Column: "id", Value: v(id)}, {Column: "nombre", Value: v(raro)},
				{Column: "padre_id", Value: nil}}})
	}
	if n, err := c.Modify(ctx, insertar("1").Bound.SQL, insertar("1").Bound.Args); err != nil || n != 1 {
		t.Fatalf("insertar con parámetros: n=%d err=%v", n, err)
	}
	exec(t, c, insertar("2").SQL)
	if a, b := leer("1"), leer("2"); a != raro || b != raro {
		t.Errorf("las dos formas tienen que dejar el mismo valor:\n  parámetros: %q\n  literales:  %q\n  original:   %q", a, b, raro)
	}

	// Actualizar por clave, dentro de una transacción, que es como lo hace el
	// apply. El conteo tiene que ser 1 aunque el valor sea el mismo que ya
	// estaba: cuenta las filas alcanzadas, no las cambiadas.
	upd := render(change.Change{Type: change.UpdateRow, Schema: esq, Table: tabla,
		Values: []change.Cell{{Column: "nombre", Value: v(raro)}},
		Key:    []change.Cell{{Column: "id", Value: v("1")}}})
	tx, err := c.Begin(ctx, engine.TxOptions{})
	if err != nil {
		t.Fatalf("Begin(): %v", err)
	}
	n, err := tx.Modify(ctx, upd.Bound.SQL, upd.Bound.Args)
	if err != nil {
		t.Fatalf("UPDATE con parámetros: %v", err)
	}
	if n != 1 {
		t.Errorf("un UPDATE que deja el mismo valor alcanzó %d filas; tiene que contar 1, "+
			"si no la grilla cree que la fila ya no está", n)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit(): %v", err)
	}

	// Una clave que no existe alcanza cero filas, sin error: es lo que el
	// apply convierte en «la fila ya no está».
	fantasma := render(change.Change{Type: change.UpdateRow, Schema: esq, Table: tabla,
		Values: []change.Cell{{Column: "nombre", Value: v("x")}},
		Key:    []change.Cell{{Column: "id", Value: v("999")}}})
	if n, err := c.Modify(ctx, fantasma.Bound.SQL, fantasma.Bound.Args); err != nil || n != 0 {
		t.Errorf("UPDATE de una fila inexistente: n=%d err=%v; se esperaba 0 sin error", n, err)
	}

	// Borrar: parámetros para una, literales para la otra.
	borrar := func(id string) change.Statement {
		return render(change.Change{Type: change.DeleteRow, Schema: esq, Table: tabla,
			Key: []change.Cell{{Column: "id", Value: v(id)}}})
	}
	if n, err := c.Modify(ctx, borrar("1").Bound.SQL, borrar("1").Bound.Args); err != nil || n != 1 {
		t.Fatalf("borrar con parámetros: n=%d err=%v", n, err)
	}
	exec(t, c, borrar("2").SQL)
	if q := unaCelda(t, c, "SELECT COUNT(*) FROM "+nom); q != "0" {
		t.Errorf("después de borrar las dos quedan %s filas", q)
	}

	// Una fila sin valores toma sus defaults. Acá todas las columnas admiten
	// NULL salvo la clave, que en los cuatro motores necesita valor, así que
	// se le da uno y el resto va por defecto.
	vacia := render(change.Change{Type: change.InsertRow, Schema: esq, Table: tabla,
		Values: []change.Cell{{Column: "id", Value: v("3")}}})
	if n, err := c.Modify(ctx, vacia.Bound.SQL, vacia.Bound.Args); err != nil || n != 1 {
		t.Fatalf("insertar solo la clave: n=%d err=%v", n, err)
	}
	if got := unaCelda(t, c, fmt.Sprintf("SELECT nombre IS NULL FROM %s WHERE id = 3", nom)); got != "true" && got != "1" && got != "t" {
		t.Errorf("la columna sin valor tendría que quedar NULL, y dice %q", got)
	}
}

/* ------------------------------------------------------------ ayudantes */

func abrir(t *testing.T, f Fixture) engine.Conn {
	t.Helper()
	c := f.Abrir(t)
	t.Cleanup(c.Close)
	return c
}

// crearTabla arma la tabla de prueba y la de su clave foránea, y las borra al
// terminar. Devuelve el nombre de la tabla hija.
func crearTabla(t *testing.T, c engine.Conn, f Fixture, esq, base string) string {
	t.Helper()
	ctx := context.Background()
	padre := base + "_padre"
	nomPadre := califica(c, esq, padre)
	nomHija := califica(c, esq, base)

	limpiar := func() {
		_ = c.Exec(ctx, "DROP TABLE IF EXISTS "+nomHija)
		_ = c.Exec(ctx, "DROP TABLE IF EXISTS "+nomPadre)
	}
	limpiar()
	t.Cleanup(limpiar)

	exec(t, c, fmt.Sprintf("CREATE TABLE %s (id %s PRIMARY KEY)", nomPadre, f.TipoEntero))
	// La clave foránea va como restricción DE TABLA y no pegada a la columna.
	// No es estilo: MySQL ACEPTA la forma inline y después la IGNORA en
	// InnoDB —no da error, simplemente no crea la clave— así que una suite
	// escrita con esa forma daría verde creyendo que probó la relación.
	// ON DELETE CASCADE no es decoración: es lo que vuelve SILENCIOSO el fallo
	// que caza «las filas sobreviven al cambio de esquema». Sin él, un DROP
	// TABLE sobre el padre da un error de clave foránea y el caso pasa por
	// haber fallado ruidosamente, no por estar protegido — que es lo contrario
	// de lo que el caso dice comprobar.
	exec(t, c, fmt.Sprintf(
		"CREATE TABLE %s (id %s PRIMARY KEY, nombre %s, padre_id %s, "+
			"FOREIGN KEY (padre_id) REFERENCES %s (id) ON DELETE CASCADE)",
		nomHija, f.TipoEntero, f.TipoTexto, f.TipoEntero, nomPadre))
	exec(t, c, fmt.Sprintf("CREATE INDEX kn_idx_%s ON %s (nombre)", base, nomHija))
	return base
}

func exec(t *testing.T, c engine.Conn, sql string) {
	t.Helper()
	if err := c.Exec(context.Background(), sql); err != nil {
		t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
	}
}

func unaCelda(t *testing.T, c engine.Conn, sql string) string {
	t.Helper()
	b, fail := c.Run(context.Background(), sql, engine.RunOptions{})
	if fail != nil {
		t.Fatalf("Run(%q): %s", sql, fail.Message)
	}
	if len(b.Results) == 0 || len(b.Results[0].Rows) == 0 {
		t.Fatalf("Run(%q) no devolvió filas", sql)
	}
	v := b.Results[0].Rows[0][0]
	if v == nil {
		return ""
	}
	return *v
}

// citar cita un identificador con las comillas del motor. Es lo mínimo que la
// suite necesita para escribir un UPDATE a mano.
func citar(c engine.Conn, ident string) string {
	if c.Kind() == engine.MySQL || c.Kind() == engine.MariaDB {
		return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// califica arma el nombre de la tabla como lo espera este motor. En SQLite no
// hay esquema, así que el prefijo desaparece.
func califica(c engine.Conn, esq, tabla string) string {
	if esq == "" {
		return tabla
	}
	return esq + "." + tabla
}

// celda saca el valor de una celda del resultado. NULL y fuera de rango dan "".
func celda(r *query.Result, fila, col int) string {
	if fila >= len(r.Rows) || col >= len(r.Rows[fila]) || r.Rows[fila][col] == nil {
		return ""
	}
	return *r.Rows[fila][col]
}

func buscarTabla(s *schema.Snapshot, esq, tabla string) *schema.Table {
	for i := range s.Schemas {
		if esq != "" && s.Schemas[i].Name != esq {
			continue
		}
		for j := range s.Schemas[i].Tables {
			if s.Schemas[i].Tables[j].Name == tabla {
				return &s.Schemas[i].Tables[j]
			}
		}
	}
	return nil
}

func nombresDeEsquemas(s *schema.Snapshot) []string {
	var out []string
	for _, e := range s.Schemas {
		out = append(out, e.Name)
	}
	return out
}

func tieneColumna(d *schema.TableDetail, nombre string) bool {
	for _, c := range d.Columns {
		if c.Name == nombre {
			return true
		}
	}
	return false
}

// cobertura comprueba que el motor sepa NOMBRAR lo que el volcado de estructura
// deja afuera.
//
// Es el caso que hace honesto al volcado de estructura, y lo que se prueba no
// es que la lista sea larga sino que **no sea silenciosa**: se crea una vista
// de verdad y se exige que aparezca. Un `Objects` que devuelva siempre vacío
// —o que se coma un error del catálogo— produce un archivo que se ve idéntico a
// uno completo, y quien lo restaura se entera meses después.
//
// Las TABLAS no pueden aparecer: el volcado sí las escribe, y listarlas como
// «queda afuera» sería el error contrario, que asusta sin motivo.
func cobertura(t *testing.T, f Fixture) {
	c := abrir(t, f)
	ctx := context.Background()
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_cob")

	vista := "kn_cob_vista"
	nombreVista := califica(c, esq, vista)
	_ = c.Exec(ctx, "DROP VIEW IF EXISTS "+nombreVista)
	exec(t, c, fmt.Sprintf("CREATE VIEW %s AS SELECT id FROM %s", nombreVista, califica(c, esq, tabla)))
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP VIEW IF EXISTS "+nombreVista) })

	fuera, err := c.Objects(ctx, esquemasDe(esq))
	if err != nil {
		t.Fatalf("Objects(): %v", err)
	}

	var laVista *schema.Object
	for i := range fuera {
		if fuera[i].Name == vista {
			laVista = &fuera[i]
		}
		if fuera[i].Name == tabla || fuera[i].Name == tabla+"_padre" {
			t.Errorf("la tabla %q figura como que queda afuera del volcado, y el volcado la escribe", fuera[i].Name)
		}
	}
	if laVista == nil {
		nombres := make([]string, 0, len(fuera))
		for _, o := range fuera {
			nombres = append(nombres, string(o.Kind)+":"+o.Name)
		}
		t.Fatalf("la vista %q no aparece en lo que queda afuera; salieron: %v", vista, nombres)
	}
	if laVista.Kind != schema.ObjView {
		t.Errorf("la vista salió como %q", laVista.Kind)
	}
	if laVista.Name != vista {
		t.Errorf("la vista salió con nombre %q", laVista.Name)
	}
}

// esquemasDe arma la lista que espera Objects. El esquema vacío de SQLite no
// se manda como una cadena vacía: se manda la lista vacía, que es lo que ese
// motor entiende.
func esquemasDe(esq string) []string {
	if esq == "" {
		return nil
	}
	return []string{esq}
}

// definicion comprueba que la definición que devuelve el motor SE PUEDA VOLVER
// A CORRER.
//
// Es la única forma de probar esto que sirve. Que el texto «contenga CREATE» no
// prueba nada: los cuatro motores devuelven el objeto en formas distintas
// —`pg_get_viewdef` da solo el SELECT, `SHOW CREATE VIEW` trae el DEFINER y el
// ALGORITHM, `sqlite_master.sql` da el texto original— y el editor promete que
// lo que se ve es lo que se ejecuta. Así que se borra la vista y se la recrea
// con lo que salió: si la definición está a medias, la vista no vuelve.
func definicion(t *testing.T, f Fixture) {
	c := abrir(t, f)
	ctx := context.Background()
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_def")

	// Tres filas y una vista que deja pasar UNA. Los números importan: con la
	// tabla vacía, `count(*)` daba 0 para cualquier vista sobre ella, así que
	// una definición que perdiera el WHERE —o que seleccionara otra cosa—
	// pasaba igual y el mensaje de error prometía algo que el test no miraba.
	nomTabla := califica(c, esq, tabla)
	for _, id := range []int{1, 2, 3} {
		exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (%d, 'x')", nomTabla, id))
	}

	vista := "kn_def_vista"
	nombreVista := califica(c, esq, vista)
	_ = c.Exec(ctx, "DROP VIEW IF EXISTS "+nombreVista)
	exec(t, c, fmt.Sprintf("CREATE VIEW %s AS SELECT id FROM %s WHERE id = 2", nombreVista, nomTabla))
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP VIEW IF EXISTS "+nombreVista) })

	var obj *schema.Object
	objetos, err := c.Objects(ctx, esquemasDe(esq))
	if err != nil {
		t.Fatalf("Objects(): %v", err)
	}
	for i := range objetos {
		if objetos[i].Name == vista && objetos[i].Kind == schema.ObjView {
			obj = &objetos[i]
		}
	}
	if obj == nil {
		t.Fatalf("la vista %q no apareció en Objects()", vista)
	}

	def, err := c.ObjectDefinition(ctx, *obj)
	if err != nil {
		t.Fatalf("ObjectDefinition(): %v", err)
	}
	if strings.TrimSpace(def.SQL) == "" {
		t.Fatal("la definición vino vacía: en el editor eso se ve igual que un objeto sin cuerpo, y guardarla lo borraría")
	}
	if def.Object.Name != vista {
		t.Errorf("la definición dice ser de %q", def.Object.Name)
	}

	// La prueba de verdad: borrar y recrear con lo que devolvió.
	exec(t, c, "DROP VIEW "+nombreVista)
	if err := c.Exec(ctx, def.SQL); err != nil {
		t.Fatalf("la definición devuelta no se puede volver a correr: %v\n%s", err, def.SQL)
	}

	// Y la vista tiene que EXISTIR después. Esta comprobación no es un extra
	// del anterior: es la única que caza el fallo más probable de todos.
	//
	// `pg_get_viewdef` devuelve solo el SELECT, así que una definición sin su
	// `CREATE VIEW … AS` adelante es un SELECT perfectamente válido: el Exec de
	// arriba lo corre sin error, no crea nada, y sin este count(*) el test
	// pasaría con la vista borrada. Ejecutar sin error y hacer lo que se pidió
	// no son lo mismo.
	if got := unaCelda(t, c, "SELECT count(*) FROM "+nombreVista); got != "1" {
		t.Errorf("la vista recreada no devuelve lo mismo: count(*) = %q, se esperaba 1 de 3 filas", got)
	}

	// Un objeto que el motor no tiene se dice, no se devuelve vacío.
	_, err = c.ObjectDefinition(ctx, schema.Object{
		Kind: schema.ObjectKind("nada-de-esto-existe"), Schema: esq, Name: "x",
	})
	if err == nil {
		t.Error("un tipo de objeto desconocido devolvió una definición en vez de un error")
	}
}

// dependientes comprueba la única promesa que este método puede hacer en los
// cuatro motores: que una lista vacía signifique «no depende nada de esto».
//
// Lo que cada motor SABE es distinto —Postgres tiene `pg_depend`, MySQL tiene
// `VIEW_TABLE_USAGE` desde 8.0.13, MariaDB no tiene nada y SQLite tampoco— así
// que exigir la lista sería exigir lo que tres de ellos no pueden dar. Lo que
// sí se exige es que el que no puede lo DIGA: una lista vacía es lo que alguien
// mira para apretar tranquilo un botón que borra, y devolverla sin saber es la
// peor respuesta posible —peor que un error, porque un error se ve—.
func dependientes(t *testing.T, f Fixture) {
	c := abrir(t, f)
	ctx := context.Background()
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_dep")

	vista := "kn_dep_vista"
	nombreVista := califica(c, esq, vista)
	_ = c.Exec(ctx, "DROP VIEW IF EXISTS "+nombreVista)
	exec(t, c, fmt.Sprintf("CREATE VIEW %s AS SELECT id FROM %s", nombreVista, califica(c, esq, tabla)))
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP VIEW IF EXISTS "+nombreVista) })

	esqObjeto := esq
	if esqObjeto == "" {
		esqObjeto = "main"
	}
	dep, err := c.Dependents(ctx, schema.Object{Kind: schema.ObjView, Schema: esqObjeto, Name: vista})
	if err != nil {
		t.Fatalf("Dependents(): %v", err)
	}

	// La invariante: o hay lista, o se dice que no se puede saber. Nunca las
	// dos vacías sin motivo.
	if dep.Unknown {
		if dep.Reason == "" {
			t.Error("se dijo que no se puede saber y no se dijo por qué: en la pantalla queda un aviso sin explicación")
		}
		if dep.Vacio() {
			t.Error("Vacio() devolvió true con Unknown puesto: eso es exactamente el «nada depende de esto» que este caso existe para impedir")
		}
		return
	}

	// Si el motor dice saber, nadie cuelga de una vista recién creada.
	if !dep.Vacio() {
		t.Errorf("una vista recién creada tiene dependientes: %+v", dep.Objects)
	}
}

// reemplazarObjeto comprueba el ciclo completo del editor de objetos: leer la
// definición, cambiarla y aplicarla, en los cuatro motores.
//
// Lo que se mira NO es que la sentencia corra sino que la vista **devuelva otra
// cosa** después. Un reemplazo que corre sin error y deja la definición vieja
// se ve exactamente igual que uno que funcionó, y es un fallo perfectamente
// posible: `CREATE OR REPLACE VIEW` sobre un nombre distinto crea una vista
// nueva y deja la vieja intacta.
func reemplazarObjeto(t *testing.T, f Fixture) {
	c := abrir(t, f)
	ctx := context.Background()
	esq := f.Esquema(c)
	tabla := crearTabla(t, c, f, esq, "kn_repl")
	nomTabla := califica(c, esq, tabla)

	for _, id := range []int{1, 2, 3} {
		exec(t, c, fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (%d, 'x')", nomTabla, id))
	}

	vista := "kn_repl_vista"
	nombreVista := califica(c, esq, vista)
	_ = c.Exec(ctx, "DROP VIEW IF EXISTS "+nombreVista)
	exec(t, c, fmt.Sprintf("CREATE VIEW %s AS SELECT id FROM %s WHERE id = 1", nombreVista, nomTabla))
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP VIEW IF EXISTS "+nombreVista) })

	if got := unaCelda(t, c, "SELECT count(*) FROM "+nombreVista); got != "1" {
		t.Fatalf("la vista de partida devuelve %q y no 1: el caso no probaría nada", got)
	}

	esqObjeto := esq
	if esqObjeto == "" {
		esqObjeto = "main"
	}
	// La definición nueva deja pasar DOS filas en vez de una. El número es lo
	// que distingue «se aplicó» de «corrió sin hacer nada».
	nueva := fmt.Sprintf("CREATE VIEW %s AS SELECT id FROM %s WHERE id <= 2", nombreVista, nomTabla)
	cambio := change.Change{
		Type:       change.ReplaceObject,
		ObjectKind: schema.ObjView,
		Schema:     esqObjeto,
		Name:       vista,
		Definition: nueva,
	}
	// Donde el motor sabe reemplazar en el lugar se usa esa forma —es la que la
	// pantalla ofrece por defecto— y donde no, se recrea.
	if !change.PuedeReemplazarEnElLugar(string(c.Kind()), schema.ObjView) {
		cambio.Recreate = true
	} else {
		cambio.Definition = strings.Replace(nueva, "CREATE VIEW", "CREATE OR REPLACE VIEW", 1)
	}

	st, err := c.RenderDDL(ctx, cambio)
	if err != nil {
		t.Fatalf("RenderDDL(): %v", err)
	}
	if cambio.Recreate && !strings.Contains(strings.ToUpper(st.SQL), "DROP ") {
		t.Errorf("se pidió recrear y no hay DROP:\n%s", st.SQL)
	}
	if !cambio.Recreate && strings.Contains(strings.ToUpper(st.SQL), "DROP ") {
		t.Errorf("hay un DROP sin que nadie lo pidiera:\n%s", st.SQL)
	}
	if err := aplicar(ctx, c, st); err != nil {
		t.Fatalf("aplicar %q: %v", st.SQL, err)
	}

	if got := unaCelda(t, c, "SELECT count(*) FROM "+nombreVista); got != "2" {
		t.Errorf("la vista sigue devolviendo %q: el reemplazo corrió sin cambiar nada", got)
	}

	// Y la definición que se lee de vuelta es la nueva: sin esto, un motor que
	// guardara el texto viejo dejaría el editor mostrando lo que ya no es.
	def, err := c.ObjectDefinition(ctx, schema.Object{
		Kind: schema.ObjView, Schema: esqObjeto, Name: vista,
	})
	if err != nil {
		t.Fatalf("ObjectDefinition(): %v", err)
	}
	if !strings.Contains(def.SQL, "2") {
		t.Errorf("la definición leída no es la nueva:\n%s", def.SQL)
	}
}
