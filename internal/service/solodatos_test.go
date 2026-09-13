package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// En el teléfono, datos sí y esquema no, y lo decide el backend: con
// soloDatos encendido, Stage rechaza cualquier cambio de esquema aunque se
// llame al binding a mano, y acepta los de filas como siempre. Corre sobre
// SQLite, que no necesita contenedor; la regla no depende del motor.
func TestConSoloDatosStageRechazaElEsquemaYAceptaLasFilas(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_solodatos", true)
	sesion.soloDatos = true

	// Un cambio de esquema, y no uno cualquiera: crear una tabla es lo menos
	// destructivo que hay, y tampoco entra.
	_, err := sesion.Stage(ctx, change.Change{
		Type: change.CreateTable, Schema: esq, Table: "kn_otra", Source: "test",
		Columns: []change.Column{{Name: "id", DataType: "integer"}}, Names: []string{"id"},
	}, "")
	if !errors.Is(err, ErrSchemaLocked) {
		t.Fatalf("Stage() de un CreateTable con soloDatos dio %v, se esperaba ErrSchemaLocked", err)
	}
	if n := sesion.mustChangeset(t).Summary.Total; n != 0 {
		t.Fatalf("el changeset tiene %d cambios; el de esquema no tenía que entrar", n)
	}

	// Una tanda mixta se rechaza entera: no se preparan las filas y se calla
	// el esquema.
	_, err = sesion.StageMany(ctx, []change.Change{
		{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
			Values:   []change.Cell{{Column: "nombre", Value: texto("uno editado")}},
			Key:      []change.Cell{{Column: "id", Value: texto("1")}},
			Previous: []change.Cell{{Column: "nombre", Value: texto("uno")}}},
		{Type: change.DropTable, Schema: esq, Table: tabla, Source: "test"},
	}, "")
	if !errors.Is(err, ErrSchemaLocked) {
		t.Fatalf("StageMany() mixto dio %v, se esperaba ErrSchemaLocked", err)
	}
	if n := sesion.mustChangeset(t).Summary.Total; n != 0 {
		t.Fatalf("la tanda mixta dejó %d cambios en el changeset", n)
	}

	// Las filas solas entran igual que en escritorio.
	if _, err := sesion.StageMany(ctx, []change.Change{
		{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
			Values:   []change.Cell{{Column: "nombre", Value: texto("uno editado")}},
			Key:      []change.Cell{{Column: "id", Value: texto("1")}},
			Previous: []change.Cell{{Column: "nombre", Value: texto("uno")}}},
		{Type: change.DeleteRow, Schema: esq, Table: tabla, Source: "grid",
			Key: []change.Cell{{Column: "id", Value: texto("2")}}},
	}, ""); err != nil {
		t.Fatalf("StageMany() de filas con soloDatos falló: %v", err)
	}
	if n := sesion.mustChangeset(t).Summary.Total; n != 2 {
		t.Fatalf("el changeset tiene %d cambios, se esperaban 2", n)
	}

	// El editor SQL es la otra puerta: un DDL escrito a mano tampoco corre, y
	// se para antes de la primera sentencia del lote. El DML sigue corriendo.
	q := NewQueries(sesion)
	res := q.Run(ctx, "r1", "update "+tabla+" set nombre = 'x' where id = 1; drop table "+tabla)
	if res.OK || res.Failure == nil || !strings.Contains(res.Failure.Message, "DROP") {
		t.Fatalf("Run() con un DROP en el lote: ok=%v failure=%+v", res.OK, res.Failure)
	}
	if res.Failure.Statement != 2 {
		t.Errorf("el fallo señala la sentencia %d, se esperaba la 2", res.Failure.Statement)
	}
	for _, ddl := range []string{"create table kn_x (id integer)", "alter table " + tabla + " add columna text", "truncate table " + tabla} {
		if res := q.Run(ctx, "r2", ddl); res.OK {
			t.Errorf("Run(%q) corrió con soloDatos", ddl)
		}
	}
	if res := q.Run(ctx, "r3", "update "+tabla+" set nombre = 'x' where id = 1"); !res.OK {
		t.Fatalf("Run() de un UPDATE con soloDatos falló: %+v", res.Failure)
	}
	if res := q.Run(ctx, "r4", "select count(*) from "+tabla); !res.OK {
		t.Fatalf("Run() de un SELECT con soloDatos falló: %+v", res.Failure)
	}

	// Y en esta plataforma el default es el de escritorio: apagado. El de
	// Android lo fija solodatos_android.go, que acá no compila.
	if soloDatosEnEstaPlataforma {
		t.Fatal("soloDatosEnEstaPlataforma es true fuera de Android")
	}
}

func (s *Session) mustChangeset(t *testing.T) ChangesetView {
	t.Helper()
	v, err := s.Changeset(context.Background())
	if err != nil {
		t.Fatalf("Changeset(): %v", err)
	}
	return v
}
