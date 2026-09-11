package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// TestAgregarUnValorDeEnumRespetaLaPosicion.
//
// El orden de un enum no es cosmético: en PostgreSQL es el orden en que sus
// valores COMPARAN y ORDENAN, así que un `ORDER BY estado` cambia de resultado
// según dónde entre el valor nuevo. Y es la única oportunidad de elegirlo:
// después no se puede mover sin recrear el tipo entero.
//
// Se comprueba contra el CATÁLOGO y no sobre el texto de la sentencia: que
// escribamos `BEFORE 'alto'` no prueba que el servidor lo haya puesto ahí.
func TestAgregarUnValorDeEnumRespetaLaPosicion(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_enum"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE TYPE %s.humor AS ENUM ('bajo', 'alto')", esq))

	st, err := c.RenderDDL(ctx, change.Change{
		Type: change.AddEnumValue, Schema: esq, Name: "humor",
		Value: "medio", Before: "alto",
	})
	if err != nil {
		t.Fatalf("RenderDDL(): %v", err)
	}
	if st.Destructive {
		t.Error("agregar un valor se marcó como destructivo, y no toca ninguna fila")
	}
	if err := c.Exec(ctx, st.SQL); err != nil {
		t.Fatalf("%q no corre: %v", st.SQL, err)
	}

	if got := valores(t, c, esq, "humor"); got != "bajo,medio,alto" {
		t.Errorf("el valor no entró donde se pidió: %q", got)
	}

	// Sin posición va al final, que es lo que significa dejarlo vacío.
	st, err = c.RenderDDL(ctx, change.Change{
		Type: change.AddEnumValue, Schema: esq, Name: "humor", Value: "altísimo",
	})
	if err != nil {
		t.Fatalf("RenderDDL(): %v", err)
	}
	if strings.Contains(st.SQL, "BEFORE") {
		t.Errorf("sin posición se escribió un BEFORE: %q", st.SQL)
	}
	if err := c.Exec(ctx, st.SQL); err != nil {
		t.Fatalf("%q no corre: %v", st.SQL, err)
	}
	if got := valores(t, c, esq, "humor"); got != "bajo,medio,alto,altísimo" {
		t.Errorf("el valor sin posición no quedó al final: %q", got)
	}
}

// TestRenombrarUnValorDeEnumCambiaLoQueLasFilasLeen.
//
// Renombrar no toca ninguna fila y sin embargo cambia lo que TODAS dicen, a la
// vez. Es lo que lo hace peligroso de una forma que un ALTER de columna no es:
// lo que compare contra el texto viejo —una consulta guardada, el código de la
// aplicación— deja de encontrarlo, y el motor no avisa.
func TestRenombrarUnValorDeEnumCambiaLoQueLasFilasLeen(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	const esq = "kn_enum_ren"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = c.Exec(ctx, "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	exec("CREATE SCHEMA " + esq)
	t.Cleanup(func() { _ = c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE") })

	exec(fmt.Sprintf("CREATE TYPE %s.humor AS ENUM ('bajo', 'alto')", esq))
	exec(fmt.Sprintf("CREATE TABLE %s.t (h %s.humor)", esq, esq))
	exec(fmt.Sprintf("INSERT INTO %s.t VALUES ('bajo'), ('bajo'), ('alto')", esq))

	st, err := c.RenderDDL(ctx, change.Change{
		Type: change.RenameEnumValue, Schema: esq, Name: "humor",
		Value: "bajo", NewName: "mínimo",
	})
	if err != nil {
		t.Fatalf("RenderDDL(): %v", err)
	}
	if err := c.Exec(ctx, st.SQL); err != nil {
		t.Fatalf("%q no corre: %v", st.SQL, err)
	}

	// Las dos filas que decían «bajo» dicen «mínimo» ahora, sin haberlas tocado.
	n := unaCeldaPG(t, c, fmt.Sprintf("SELECT count(*) FROM %s.t WHERE h = 'mínimo'", esq))
	if n != "2" {
		t.Errorf("las filas con el valor viejo no se leen con el nuevo: son %s de 2", n)
	}
	if got := valores(t, c, esq, "humor"); got != "mínimo,alto" {
		t.Errorf("el orden cambió al renombrar: %q", got)
	}
	if st.Note == "" {
		t.Error("renombrar no avisa nada, y rompe lo que compare contra el texto viejo")
	}
}

// TestUnCambioDeEnumIncompletoNoSeEscribe: el candado de abajo, que es el que
// vale. Un valor vacío en un ALTER TYPE es `”`, un valor legítimo y distinto
// del que alguien quiso poner.
func TestUnCambioDeEnumIncompletoNoSeEscribe(t *testing.T) {
	c := abrirPG(t)
	ctx := context.Background()
	casos := []change.Change{
		{Type: change.AddEnumValue, Schema: "demo", Name: "humor"},
		{Type: change.AddEnumValue, Schema: "demo", Value: "x"},
		{Type: change.RenameEnumValue, Schema: "demo", Name: "humor", Value: "a"},
		{Type: change.RenameEnumValue, Schema: "demo", Name: "humor", Value: "a", NewName: "a"},
	}
	for _, caso := range casos {
		if _, err := c.RenderDDL(ctx, caso); err == nil {
			t.Errorf("se escribió un cambio incompleto: %+v", caso)
		}
	}
}

// valores devuelve los del enum, en su orden de declaración.
func valores(t *testing.T, c engine.Conn, esq, tipo string) string {
	t.Helper()
	return unaCeldaPG(t, c, fmt.Sprintf(
		`SELECT string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder)
		 FROM pg_catalog.pg_enum e
		 JOIN pg_catalog.pg_type ty ON ty.oid = e.enumtypid
		 JOIN pg_catalog.pg_namespace n ON n.oid = ty.typnamespace
		 WHERE n.nspname = '%s' AND ty.typname = '%s'`, esq, tipo))
}

// unaCeldaPG saca el primer valor de una consulta de una sola celda.
func unaCeldaPG(t *testing.T, c engine.Conn, sql string) string {
	t.Helper()
	b, fail := c.Run(context.Background(), sql, engine.RunOptions{})
	if fail != nil {
		t.Fatalf("Run(%q): %s", sql, fail.Message)
	}
	if len(b.Results) == 0 || len(b.Results[0].Rows) == 0 {
		t.Fatalf("Run(%q) no devolvió filas", sql)
	}
	if v := b.Results[0].Rows[0][0]; v != nil {
		return *v
	}
	return ""
}
