package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
)

// familia arma padre ← hija (ON DELETE CASCADE) y padre ← estricta (ON DELETE
// RESTRICT), con el padre 1 con hijas y el padre 2 solo. La hija tiene además
// una columna NOT NULL sin default, para el alta que se olvida de cargarla.
func familia(t *testing.T, sesion *Session, c connection.Connection) (esq string) {
	t.Helper()
	ctx := context.Background()
	esq = esquemaDeApply(t, sesion, c)
	abierta, _ := sesion.abierta()
	q := func(tabla string) string { return califica(c, esq, tabla) }
	entero, texto := tipoEnteroDe(c.Engine), tipoTextoDe(c.Engine)

	limpiar := func() {
		for _, tabla := range []string{"kn_rev_estricta", "kn_rev_hija", "kn_rev_padre"} {
			_ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+q(tabla))
		}
	}
	limpiar()
	t.Cleanup(limpiar)

	for _, sql := range []string{
		fmt.Sprintf("CREATE TABLE %s (id %s PRIMARY KEY, nombre %s)", q("kn_rev_padre"), entero, texto),
		fmt.Sprintf("CREATE TABLE %s (id %s PRIMARY KEY, padre_id %s, titulo %s NOT NULL, "+
			"CONSTRAINT kn_rev_hija_fk FOREIGN KEY (padre_id) REFERENCES %s (id) ON DELETE CASCADE)",
			q("kn_rev_hija"), entero, entero, texto, q("kn_rev_padre")),
		fmt.Sprintf("CREATE TABLE %s (id %s PRIMARY KEY, padre_id %s, "+
			"CONSTRAINT kn_rev_estricta_fk FOREIGN KEY (padre_id) REFERENCES %s (id) ON DELETE RESTRICT)",
			q("kn_rev_estricta"), entero, entero, q("kn_rev_padre")),
		fmt.Sprintf("INSERT INTO %s (id, nombre) VALUES (1, 'con hijas'), (2, 'solo')", q("kn_rev_padre")),
		fmt.Sprintf("INSERT INTO %s (id, padre_id, titulo) VALUES (10, 1, 'a'), (11, 1, 'b')", q("kn_rev_hija")),
		fmt.Sprintf("INSERT INTO %s (id, padre_id) VALUES (20, 1)", q("kn_rev_estricta")),
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	return esq
}

func revisar(t *testing.T, sesion *Session, c change.Change) RowReview {
	t.Helper()
	ctx := context.Background()
	v, err := sesion.Stage(ctx, c, "")
	if err != nil {
		t.Fatalf("Stage(%s): %v", c.Type, err)
	}
	r, err := sesion.ReviewRow(ctx, v.Change.ID)
	if err != nil {
		t.Fatalf("ReviewRow(): %v", err)
	}
	if err := sesion.Unstage(v.Change.ID); err != nil {
		t.Fatal(err)
	}
	return r
}

// hay busca una comprobación por nivel y por un pedazo de su etiqueta.
func hay(r RowReview, level, etiqueta string) bool {
	for _, ck := range r.Checks {
		if ck.Level == level && strings.Contains(ck.Label, etiqueta) {
			return true
		}
	}
	return false
}

func describir(r RowReview) string {
	var b strings.Builder
	for _, ck := range r.Checks {
		fmt.Fprintf(&b, "\n  [%s] %s — %s", ck.Level, ck.Label, ck.Detail)
	}
	return b.String()
}

// TestLaRevisionDeUnBorradoCuentaLasHijasYDiceQueLesPasa.
//
// Es la comprobación que más vale antes de aplicar: un borrado con cascada se
// lleva filas que no están en pantalla, y uno con RESTRICT va a fallar. Las
// dos cosas se dicen con el número, y en los cuatro motores.
func TestLaRevisionDeUnBorradoCuentaLasHijasYDiceQueLesPasa(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			esq := familia(t, sesion, c)

			conHijas := revisar(t, sesion, change.Change{
				Type: change.DeleteRow, Schema: esq, Table: "kn_rev_padre", Source: "grid",
				Key: []change.Cell{{Column: "id", Value: texto("1")}},
			})
			if !hay(conHijas, "ok", "identifica una sola fila") {
				t.Errorf("falta la comprobación de la clave:%s", describir(conHijas))
			}
			if !hay(conHijas, "warn", "2 filas hijas en kn_rev_hija se borran también") {
				t.Errorf("no avisa la cascada:%s", describir(conHijas))
			}
			if !hay(conHijas, "bad", "1 fila hija en kn_rev_estricta: el borrado va a fallar") {
				t.Errorf("no avisa el RESTRICT:%s", describir(conHijas))
			}

			solo := revisar(t, sesion, change.Change{
				Type: change.DeleteRow, Schema: esq, Table: "kn_rev_padre", Source: "grid",
				Key: []change.Cell{{Column: "id", Value: texto("2")}},
			})
			for _, ck := range solo.Checks {
				if ck.Level != "ok" {
					t.Errorf("el padre sin hijas tiene algo que no es ok:%s", describir(solo))
					break
				}
			}

			fantasma := revisar(t, sesion, change.Change{
				Type: change.DeleteRow, Schema: esq, Table: "kn_rev_padre", Source: "grid",
				Key: []change.Cell{{Column: "id", Value: texto("999")}},
			})
			if !hay(fantasma, "bad", "ya no está") {
				t.Errorf("no dice que la fila no existe:%s", describir(fantasma))
			}
		})
	}
}

// TestLaRevisionDeUnAltaMiraLosPadresYLasColumnasObligatorias.
func TestLaRevisionDeUnAltaMiraLosPadresYLasColumnasObligatorias(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			esq := familia(t, sesion, c)

			// Padre inexistente y sin el título obligatorio.
			mala := revisar(t, sesion, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_hija", Source: "grid",
				Values: []change.Cell{{Column: "id", Value: texto("30")}, {Column: "padre_id", Value: texto("999")}},
			})
			if !hay(mala, "bad", "No existe kn_rev_padre con id = 999") {
				t.Errorf("no ve el padre que falta:%s", describir(mala))
			}
			if !hay(mala, "bad", "sin valor: titulo") {
				t.Errorf("no ve la columna NOT NULL sin valor:%s", describir(mala))
			}

			buena := revisar(t, sesion, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_hija", Source: "grid",
				Values: []change.Cell{{Column: "id", Value: texto("31")}, {Column: "padre_id", Value: texto("2")},
					{Column: "titulo", Value: texto("c")}},
			})
			for _, ck := range buena.Checks {
				if ck.Level != "ok" {
					t.Errorf("el alta correcta tiene algo que no es ok:%s", describir(buena))
					break
				}
			}
			if !hay(buena, "ok", "Existe kn_rev_padre con id = 2") {
				t.Errorf("no confirma el padre:%s", describir(buena))
			}

			// Un NULL en la clave foránea no apunta a nada: no se comprueba.
			nula := revisar(t, sesion, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_hija", Source: "grid",
				Values: []change.Cell{{Column: "id", Value: texto("32")}, {Column: "padre_id", Value: nil},
					{Column: "titulo", Value: texto("d")}},
			})
			if !hay(nula, "ok", "en NULL") {
				t.Errorf("la clave foránea nula tendría que ser ok:%s", describir(nula))
			}
		})
	}
}

// TestLaRevisionDeUnaEdicionVeLaClaveElPadreYLoQueNoCambia.
func TestLaRevisionDeUnaEdicionVeLaClaveElPadreYLoQueNoCambia(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			esq := familia(t, sesion, c)

			fila := []change.Cell{{Column: "id", Value: texto("10")}, {Column: "padre_id", Value: texto("1")}, {Column: "titulo", Value: texto("a")}}
			r := revisar(t, sesion, change.Change{
				Type: change.UpdateRow, Schema: esq, Table: "kn_rev_hija", Source: "grid",
				Values:   []change.Cell{{Column: "padre_id", Value: texto("999")}, {Column: "titulo", Value: texto("a")}},
				Key:      []change.Cell{{Column: "id", Value: texto("10")}},
				Previous: fila,
			})
			if !hay(r, "ok", "identifica una sola fila") || !hay(r, "bad", "No existe kn_rev_padre con id = 999") ||
				!hay(r, "warn", "titulo no cambia") {
				t.Errorf("faltan comprobaciones:%s", describir(r))
			}

			// NULL en una columna NOT NULL.
			nulo := revisar(t, sesion, change.Change{
				Type: change.UpdateRow, Schema: esq, Table: "kn_rev_hija", Source: "grid",
				Values: []change.Cell{{Column: "titulo", Value: nil}},
				Key:    []change.Cell{{Column: "id", Value: texto("10")}}, Previous: fila,
			})
			if !hay(nulo, "bad", "titulo no admite NULL") {
				t.Errorf("no ve el NULL en NOT NULL:%s", describir(nulo))
			}
			// Y las columnas vienen en el orden de la tabla, para dibujar la fila.
			if len(nulo.Columns) != 3 || nulo.Columns[0].Name != "id" || nulo.Columns[2].Name != "titulo" {
				t.Errorf("Columns = %+v", nulo.Columns)
			}
		})
	}
}

func TestLaRevisionSoloEsParaCambiosDeDatos(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	esq := familia(t, sesion, c)
	v, err := sesion.Stage(ctx, change.Change{Type: change.DropTable, Schema: esq, Table: "kn_rev_estricta", Source: "test"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sesion.ReviewRow(ctx, v.Change.ID); err == nil {
		t.Error("revisó un DROP TABLE como si fuera una fila")
	}
	if _, err := sesion.ReviewRow(ctx, "c999"); err == nil {
		t.Error("revisó un cambio que no existe")
	}
}

// TestLaRevisionCuentaLoQueLaMismaTandaHaceAntes.
//
// Las comprobaciones miran la base Y lo que corre antes en el mismo apply: el
// padre que se inserta dos filas arriba existe para el hijo, y las hijas que se
// borran antes ya no frenan el borrado del padre. Sin esto la revisión decía
// «va a fallar» sobre un apply que anda.
func TestLaRevisionCuentaLoQueLaMismaTandaHaceAntes(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := familia(t, sesion, c)

			// El padre nuevo primero, después el hijo que lo apunta.
			if _, err := sesion.Stage(ctx, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_padre", Source: "grid",
				Values: []change.Cell{{Column: "id", Value: texto("5")}, {Column: "nombre", Value: texto("nuevo")}},
			}, ""); err != nil {
				t.Fatal(err)
			}
			hijo, err := sesion.Stage(ctx, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_hija", Source: "grid",
				Values: []change.Cell{{Column: "id", Value: texto("50")}, {Column: "padre_id", Value: texto("5")},
					{Column: "titulo", Value: texto("x")}},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			r, err := sesion.ReviewRow(ctx, hijo.Change.ID)
			if err != nil {
				t.Fatal(err)
			}
			if hay(r, "bad", "No existe kn_rev_padre") || !hay(r, "ok", "lo pone esta misma tanda") {
				t.Errorf("el padre que inserta la tanda no cuenta:%s", describir(r))
			}

			// Un hijo EXCLUIDO cuyo padre se inserta DESPUÉS en la lista: lo que
			// corre después no cuenta, y que el hijo esté excluido no cambia eso.
			// Antes se recorría Ordered, que no tiene a los excluidos: el corte no
			// llegaba nunca y «antes» era la tanda entera.
			huerfano, err := sesion.Stage(ctx, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_hija", Source: "grid",
				Values: []change.Cell{{Column: "id", Value: texto("60")}, {Column: "padre_id", Value: texto("6")},
					{Column: "titulo", Value: texto("y")}},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := sesion.IncludeChange(huerfano.Change.ID, false); err != nil {
				t.Fatal(err)
			}
			if _, err := sesion.Stage(ctx, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_padre", Source: "grid",
				Values: []change.Cell{{Column: "id", Value: texto("6")}, {Column: "nombre", Value: texto("tarde")}},
			}, ""); err != nil {
				t.Fatal(err)
			}
			r, err = sesion.ReviewRow(ctx, huerfano.Change.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !hay(r, "bad", "No existe kn_rev_padre") || hay(r, "ok", "lo pone esta misma tanda") {
				t.Errorf("un padre que se inserta después contó como anterior:%s", describir(r))
			}

			// Borrar la hija estricta y después el padre 1: el RESTRICT ya no frena.
			if _, err := sesion.Stage(ctx, change.Change{
				Type: change.DeleteRow, Schema: esq, Table: "kn_rev_estricta", Source: "grid",
				Key:      []change.Cell{{Column: "id", Value: texto("20")}},
				Previous: []change.Cell{{Column: "id", Value: texto("20")}, {Column: "padre_id", Value: texto("1")}},
			}, ""); err != nil {
				t.Fatal(err)
			}
			padre, err := sesion.Stage(ctx, change.Change{
				Type: change.DeleteRow, Schema: esq, Table: "kn_rev_padre", Source: "grid",
				Key: []change.Cell{{Column: "id", Value: texto("1")}},
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			r, err = sesion.ReviewRow(ctx, padre.Change.ID)
			if err != nil {
				t.Fatal(err)
			}
			if hay(r, "bad", "el borrado va a fallar") || !hay(r, "ok", "Sin filas hijas en kn_rev_estricta al momento de borrar") {
				t.Errorf("la hija que la tanda borra antes sigue contando:%s", describir(r))
			}
			// Y la tanda entera aplica, que es lo que la revisión afirmó.
			res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
			if err != nil || !res.OK {
				t.Fatalf("Apply(): err=%v res=%+v", err, res.Failure)
			}
		})
	}
}

// TestEnSQLiteLaClaveEnteraSeAsignaSolaYLaRevisionLoSabe.
//
// `id INTEGER PRIMARY KEY` en SQLite es un alias del rowid: un alta que la
// omite recibe el número siguiente. La revisión no puede decir que la columna
// NOT NULL quedó sin valor. En los otros motores una clave sin default sí
// falla, y eso también se comprueba.
func TestEnSQLiteLaClaveEnteraSeAsignaSolaYLaRevisionLoSabe(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			esq := familia(t, sesion, c)
			r := revisar(t, sesion, change.Change{
				Type: change.InsertRow, Schema: esq, Table: "kn_rev_padre", Source: "grid",
				Values: []change.Cell{{Column: "nombre", Value: texto("sin id")}},
			})
			if caso.nombre == "sqlite" {
				if hay(r, "bad", "sin valor") {
					t.Errorf("SQLite asigna el id solo y la revisión dice que falta:%s", describir(r))
				}
			} else if !hay(r, "bad", "sin valor: id") {
				t.Errorf("%s: la clave sin default tendría que faltar:%s", caso.nombre, describir(r))
			}
		})
	}
}
