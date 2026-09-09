package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/sqlite"
)

// preparar abre una base nueva y corre las sentencias que se le den.
func preparar(t *testing.T, sqls ...string) *sqlite.Conn {
	t.Helper()
	c := abrir(t).(*sqlite.Conn)
	for _, s := range sqls {
		if err := c.Exec(context.Background(), s); err != nil {
			t.Fatalf("preparar %q: %v", s, err)
		}
	}
	return c
}

// aplicarRebuild renderiza el cambio y lo ejecuta por el camino protegido.
func aplicarRebuild(t *testing.T, c *sqlite.Conn, ch change.Change) change.Statement {
	t.Helper()
	ctx := context.Background()
	st, err := c.RenderDDL(ctx, ch)
	if err != nil {
		t.Fatalf("RenderDDL(%s): %v", ch.Type, err)
	}
	if !st.RebuildsTable {
		t.Fatalf("%s tendría que estar marcado como reconstrucción, y no lo está:\n%s",
			ch.Type, st.SQL)
	}
	tx, err := c.Begin(ctx, engine.TxOptions{RebuildsTables: true})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := tx.Exec(ctx, st.SQL); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ejecutar la reconstrucción:\n%s\n%v", st.SQL, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit:\n%s\n%v", st.SQL, err)
	}
	return st
}

func unaCelda(t *testing.T, c *sqlite.Conn, sql string) string {
	t.Helper()
	b, fail := c.Run(context.Background(), sql, engine.RunOptions{})
	if fail != nil {
		t.Fatalf("Run(%q): %s", sql, fail.Message)
	}
	if len(b.Results) == 0 || len(b.Results[0].Rows) == 0 || b.Results[0].Rows[0][0] == nil {
		return ""
	}
	return *b.Results[0].Rows[0][0]
}

// TestElRebuildNoDisparaElCascade es el test más importante de este archivo.
//
// Reconstruir una tabla implica tirarla, y con las claves foráneas encendidas
// un DROP TABLE hace un DELETE implícito que dispara los ON DELETE CASCADE de
// las tablas que la referencian. Las filas hijas desaparecen SIN NINGÚN ERROR:
// la sentencia funciona, el commit funciona, y hasta PRAGMA foreign_key_check
// devuelve limpio después, porque las hijas no quedaron huérfanas —quedaron
// borradas—.
//
// Va aparte de la batería común y no adentro porque el CASCADE es lo que lo
// vuelve silencioso, y ponerlo en la batería cambiaría lo que prueban los otros
// tres motores. Sin CASCADE el DROP falla ruidosamente y el caso no distingue
// entre estar protegido y tener suerte.
func TestElRebuildNoDisparaElCascade(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE padre (id integer PRIMARY KEY, nombre text)`,
		`CREATE TABLE hija (
			id integer PRIMARY KEY,
			pid integer REFERENCES padre(id) ON DELETE CASCADE)`,
		`INSERT INTO padre VALUES (1,'a'), (2,'b')`,
		`INSERT INTO hija VALUES (10,1), (11,2)`,
		`UPDATE padre SET nombre = 'x' WHERE nombre IS NULL`,
	)

	aplicarRebuild(t, c, change.Change{
		Type: change.SetNotNull, Table: "padre",
		Column: &change.Column{Name: "nombre", DataType: "text"},
	})

	if n := unaCelda(t, c, "SELECT count(*) FROM hija"); n != "2" {
		t.Fatalf("después de reconstruir «padre», «hija» quedó con %s filas de 2.\n"+
			"El DROP TABLE de la reconstrucción disparó el ON DELETE CASCADE y borró "+
			"datos de otra tabla sin dar un solo error.", n)
	}
	if n := unaCelda(t, c, "SELECT count(*) FROM padre"); n != "2" {
		t.Errorf("«padre» quedó con %s filas de 2", n)
	}
	// Y la clave foránea tiene que seguir apuntando a «padre», no al nombre
	// temporal que la tabla usó durante tres sentencias.
	ddl := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='hija'")
	if strings.Contains(ddl, "kn_rebuild") {
		t.Errorf("la clave foránea de «hija» quedó apuntando al nombre temporal:\n%s", ddl)
	}
}

// TestLasClavesForaneasVuelvenAEncenderse comprueba que la conexión no queda
// envenenada.
//
// La reconstrucción apaga las claves foráneas en una conexión del pool. Si no
// se vuelven a encender antes de devolverla, todas las escrituras que salgan
// por ella después pasan sin control de integridad — y cuál sale por cuál no
// se puede saber desde afuera.
func TestLasClavesForaneasVuelvenAEncenderse(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE padre (id integer PRIMARY KEY, nombre text)`,
		`CREATE TABLE hija (id integer PRIMARY KEY, pid integer REFERENCES padre(id))`,
		`INSERT INTO padre VALUES (1,'a')`,
	)
	aplicarRebuild(t, c, change.Change{
		Type: change.SetNotNull, Table: "padre",
		Column: &change.Column{Name: "nombre", DataType: "text"},
	})

	// Se insertan varias filas huérfanas para que salgan por conexiones
	// distintas del pool: con una sola, la que se envenenó podría no tocar.
	for i := 0; i < 8; i++ {
		err := c.Exec(context.Background(),
			"INSERT INTO hija VALUES ("+string(rune('0'+i))+", 999)")
		if err == nil {
			t.Fatalf("se pudo insertar una fila huérfana en el intento %d: la conexión "+
				"quedó con las claves foráneas apagadas después de reconstruir", i)
		}
	}
}

// TestElRebuildNoPierdeNada es la otra mitad del riesgo.
//
// SQLite no expone los CHECK en ningún pragma ni en ninguna tabla del catálogo:
// solo existen en el texto del CREATE TABLE. Una reconstrucción que arme la
// definición nueva desde los pragmas —que es lo natural, y lo que hacen varias
// herramientas— los pierde en silencio. Lo mismo con WITHOUT ROWID, con los
// COLLATE y con las cláusulas ON CONFLICT.
//
// Es la misma clase de error que este proyecto ya decidió no cometer con Atlas
// y las columnas VIRTUAL. Ver kaname-plan.md § 6.
func TestElRebuildNoPierdeNada(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE cosas (
			id      integer PRIMARY KEY,
			nombre  text COLLATE NOCASE,
			monto   integer NOT NULL DEFAULT 0 CHECK (monto >= 0),
			estado  text,
			CONSTRAINT estado_valido CHECK (estado IN ('a','b','c')),
			CONSTRAINT nombre_unico UNIQUE (nombre) ON CONFLICT REPLACE
		)`,
		`CREATE INDEX ix_estado ON cosas (estado) WHERE estado IS NOT NULL`,
		`CREATE TRIGGER tg_cosas AFTER INSERT ON cosas BEGIN SELECT 1; END`,
		`INSERT INTO cosas VALUES (1,'uno',10,'a'), (2,'dos',20,'b')`,
	)

	aplicarRebuild(t, c, change.Change{
		Type: change.SetColumnType, Table: "cosas",
		Column: &change.Column{Name: "estado"}, DataType: "varchar(16)",
	})

	ddl := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='cosas'")

	debeQuedar := []struct{ que, texto string }{
		{"el CHECK de columna", "monto >= 0"},
		{"el CHECK de tabla con nombre", "estado_valido"},
		{"la expresión del CHECK de tabla", "'a','b','c'"},
		{"el COLLATE", "COLLATE NOCASE"},
		{"la restricción UNIQUE con nombre", "nombre_unico"},
		{"la cláusula ON CONFLICT", "ON CONFLICT REPLACE"},
		{"el tipo nuevo", "varchar(16)"},
	}
	for _, d := range debeQuedar {
		if !strings.Contains(ddl, d.texto) {
			t.Errorf("la reconstrucción perdió %s (%q).\nQuedó:\n%s", d.que, d.texto, ddl)
		}
	}

	// El índice parcial y el trigger se van con el DROP TABLE: hay que
	// recrearlos, y con su predicado.
	ixDDL := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='ix_estado'")
	if !strings.Contains(ixDDL, "WHERE estado IS NOT NULL") {
		t.Errorf("el índice parcial no volvió, o volvió sin su predicado: %q", ixDDL)
	}
	if tg := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='tg_cosas'"); tg == "" {
		t.Error("el trigger no volvió después de la reconstrucción")
	}

	// Y los datos.
	if n := unaCelda(t, c, "SELECT count(*) FROM cosas"); n != "2" {
		t.Errorf("quedaron %s filas de 2", n)
	}
	if v := unaCelda(t, c, "SELECT estado FROM cosas WHERE id=2"); v != "b" {
		t.Errorf("el valor de la fila 2 quedó en %q", v)
	}

	// El CHECK tiene que seguir HACIÉNDOSE CUMPLIR, no solo estar escrito.
	// Comprobar el texto y no el comportamiento sería mirar la calcomanía.
	if err := c.Exec(context.Background(),
		"INSERT INTO cosas VALUES (3,'tres',-1,'a')"); err == nil {
		t.Error("se pudo insertar un monto negativo: el CHECK quedó escrito pero no se cumple")
	}
	if err := c.Exec(context.Background(),
		"INSERT INTO cosas VALUES (4,'cuatro',1,'zzz')"); err == nil {
		t.Error("se pudo insertar un estado inválido: el CHECK con nombre no se cumple")
	}
}

// TestElRebuildConservaWithoutRowid: WITHOUT ROWID y STRICT van DESPUÉS del
// paréntesis de cierre, así que un rebuild que solo mire la lista de columnas
// las pierde. Y perderlas cambia la tabla sin decirlo: una WITHOUT ROWID que
// vuelve con rowid ocupa distinto y se comporta distinto.
func TestElRebuildConservaWithoutRowid(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE clave (
			k text NOT NULL,
			v text,
			PRIMARY KEY (k)
		) WITHOUT ROWID`,
		`INSERT INTO clave VALUES ('a','1')`,
	)
	aplicarRebuild(t, c, change.Change{
		Type: change.SetNotNull, Table: "clave",
		Column: &change.Column{Name: "v", DataType: "text"},
	})
	ddl := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='clave'")
	if !strings.Contains(strings.ToUpper(ddl), "WITHOUT ROWID") {
		t.Errorf("la reconstrucción perdió el WITHOUT ROWID:\n%s", ddl)
	}
}

// TestElRebuildSeNiegaConColumnasGeneradas: una columna generada no se
// reescribe. Su expresión puede tener cualquier cosa adentro y no hay forma de
// cambiarle el tipo o la nulabilidad sin entenderla.
//
// Negarse es la respuesta correcta: es la regla del proyecto de que una
// operación que no se sabe expresar es una operación que no se ofrece. Ver
// CLAUDE.md.
func TestElRebuildSeNiegaConColumnasGeneradas(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE calc (
			id    integer PRIMARY KEY,
			base  integer NOT NULL,
			doble integer GENERATED ALWAYS AS (base * 2) STORED
		)`,
		`INSERT INTO calc (id, base) VALUES (1, 21)`,
	)
	_, err := c.RenderDDL(context.Background(), change.Change{
		Type: change.SetNotNull, Table: "calc",
		Column: &change.Column{Name: "doble", DataType: "integer"},
	})
	if err == nil {
		t.Fatal("RenderDDL aceptó modificar una columna generada")
	}
	var noSoporta *engine.ErrUnsupported
	if !errorsAs(err, &noSoporta) {
		t.Errorf("el error tendría que ser *engine.ErrUnsupported y es %T: %v", err, err)
	}

	// Pero una columna NORMAL de una tabla que TIENE una generada sí se puede
	// tocar, y la generada tiene que sobrevivir. Es el caso realista.
	aplicarRebuild(t, c, change.Change{
		Type: change.SetDefault, Table: "calc",
		Column: &change.Column{Name: "base", Default: "0"},
	})
	ddl := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='calc'")
	if !strings.Contains(ddl, "GENERATED ALWAYS AS (base * 2) STORED") {
		t.Errorf("la reconstrucción perdió la columna generada:\n%s", ddl)
	}
	if v := unaCelda(t, c, "SELECT doble FROM calc WHERE id=1"); v != "42" {
		t.Errorf("la columna generada quedó en %q y tendría que valer 42", v)
	}
}

// TestElRebuildSeNiegaSiElNombreTemporalEstaOcupado: el guion crea una tabla
// con un nombre fijo y la borra al final. Si ya existía algo con ese nombre, la
// reconstrucción se lo llevaría puesto.
func TestElRebuildSeNiegaSiElNombreTemporalEstaOcupado(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE t (id integer PRIMARY KEY, v text)`,
		`CREATE TABLE kn_rebuild_t (importante text)`,
	)
	_, err := c.RenderDDL(context.Background(), change.Change{
		Type: change.SetNotNull, Table: "t",
		Column: &change.Column{Name: "v", DataType: "text"},
	})
	if err == nil {
		t.Fatal("RenderDDL siguió adelante con el nombre temporal ocupado: la " +
			"reconstrucción habría borrado la tabla que estaba ahí")
	}
	if !strings.Contains(err.Error(), "kn_rebuild_t") {
		t.Errorf("el error no dice cuál es el nombre que estorba: %v", err)
	}
}

// errorsAs evita importar errors solo para una línea.
func errorsAs(err error, destino **engine.ErrUnsupported) bool {
	for err != nil {
		if e, ok := err.(*engine.ErrUnsupported); ok {
			*destino = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestLaComprobacionDeForaneasRechazaElCommit ejercita el otro extremo del
// procedimiento.
//
// Mientras la reconstrucción corre, las claves foráneas están APAGADAS: no se
// comprueba nada. Lo que las comprueba es el PRAGMA foreign_key_check de antes
// del commit, que es la contraparte de haberlas apagado. Si ese pragma no se
// lee bien —son cuatro columnas y la segunda puede venir NULL—, pasan dos cosas
// malas y opuestas: o una reconstrucción correcta falla al commitear, o una que
// deja huérfanos se commitea igual.
//
// Acá se agrega una clave foránea a una tabla que YA tiene filas que no la
// cumplen. Sin la comprobación, quedaría aplicada.
func TestLaComprobacionDeForaneasRechazaElCommit(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE padre (id integer PRIMARY KEY)`,
		`CREATE TABLE hija (id integer PRIMARY KEY, pid integer)`,
		`INSERT INTO padre VALUES (1)`,
		`INSERT INTO hija VALUES (10, 1), (11, 999)`, // 999 no existe en padre
	)
	ctx := context.Background()

	st, err := c.RenderDDL(ctx, change.Change{
		Type: change.AddForeignKey, Table: "hija",
		Names: []string{"pid"}, RefTable: "padre", RefNames: []string{"id"},
	})
	if err != nil {
		t.Fatalf("RenderDDL: %v", err)
	}

	tx, err := c.Begin(ctx, engine.TxOptions{RebuildsTables: true})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// La copia NO falla: las claves están apagadas mientras corre. Que esto
	// pase es parte de lo que se prueba — si fallara acá, el foreign_key_check
	// nunca llegaría a ejercitarse y el test no probaría lo que dice probar.
	if err := tx.Exec(ctx, st.SQL); err != nil {
		t.Fatalf("la copia falló antes de llegar a la comprobación:\n%s\n%v", st.SQL, err)
	}
	err = tx.Commit(ctx)
	if err == nil {
		t.Fatal("el commit pasó con una fila huérfana: la clave foránea quedó declarada " +
			"y los datos no la cumplen")
	}
	if !strings.Contains(err.Error(), "hija") || !strings.Contains(err.Error(), "padre") {
		t.Errorf("el error no dice qué tabla apunta a cuál, que es lo único accionable: %v", err)
	}

	// Y no tiene que haber quedado nada.
	if n := unaCelda(t, c, "SELECT count(*) FROM hija"); n != "2" {
		t.Errorf("«hija» quedó con %s filas de 2", n)
	}
	ddl := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='hija'")
	if strings.Contains(strings.ToUpper(ddl), "FOREIGN KEY") {
		t.Errorf("la clave foránea quedó aplicada aunque el commit falló:\n%s", ddl)
	}
	if strings.Contains(ddl, "kn_rebuild") {
		t.Errorf("quedó la tabla temporal de la reconstrucción:\n%s", ddl)
	}
}

// TestElRebuildSeNiegaConTablasVirtuales.
//
// Una tabla virtual —FTS5, R-Tree— figura como `type='table'` igual que
// cualquier otra, y su definición es `CREATE VIRTUAL TABLE t USING fts5(...)`.
// Reconstruirla con el procedimiento normal no da error: escribe un CREATE
// TABLE común, copia, y deja en su lugar una tabla corriente con los mismos
// nombres de columna y sin el módulo. Un índice de texto completo convertido en
// texto suelto, en silencio.
func TestElRebuildSeNiegaConTablasVirtuales(t *testing.T) {
	c := abrir(t).(*sqlite.Conn)
	ctx := context.Background()
	if err := c.Exec(ctx, `CREATE VIRTUAL TABLE docs USING fts5(titulo, cuerpo)`); err != nil {
		t.Skipf("este build de SQLite no trae FTS5: %v", err)
	}
	if err := c.Exec(ctx, `INSERT INTO docs VALUES ('uno','hola mundo')`); err != nil {
		t.Fatalf("insertar: %v", err)
	}

	_, err := c.RenderDDL(ctx, change.Change{
		Type: change.SetNotNull, Table: "docs",
		Column: &change.Column{Name: "titulo", DataType: "text"},
	})
	if err == nil {
		t.Fatal("RenderDDL aceptó reconstruir una tabla virtual: la habría convertido " +
			"en una tabla común, sin su módulo y sin decir nada")
	}
	var noSoporta *engine.ErrUnsupported
	if !errorsAs(err, &noSoporta) {
		t.Errorf("el error tendría que ser *engine.ErrUnsupported y es %T: %v", err, err)
	}

	// Y la búsqueda de texto completo tiene que seguir funcionando.
	if n := unaCelda(t, c, `SELECT count(*) FROM docs WHERE docs MATCH 'mundo'`); n != "1" {
		t.Errorf("la búsqueda de texto completo devolvió %q filas", n)
	}
}

// TestElRebuildFuncionaConVistasEncima.
//
// Desde SQLite 3.25 un ALTER TABLE ... RENAME TO vuelve a analizar el esquema
// entero para arreglar las referencias al nombre viejo. En el medio de una
// reconstrucción la tabla original ya no existe —la tiró el paso anterior— así
// que cualquier vista que la nombre hace fallar ese análisis con «error in view
// vt: no such table». Sin PRAGMA legacy_alter_table, ningún cambio que
// reconstruya se puede aplicar sobre una tabla que tenga una vista encima.
func TestElRebuildFuncionaConVistasEncima(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE t (id integer PRIMARY KEY, v text)`,
		`CREATE VIEW vt AS SELECT id, v FROM t WHERE v IS NOT NULL`,
		`INSERT INTO t VALUES (1,'a'), (2,'b')`,
	)
	aplicarRebuild(t, c, change.Change{
		Type: change.SetNotNull, Table: "t",
		Column: &change.Column{Name: "v", DataType: "text"},
	})

	// La vista tiene que seguir existiendo y funcionando.
	if n := unaCelda(t, c, "SELECT count(*) FROM vt"); n != "2" {
		t.Errorf("la vista devolvió %q filas y tendría que devolver 2", n)
	}
	ddl := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='vt'")
	if strings.Contains(ddl, "kn_rebuild") {
		t.Errorf("la vista quedó apuntando al nombre temporal:\n%s", ddl)
	}
}

// TestElRebuildNoRetrocedeElAutoincrement.
//
// El contador de AUTOINCREMENT vive en sqlite_sequence y el DROP TABLE se lleva
// su fila. La copia crea una nueva con el máximo de las filas que quedaron, y
// eso NO es lo mismo: si se habían borrado las últimas, el contador retrocede y
// la próxima fila recibe un id que ya se usó. AUTOINCREMENT existe justamente
// para garantizar que eso no pase — es la única diferencia entre él y un
// `INTEGER PRIMARY KEY` a secas.
func TestElRebuildNoRetrocedeElAutoincrement(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE t (id integer PRIMARY KEY AUTOINCREMENT, v text)`,
		`INSERT INTO t (v) VALUES ('a'),('b'),('c')`,
		`DELETE FROM t WHERE id IN (2,3)`,
	)
	if seq := unaCelda(t, c, "SELECT seq FROM sqlite_sequence WHERE name='t'"); seq != "3" {
		t.Fatalf("el contador arrancaba en %q y no en 3: el caso no probaría nada", seq)
	}

	aplicarRebuild(t, c, change.Change{
		Type: change.SetNotNull, Table: "t",
		Column: &change.Column{Name: "v", DataType: "text"},
	})

	if err := c.Exec(context.Background(), `INSERT INTO t (v) VALUES ('d')`); err != nil {
		t.Fatalf("insertar después de reconstruir: %v", err)
	}
	if id := unaCelda(t, c, "SELECT max(id) FROM t"); id != "4" {
		t.Errorf("la fila nueva recibió el id %s y tendría que recibir 4: la "+
			"reconstrucción hizo retroceder el contador de AUTOINCREMENT y se reusó "+
			"un id que ya había existido", id)
	}
}

// TestLasAccionesDeClaveForaneaSobrevivenAlCambioDeDefault.
//
// `ON DELETE SET NULL` y `ON DELETE SET DEFAULT` terminan con dos palabras que
// en cualquier otro lado de la definición abren una cláusula nueva. Si el
// lector no lo tiene en cuenta, tocar el valor por defecto de esa columna
// escribe SQL rota — o peor: sacarle el default a una columna con
// `ON DELETE SET DEFAULT` dejaba `ON DELETE SET`, destruyendo la acción de la
// clave sin dar ningún error.
func TestLasAccionesDeClaveForaneaSobrevivenAlCambioDeDefault(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE padre (id integer PRIMARY KEY)`,
		`CREATE TABLE hija (
			id  integer PRIMARY KEY,
			n   integer DEFAULT 2 REFERENCES padre(id) ON DELETE SET DEFAULT,
			m   integer REFERENCES padre(id) ON DELETE SET NULL,
			otra text)`,
		`INSERT INTO padre VALUES (1), (2)`,
		`INSERT INTO hija VALUES (10, 1, 1, 'x')`,
	)

	aplicarRebuild(t, c, change.Change{
		Type: change.SetNotNull, Table: "hija",
		Column: &change.Column{Name: "otra", DataType: "text"},
	})
	ddl := unaCelda(t, c, "SELECT sql FROM sqlite_schema WHERE name='hija'")
	for _, quiere := range []string{"ON DELETE SET DEFAULT", "ON DELETE SET NULL"} {
		if !strings.Contains(ddl, quiere) {
			t.Errorf("la reconstrucción perdió o rompió %q:\n%s", quiere, ddl)
		}
	}

	// Y las acciones tienen que seguir HACIÉNDOSE CUMPLIR, no solo estar
	// escritas: borrar el padre pone una en su default y la otra en NULL.
	if err := c.Exec(context.Background(), `DELETE FROM padre WHERE id = 1`); err != nil {
		t.Fatalf("borrar el padre: %v", err)
	}
	if v := unaCelda(t, c, `SELECT n FROM hija WHERE id=10`); v != "2" {
		t.Errorf("ON DELETE SET DEFAULT dejó n en %q y tendría que dejar 2, que es el "+
			"valor por defecto declarado", v)
	}
	if v := unaCelda(t, c, `SELECT COALESCE(m, 'NULO') FROM hija WHERE id=10`); v != "NULO" {
		t.Errorf("ON DELETE SET NULL dejó m en %q", v)
	}
}

// TestSacarUnDefaultQueNoEstaEsUnError. La otra mitad del mismo problema:
// `sacarClausula("DEFAULT")` encontraba el DEFAULT de un `ON DELETE SET
// DEFAULT` y decía que sí, sobre una columna que no tenía valor por defecto.
func TestSacarUnDefaultQueNoEstaEsUnError(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE padre (id integer PRIMARY KEY)`,
		`CREATE TABLE hija (id integer PRIMARY KEY,
			n integer REFERENCES padre(id) ON DELETE SET DEFAULT)`,
	)
	_, err := c.RenderDDL(context.Background(), change.Change{
		Type: change.DropDefault, Table: "hija",
		Column: &change.Column{Name: "n"},
	})
	if err == nil {
		t.Fatal("RenderDDL aceptó sacar un valor por defecto que no existe; la " +
			"sentencia habría destruido el ON DELETE de la clave foránea")
	}
}

// TestRunNoEjecutaDosVeces.
//
// El driver de SQLite ejecuta TODAS las sentencias de la cadena en una sola
// llamada. Reintentar con Exec cuando el Query falla vuelve a correr las que ya
// corrieron: el error se reporta igual y las filas quedan duplicadas.
func TestRunNoEjecutaDosVeces(t *testing.T) {
	c := preparar(t, `CREATE TABLE t (id integer)`)
	_, fail := c.Run(context.Background(),
		"INSERT INTO t VALUES (1); SELECT * FROM no_existe;", engine.RunOptions{})
	if fail == nil {
		t.Fatal("la consulta no falló, así que el caso no prueba nada")
	}
	if n := unaCelda(t, c, "SELECT count(*) FROM t"); n != "1" {
		t.Errorf("quedaron %s filas y se insertó una sola: la sentencia se ejecutó "+
			"de nuevo al reintentar", n)
	}
}

// TestRunInformaLasFilasAfectadas: un UPDATE tiene que decir cuántas filas
// tocó, no mostrar una grilla vacía.
func TestRunInformaLasFilasAfectadas(t *testing.T) {
	c := preparar(t, `CREATE TABLE t (id integer)`, `INSERT INTO t VALUES (1),(2),(3)`)
	b, fail := c.Run(context.Background(), "UPDATE t SET id = id + 10", engine.RunOptions{})
	if fail != nil {
		t.Fatalf("Run: %s", fail.Message)
	}
	r := b.Results[0]
	if r.ReturnsRows {
		t.Error("un UPDATE quedó marcado como que devuelve filas: la pantalla mostraría " +
			"una grilla vacía en vez del recuento")
	}
	if r.AffectedRows != 3 {
		t.Errorf("AffectedRows = %d y se actualizaron 3 filas", r.AffectedRows)
	}
}
