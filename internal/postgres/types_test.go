package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// La razón de leer los tipos del catálogo en vez de tener una lista: los enums y
// dominios de ESTA base son tipos válidos, y ninguna lista escrita a mano los
// tendría.
func TestColumnTypesTraeLosTiposPropiosDeLaBase(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TYPE %[1]s.estado AS ENUM ('pendiente', 'pagado');
		CREATE DOMAIN %[1]s.email AS text CHECK (VALUE ~ '@')`, esq))

	ts, err := ColumnTypes(context.Background(), pool)
	if err != nil {
		t.Fatalf("ColumnTypes() falló: %v", err)
	}

	porNombre := map[string]schema.TypeOption{}
	for _, x := range ts {
		porNombre[x.Name] = x
	}

	enum, hay := porNombre[esq+".estado"]
	if !hay {
		t.Fatalf("el enum de esta base no está en la lista (%d tipos)", len(ts))
	}
	if enum.Kind != "enum" || enum.BuiltIn {
		t.Errorf("%s: Kind = %q, BuiltIn = %v", enum.Name, enum.Kind, enum.BuiltIn)
	}

	dominio, hay := porNombre[esq+".email"]
	if !hay {
		t.Error("el dominio de esta base no está en la lista")
	} else if dominio.Kind != "domain" {
		t.Errorf("%s: Kind = %q", dominio.Name, dominio.Kind)
	}

	// Los del sistema tienen que estar con su nombre SQL canónico: `integer` y
	// no `int4`, que es lo que se lee en cualquier otra herramienta.
	for _, quiero := range []string{"text", "integer", "numeric", "timestamp with time zone"} {
		if _, hay := porNombre[quiero]; !hay {
			t.Errorf("falta el tipo %q", quiero)
		}
	}
	if _, hay := porNombre["int4"]; hay {
		t.Error("aparece el alias interno int4 en vez del nombre canónico")
	}

	// Y NO tienen que estar los que no se pueden usar como tipo de columna.
	for _, sobra := range []string{"anyelement", "record", "trigger", "_text", "text[]"} {
		if _, hay := porNombre[sobra]; hay {
			t.Errorf("aparece %q, que no es un tipo de columna usable", sobra)
		}
	}
}

// Sin esto, elegir un tipo es buscar entre cuatrocientos: `abstime` y `aclitem`
// aparecerían antes que `text`.
func TestLosTiposComunesVanPrimero(t *testing.T) {
	pool, _ := conectar(t)

	ts, err := ColumnTypes(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) < 20 {
		t.Fatalf("solo %d tipos: la consulta está filtrando de más", len(ts))
	}
	if ts[0].Name != "text" {
		t.Errorf("el primero es %q y se esperaba text", ts[0].Name)
	}

	// Los primeros tienen que ser todos comunes, no alfabéticos.
	for i := 0; i < len(comunes) && i < len(ts); i++ {
		if ts[i].Name != comunes[i] {
			t.Errorf("posición %d = %q, se esperaba %q", i, ts[i].Name, comunes[i])
		}
	}
}

// El modificador es lo que más se tipea mal: numeric(10,2), varchar(255). Saber
// qué tipos lo admiten es lo que permite ofrecer el campo solo donde sirve.
func TestSeSabeQueTiposAdmitenModificador(t *testing.T) {
	pool, _ := conectar(t)

	ts, err := ColumnTypes(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	porNombre := map[string]schema.TypeOption{}
	for _, x := range ts {
		porNombre[x.Name] = x
	}

	for _, n := range []string{"numeric", "character varying", "character", "bit"} {
		if !porNombre[n].AcceptsModifier {
			t.Errorf("%q: AcceptsModifier = false y admite parámetros", n)
		}
	}
	for _, n := range []string{"text", "boolean", "uuid", "jsonb", "bigint"} {
		if porNombre[n].AcceptsModifier {
			t.Errorf("%q: AcceptsModifier = true y no admite parámetros", n)
		}
	}
}
