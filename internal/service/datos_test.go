package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Los cuatro motores. Acá no hay columna «puede»: editar filas tiene la misma
// garantía en los cuatro, que es todo el punto de estos tests.
var motoresDeDatos = []struct {
	nombre string
	uri    string
}{
	{"postgres", "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable"},
	{"mysql", "mysql://kaname:kaname@127.0.0.1:53306/kaname_test"},
	{"mariadb", "mariadb://kaname:kaname@127.0.0.1:53307/kaname_test"},
	{"sqlite", ""},
}

func texto(s string) *string { return &s }

// tablaDeDatos crea una tabla con clave y dos filas —(1, 'uno') y (2, 'dos')—
// y la borra al terminar. Con `conClave` en false la crea SIN clave primaria y
// con las dos filas compartiendo el id, para el caso de la clave que no es
// clave.
func tablaDeDatos(t *testing.T, sesion *Session, c connection.Connection, nombre string, conClave bool) (esq, tabla string) {
	t.Helper()
	ctx := context.Background()
	esq = esquemaDeApply(t, sesion, c)
	tabla = nombre
	nom := califica(c, esq, tabla)
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta(): %v", err)
	}
	limpiar := func() { _ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+nom) }
	limpiar()
	t.Cleanup(limpiar)

	clave := " PRIMARY KEY"
	segunda := "2"
	if !conClave {
		clave, segunda = "", "1"
	}
	if err := abierta.db.Exec(ctx, fmt.Sprintf(
		"CREATE TABLE %s (id %s%s, nombre %s)", nom, tipoEnteroDe(c.Engine), clave, tipoTextoDe(c.Engine))); err != nil {
		t.Fatalf("crear la tabla: %v", err)
	}
	if err := abierta.db.Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, nombre) VALUES (1, 'uno'), (%s, 'dos')", nom, segunda)); err != nil {
		t.Fatalf("cargar las filas: %v", err)
	}
	return esq, tabla
}

func tipoTextoDe(e connection.Engine) string {
	if e == connection.MySQL || e == connection.MariaDB {
		return "varchar(64)"
	}
	return "text"
}

// filas lee la tabla ordenada por id y la devuelve como "id=nombre" por fila.
func filas(t *testing.T, sesion *Session, c connection.Connection, esq, tabla string) []string {
	t.Helper()
	return filasCon(t, sesion, c, esq, tabla, "nombre")
}

// filasCon es filas con otra segunda columna.
func filasCon(t *testing.T, sesion *Session, c connection.Connection, esq, tabla, col string) []string {
	t.Helper()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta(): %v", err)
	}
	b, f := abierta.db.Run(context.Background(),
		"SELECT id, "+col+" FROM "+califica(c, esq, tabla)+" ORDER BY 1, 2", engine.RunOptions{})
	if f != nil {
		t.Fatalf("leer la tabla: %s", f.Message)
	}
	var out []string
	for _, fila := range b.Results[0].Rows {
		nombre := "NULL"
		if fila[1] != nil {
			nombre = *fila[1]
		}
		out = append(out, *fila[0]+"="+nombre)
	}
	return out
}

func igualesFilas(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestEditarFilasEsUnSoloTramoEnLosCuatroMotores.
//
// Es la promesa de la iteración: insertar, actualizar y borrar desde la
// grilla es UNA transacción en Postgres, MySQL, MariaDB y SQLite, porque el
// DML de InnoDB sí es transaccional. La casilla de S15 no miente para datos.
func TestEditarFilasEsUnSoloTramoEnLosCuatroMotores(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla", true)

			vs, err := sesion.StageMany(ctx, []change.Change{
				{Type: change.InsertRow, Schema: esq, Table: tabla, Source: "grid",
					Values: []change.Cell{{Column: "id", Value: texto("3")}, {Column: "nombre", Value: texto("tres")}}},
				{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
					Values:   []change.Cell{{Column: "nombre", Value: texto("uno editado")}},
					Key:      []change.Cell{{Column: "id", Value: texto("1")}},
					Previous: []change.Cell{{Column: "nombre", Value: texto("uno")}}},
				{Type: change.DeleteRow, Schema: esq, Table: tabla, Source: "grid",
					Key: []change.Cell{{Column: "id", Value: texto("2")}}},
			}, "")
			if err != nil {
				t.Fatalf("StageMany(): %v", err)
			}
			if len(vs) != 3 {
				t.Fatalf("StageMany devolvió %d vistas", len(vs))
			}
			for _, v := range vs {
				if v.Statement.Impact != change.ImpactData {
					t.Errorf("%s: Impact = %q", v.Change.Type, v.Statement.Impact)
				}
			}

			vista, err := sesion.Changeset(ctx)
			if err != nil {
				t.Fatalf("Changeset(): %v", err)
			}
			if vista.Summary.Data != 3 || vista.Summary.Schema != 0 || vista.Summary.Destructive != 1 {
				t.Errorf("Summary = %+v; se esperaban 3 de datos, 0 de esquema, 1 destructivo", vista.Summary)
			}
			if vista.Tramos != 1 {
				t.Errorf("%s: un changeset de puros datos va en %d tramos y tiene que ir en 1", caso.nombre, vista.Tramos)
			}
			// La vista previa muestra los valores escritos, nunca marcadores.
			for _, v := range vista.Order {
				if strings.Contains(v.Statement.SQL, "$1") || strings.Contains(v.Statement.SQL, "?") {
					t.Errorf("la SQL legible tiene marcadores: %s", v.Statement.SQL)
				}
			}
			if !strings.Contains(vista.Script, "uno editado") {
				t.Errorf("el guion no muestra el valor nuevo:\n%s", vista.Script)
			}

			res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
			if err != nil {
				t.Fatalf("Apply(): %v", err)
			}
			if !res.OK {
				t.Fatalf("Apply falló: %+v", res.Failure)
			}
			if res.Tramos != 1 {
				t.Errorf("Apply corrió en %d tramos", res.Tramos)
			}
			quiero := []string{"1=uno editado", "3=tres"}
			if got := filas(t, sesion, c, esq, tabla); !igualesFilas(got, quiero) {
				t.Errorf("la tabla quedó %v, se esperaba %v", got, quiero)
			}
			// Lo aplicado sale del changeset.
			if vista, _ := sesion.Changeset(ctx); vista.Summary.Total != 0 {
				t.Errorf("quedaron %d cambios en el changeset después de aplicar", vista.Summary.Total)
			}
		})
	}
}

// TestUnaFilaQueOtroBorroRevierteLaTandaEntera.
//
// La grilla leyó la fila 2, alguien la borró desde otra sesión, y el usuario
// la edita. El UPDATE por clave corre sin error y no toca nada: sin la
// comprobación de filas, el apply diría «aplicado» sobre una fila que no
// existe. Con ella, falla — y como el tramo es transaccional, la edición de la
// fila 1, que sí estaba bien, TAMBIÉN se revierte. En los cuatro.
func TestUnaFilaQueOtroBorroRevierteLaTandaEntera(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_ajena", true)

			if _, err := sesion.StageMany(ctx, []change.Change{
				{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
					Values: []change.Cell{{Column: "nombre", Value: texto("uno editado")}},
					Key:    []change.Cell{{Column: "id", Value: texto("1")}}},
				{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
					Values: []change.Cell{{Column: "nombre", Value: texto("dos editado")}},
					Key:    []change.Cell{{Column: "id", Value: texto("2")}}},
			}, ""); err != nil {
				t.Fatalf("StageMany(): %v", err)
			}

			// Otra sesión borra la fila 2.
			abierta, _ := sesion.abierta()
			if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)+" WHERE id = 2"); err != nil {
				t.Fatalf("borrar por afuera: %v", err)
			}

			res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
			if err != nil {
				t.Fatalf("Apply(): %v", err)
			}
			if res.OK {
				t.Fatal("Apply dijo OK con una fila que ya no existe")
			}
			if res.Failure == nil || res.Failure.Kind != engine.FailureData {
				t.Fatalf("Failure = %+v; se esperaba uno de datos", res.Failure)
			}
			if !strings.Contains(res.Failure.Message, "ya no está") {
				t.Errorf("el mensaje no explica qué pasó: %q", res.Failure.Message)
			}
			if !res.RolledBack {
				t.Error("RolledBack = false: la edición de la fila 1 quedó sin que el usuario lo sepa")
			}
			if len(res.Results) != 2 || res.Results[0].Applied {
				t.Errorf("la primera sentencia figura aplicada y se revirtió: %+v", res.Results)
			}
			quiero := []string{"1=uno"}
			if got := filas(t, sesion, c, esq, tabla); !igualesFilas(got, quiero) {
				t.Errorf("%s: la tabla quedó %v, se esperaba %v — el tramo no se revirtió", caso.nombre, got, quiero)
			}
			// Y las dos ediciones siguen pendientes: nada se aplicó, nada se olvida.
			if vista, _ := sesion.Changeset(ctx); vista.Summary.Total != 2 {
				t.Errorf("quedaron %d cambios pendientes y tenían que quedar 2", vista.Summary.Total)
			}
		})
	}
}

// TestUnaClaveQueAlcanzaDosFilasNoSigue.
//
// La otra mitad de la comprobación. Una tabla sin clave primaria no se edita
// desde la grilla, pero el servicio no puede confiar en que la pantalla lo
// recuerde: si un UPDATE por «clave» alcanza dos filas, la segunda se
// pisaría en silencio. Se corta y se revierte.
func TestUnaClaveQueAlcanzaDosFilasNoSigue(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_sinclave", false)

			if _, err := sesion.Stage(ctx, change.Change{
				Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
				Values: []change.Cell{{Column: "nombre", Value: texto("pisado")}},
				Key:    []change.Cell{{Column: "id", Value: texto("1")}},
			}, ""); err != nil {
				t.Fatalf("Stage(): %v", err)
			}
			res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
			if err != nil {
				t.Fatalf("Apply(): %v", err)
			}
			if res.OK {
				t.Fatal("Apply dijo OK con un UPDATE que alcanzó dos filas")
			}
			if res.Failure == nil || !strings.Contains(res.Failure.Message, "2 filas") {
				t.Errorf("el mensaje no dice cuántas alcanzó: %+v", res.Failure)
			}
			quiero := []string{"1=dos", "1=uno"}
			if got := filas(t, sesion, c, esq, tabla); !igualesFilas(got, quiero) {
				t.Errorf("%s: la tabla quedó %v; el UPDATE de dos filas no se revirtió", caso.nombre, got)
			}
		})
	}
}

// TestElEnsayoVeUnaFilaQueYaNoEsta.
//
// El ensayo de S15 sirve para datos igual que para esquema: la fila que otro
// borró se descubre ensayando, sin aplicar nada.
func TestElEnsayoVeUnaFilaQueYaNoEsta(t *testing.T) {
	for _, caso := range motoresDeDatos {
		if caso.nombre == "mysql" || caso.nombre == "mariadb" {
			// El ensayo exige DDL transaccional y estos no lo tienen. Que un
			// changeset de puros datos podría ensayarse ahí es cierto, y está
			// anotado en el plan como pendiente.
			continue
		}
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_ensayo", true)

			if _, err := sesion.Stage(ctx, change.Change{
				Type: change.DeleteRow, Schema: esq, Table: tabla, Source: "grid",
				Key: []change.Cell{{Column: "id", Value: texto("2")}},
			}, ""); err != nil {
				t.Fatalf("Stage(): %v", err)
			}
			abierta, _ := sesion.abierta()
			if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)+" WHERE id = 2"); err != nil {
				t.Fatalf("borrar por afuera: %v", err)
			}
			res, err := sesion.DryRun(ctx, "")
			if err != nil {
				t.Fatalf("DryRun(): %v", err)
			}
			if res.OK {
				t.Fatal("el ensayo dijo que va a andar sobre una fila que no existe")
			}
			if res.Failure == nil || res.Failure.Kind != engine.FailureData {
				t.Errorf("Failure = %+v", res.Failure)
			}
		})
	}
}

// TestBorrarUnaFilaEnProduccionPideConfirmacionYEditarlaNo.
//
// Borrar una fila es destructivo —no vuelve—, así que contra producción entra
// al changeset con la misma confirmación que borrar una columna. Editarla no:
// el valor viejo está en Previous y se puede volver a escribir.
func TestBorrarUnaFilaEnProduccionPideConfirmacionYEditarlaNo(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_prod", true)
	abierta, _ := sesion.abierta()
	abierta.conn.Environment = connection.Production

	borrar := change.Change{Type: change.DeleteRow, Schema: esq, Table: tabla, Source: "grid",
		Key: []change.Cell{{Column: "id", Value: texto("2")}}}
	editar := change.Change{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
		Values: []change.Cell{{Column: "nombre", Value: texto("x")}},
		Key:    []change.Cell{{Column: "id", Value: texto("1")}}}

	// Una tanda con un borrado adentro se rechaza ENTERA sin la palabra.
	if _, err := sesion.StageMany(ctx, []change.Change{editar, borrar}, ""); !errors.Is(err, ErrNeedsConfirmation) {
		t.Fatalf("se esperaba ErrNeedsConfirmation y llegó %v", err)
	}
	if vista, _ := sesion.Changeset(ctx); vista.Summary.Total != 0 {
		t.Fatalf("entraron %d cambios de una tanda rechazada", vista.Summary.Total)
	}
	// Editar sola no la necesita.
	if _, err := sesion.StageMany(ctx, []change.Change{editar}, ""); err != nil {
		t.Fatalf("editar una fila en producción no es destructivo: %v", err)
	}
	// Y con la palabra correcta el borrado entra.
	if _, err := sesion.StageMany(ctx, []change.Change{borrar}, nombreDeLaBase(abierta)); err != nil {
		t.Fatalf("con la confirmación correcta: %v", err)
	}
}

// TestPrepararVariosEsTodoONada.
func TestPrepararVariosEsTodoONada(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_tanda", true)

	_, err := sesion.StageMany(ctx, []change.Change{
		{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
			Values: []change.Cell{{Column: "nombre", Value: texto("x")}},
			Key:    []change.Cell{{Column: "id", Value: texto("1")}}},
		// Sin clave: no valida.
		{Type: change.DeleteRow, Schema: esq, Table: tabla, Source: "grid"},
	}, "")
	if err == nil {
		t.Fatal("una tanda con un cambio inválido tiene que rechazarse")
	}
	if vista, _ := sesion.Changeset(ctx); vista.Summary.Total != 0 {
		t.Errorf("el cambio válido entró solo: quedaron %d", vista.Summary.Total)
	}
}

// ejecutorFalso anota qué camino se usó.
type ejecutorFalso struct {
	exec   []string
	modify []string
	args   [][]any
	filas  int64
}

func (e *ejecutorFalso) Exec(_ context.Context, sql string) error {
	e.exec = append(e.exec, sql)
	return nil
}

func (e *ejecutorFalso) Modify(_ context.Context, sql string, args []any) (int64, error) {
	e.modify = append(e.modify, sql)
	e.args = append(e.args, args)
	return e.filas, nil
}

// TestLoQueCorreEsLaFormaConParametrosYNoLaLegible.
//
// Desde afuera no se distingue: las dos formas dejan la misma fila. Por eso
// se mira el camino y no el resultado. Si esto pasa a correr la SQL legible,
// los tests contra los motores seguirían verdes y CLAUDE.md dejaría de ser
// verdad.
func TestLoQueCorreEsLaFormaConParametrosYNoLaLegible(t *testing.T) {
	ctx := context.Background()
	st := change.Statement{
		SQL:   `UPDATE "t" SET "a" = 'x' WHERE "id" = '1'`,
		Bound: &change.Bound{SQL: `UPDATE "t" SET "a" = $1 WHERE "id" = $2`, Args: []any{texto("x"), texto("1")}, Rows: 1},
	}
	ej := &ejecutorFalso{filas: 1}
	if err := ejecutar(ctx, ej, st); err != nil {
		t.Fatalf("ejecutar(): %v", err)
	}
	if len(ej.exec) != 0 {
		t.Errorf("un cambio de datos corrió por Exec, con los valores adentro del texto: %v", ej.exec)
	}
	if len(ej.modify) != 1 || ej.modify[0] != st.Bound.SQL || len(ej.args[0]) != 2 {
		t.Errorf("Modify recibió %v con %v", ej.modify, ej.args)
	}

	// Un DDL va por Exec, tal cual.
	ej = &ejecutorFalso{}
	if err := ejecutar(ctx, ej, change.Statement{SQL: "DROP TABLE t"}); err != nil {
		t.Fatalf("ejecutar(DDL): %v", err)
	}
	if len(ej.exec) != 1 || len(ej.modify) != 0 {
		t.Errorf("el DDL fue por Modify: exec=%v modify=%v", ej.exec, ej.modify)
	}

	// Y el conteo se exige.
	for _, n := range []int64{0, 2} {
		ej = &ejecutorFalso{filas: n}
		err := ejecutar(ctx, ej, st)
		if !errors.Is(err, ErrRowCount) {
			t.Errorf("con %d filas alcanzadas se esperaba ErrRowCount y llegó %v", n, err)
		}
	}
}

// TestEnSQLiteUnaReconstruccionApagaLaCascadaDeLosBorradosDeLaMismaTanda.
//
// La reconstrucción de una tabla corre con las claves foráneas apagadas
// —es la única forma de no disparar los ON DELETE CASCADE de las hijas— y los
// cambios de datos van en la misma transacción. Un borrado de padre que
// depende de la cascada no arrastra nada, deja huérfanas, y el
// foreign_key_check del cierre rechaza todo. No es silencioso y no deja nada a
// medias; pero hay que avisarlo, porque el mismo borrado aplicado solo anda.
func TestEnSQLiteUnaReconstruccionApagaLaCascadaDeLosBorradosDeLaMismaTanda(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq := esquemaDeApply(t, sesion, c)
	abierta, _ := sesion.abierta()

	for _, sql := range []string{
		`DROP TABLE IF EXISTS "kn_hija"`, `DROP TABLE IF EXISTS "kn_padre"`, `DROP TABLE IF EXISTS "kn_otra"`,
		`CREATE TABLE "kn_padre" (id integer PRIMARY KEY)`,
		`CREATE TABLE "kn_hija" (id integer PRIMARY KEY, padre_id integer REFERENCES "kn_padre"(id) ON DELETE CASCADE)`,
		`CREATE TABLE "kn_otra" (id integer PRIMARY KEY, n integer)`,
		`INSERT INTO "kn_padre" VALUES (1)`, `INSERT INTO "kn_hija" VALUES (10, 1)`,
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	t.Cleanup(func() {
		for _, tabla := range []string{"kn_hija", "kn_padre", "kn_otra"} {
			_ = abierta.db.Exec(context.Background(), `DROP TABLE IF EXISTS "`+tabla+`"`)
		}
	})

	borrar := change.Change{Type: change.DeleteRow, Schema: esq, Table: "kn_padre", Source: "grid",
		Key: []change.Cell{{Column: "id", Value: texto("1")}}}
	if _, err := sesion.StageMany(ctx, []change.Change{
		// SET NOT NULL reconstruye la tabla en SQLite.
		{Type: change.SetNotNull, Schema: esq, Table: "kn_otra", Source: "structure",
			Column: &change.Column{Name: "n", DataType: "integer"}},
		borrar,
	}, ""); err != nil {
		t.Fatalf("StageMany(): %v", err)
	}

	vista, err := sesion.Changeset(ctx)
	if err != nil {
		t.Fatalf("Changeset(): %v", err)
	}
	avisado := false
	for _, w := range vista.Warnings {
		if strings.Contains(w, "claves foráneas están apagadas") {
			avisado = true
		}
	}
	if !avisado {
		t.Errorf("no avisa que la reconstrucción apaga la cascada de los borrados: %v", vista.Warnings)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil {
		t.Fatalf("Apply(): %v", err)
	}
	if res.OK {
		t.Fatal("aplicó un borrado que dejó huérfanas: la cascada no corrió y nadie lo vio")
	}
	if !res.RolledBack {
		t.Error("no se revirtió: algo quedó a medias")
	}
	// Nada a medias: el padre sigue, la hija sigue, la columna sigue nula.
	if got := filasCon(t, sesion, c, esq, "kn_hija", "padre_id"); !igualesFilas(got, []string{"10=1"}) {
		t.Errorf("kn_hija quedó %v", got)
	}

	// El mismo borrado, solo, anda y arrastra a la hija.
	if err := sesion.DiscardChanges(); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(ctx, borrar, ""); err != nil {
		t.Fatalf("Stage(): %v", err)
	}
	res, err = sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil || !res.OK {
		t.Fatalf("el borrado solo tendría que andar: err=%v res=%+v", err, res.Failure)
	}
	if got := filasCon(t, sesion, c, esq, "kn_hija", "padre_id"); len(got) != 0 {
		t.Errorf("la cascada no borró a la hija: %v", got)
	}
	if res.SchemaChanged {
		t.Error("un apply de puros datos no cambia el esquema")
	}
}

// TestUnApplyDePurosDatosNoTiraElSnapshot.
//
// Editar una celda no cambia el árbol, y volver a leer el catálogo entero por
// cada celda es justo lo que CLAUDE.md prohíbe. Un cambio de esquema sí lo
// invalida.
func TestUnApplyDePurosDatosNoTiraElSnapshot(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_grilla_snap", true)
	abierta, _ := sesion.abierta()

	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.Stage(ctx, change.Change{Type: change.UpdateRow, Schema: esq, Table: tabla, Source: "grid",
		Values: []change.Cell{{Column: "nombre", Value: texto("x")}},
		Key:    []change.Cell{{Column: "id", Value: texto("1")}}}, ""); err != nil {
		t.Fatal(err)
	}
	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil || !res.OK {
		t.Fatalf("Apply(): err=%v res=%+v", err, res.Failure)
	}
	if res.SchemaChanged {
		t.Error("SchemaChanged = true con un UPDATE")
	}
	if abierta.snapshot == nil {
		t.Error("el snapshot se tiró por un cambio de datos")
	}

	if _, err := sesion.Stage(ctx, change.Change{Type: change.AddColumn, Schema: esq, Table: tabla, Source: "structure",
		Column: &change.Column{Name: "extra", DataType: "text", Nullable: true}}, ""); err != nil {
		t.Fatal(err)
	}
	res, err = sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
	if err != nil || !res.OK {
		t.Fatalf("Apply(): err=%v res=%+v", err, res.Failure)
	}
	if !res.SchemaChanged {
		t.Error("SchemaChanged = false con un ADD COLUMN")
	}
	if abierta.snapshot != nil {
		t.Error("el snapshot sigue después de un cambio de esquema")
	}
}

// Un INSERT que alcanzó cero filas no es «la fila ya no está»: nunca entró.
// El mensaje tiene que decir eso y no culpar a otra sesión.
func TestUnInsertQueNoInsertoSeExplicaComoTal(t *testing.T) {
	ctx := context.Background()
	ins := change.Statement{Bound: &change.Bound{SQL: "INSERT INTO t (a) VALUES ($1)", Args: []any{texto("x")}, Rows: 1, Op: change.OpInsert}}
	err := ejecutar(ctx, &ejecutorFalso{filas: 0}, ins)
	if !errors.Is(err, ErrRowCount) || !strings.Contains(err.Error(), "no insertó") || strings.Contains(err.Error(), "ya no está") {
		t.Errorf("mensaje del INSERT vacío: %v", err)
	}
	upd := change.Statement{Bound: &change.Bound{SQL: "UPDATE t SET a = $1", Args: []any{texto("x")}, Rows: 1, Op: change.OpUpdate}}
	err = ejecutar(ctx, &ejecutorFalso{filas: 0}, upd)
	if !errors.Is(err, ErrRowCount) || !strings.Contains(err.Error(), "ya no está") {
		t.Errorf("mensaje del UPDATE vacío: %v", err)
	}
}
