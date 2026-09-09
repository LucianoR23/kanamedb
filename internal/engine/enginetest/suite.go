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
	t.Run("transacciones de datos", func(t *testing.T) { transacciones(t, f) })
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

	n, fail := c.Count(ctx, esq, tabla)
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

		n, fail := c.Count(ctx, esq, hija)
		if fail != nil {
			t.Fatalf("Count() sobre la hija: %s", fail.Message)
		}
		if n != 2 {
			t.Fatalf("después de %s sobre %q, la tabla que la referencia quedó con %d "+
				"filas de 2. Un cambio de esquema borró datos de otra tabla, y sin "+
				"un solo error.\nSQL:\n%s", ch.Type, padre, n, st.SQL)
		}
		if n, fail := c.Count(ctx, esq, padre); fail != nil || n != 2 {
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
