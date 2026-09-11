package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// TestLaVistaNoPierdeSusOpcionesAlVolverAEscribirla.
//
// Es la pérdida más cara que puede tener este código, y la más silenciosa.
// `pg_get_viewdef` devuelve el SELECT y NADA MÁS: las opciones de la vista
// viven en `reloptions`, aparte. Sin ellas, alguien abre una vista en el
// editor, la guarda sin tocar nada, y:
//
//   - `security_invoker` desaparece, así que la vista pasa a correr con los
//     permisos de su DUEÑO en vez de los de quien consulta. Una vista endurecida
//     se convierte en una permeable, con el mismo nombre y el mismo cuerpo.
//   - `check_option` desaparece, así que la vista deja de rechazar las
//     escrituras que se saldrían de ella: se aceptan, y la fila desaparece de
//     la vista donde se escribió.
//
// Las dos cosas se ven idénticas a que todo esté bien. Este caso no vive en la
// suite compartida porque `security_invoker` es de Postgres.
func TestLaVistaNoPierdeSusOpcionesAlVolverAEscribirla(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_opts"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	// `security_invoker` existe desde Postgres 15. En la 14, que sigue en la
	// matriz, se prueba lo que la 14 sabe: `check_option`.
	opciones := []string{"security_invoker = true", "check_option = cascaded"}
	if c.Server().VersionNum < 150000 {
		opciones = opciones[1:]
	}

	exec(fmt.Sprintf("CREATE TABLE %s.t (id integer PRIMARY KEY)", esq))
	exec(fmt.Sprintf(
		"CREATE VIEW %s.v WITH (%s) AS SELECT id FROM %s.t WHERE id > 0",
		esq, strings.Join(opciones, ", "), esq))

	def, err := c.ObjectDefinition(ctx, schema.Object{
		Kind: schema.ObjView, Schema: esq, Name: "v",
	})
	if err != nil {
		t.Fatalf("ObjectDefinition(): %v", err)
	}
	for _, q := range opciones {
		nombre, _, _ := strings.Cut(q, " ")
		if !strings.Contains(def.SQL, nombre) {
			t.Errorf("la definición perdió %q:\n%s", nombre, def.SQL)
		}
	}

	// Y no alcanza con que el texto las mencione: se borra la vista, se la
	// recrea con lo que salió, y se le pregunta al CATÁLOGO. Mirar el texto
	// probaría que escribimos las palabras, no que el motor las entendió.
	exec(fmt.Sprintf("DROP VIEW %s.v", esq))
	if err := c.Exec(ctx, def.SQL); err != nil {
		t.Fatalf("la definición no se puede volver a correr: %v\n%s", err, def.SQL)
	}

	b, fail := c.Run(ctx, fmt.Sprintf(
		"SELECT array_to_string(c.reloptions, ',') FROM pg_class c "+
			"JOIN pg_namespace n ON n.oid = c.relnamespace "+
			"WHERE n.nspname = '%s' AND c.relname = 'v'", esq), engine.RunOptions{})
	if fail != nil {
		t.Fatalf("leer las reloptions: %s", fail.Message)
	}
	if len(b.Results) == 0 || len(b.Results[0].Rows) == 0 {
		t.Fatal("la vista no volvió a existir")
	}
	vueltas := ""
	if v := b.Results[0].Rows[0][0]; v != nil {
		vueltas = *v
	}
	for _, q := range opciones {
		q = strings.ReplaceAll(q, " ", "")
		if !strings.Contains(vueltas, q) {
			t.Errorf("la vista recreada no tiene %q; el catálogo dice: %q", q, vueltas)
		}
	}
}

// TestUnaFuncionSobrecargadaSeDistingueDeLaOtra.
//
// Dos funciones con el mismo nombre y distinta firma son dos objetos. Sin la
// firma, pedir la definición de una devolvía cualquiera de las dos —o fallaba—
// y el árbol las mostraba como dos nodos idénticos.
func TestUnaFuncionSobrecargadaSeDistingueDeLaOtra(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_sobrecarga"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE FUNCTION %s.calcular(n integer) RETURNS integer LANGUAGE sql AS 'SELECT n * 2'", esq))
	exec(fmt.Sprintf("CREATE FUNCTION %s.calcular(t text) RETURNS text LANGUAGE sql AS 'SELECT upper(t)'", esq))

	objetos, err := c.Objects(ctx, []string{esq})
	if err != nil {
		t.Fatalf("Objects(): %v", err)
	}
	var funciones []schema.Object
	for _, o := range objetos {
		if o.Kind == schema.ObjFunction && o.Name == "calcular" {
			funciones = append(funciones, o)
		}
	}
	if len(funciones) != 2 {
		t.Fatalf("salieron %d funciones «calcular», se esperaban 2", len(funciones))
	}
	// Las dos entradas tienen que ser DISTINGUIBLES. Con el nombre solo, la
	// cobertura del volcado decía «quedan afuera demo.calcular y demo.calcular».
	if funciones[0].Completo() == funciones[1].Completo() {
		t.Fatalf("las dos sobrecargas se nombran igual: %q", funciones[0].Completo())
	}

	// Y cada una devuelve SU cuerpo, no el de la otra.
	cuerpos := map[string]bool{}
	for _, f := range funciones {
		def, err := c.ObjectDefinition(ctx, f)
		if err != nil {
			t.Fatalf("ObjectDefinition(%s): %v", f.Completo(), err)
		}
		cuerpos[def.SQL] = true
		esEntera := strings.Contains(f.Args, "integer")
		if esEntera && !strings.Contains(def.SQL, "n * 2") {
			t.Errorf("la de integer devolvió otro cuerpo:\n%s", def.SQL)
		}
		if !esEntera && !strings.Contains(def.SQL, "upper(t)") {
			t.Errorf("la de text devolvió otro cuerpo:\n%s", def.SQL)
		}
	}
	if len(cuerpos) != 2 {
		t.Error("las dos sobrecargas devolvieron la MISMA definición")
	}

	// Con una firma que ya no existe —el esquema se recargó tarde— no se
	// adivina entre dos: se dice.
	_, err = c.ObjectDefinition(ctx, schema.Object{
		Kind: schema.ObjFunction, Schema: esq, Name: "calcular", Args: "n bigint",
	})
	if err == nil {
		t.Error("con una firma que no existe y dos candidatas, se devolvió una igual")
	} else if !strings.Contains(err.Error(), "2 versiones") {
		t.Errorf("el error no dice que hay que elegir: %v", err)
	}
}

// TestUnaFuncionSolaSeEncuentraAunqueLaFirmaNoCoincida.
//
// `pg_get_function_identity_arguments` escribe los tipos según el `search_path`
// de la conexión que la corre, y `Objects` y `ObjectDefinition` son dos viajes
// a un POOL: pueden caer en conexiones distintas. Basta con que alguien haya
// corrido un `SET search_path` en el editor —que se pega a UNA conexión— para
// que los dos textos difieran. Si eso dejara la función sin definición, el
// usuario vería «ya no está en la base» sobre una función que está.
func TestUnaFuncionSolaSeEncuentraAunqueLaFirmaNoCoincida(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_firma"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE TYPE %s.humor AS ENUM ('bien', 'mal')", esq))
	exec(fmt.Sprintf("CREATE FUNCTION %s.mirar(h %s.humor) RETURNS text LANGUAGE sql AS 'SELECT h::text'", esq, esq))

	// La firma como la escribiría una conexión con OTRO search_path: sin
	// calificar el tipo. Es exactamente lo que devuelve el catálogo después de
	// un `SET search_path TO kn_firma`.
	def, err := c.ObjectDefinition(ctx, schema.Object{
		Kind: schema.ObjFunction, Schema: esq, Name: "mirar", Args: "h humor",
	})
	if err != nil {
		t.Fatalf("con una sola candidata la firma no debería hacer falta: %v", err)
	}
	if !strings.Contains(def.SQL, "mirar") {
		t.Errorf("devolvió otra cosa:\n%s", def.SQL)
	}
}
