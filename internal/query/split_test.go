package query

import (
	"strings"
	"testing"
)

// Los cuatro dialectos, tal como los devuelve cada motor.
var (
	pg     = Dialect{DollarQuotes: true}
	my     = Dialect{Backtick: true, HashComments: true, BackslashEscapes: true, Compound: true}
	myAnsi = Dialect{Backtick: true, HashComments: true, Compound: true}
	lite   = Dialect{Brackets: true, Compound: true}
)

// TestPartirLoFacil: lo que se escribe todos los días.
func TestPartirLoFacil(t *testing.T) {
	casos := []struct {
		nombre string
		sql    string
		quiere []string
	}{
		{"una sola", "SELECT 1", []string{"SELECT 1"}},
		{"una con punto y coma", "SELECT 1;", []string{"SELECT 1"}},
		{"tres", "SELECT 1; SELECT 2; SELECT 3;", []string{"SELECT 1", "SELECT 2", "SELECT 3"}},
		{"sin el ultimo punto y coma", "SELECT 1; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		// Un punto y coma de más no es una sentencia vacía: mandarla haría que
		// el motor conteste un error por algo que no lo es.
		{"puntos y comas de mas", "SELECT 1;;;SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"vacio", "", nil},
		{"solo espacio", "  \n\t ", nil},
		{"solo un punto y coma", ";", nil},
		// Un texto que es solo un comentario no tiene nada que ejecutar.
		{"solo un comentario", "-- nada que hacer", nil},
		{"solo un bloque", "/* nada\n   que hacer */", nil},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			comparar(t, Split(c.sql, pg), c.quiere)
		})
	}
}

// TestElPuntoYComaDeAdentroNoSepara es el test que justifica el tokenizador.
//
// Un separador ingenuo parte por cada `;` y rompe todo esto. Cada caso es una
// forma distinta de que un punto y coma NO sea un separador.
func TestElPuntoYComaDeAdentroNoSepara(t *testing.T) {
	casos := []struct {
		nombre string
		d      Dialect
		sql    string
		quiere []string
	}{
		{
			"adentro de una cadena", pg,
			`SELECT 'uno; dos'; SELECT 2`,
			[]string{`SELECT 'uno; dos'`, "SELECT 2"},
		},
		{
			"comilla duplicada adentro de la cadena", pg,
			`SELECT 'no'' termina; aca'; SELECT 2`,
			[]string{`SELECT 'no'' termina; aca'`, "SELECT 2"},
		},
		{
			"adentro de un identificador citado", pg,
			`SELECT "col;umna" FROM t; SELECT 2`,
			[]string{`SELECT "col;umna" FROM t`, "SELECT 2"},
		},
		{
			"adentro de un acento invertido", my,
			"SELECT `col;umna` FROM t; SELECT 2",
			[]string{"SELECT `col;umna` FROM t", "SELECT 2"},
		},
		{
			"adentro de corchetes", lite,
			"SELECT [col;umna] FROM t; SELECT 2",
			[]string{"SELECT [col;umna] FROM t", "SELECT 2"},
		},
		{
			"adentro de un comentario de linea", pg,
			"SELECT 1 -- esto; no separa\n; SELECT 2",
			[]string{"SELECT 1 -- esto; no separa", "SELECT 2"},
		},
		{
			"adentro de un comentario con almohadilla", my,
			"SELECT 1 # esto; no separa\n; SELECT 2",
			[]string{"SELECT 1 # esto; no separa", "SELECT 2"},
		},
		{
			"adentro de un bloque", pg,
			"SELECT 1 /* esto; tampoco */; SELECT 2",
			[]string{"SELECT 1 /* esto; tampoco */", "SELECT 2"},
		},
		{
			// La almohadilla NO es comentario en Postgres ni en SQLite.
			"la almohadilla no es comentario en Postgres", pg,
			"SELECT '#'; SELECT 2",
			[]string{"SELECT '#'", "SELECT 2"},
		},
		{
			"barra invertida escapa la comilla en MySQL", my,
			`SELECT 'no\' termina; aca'; SELECT 2`,
			[]string{`SELECT 'no\' termina; aca'`, "SELECT 2"},
		},
		{
			// Con NO_BACKSLASH_ESCAPES la barra no escapa nada, así que la
			// cadena SÍ termina en esa comilla.
			"y no la escapa con NO_BACKSLASH_ESCAPES", myAnsi,
			`SELECT 'termina\'; SELECT 2`,
			[]string{`SELECT 'termina\'`, "SELECT 2"},
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			comparar(t, Split(c.sql, c.d), c.quiere)
		})
	}
}

// TestElCuerpoDeUnaRutinaNoSePartte.
//
// Es el caso que rompe a los separadores ingenuos y el motivo por el que esto
// no es un `strings.Split(sql, ";")`.
func TestElCuerpoDeUnaRutinaNoSeParte(t *testing.T) {
	trigger := `CREATE TRIGGER t BEFORE INSERT ON libros
FOR EACH ROW WHEN trim(NEW.titulo) = ''
BEGIN
  SELECT raise(ABORT, 'sin titulo');
  UPDATE contadores SET n = n + 1;
END;`
	got := Split(trigger+"\nSELECT 1;", lite)
	if len(got) != 2 {
		t.Fatalf("el trigger se partió en pedazos: %d sentencias\n%#v", len(got), textos(got))
	}
	if !strings.HasSuffix(got[0].SQL, "END") {
		t.Errorf("la primera sentencia no llega hasta el END:\n%s", got[0].SQL)
	}

	// Y en Postgres el cuerpo va entre $$, que es otro mecanismo.
	fn := `CREATE FUNCTION f() RETURNS int AS $$
BEGIN
  PERFORM 1;
  RETURN 2;
END;
$$ LANGUAGE plpgsql;`
	got = Split(fn+"\nSELECT 1;", pg)
	if len(got) != 2 {
		t.Fatalf("la función se partió en pedazos: %d sentencias\n%#v", len(got), textos(got))
	}

	// Con etiqueta, que es lo que se usa cuando el cuerpo tiene $$ adentro.
	conEtiqueta := `CREATE FUNCTION f() RETURNS text AS $cuerpo$ SELECT 'a;b'; $cuerpo$ LANGUAGE sql;`
	got = Split(conEtiqueta+"SELECT 1;", pg)
	if len(got) != 2 {
		t.Fatalf("la función con etiqueta se partió: %d\n%#v", len(got), textos(got))
	}
}

// TestElBeginDeUnaTransaccionNoAbreBloque.
//
// Es la trampa del caso anterior: si `BEGIN` contara siempre como apertura de
// bloque, nunca encontraría su `END` y a partir de ahí NO SE PARTIRÍA NADA. El
// editor quedaría peor que antes y sin ningún error que lo delate.
func TestElBeginDeUnaTransaccionNoAbreBloque(t *testing.T) {
	for _, d := range []Dialect{lite, my} {
		got := Split("BEGIN; INSERT INTO t VALUES (1); COMMIT;", d)
		comparar(t, got, []string{"BEGIN", "INSERT INTO t VALUES (1)", "COMMIT"})
	}
	// Y `CREATE TABLE` tampoco abre bloque aunque empiece con CREATE.
	comparar(t, Split("CREATE TABLE t (id int); SELECT 1;", lite),
		[]string{"CREATE TABLE t (id int)", "SELECT 1"})
}

// TestCadaSentenciaSabeDeQueLineaSalio.
//
// Sin esto el editor solo puede decir «falló la tercera», y quien escribió el
// texto está mirando números de línea.
func TestCadaSentenciaSabeDeQueLineaSalio(t *testing.T) {
	sql := "-- un comentario\n" + // 1
		"SELECT 1;\n" + // 2
		"\n" + // 3
		"/* un bloque\n" + // 4
		"   de dos líneas */\n" + // 5
		"SELECT 'con\nsalto';\n" + // 6 y 7
		"SELECT 3;" // 8
	got := Split(sql, pg)
	if len(got) != 3 {
		t.Fatalf("se esperaban 3 sentencias, hay %d: %#v", len(got), textos(got))
	}
	// La línea es la del primer token de VERDAD: el bloque de comentario de
	// las líneas 4 y 5 viaja adentro de la sentencia pero no la empieza.
	quiere := []int{2, 6, 8}
	for i, l := range quiere {
		if got[i].Line != l {
			t.Errorf("la sentencia %d dice línea %d y empieza en la %d:\n%s",
				i+1, got[i].Line, l, got[i].SQL)
		}
	}
}

// TestUnaCadenaSinCerrarNoParteElTexto: escribir es un proceso, y a mitad de
// escribir una cadena el texto está roto. Partir ahí mandaría un fragmento sin
// sentido; que el motor se queje del texto entero es más claro.
func TestUnaCadenaSinCerrarNoParteElTexto(t *testing.T) {
	got := Split("SELECT 'sin cerrar; SELECT 2;", pg)
	if len(got) != 1 {
		t.Errorf("una cadena sin cerrar partió el texto en %d: %#v", len(got), textos(got))
	}
}

func comparar(t *testing.T, got []Statement, quiere []string) {
	t.Helper()
	if len(got) != len(quiere) {
		t.Fatalf("salieron %d sentencias y se esperaban %d:\n%#v", len(got), len(quiere), textos(got))
	}
	for i := range quiere {
		if got[i].SQL != quiere[i] {
			t.Errorf("sentencia %d:\n  salió    %q\n  esperada %q", i+1, got[i].SQL, quiere[i])
		}
	}
}

func textos(s []Statement) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.SQL
	}
	return out
}
