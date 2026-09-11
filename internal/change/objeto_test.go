package change

import (
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

func reemplazo(def string) Change {
	return Change{
		Type:       ReplaceObject,
		ObjectKind: schema.ObjView,
		Schema:     "demo",
		Name:       "v",
		Definition: def,
	}
}

// TestUnaDefinicionQueNoCreaNadaNoPuedeBorrarElObjeto.
//
// Es el fallo más caro que puede tener el editor de objetos y el más fácil de
// provocar: pegar un SELECT —o el cuerpo suelto de una función— donde iba el
// CREATE entero, y guardar.
//
// Con «borrar y volver a crear» puesto, eso sería un DROP seguido de una
// sentencia que no crea nada. Ningún motor se queja: un SELECT es SQL
// perfectamente válida, corre, devuelve filas y no deja nada. El objeto
// desaparece y el apply termina en verde.
//
// Lo que NO se hace es validar más que esto. Decidir si un CREATE entero es
// correcto es trabajo del servidor, y adivinarlo acá terminaría rechazando SQL
// válida que Kaname no entendió.
func TestUnaDefinicionQueNoCreaNadaNoPuedeBorrarElObjeto(t *testing.T) {
	for _, def := range []string{
		"SELECT id FROM demo.t",
		"  select 1  ",
		"BEGIN RETURN NEW; END",
		"DROP VIEW demo.v",
		"-- CREATE VIEW demo.v AS SELECT 1",
	} {
		c := reemplazo(def)
		c.Recreate = true
		if _, _, err := ObjetoAReemplazar(c, "postgres", `"demo"."v"`, ""); err == nil {
			t.Errorf("se aceptó como definición algo que no crea nada: %q", def)
		}
	}

	// Y lo que SÍ es un CREATE pasa, escrito como sea. El motor decide si está
	// bien; acá solo se comprueba que cree algo.
	for _, def := range []string{
		"CREATE VIEW demo.v AS SELECT 1",
		"create or replace view demo.v as select 1",
		"\n\n  CREATE\tVIEW demo.v AS SELECT 1",
	} {
		c := reemplazo(def)
		c.Recreate = true
		if _, _, err := ObjetoAReemplazar(c, "postgres", `"demo"."v"`, ""); err != nil {
			t.Errorf("se rechazó un CREATE válido %q: %v", def, err)
		}
	}
}

// TestReemplazarEnElLugarNoEscribeNingunDrop.
//
// Es la propiedad que hace segura la opción por defecto: si no hay DROP, no hay
// forma de que un CREATE que falla deje el objeto borrado.
func TestReemplazarEnElLugarNoEscribeNingunDrop(t *testing.T) {
	c := reemplazo("CREATE OR REPLACE VIEW demo.v AS SELECT 1")
	drop, def, err := ObjetoAReemplazar(c, "postgres", `"demo"."v"`, "")
	if err != nil {
		t.Fatalf("ObjetoAReemplazar(): %v", err)
	}
	if drop != "" {
		t.Errorf("se escribió un DROP sin que nadie lo pidiera: %q", drop)
	}
	if !strings.Contains(def, "CREATE") {
		t.Errorf("la definición salió cambiada: %q", def)
	}
	if c.Destructive() {
		t.Error("reemplazar en el lugar se marcó como destructivo, y no puede perder nada")
	}

	// Y pedirlo sí lo marca: es lo que hace que la pantalla de revisión lo
	// resalte y que la confirmación de producción lo trate como tal.
	c.Recreate = true
	if !c.Destructive() {
		t.Error("borrar y volver a crear NO se marcó como destructivo")
	}
}

// TestLoQueElMotorNoSabeReemplazarSeExigeRecrear.
//
// Sin este candado, una vista materializada se intentaba reemplazar con un
// `CREATE MATERIALIZED VIEW` sobre una que ya existe: falla con «already
// exists», sin daño pero sin decir qué hacer. El arreglo es el mismo que este
// error obliga a elegir ANTES, con la lista de dependientes a la vista.
func TestLoQueElMotorNoSabeReemplazarSeExigeRecrear(t *testing.T) {
	casos := []struct {
		motor string
		kind  schema.ObjectKind
		puede bool
	}{
		{"postgres", schema.ObjView, true},
		{"postgres", schema.ObjFunction, true},
		{"postgres", schema.ObjTrigger, true},
		{"postgres", schema.ObjMatView, false},
		{"postgres", schema.ObjSequence, false},
		{"mysql", schema.ObjView, true},
		{"mysql", schema.ObjProcedure, false},
		{"mariadb", schema.ObjView, true},
		{"sqlite", schema.ObjView, false},
		{"sqlite", schema.ObjTrigger, false},
	}
	for _, caso := range casos {
		if got := PuedeReemplazarEnElLugar(caso.motor, caso.kind); got != caso.puede {
			t.Errorf("%s/%s: PuedeReemplazarEnElLugar = %v, se esperaba %v",
				caso.motor, caso.kind, got, caso.puede)
		}

		c := reemplazo("CREATE X")
		c.ObjectKind = caso.kind
		if caso.kind == schema.ObjTrigger {
			c.Table = "t"
		}
		_, _, err := ObjetoAReemplazar(c, caso.motor, "x", "")
		if caso.puede && err != nil {
			t.Errorf("%s/%s: se rechazó reemplazar en el lugar: %v", caso.motor, caso.kind, err)
		}
		if !caso.puede && err == nil {
			t.Errorf("%s/%s: se aceptó reemplazar en el lugar algo que el motor no sabe reemplazar",
				caso.motor, caso.kind)
		}
	}
}

// TestUnTriggerSinSuTablaNoSePuedeBorrar: el DROP de un trigger necesita la
// tabla en Postgres, y sin ella la sentencia sale a medias.
func TestUnTriggerSinSuTablaNoSePuedeBorrar(t *testing.T) {
	c := reemplazo("CREATE TRIGGER x BEFORE UPDATE ON demo.t FOR EACH ROW EXECUTE FUNCTION f()")
	c.ObjectKind = schema.ObjTrigger
	c.Recreate = true
	if _, _, err := ObjetoAReemplazar(c, "postgres", "x", ""); err == nil {
		t.Error("se aceptó un trigger sin su tabla")
	}

	c.Table = "t"
	drop, _, err := ObjetoAReemplazar(c, "postgres", `"demo"."x"`, ` ON "demo"."t"`)
	if err != nil {
		t.Fatalf("ObjetoAReemplazar(): %v", err)
	}
	if !strings.Contains(drop, `ON "demo"."t"`) {
		t.Errorf("el DROP del trigger no dice sobre qué tabla: %q", drop)
	}
}

// TestLaFirmaViajaEnElDropDeUnaFuncionSobrecargada: sin ella el DROP falla con
// «is not unique» — o, con una sola sobrecarga, borra la que no era.
func TestLaFirmaViajaEnElDropDeUnaFuncionSobrecargada(t *testing.T) {
	c := reemplazo("CREATE OR REPLACE FUNCTION demo.calcular(n integer) RETURNS integer AS 'SELECT 1' LANGUAGE sql")
	c.ObjectKind = schema.ObjFunction
	c.Name = "calcular"
	c.Args = "n integer"
	c.Recreate = true

	drop, _, err := ObjetoAReemplazar(c, "postgres", `"demo"."calcular"(n integer)`, "")
	if err != nil {
		t.Fatalf("ObjetoAReemplazar(): %v", err)
	}
	if !strings.Contains(drop, "(n integer)") {
		t.Errorf("el DROP no lleva la firma: %q", drop)
	}
}

// TestLaDefinicionSeMuestraEnLaFormaQueSePuedeVolverACorrer.
//
// `CREATE VIEW x` sobre una vista que YA EXISTE falla con «already exists», y
// esa definición es la que el editor va a ejecutar: el modo por defecto
// —reemplazar en el lugar— no funcionaría contra ningún objeto real. Se eligió
// arreglarlo al LEER, cuando se arma lo que se muestra, en vez de al ejecutar:
// así lo que la persona ve es literalmente lo que corre, y nadie reescribe
// después lo que ella escribió.
func TestLaDefinicionSeMuestraEnLaFormaQueSePuedeVolverACorrer(t *testing.T) {
	casos := []struct{ dado, quiero string }{
		{"CREATE VIEW x AS SELECT 1", "CREATE OR REPLACE VIEW x AS SELECT 1"},
		{"create view x as select 1", "create OR REPLACE view x as select 1"},
		// El resto de la línea no se toca: el ALGORITHM y el DEFINER de MySQL
		// son parte del objeto y sacarlos lo cambiaría.
		{
			"CREATE ALGORITHM=UNDEFINED DEFINER=`k`@`%` SQL SECURITY DEFINER VIEW `v` AS select 1",
			"CREATE OR REPLACE ALGORITHM=UNDEFINED DEFINER=`k`@`%` SQL SECURITY DEFINER VIEW `v` AS select 1",
		},
		// Idempotente: si ya la tiene, no se duplica.
		{"CREATE OR REPLACE VIEW x AS SELECT 1", "CREATE OR REPLACE VIEW x AS SELECT 1"},
		{"CREATE  OR  REPLACE  VIEW x AS SELECT 1", "CREATE  OR  REPLACE  VIEW x AS SELECT 1"},
		// Lo que no empieza con CREATE no se toca: no es tarea de esta función
		// decidir si tiene sentido.
		{"SELECT 1", "SELECT 1"},
	}
	for _, c := range casos {
		if got := ConOrReplace(c.dado); got != c.quiero {
			t.Errorf("ConOrReplace(%q)\n = %q\nse esperaba %q", c.dado, got, c.quiero)
		}
	}

	// Y la indentación de adelante se conserva: el texto que sigue es el cuerpo
	// tal como lo devolvió el motor.
	if got := ConOrReplace("\n  CREATE VIEW x AS SELECT 1"); got != "\n  CREATE OR REPLACE VIEW x AS SELECT 1" {
		t.Errorf("se perdió lo de adelante: %q", got)
	}
}
