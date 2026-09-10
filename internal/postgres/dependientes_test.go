package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// TestUnaVistaQueCuelgaDeOtraApareceComoDependiente.
//
// Es el caso que importa y el que se olvida. Una vista NO depende de sus tablas
// directamente: depende a través de su regla de reescritura, en `pg_rewrite`.
// Sin ese salto la consulta devuelve vacío, y vacío significa «no depende nada
// de esto» — que es justo lo que alguien mira antes de reemplazar una vista.
//
// La inyección que lo pone en rojo es sacar el JOIN con `pg_rewrite` y buscar
// el `refobjid` contra `pg_class` directamente: es la forma que parece obvia.
func TestUnaVistaQueCuelgaDeOtraApareceComoDependiente(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_dep"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE TABLE %s.t (id integer PRIMARY KEY)", esq))
	exec(fmt.Sprintf("CREATE VIEW %s.base AS SELECT id FROM %s.t", esq, esq))
	exec(fmt.Sprintf("CREATE VIEW %s.encima AS SELECT id FROM %s.base", esq, esq))
	exec(fmt.Sprintf("CREATE MATERIALIZED VIEW %s.mat AS SELECT id FROM %s.base", esq, esq))
	// Una vista que NO usa a `base`: no puede aparecer, o el aviso asustaría
	// nombrando cosas que no se rompen.
	exec(fmt.Sprintf("CREATE VIEW %s.ajena AS SELECT id FROM %s.t", esq, esq))

	dep, err := c.Dependents(ctx, schema.Object{Kind: schema.ObjView, Schema: esq, Name: "base"})
	if err != nil {
		t.Fatalf("Dependents(): %v", err)
	}
	if dep.Unknown {
		t.Fatal("Postgres SÍ puede contestar esto y dijo que no se puede saber")
	}

	visto := map[string]string{}
	for _, o := range dep.Objects {
		visto[o.Name] = string(o.Kind)
	}
	if visto["encima"] != "view" {
		t.Errorf("la vista que cuelga no apareció; salieron: %v", visto)
	}
	if visto["mat"] != "materializedView" {
		t.Errorf("la vista materializada que cuelga no apareció como tal; salieron: %v", visto)
	}
	if _, hay := visto["ajena"]; hay {
		t.Errorf("apareció una vista que NO depende de ésta: %v", visto)
	}
	if _, hay := visto["base"]; hay {
		t.Error("la vista se listó como dependiente de sí misma")
	}
}

// TestUnObjetoSinDependientesLoDiceYNoEsLoMismoQueNoSaber.
//
// Las dos respuestas se ven igual en una lista vacía y significan lo contrario:
// «podés reemplazarla tranquilo» contra «no tengo idea de qué se rompe».
func TestUnObjetoSinDependientesLoDiceYNoEsLoMismoQueNoSaber(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_dep_sola"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE TABLE %s.t (id integer)", esq))
	exec(fmt.Sprintf("CREATE VIEW %s.sola AS SELECT id FROM %s.t", esq, esq))

	dep, err := c.Dependents(ctx, schema.Object{Kind: schema.ObjView, Schema: esq, Name: "sola"})
	if err != nil {
		t.Fatalf("Dependents(): %v", err)
	}
	if !dep.Vacio() {
		t.Errorf("Vacio() = false con %d dependientes y Unknown=%v", len(dep.Objects), dep.Unknown)
	}
}

// TestUnTriggerQueUsaUnaFuncionApareceComoDependiente.
//
// Es la otra mitad de la consulta y es la que hace peligroso reemplazar una
// función: borrarla y volver a crearla rompe cada trigger que la llame, y esos
// triggers están en OTRA pantalla.
func TestUnTriggerQueUsaUnaFuncionApareceComoDependiente(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_dep_fn"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE TABLE %s.t (id integer)", esq))
	exec(fmt.Sprintf(
		"CREATE FUNCTION %s.tocar() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$", esq))
	exec(fmt.Sprintf(
		"CREATE TRIGGER antes BEFORE UPDATE ON %s.t FOR EACH ROW EXECUTE FUNCTION %s.tocar()", esq, esq))

	dep, err := c.Dependents(ctx, schema.Object{
		Kind: schema.ObjFunction, Schema: esq, Name: "tocar", Args: "",
	})
	if err != nil {
		t.Fatalf("Dependents(): %v", err)
	}
	var trigger *schema.Object
	for i := range dep.Objects {
		if dep.Objects[i].Name == "antes" && dep.Objects[i].Kind == schema.ObjTrigger {
			trigger = &dep.Objects[i]
		}
	}
	if trigger == nil {
		t.Fatalf("el trigger que usa la función no apareció; salieron: %+v", dep.Objects)
	}
	// Y con SU TABLA. Los nombres de trigger son únicos por tabla y no por
	// esquema: sin ella, dos «auditar» sobre tablas distintas son el mismo
	// renglón repetido, y la tabla es lo único que dice dónde ir a arreglarlo.
	if trigger.Table != "t" {
		t.Errorf("el trigger vino sin su tabla: %+v", *trigger)
	}
}

// TestUnEnumQueUsaUnaTablaNoDiceQueNadaDependeDeEl.
//
// Es el hallazgo más caro del review de esta unidad y la mentira exacta que
// `schema.Dependents` existe para impedir: la pantalla decía «Nada.
// Reemplazarlo no rompe ningún otro objeto» sobre un enum que un `DROP TYPE`
// se niega a borrar.
//
// El motivo es que `pg_depend` NO apunta al objeto que uno tiene en la cabeza.
// Una columna tipada con un enum se registra como una fila de `pg_class` con
// `objsubid`, y el default `nextval(…)` de una columna como una de
// `pg_attrdef`: ninguna de las dos entra por las ramas de las vistas ni de los
// triggers. La inyección que lo pone en rojo es sacar la tercera rama, la de
// `pg_identify_object`.
func TestUnEnumQueUsaUnaTablaNoDiceQueNadaDependeDeEl(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_dep_tipo"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE TYPE %s.humor AS ENUM ('bajo','alto')", esq))
	exec(fmt.Sprintf("CREATE SEQUENCE %s.folio", esq))
	exec(fmt.Sprintf(
		"CREATE TABLE %s.t (id integer DEFAULT nextval('%s.folio'), m %s.humor)", esq, esq, esq))

	// El servidor mismo dice que hay dependencias: si esto dejara de fallar, el
	// caso no probaría nada y habría que cambiarlo.
	if err := c.Exec(ctx, fmt.Sprintf("DROP TYPE %s.humor", esq)); err == nil {
		t.Fatal("el DROP TYPE no falló, así que la tabla no depende del enum y el caso no prueba nada")
	}

	for _, caso := range []struct {
		nombre string
		obj    schema.Object
	}{
		{"el enum", schema.Object{Kind: schema.ObjEnum, Schema: esq, Name: "humor"}},
		{"la secuencia", schema.Object{Kind: schema.ObjSequence, Schema: esq, Name: "folio"}},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			dep, err := c.Dependents(ctx, caso.obj)
			if err != nil {
				t.Fatalf("Dependents(): %v", err)
			}
			if dep.Vacio() {
				t.Fatal("dijo que NADA depende de esto, y el servidor se niega a borrarlo: " +
					"es el «reemplazá tranquilo» sobre algo que rompe una tabla")
			}
			if dep.Unknown {
				t.Fatal("Postgres SÍ puede contestar esto")
			}
			// Y el nombre sirve para ir a mirarlo: tiene que decir la tabla.
			var nombra bool
			for _, o := range dep.Objects {
				if strings.Contains(o.Name, ".t") || o.Table == "t" {
					nombra = true
				}
			}
			if !nombra {
				t.Errorf("los dependientes no nombran la tabla que lo usa: %+v", dep.Objects)
			}
		})
	}
}

// TestUnaClaseQueNoSeSabeBuscarLoDiceEnVezDeContestarVacio.
//
// El `default` de la búsqueda contestaba con una lista vacía para todo lo que
// no reconocía —políticas, extensiones, y lo que aparezca mañana—. Un `DROP
// EXTENSION postgis` arrastra cientos de objetos: «no depende nada» ahí es la
// respuesta más cara que esta función puede dar.
func TestUnaClaseQueNoSeSabeBuscarLoDiceEnVezDeContestarVacio(t *testing.T) {
	c := abrirPG(t)
	dep, err := c.Dependents(context.Background(), schema.Object{
		Kind: schema.ObjExtension, Schema: "public", Name: "postgis",
	})
	if err != nil {
		t.Fatalf("Dependents(): %v", err)
	}
	if !dep.Unknown {
		t.Error("una clase que no se sabe buscar contestó como si supiera")
	}
	if dep.Vacio() {
		t.Error("Vacio() con una clase que no se sabe buscar: eso es «reemplazá tranquilo»")
	}
	if dep.Reason == "" {
		t.Error("no se dice por qué no se puede saber")
	}

	// Un trigger es lo contrario: vacío Y seguro, porque nada puede colgar de
	// él. Decirlo como «no se sabe» sería asustar sin motivo.
	dep, err = c.Dependents(context.Background(), schema.Object{
		Kind: schema.ObjTrigger, Schema: "public", Name: "el_que_sea", Table: "t",
	})
	if err != nil {
		t.Fatalf("Dependents(): %v", err)
	}
	if !dep.Vacio() {
		t.Errorf("un trigger tendría que ser vacío y seguro: Unknown=%v, %d objetos",
			dep.Unknown, len(dep.Objects))
	}
}
