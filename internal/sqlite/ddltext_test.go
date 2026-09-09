package sqlite

import (
	"strings"
	"testing"
)

// El lector del CREATE TABLE es la pieza de la que depende que una
// reconstrucción no pierda nada, así que se prueba sola y con los casos que la
// romperían. Todos son SQL válida de SQLite, no invenciones: la coma adentro
// de `decimal(10,2)`, la coma adentro de un literal, una columna que se llama
// como una palabra clave, un comentario en el medio de la definición.

func TestSeparaLaListaDeColumnas(t *testing.T) {
	casos := []struct {
		nombre  string
		ddl     string
		partes  []string // Clase:Nombre de cada parte, en orden
		cola    string
		encabez string
	}{
		{
			nombre: "lo simple",
			ddl:    `CREATE TABLE t (a integer, b text)`,
			partes: []string{"columna:a", "columna:b"},
		},
		{
			nombre: "la coma del modificador no separa",
			ddl:    `CREATE TABLE t (a decimal(10,2), b numeric(5, 3))`,
			partes: []string{"columna:a", "columna:b"},
		},
		{
			nombre: "la coma adentro de un literal no separa",
			ddl:    `CREATE TABLE t (a text DEFAULT 'uno,dos', b integer)`,
			partes: []string{"columna:a", "columna:b"},
		},
		{
			nombre: "la coma adentro de un CHECK anidado no separa",
			ddl:    `CREATE TABLE t (a text, CHECK (a IN ('x','y') AND length(a) > 0))`,
			partes: []string{"columna:a", "check:"},
		},
		{
			nombre: "la coma adentro de un identificador citado no separa",
			ddl:    `CREATE TABLE t ("col,rara" integer, b text)`,
			partes: []string{"columna:col,rara", "columna:b"},
		},
		{
			nombre: "una columna se puede llamar como una palabra clave, si va citada",
			ddl:    `CREATE TABLE t ("check" integer, [unique] text, ` + "`primary`" + ` real)`,
			partes: []string{"columna:check", "columna:unique", "columna:primary"},
		},
		{
			nombre: "los comentarios de línea no cuentan",
			ddl: `CREATE TABLE t (
				a integer, -- esto tiene una coma, y un paréntesis (
				b text
			)`,
			partes: []string{"columna:a", "columna:b"},
		},
		{
			nombre: "los comentarios de bloque tampoco",
			ddl:    `CREATE TABLE t (a integer /* , esto no separa */, b text)`,
			partes: []string{"columna:a", "columna:b"},
		},
		{
			nombre: "las restricciones de tabla se distinguen de las columnas",
			ddl: `CREATE TABLE t (
				a integer,
				b integer,
				PRIMARY KEY (a),
				CONSTRAINT u UNIQUE (b),
				CONSTRAINT ck CHECK (b > 0),
				FOREIGN KEY (b) REFERENCES otra(id)
			)`,
			partes: []string{"columna:a", "columna:b", "primary:", "unique:u", "check:ck", "foreign:"},
		},
		{
			nombre:  "WITHOUT ROWID queda afuera de la lista",
			ddl:     `CREATE TABLE t (k text PRIMARY KEY, v text) WITHOUT ROWID`,
			partes:  []string{"columna:k", "columna:v"},
			cola:    "WITHOUT ROWID",
			encabez: "CREATE TABLE t",
		},
		{
			nombre: "STRICT también",
			ddl:    `CREATE TABLE t (a int) STRICT`,
			partes: []string{"columna:a"},
			cola:   "STRICT",
		},
		{
			nombre: "una columna sin tipo es válida",
			ddl:    `CREATE TABLE t (a, b)`,
			partes: []string{"columna:a", "columna:b"},
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			td, err := leerCreateTable(c.ddl)
			if err != nil {
				t.Fatalf("leerCreateTable: %v", err)
			}
			var got []string
			for _, p := range td.Partes {
				got = append(got, string(p.Clase)+":"+p.Nombre)
			}
			if strings.Join(got, "|") != strings.Join(c.partes, "|") {
				t.Errorf("partes = %v\n         se esperaba %v", got, c.partes)
			}
			if c.cola != "" && td.Cola != c.cola {
				t.Errorf("Cola = %q, se esperaba %q", td.Cola, c.cola)
			}
			if c.cola == "" && td.Cola != "" {
				t.Errorf("Cola = %q y no tendría que haber ninguna", td.Cola)
			}
			if c.encabez != "" && td.Encabezado != c.encabez {
				t.Errorf("Encabezado = %q, se esperaba %q", td.Encabezado, c.encabez)
			}
		})
	}
}

func TestSeparaLaDefinicionDeUnaColumna(t *testing.T) {
	casos := []struct {
		nombre    string
		def       string
		tipo      string
		clausulas []string
		generada  bool
	}{
		{
			nombre: "nombre y tipo",
			def:    `a integer`, tipo: "integer",
		},
		{
			nombre: "el tipo con modificador",
			def:    `a decimal(10,2)`, tipo: "decimal(10,2)",
		},
		{
			// SQLite acepta tipos de varias palabras y hay que no cortarlos.
			nombre: "el tipo de varias palabras",
			def:    `a unsigned big int NOT NULL`, tipo: "unsigned big int",
			clausulas: []string{"NOT NULL"},
		},
		{
			nombre: "NOT NULL es una cláusula sola, no dos",
			def:    `a integer NOT NULL DEFAULT 0`, tipo: "integer",
			clausulas: []string{"NOT NULL", "DEFAULT 0"},
		},
		{
			nombre: "NULL sola también es una cláusula",
			def:    `a integer NULL`, tipo: "integer",
			clausulas: []string{"NULL"},
		},
		{
			nombre: "el default que es una expresión con paréntesis",
			def:    `a text DEFAULT (datetime('now')) NOT NULL`, tipo: "text",
			clausulas: []string{"DEFAULT (datetime('now'))", "NOT NULL"},
		},
		{
			nombre: "REFERENCES se lleva su ON DELETE",
			def:    `pid integer REFERENCES padre(id) ON DELETE CASCADE`, tipo: "integer",
			clausulas: []string{"REFERENCES padre(id) ON DELETE CASCADE"},
		},
		{
			// NULL y DEFAULT abren cláusula en cualquier otro lado, y acá no:
			// son parte de la acción de la clave. Partirlos rompe todo lo que
			// después toque el valor por defecto de la columna.
			nombre: "ON DELETE SET NULL no se parte",
			def:    `pid integer REFERENCES padre(id) ON DELETE SET NULL`, tipo: "integer",
			clausulas: []string{"REFERENCES padre(id) ON DELETE SET NULL"},
		},
		{
			nombre: "ON DELETE SET DEFAULT tampoco",
			def:    `pid integer REFERENCES padre(id) ON DELETE SET DEFAULT`, tipo: "integer",
			clausulas: []string{"REFERENCES padre(id) ON DELETE SET DEFAULT"},
		},
		{
			nombre: "y el default de verdad convive con la acción",
			def:    `pid integer DEFAULT 2 REFERENCES padre(id) ON UPDATE SET NULL`, tipo: "integer",
			clausulas: []string{"DEFAULT 2", "REFERENCES padre(id) ON UPDATE SET NULL"},
		},
		{
			nombre: "PRIMARY KEY se lleva su AUTOINCREMENT y su ON CONFLICT",
			def:    `id integer PRIMARY KEY AUTOINCREMENT ON CONFLICT ABORT`, tipo: "integer",
			clausulas: []string{"PRIMARY KEY AUTOINCREMENT ON CONFLICT ABORT"},
		},
		{
			nombre: "COLLATE y CHECK conviven",
			def:    `n text COLLATE NOCASE CHECK (length(n) > 2)`, tipo: "text",
			clausulas: []string{"COLLATE NOCASE", "CHECK (length(n) > 2)"},
		},
		{
			nombre: "una columna generada se marca",
			def:    `d integer GENERATED ALWAYS AS (a * 2) STORED`, tipo: "integer",
			clausulas: []string{"GENERATED ALWAYS AS (a * 2) STORED"},
			generada:  true,
		},
		{
			nombre: "la forma corta de una generada también",
			def:    `d integer AS (a * 2) VIRTUAL`, tipo: "integer",
			clausulas: []string{"AS (a * 2) VIRTUAL"},
			generada:  true,
		},
		{
			nombre: "sin tipo, directo a las cláusulas",
			def:    `a PRIMARY KEY`, tipo: "",
			clausulas: []string{"PRIMARY KEY"},
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			col, err := leerColumnaDDL(c.def)
			if err != nil {
				t.Fatalf("leerColumnaDDL: %v", err)
			}
			if col.Tipo != c.tipo {
				t.Errorf("Tipo = %q, se esperaba %q", col.Tipo, c.tipo)
			}
			if strings.Join(col.Clausulas, "|") != strings.Join(c.clausulas, "|") {
				t.Errorf("Clausulas = %q\n            se esperaba %q", col.Clausulas, c.clausulas)
			}
			if col.Generada != c.generada {
				t.Errorf("Generada = %v, se esperaba %v", col.Generada, c.generada)
			}
			// Y lo más importante: volver a armarla tiene que dar lo mismo.
			// Si esto falla, una reconstrucción cambia una columna que nadie
			// pidió cambiar.
			if vuelta := col.String(); normalizar(vuelta) != normalizar(c.def) {
				t.Errorf("armar de nuevo la columna dio otra cosa:\n  antes:   %s\n  después: %s",
					c.def, vuelta)
			}
		})
	}
}

// normalizar aplasta los espacios, que es lo único que la vuelta puede cambiar.
func normalizar(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestModificaLasClausulasDeUnaColumna(t *testing.T) {
	t.Run("poner NOT NULL cuando no estaba", func(t *testing.T) {
		col, _ := leerColumnaDDL(`a integer DEFAULT 0`)
		col.ponerClausula("NOT NULL", "NOT NULL")
		if got := normalizar(col.String()); got != `a integer DEFAULT 0 NOT NULL` {
			t.Errorf("quedó %q", got)
		}
	})
	t.Run("poner NOT NULL cuando ya estaba no la duplica", func(t *testing.T) {
		col, _ := leerColumnaDDL(`a integer NOT NULL`)
		col.ponerClausula("NOT NULL", "NOT NULL")
		if got := normalizar(col.String()); got != `a integer NOT NULL` {
			t.Errorf("quedó %q", got)
		}
	})
	t.Run("cambiar el default reemplaza y no agrega", func(t *testing.T) {
		col, _ := leerColumnaDDL(`a integer DEFAULT 0 NOT NULL`)
		col.ponerClausula("DEFAULT", "DEFAULT 42")
		if got := normalizar(col.String()); got != `a integer DEFAULT 42 NOT NULL` {
			t.Errorf("quedó %q", got)
		}
	})
	t.Run("sacar el NOT NULL deja el resto", func(t *testing.T) {
		col, _ := leerColumnaDDL(`a integer NOT NULL DEFAULT 0 CHECK (a > 0)`)
		if !col.sacarClausula("NOT NULL") {
			t.Fatal("dijo que no había NOT NULL")
		}
		if got := normalizar(col.String()); got != `a integer DEFAULT 0 CHECK (a > 0)` {
			t.Errorf("quedó %q", got)
		}
	})
	t.Run("sacar algo que no está lo dice", func(t *testing.T) {
		col, _ := leerColumnaDDL(`a integer`)
		if col.sacarClausula("DEFAULT") {
			t.Error("dijo que sacó un DEFAULT que no existía")
		}
	})
	t.Run("el DEFAULT de un ON DELETE no es un valor por defecto", func(t *testing.T) {
		col, _ := leerColumnaDDL(`n integer REFERENCES padre(id) ON DELETE SET DEFAULT`)
		if col.sacarClausula("DEFAULT") {
			t.Error("dijo que sacó un valor por defecto que no existe; la columna " +
				"quedaría como «... ON DELETE SET», que no es SQL válida y además " +
				"destruye la acción de la clave foránea")
		}
		if got := normalizar(col.String()); got != `n integer REFERENCES padre(id) ON DELETE SET DEFAULT` {
			t.Errorf("la definición quedó en %q", got)
		}
	})
	t.Run("ponerle un default a una columna con ON DELETE SET DEFAULT", func(t *testing.T) {
		col, _ := leerColumnaDDL(`n integer REFERENCES padre(id) ON DELETE SET DEFAULT`)
		col.ponerClausula("DEFAULT", "DEFAULT 7")
		got := normalizar(col.String())
		if got != `n integer REFERENCES padre(id) ON DELETE SET DEFAULT DEFAULT 7` {
			t.Errorf("quedó %q", got)
		}
	})
	t.Run("cambiar el tipo no toca las cláusulas", func(t *testing.T) {
		col, _ := leerColumnaDDL(`a integer NOT NULL DEFAULT 0 REFERENCES otra(id)`)
		col.Tipo = "text"
		if got := normalizar(col.String()); got != `a text NOT NULL DEFAULT 0 REFERENCES otra(id)` {
			t.Errorf("quedó %q", got)
		}
	})
}

// Una definición que el lector no entiende tiene que dar ERROR y no una
// interpretación a medias: sobre eso se decide si se reconstruye la tabla.
func TestSeNiegaConLoQueNoEntiende(t *testing.T) {
	malos := []string{
		`CREATE TABLE t (a integer`,                    // sin cerrar
		`CREATE TABLE t (a text DEFAULT 'x)`,           // literal sin cerrar
		`CREATE TABLE t (a text /* sin fin`,            // comentario sin cerrar
		`CREATE TABLE t`,                               // sin lista
		`CREATE TABLE t ()`,                            // lista vacía
		`CREATE TABLE t (CONSTRAINT c LO_QUE_SEA (a))`, // restricción desconocida
	}
	for _, m := range malos {
		if _, err := leerCreateTable(m); err == nil {
			t.Errorf("aceptó una definición que no se entiende: %q", m)
		}
	}
}

func TestCitaYDescita(t *testing.T) {
	casos := [][2]string{
		{`simple`, `"simple"`},
		{`con espacio`, `"con espacio"`},
		{`con"comilla`, `"con""comilla"`},
	}
	for _, c := range casos {
		if got := QuoteIdent(c[0]); got != c[1] {
			t.Errorf("QuoteIdent(%q) = %q, se esperaba %q", c[0], got, c[1])
		}
		if got := desCitar(c[1]); got != c[0] {
			t.Errorf("desCitar(%q) = %q, se esperaba %q", c[1], got, c[0])
		}
	}
	// Las otras tres formas de citar que SQLite acepta, que aparecen en
	// esquemas creados con otras herramientas.
	for _, c := range [][2]string{{"`x`", "x"}, {"[x]", "x"}, {`"x"`, "x"}} {
		if got := desCitar(c[0]); got != c[1] {
			t.Errorf("desCitar(%q) = %q, se esperaba %q", c[0], got, c[1])
		}
	}
}
