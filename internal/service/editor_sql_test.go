package service

import (
	"context"
	"strings"
	"testing"
)

// Comentarios de todas las formas alrededor de UNA sentencia.
const sqlConComentarios = `-- un comentario de linea al principio
/* y uno de bloque
   en varias lineas */
SELECT 1 AS uno  -- al final de la linea
/* y un bloque antes del punto y coma */ ;
-- y uno suelto al final, sin sentencia despues`

// TestLosComentariosNoRompenLaConsulta.
//
// Vale para LOS CUATRO motores y no admite excepción: un comentario no es una
// sentencia, y una consulta rodeada de comentarios tiene que correr igual.
//
// Se comprueba contra todos y no contra los dos que estaban a mano porque «anda
// en los que probé» ya se dijo una vez en este proyecto, y no es lo mismo que
// «anda».
func TestLosComentariosNoRompenLaConsulta(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, _ := sesionDe(t, caso.nombre, caso.uri)
			q := NewQueries(sesion)

			res := q.Run(context.Background(), "t1", sqlConComentarios)
			if !res.OK {
				t.Fatalf("una sentencia rodeada de comentarios falló: %s — %s",
					mensajeDe(res), detalleDe(res))
			}
			if res.Batch == nil || len(res.Batch.Results) != 1 {
				t.Fatalf("se esperaba exactamente 1 resultado")
			}
			if n := len(res.Batch.Results[0].Rows); n != 1 {
				t.Errorf("devolvió %d filas y tenía que devolver 1", n)
			}
		})
	}
}

// TestVariasSentenciasCorrenYSeVenLasCuatro.
//
// Antes esto se llamaba «dependen del motor», y dependían. Cada driver hacía
// una cosa distinta con el mismo texto —comprobado contando FILAS, porque con
// tres SELECT los tres casos se ven casi iguales—:
//
//	Postgres        las tres corrían   tres resultados
//	MySQL/MariaDB   ninguna corría    error de sintaxis
//	SQLite          las tres corrían   UN resultado
//
// El tercero era el grave: `INSERT; INSERT; INSERT` escribía tres filas y la
// pantalla mostraba una, así que nadie se enteraba de las otras dos.
//
// Ahora el editor parte el texto antes de mandarlo y los cuatro hacen lo mismo:
// corren las tres y devuelven tres resultados, cada uno con su línea. Este test
// es el que lo sostiene, y sigue contando filas: lo que importa no es cuántos
// resultados se ven sino que lo que se ve coincida con lo que quedó escrito.
func TestVariasSentenciasCorrenYSeVenLasCuatro(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			abierta, err := sesion.abierta()
			if err != nil {
				t.Fatalf("abierta(): %v", err)
			}
			esq := esquemaDeApply(t, sesion, c)
			tabla := califica(c, esq, "kn_multi")
			limpiar := func() {
				_ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+tabla)
			}
			limpiar()
			t.Cleanup(limpiar)
			if err := abierta.db.Exec(ctx,
				"CREATE TABLE "+tabla+" (id "+tipoEnteroDe(c.Engine)+")"); err != nil {
				t.Fatalf("crear la tabla: %v", err)
			}

			// Con EFECTO, para poder contar qué corrió de verdad.
			sql := "INSERT INTO " + tabla + " (id) VALUES (1);\n" +
				"-- un comentario entre sentencias\n" +
				"INSERT INTO " + tabla + " (id) VALUES (2);\n" +
				"/* y un bloque */ INSERT INTO " + tabla + " (id) VALUES (3);"

			res := NewQueries(sesion).Run(ctx, "t2", sql)
			if !res.OK {
				t.Fatalf("falló: %s — %s", mensajeDe(res), detalleDe(res))
			}

			filas, fail := abierta.db.Count(ctx, esq, "kn_multi", nil)
			if fail != nil {
				t.Fatalf("Count(): %s", fail.Message)
			}
			if filas != 3 {
				t.Errorf("corrieron %d de 3 sentencias", filas)
			}
			n := 0
			if res.Batch != nil {
				n = len(res.Batch.Results)
			}
			if n != 3 {
				t.Fatalf("se ejecutaron 3 sentencias y se ven %d resultados: lo que "+
					"corrió y lo que se muestra no coinciden", n)
			}
			// Y cada resultado sabe de qué línea salió.
			for i, l := range []int{1, 3, 4} {
				if res.Batch.Results[i].Line != l {
					t.Errorf("el resultado %d dice línea %d y su sentencia está en la %d",
						i+1, res.Batch.Results[i].Line, l)
				}
			}
		})
	}
}

// TestLaSentenciaQueFallaSeUbica.
//
// Correr diez sentencias y ver «error de sintaxis» obliga a buscar a mano cuál
// fue. Y lo que ya corrió tiene que viajar igual: es distinto de «no pasó
// nada».
func TestLaSentenciaQueFallaSeUbica(t *testing.T) {
	for _, caso := range motoresDeEnsayo {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			abierta, err := sesion.abierta()
			if err != nil {
				t.Fatalf("abierta(): %v", err)
			}
			esq := esquemaDeApply(t, sesion, c)
			tabla := califica(c, esq, "kn_falla")
			limpiar := func() {
				_ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+tabla)
			}
			limpiar()
			t.Cleanup(limpiar)
			if err := abierta.db.Exec(ctx,
				"CREATE TABLE "+tabla+" (id "+tipoEnteroDe(c.Engine)+")"); err != nil {
				t.Fatalf("crear la tabla: %v", err)
			}

			// La SEGUNDA es imposible: una tabla que no existe.
			sql := "INSERT INTO " + tabla + " (id) VALUES (1);\n" +
				"SELECT * FROM kn_no_existe_para_nada;\n" +
				"INSERT INTO " + tabla + " (id) VALUES (3);"

			res := NewQueries(sesion).Run(ctx, "t3", sql)
			if res.OK {
				t.Fatal("dijo que salió bien con una tabla que no existe")
			}
			if res.Failure.Statement != 2 {
				t.Errorf("dice que falló la sentencia %d y fue la 2", res.Failure.Statement)
			}
			if res.Failure.Line != 2 {
				t.Errorf("dice línea %d y la sentencia está en la 2", res.Failure.Line)
			}
			if res.Failure.TotalStatements != 3 {
				t.Errorf("dice %d sentencias en total y son 3", res.Failure.TotalStatements)
			}
			// Lo que YA corrió viaja: la primera se ejecutó.
			if res.Batch == nil || len(res.Batch.Results) != 1 {
				t.Errorf("no viajó el resultado de la que sí corrió")
			}
			filas, fail := abierta.db.Count(ctx, esq, "kn_falla", nil)
			if fail != nil {
				t.Fatalf("Count(): %s", fail.Message)
			}
			// Se corta en la que falla: la tercera NO corre.
			if filas != 1 {
				t.Errorf("quedaron %d filas; tenía que correr la primera y cortar", filas)
			}
		})
	}
}

func q(s *Session) *Queries { return NewQueries(s) }

func mensajeDe(r RunResult) string {
	if r.Failure == nil {
		return "(sin fallo)"
	}
	return r.Failure.Message
}

func detalleDe(r RunResult) string {
	if r.Failure == nil {
		return ""
	}
	return strings.SplitN(r.Failure.Detail, "\n", 2)[0]
}
