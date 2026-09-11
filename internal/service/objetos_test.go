package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// TestElEsquemaTraeLosObjetosQueNoSonTablas.
//
// Es lo que hace posible el árbol de S05: sin esto, un esquema con diez vistas
// se ve exactamente igual que uno que no tiene ninguna.
//
// Se corre contra los cuatro motores porque lo que cada uno PUEDE tener es
// distinto —SQLite no tiene funciones ni enums— pero lo que se exige es lo
// mismo: que lo que existe aparezca, y que las tablas NO aparezcan acá.
func TestElEsquemaTraeLosObjetosQueNoSonTablas(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)
			abierta, err := sesion.abierta()
			if err != nil {
				t.Fatalf("abierta(): %v", err)
			}

			tabla := califica(c, esq, "kn_obj_t")
			vista := califica(c, esq, "kn_obj_v")
			ejecutar := func(sql string) {
				t.Helper()
				if err := abierta.db.Exec(ctx, sql); err != nil {
					t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
				}
			}
			_ = abierta.db.Exec(ctx, "DROP VIEW IF EXISTS "+vista)
			_ = abierta.db.Exec(ctx, "DROP TABLE IF EXISTS "+tabla)
			ejecutar(fmt.Sprintf("CREATE TABLE %s (id integer PRIMARY KEY)", tabla))
			ejecutar(fmt.Sprintf("CREATE VIEW %s AS SELECT id FROM %s", vista, tabla))
			t.Cleanup(func() {
				_ = abierta.db.Exec(context.Background(), "DROP VIEW IF EXISTS "+vista)
				_ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+tabla)
			})

			snap, err := sesion.Schema(ctx, true)
			if err != nil {
				t.Fatalf("Schema(): %v", err)
			}
			if snap.ObjectsError != "" {
				t.Fatalf("no se pudieron leer los objetos: %s", snap.ObjectsError)
			}

			var laVista *schema.Object
			for i := range snap.Schemas {
				for j := range snap.Schemas[i].Objects {
					o := &snap.Schemas[i].Objects[j]
					if o.Name == "kn_obj_v" && o.Kind == schema.ObjView {
						laVista = o
					}
					if o.Name == "kn_obj_t" {
						t.Errorf("la tabla apareció entre los objetos, como %q", o.Kind)
					}
				}
			}
			if laVista == nil {
				t.Fatal("la vista no llegó al snapshot: el árbol no la mostraría")
			}
		})
	}
}

// TestLosObjetosLleganOrdenadosPorClase: el árbol los agrupa por clase, así que
// vienen agrupados. Ordenarlos solo por nombre dejaría una vista entre dos
// funciones y obligaría a la UI a reordenar la misma lista otra vez.
func TestLosObjetosLleganOrdenadosPorClase(t *testing.T) {
	snap := &schema.Snapshot{Schemas: []schema.Schema{{
		Name: "demo",
		Objects: []schema.Object{
			{Kind: schema.ObjView, Name: "zeta"},
			{Kind: schema.ObjFunction, Name: "beta"},
			{Kind: schema.ObjView, Name: "alfa"},
			{Kind: schema.ObjFunction, Name: "alfa"},
		},
	}}}
	snap.Normalize()

	got := make([]string, 0, 4)
	for _, o := range snap.Schemas[0].Objects {
		got = append(got, string(o.Kind)+":"+o.Name)
	}
	quiero := []string{"function:alfa", "function:beta", "view:alfa", "view:zeta"}
	for i := range quiero {
		if got[i] != quiero[i] {
			t.Fatalf("el orden salió %v, se esperaba %v", got, quiero)
		}
	}
}

// TestSiElCatalogoNoSePuedeLeerElArbolSigueYLoDice.
//
// Las dos mitades importan y son opuestas:
//
//   - NO puede romper la lectura del esquema. Un «Conectar» que falla entero
//     porque una consulta al catálogo no se pudo leer es una regresión sobre lo
//     que venía funcionando: las tablas se leyeron bien y el árbol sirve.
//   - NO puede callarse. Sin el motivo a la vista, un esquema lleno de vistas
//     se ve idéntico a uno que no tiene ninguna, y nadie se entera de que lo
//     que está mirando está incompleto.
func TestSiElCatalogoNoSePuedeLeerElArbolSigueYLoDice(t *testing.T) {
	snap := &schema.Snapshot{Schemas: []schema.Schema{
		{Name: "demo", Tables: []schema.Table{{Name: "clientes"}}},
	}}
	conObjetos(context.Background(), catalogoRoto{}, snap)

	if len(snap.Schemas) != 1 || len(snap.Schemas[0].Tables) != 1 {
		t.Fatal("se perdieron las tablas: el árbol se quedaría vacío por un fallo que no lo afecta")
	}
	if snap.ObjectsError == "" {
		t.Fatal("el fallo quedó en silencio: un esquema con vistas se vería igual que uno sin ninguna")
	}
	if snap.Schemas[0].Objects != nil {
		t.Error("se inventaron objetos con el catálogo roto")
	}
}

// TestUnCatalogoRotoAMediasNoCuestaLoQueSiSeLeyo.
//
// `Objects` son VARIAS consultas al catálogo y una puede fallar sola: la de los
// eventos va última y en algunas variantes de MariaDB no se puede leer. Cortar
// ahí tiraba las vistas, las funciones y los triggers que ya se habían leído
// bien, y el árbol quedaba sin NADA por una consulta que no le importaba.
//
// Las dos mitades siguen siendo obligatorias: lo que se leyó se muestra, y lo
// que faltó se dice.
func TestUnCatalogoRotoAMediasNoCuestaLoQueSiSeLeyo(t *testing.T) {
	snap := &schema.Snapshot{Schemas: []schema.Schema{
		{Name: "demo", Tables: []schema.Table{{Name: "clientes"}}},
	}}
	conObjetos(context.Background(), catalogoAMedias{}, snap)

	if len(snap.Schemas[0].Objects) != 1 {
		t.Fatalf("se perdieron los objetos que SÍ se leían: quedaron %d",
			len(snap.Schemas[0].Objects))
	}
	if snap.Schemas[0].Objects[0].Name != "v_ventas" {
		t.Errorf("llegó otro objeto: %q", snap.Schemas[0].Objects[0].Name)
	}
	if snap.ObjectsError == "" {
		t.Error("la parte que falló quedó en silencio")
	}
}

// catalogoRoto es una conexión cuyo único comportamiento es fallar al listar
// objetos. Los demás métodos no se llaman.
type catalogoRoto struct{ engine.Conn }

func (catalogoRoto) Objects(context.Context, []string) ([]schema.Object, error) {
	return nil, errors.New("permission denied for table pg_policy")
}

// catalogoAMedias devuelve lo que pudo leer JUNTO con el error de lo que no,
// que es el contrato de `Conn.Objects`.
type catalogoAMedias struct{ engine.Conn }

func (catalogoAMedias) Objects(context.Context, []string) ([]schema.Object, error) {
	return []schema.Object{{Kind: schema.ObjView, Schema: "demo", Name: "v_ventas"}},
		errors.New("leer los eventos del catálogo: Table 'information_schema.EVENTS' doesn't exist")
}

// TestAgregarUnValorDeEnumYUsarloEnElMismoChangeset.
//
// Es el caso que motiva poner el enum en la primera fase, y el que fallaba por
// ponerlo ahí: PostgreSQL agrega el valor dentro de la transacción pero NO lo
// deja usar hasta que ésta confirma. Con todo el changeset en un solo BEGIN
// —que es lo que la casilla «Una sola transacción» promete y cumple contra
// Postgres— el par «ADD VALUE» + «insertar una fila con ese valor» daba «unsafe
// use of new value» y revertía el changeset entero.
//
// Comprobado contra PostgreSQL 14, 16, 17 y 18: falla en las cuatro. La primera
// medición dijo que la 17 y la 18 estaban bien, y era un artefacto de mandar las
// sentencias en una sola cadena con `psql -c`; de a una fallan todas.
func TestAgregarUnValorDeEnumYUsarloEnElMismoChangeset(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq := esquemaDeApply(t, sesion, c)
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta(): %v", err)
	}

	ejecutar := func(sql string) {
		t.Helper()
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
		}
	}
	_ = abierta.db.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.kn_enum_t", esq))
	_ = abierta.db.Exec(ctx, fmt.Sprintf("DROP TYPE IF EXISTS %s.kn_humor", esq))
	ejecutar(fmt.Sprintf("CREATE TYPE %s.kn_humor AS ENUM ('bajo', 'alto')", esq))
	ejecutar(fmt.Sprintf("CREATE TABLE %s.kn_enum_t (id integer PRIMARY KEY)", esq))
	t.Cleanup(func() {
		_ = abierta.db.Exec(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s.kn_enum_t", esq))
		_ = abierta.db.Exec(context.Background(), fmt.Sprintf("DROP TYPE IF EXISTS %s.kn_humor", esq))
	})

	// El valor nuevo, y una columna que lo usa como default. Los dos en el
	// mismo apply y con una sola transacción pedida.
	if _, err := sesion.StageMany(ctx, []change.Change{
		{Type: change.AddEnumValue, Schema: esq, Name: "kn_humor", Value: "medio"},
		{
			Type: change.AddColumn, Schema: esq, Table: "kn_enum_t",
			Column: &change.Column{
				Name: "h", DataType: esq + ".kn_humor", Nullable: true, Default: "'medio'",
			},
		},
	}, ""); err != nil {
		t.Fatalf("StageMany(): %v", err)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil {
		t.Fatalf("Apply(): %v", err)
	}
	if !res.OK {
		t.Fatalf("el apply falló: %+v", res)
	}

	// Y quedaron las dos cosas.
	d, err := abierta.db.Detail(ctx, esq, "kn_enum_t")
	if err != nil {
		t.Fatalf("Detail(): %v", err)
	}
	var tiene bool
	for _, col := range d.Columns {
		if col.Name == "h" {
			tiene = true
		}
	}
	if !tiene {
		t.Error("la columna que usaba el valor nuevo no quedó")
	}
}
