package service

import (
	"context"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

func celdasTexto(cs []change.Cell) string {
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(c.Column + "=")
		if c.Value == nil {
			b.WriteString("NULL")
		} else {
			b.WriteString(*c.Value)
		}
	}
	return b.String()
}

// TestLaGrillaSeConvierteEnCambiosEnElOrdenDeSusColumnas.
//
// El mapa de TypeScript no tiene orden; la sentencia sí tiene que tenerlo. Y
// la clave y los valores viejos salen de la fila LEÍDA, no de lo editado: si
// se edita la clave misma, el WHERE lleva la de antes.
func TestLaGrillaSeConvierteEnCambiosEnElOrdenDeSusColumnas(t *testing.T) {
	cs, err := cambiosDeGrilla(GridEdits{
		Schema: "p", Table: "t",
		Columns: []string{"id", "nombre", "apodo", "edad"},
		Key:     []string{"id"},
		Updates: []RowEdit{{
			Before: []*string{texto("7"), texto("juan"), nil, texto("30")},
			// Desordenado a propósito, y con la clave editada.
			After: map[string]*string{"edad": texto("31"), "id": texto("8"), "apodo": texto("juanci")},
		}},
		Deletes: [][]*string{{texto("9"), texto("ana"), texto("anita"), nil}},
		Inserts: []NewRow{{Values: map[string]*string{"nombre": texto("nueva"), "id": nil}}},
	})
	if err != nil {
		t.Fatalf("cambiosDeGrilla(): %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("salieron %d cambios y se esperaban 3", len(cs))
	}

	u := cs[0]
	if u.Type != change.UpdateRow || u.Source != "grid" || u.Schema != "p" {
		t.Errorf("update = %+v", u)
	}
	if got := celdasTexto(u.Values); got != "id=8 apodo=juanci edad=31" {
		t.Errorf("Values en orden de grilla: %q", got)
	}
	if got := celdasTexto(u.Key); got != "id=7" {
		t.Errorf("la clave tiene que ser la LEÍDA, no la editada: %q", got)
	}
	if got := celdasTexto(u.Previous); got != "id=7 nombre=juan apodo=NULL edad=30" {
		t.Errorf("Previous = %q", got)
	}

	d := cs[1]
	if d.Type != change.DeleteRow || celdasTexto(d.Key) != "id=9" {
		t.Errorf("delete = %+v", d)
	}
	if got := celdasTexto(d.Previous); got != "id=9 nombre=ana apodo=anita edad=NULL" {
		t.Errorf("el borrado guarda la fila entera para la revisión: %q", got)
	}

	i := cs[2]
	if i.Type != change.InsertRow || celdasTexto(i.Values) != "id=NULL nombre=nueva" {
		t.Errorf("insert = %+v (%s)", i, celdasTexto(i.Values))
	}
	if len(i.Key) != 0 {
		t.Errorf("una fila nueva no tiene clave que identificar")
	}
}

func TestLaGrillaRechazaLoQueNoPuedeIdentificar(t *testing.T) {
	fila := []*string{texto("1"), texto("a")}
	casos := []struct {
		nombre string
		e      GridEdits
		quiero string
	}{
		{"sin clave", GridEdits{Table: "t", Columns: []string{"id", "n"},
			Deletes: [][]*string{fila}}, "clave primaria"},
		{"clave que no está en la grilla", GridEdits{Table: "t", Columns: []string{"id", "n"}, Key: []string{"uuid"},
			Deletes: [][]*string{fila}}, `"uuid"`},
		{"fila con menos valores que columnas", GridEdits{Table: "t", Columns: []string{"id", "n", "x"}, Key: []string{"id"},
			Deletes: [][]*string{fila}}, "2 valores"},
		{"columna editada que no existe", GridEdits{Table: "t", Columns: []string{"id", "n"}, Key: []string{"id"},
			Updates: []RowEdit{{Before: fila, After: map[string]*string{"zzz": texto("1")}}}}, `"zzz"`},
		{"edición vacía", GridEdits{Table: "t", Columns: []string{"id", "n"}, Key: []string{"id"},
			Updates: []RowEdit{{Before: fila, After: map[string]*string{}}}}, "ningún valor"},
		{"nada que preparar", GridEdits{Table: "t", Columns: []string{"id", "n"}, Key: []string{"id"}}, "ninguna edición"},
		{"columna repetida", GridEdits{Table: "t", Columns: []string{"id", "id"}, Key: []string{"id"},
			Deletes: [][]*string{fila}}, "dos veces"},
	}
	for _, tc := range casos {
		_, err := cambiosDeGrilla(tc.e)
		if err == nil {
			t.Errorf("%s: tendría que fallar", tc.nombre)
			continue
		}
		if !strings.Contains(err.Error(), tc.quiero) {
			t.Errorf("%s: el error no dice %q: %v", tc.nombre, tc.quiero, err)
		}
	}
}

// TestStageGridPreparaTodoOLoRechazaEntero llega hasta la base: la tanda
// entera entra al changeset y se aplica como un solo tramo.
func TestStageGridPreparaTodoOLoRechazaEntero(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_stage", true)

	vs, err := sesion.StageGrid(ctx, GridEdits{
		Schema: esq, Table: tabla, Columns: []string{"id", "nombre"}, Key: []string{"id"},
		Updates: []RowEdit{{Before: []*string{texto("1"), texto("uno")}, After: map[string]*string{"nombre": texto("uno!")}}},
		Deletes: [][]*string{{texto("2"), texto("dos")}},
		Inserts: []NewRow{{Values: map[string]*string{"id": texto("3"), "nombre": nil}}},
	}, "")
	if err != nil {
		t.Fatalf("StageGrid(): %v", err)
	}
	if len(vs) != 3 {
		t.Fatalf("StageGrid devolvió %d vistas", len(vs))
	}
	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil || !res.OK {
		t.Fatalf("Apply(): err=%v res=%+v", err, res.Failure)
	}
	if got := filas(t, sesion, c, esq, tabla); !igualesFilas(got, []string{"1=uno!", "3=NULL"}) {
		t.Errorf("la tabla quedó %v", got)
	}

	// Una tanda con una fila que no cierra no deja entrar nada.
	_, err = sesion.StageGrid(ctx, GridEdits{
		Schema: esq, Table: tabla, Columns: []string{"id", "nombre"}, Key: []string{"id"},
		Updates: []RowEdit{
			{Before: []*string{texto("1"), texto("uno!")}, After: map[string]*string{"nombre": texto("x")}},
			{Before: []*string{texto("1")}, After: map[string]*string{"nombre": texto("y")}},
		},
	}, "")
	if err == nil {
		t.Fatal("una fila con menos valores que columnas tendría que rechazar la tanda")
	}
	if vista, _ := sesion.Changeset(ctx); vista.Summary.Total != 0 {
		t.Errorf("entraron %d cambios de una tanda rechazada", vista.Summary.Total)
	}
}

// TestStageGridRechazaUnaClaveQueNoEsLaPrimaria.
//
// La grilla dice por qué columnas identifica la fila; el servicio no le cree:
// la compara contra la clave primaria del catálogo. Una columna que no es
// clave identifica más filas de las que debe.
func TestStageGridRechazaUnaClaveQueNoEsLaPrimaria(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_clave", true)

	_, err := sesion.StageGrid(ctx, GridEdits{
		Schema: esq, Table: tabla, Columns: []string{"id", "nombre"}, Key: []string{"nombre"},
		Deletes: [][]*string{{texto("1"), texto("uno")}},
	}, "")
	if err == nil {
		t.Fatal("aceptó «nombre» como clave de una tabla cuya clave es «id»")
	}
	if !strings.Contains(err.Error(), "clave primaria") {
		t.Errorf("el error no explica: %v", err)
	}
	if vista, _ := sesion.Changeset(ctx); vista.Summary.Total != 0 {
		t.Errorf("entraron %d cambios con una clave equivocada", vista.Summary.Total)
	}
	// Un alta no identifica ninguna fila, así que no necesita clave.
	if _, err := sesion.StageGrid(ctx, GridEdits{
		Schema: esq, Table: tabla, Columns: []string{"id", "nombre"}, Key: []string{"id"},
		Inserts: []NewRow{{Values: map[string]*string{"id": texto("9")}}},
	}, ""); err != nil {
		t.Fatalf("un alta con la clave correcta: %v", err)
	}
}
