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

// TestVariasSentenciasDeUnaVezDependenDelMotor.
//
// Acá NO son todos iguales, y la diferencia importa por lo que puede quedar
// escrito en la base sin que se vea.
//
// El editor manda el texto entero en una sola llamada, así que lo que pasa lo
// decide el driver:
//
//   - Postgres corre las tres y devuelve un resultado por cada una. El
//     protocolo simple de pgx acepta varias, y por eso `query.Batch` tiene una
//     lista.
//   - MySQL y MariaDB se niegan: `multiStatements` viene apagado en el driver y
//     el servidor contesta un error de sintaxis señalando la segunda. No corre
//     ninguna.
//   - SQLite las corre TODAS y devuelve el resultado de UNA.
//
// El tercero es el peligroso y es el motivo de que este test cuente FILAS y no
// resultados: con tres `SELECT` los tres casos se ven casi iguales, y con tres
// `INSERT` se ve lo único que importa, que es qué quedó escrito. Alguien que
// selecciona tres sentencias contra SQLite ve un resultado y tiene que poder
// saber que las tres corrieron.
//
// El test fija que el comportamiento coincida con `Caps.MultiStatement`, no un
// valor por motor. Encender multiStatements o partir las sentencias del lado
// del cliente obliga entonces a mover la capacidad —y con ella lo que la
// pantalla dice— en vez de cambiar el comportamiento en silencio.
func TestVariasSentenciasDeUnaVezDependenDelMotor(t *testing.T) {
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

			// Tres sentencias con EFECTO, para poder contar qué corrió de
			// verdad. Con tres SELECT los tres motores se ven parecidos.
			sql := "INSERT INTO " + tabla + " (id) VALUES (1);\n" +
				"-- un comentario entre sentencias\n" +
				"INSERT INTO " + tabla + " (id) VALUES (2);\n" +
				"/* y un bloque */ INSERT INTO " + tabla + " (id) VALUES (3);"

			multi := abierta.db.Caps().MultiStatement
			res := q(sesion).Run(ctx, "t2", sql)

			filas, fail := abierta.db.Count(ctx, esq, "kn_multi")
			if fail != nil {
				t.Fatalf("Count(): %s", fail.Message)
			}

			if !multi {
				if res.OK {
					t.Fatalf("%s corrió varias sentencias de una vez y Caps dice que no "+
						"puede: la tabla de capacidades está mintiendo", caso.nombre)
				}
				// Y lo que importa: no quedó nada a medias. Un motor que
				// rechaza el texto tiene que rechazarlo ENTERO.
				if filas != 0 {
					t.Errorf("la consulta falló y sin embargo quedaron %d filas: se "+
						"ejecutó parte del texto y el usuario ve un error", filas)
				}
				return
			}

			if !res.OK {
				t.Fatalf("Caps dice que corre varias sentencias y falló: %s — %s",
					mensajeDe(res), detalleDe(res))
			}
			if filas != 3 {
				t.Errorf("corrieron %d de 3 sentencias", filas)
			}
			// Lo que se VE tiene que coincidir con lo que PASÓ. Si corrieron
			// tres y se muestra una, quien mira la pantalla no puede saber que
			// las otras dos escribieron.
			n := 0
			if res.Batch != nil {
				n = len(res.Batch.Results)
			}
			if abierta.db.Caps().ResultPerStatement {
				if n != 3 {
					t.Errorf("se ejecutaron 3 sentencias y la pantalla muestra %d: el motor "+
						"declara un resultado por sentencia y no los está dando", n)
				}
				return
			}
			// SQLite: corrió las tres y da una. Se comprueba que siga siendo
			// ASÍ y no otra cosa —si algún día da tres, la capacidad tiene que
			// moverse y la advertencia de la pantalla tiene que irse—.
			if n != 1 {
				t.Errorf("devolvió %d resultados; este motor declara que da uno solo", n)
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
