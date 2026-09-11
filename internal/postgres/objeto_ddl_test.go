package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// TestRecrearUnTriggerNoLoCalificaConSuEsquema.
//
// La gramática de Postgres es `DROP TRIGGER nombre ON tabla`, donde el nombre
// es un identificador PELADO: el esquema va del lado de la tabla. Con el
// esquema adelante —`DROP TRIGGER "s"."t" ON …`— el servidor devuelve un error
// de sintaxis, así que recrear un trigger fallaba SIEMPRE.
//
// Se prueba corriéndolo de verdad y no mirando el texto: la gramática la decide
// el servidor, y una comprobación sobre la cadena solo repetiría lo que el
// código ya hizo.
func TestRecrearUnTriggerNoLoCalificaConSuEsquema(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_trg"

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

	cambio := change.Change{
		Type:       change.ReplaceObject,
		ObjectKind: schema.ObjTrigger,
		Schema:     esq,
		Name:       "antes",
		Table:      "t",
		Recreate:   true,
		Definition: fmt.Sprintf(
			"CREATE TRIGGER antes AFTER UPDATE ON %s.t FOR EACH ROW EXECUTE FUNCTION %s.tocar()",
			esq, esq),
	}
	st, err := c.RenderDDL(ctx, cambio)
	if err != nil {
		t.Fatalf("RenderDDL(): %v", err)
	}
	if len(st.Steps) != 2 {
		t.Fatalf("se esperaban dos pasos y hay %d: %v", len(st.Steps), st.Steps)
	}
	for _, paso := range st.Steps {
		if err := c.Exec(ctx, paso); err != nil {
			t.Fatalf("el paso %q no corre: %v", paso, err)
		}
	}

	// Y quedó el nuevo: BEFORE pasó a AFTER.
	def, err := c.ObjectDefinition(ctx, schema.Object{
		Kind: schema.ObjTrigger, Schema: esq, Name: "antes", Table: "t",
	})
	if err != nil {
		t.Fatalf("ObjectDefinition(): %v", err)
	}
	if !strings.Contains(strings.ToUpper(def.SQL), "AFTER UPDATE") {
		t.Errorf("el trigger no se reemplazó:\n%s", def.SQL)
	}
}

// TestUnaFirmaConSQLAdentroNoLlegaALaSentencia.
//
// `Args` viaja desde el frontend y se CONCATENA en el `DROP FUNCTION`. pgx
// manda un Exec sin parámetros por el protocolo simple, así que un `;` adentro
// de la firma serían sentencias extra en el mismo envío. Es la misma disciplina
// que ya tenía el nombre de un tipo: no se valida que exista, se valida que no
// pueda ser otra cosa.
func TestUnaFirmaConSQLAdentroNoLlegaALaSentencia(t *testing.T) {
	c := abrirPG(t)
	cambio := change.Change{
		Type:       change.ReplaceObject,
		ObjectKind: schema.ObjFunction,
		Schema:     "demo",
		Name:       "calcular",
		Args:       "n integer) ; DROP TABLE clientes; --",
		Recreate:   true,
		Definition: "CREATE OR REPLACE FUNCTION demo.calcular(n integer) RETURNS integer LANGUAGE sql AS 'SELECT 1'",
	}
	if _, err := c.RenderDDL(context.Background(), cambio); err == nil {
		t.Fatal("se aceptó una firma con SQL adentro")
	}

	// Y una firma normal sigue pasando, incluida la de un VARIADIC con tipo
	// calificado, que es como la escribe el propio catálogo.
	cambio.Args = "m demo.humor, VARIADIC extra integer[]"
	if _, err := c.RenderDDL(context.Background(), cambio); err != nil {
		t.Errorf("se rechazó una firma normal: %v", err)
	}
}
