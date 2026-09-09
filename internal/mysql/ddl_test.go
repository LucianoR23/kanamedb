package mysql

import (
	"strings"
	"testing"

	sqldriver "github.com/go-sql-driver/mysql"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Este archivo son los tests que NO necesitan un servidor.
//
// Existe porque su ausencia dejó pasar un bug: el resto del paquete se prueba
// con la batería de enginetest, que se saltea entera si no hay un MySQL
// escuchando. Un `go test ./internal/mysql/` sin Docker daba verde sin haber
// ejecutado una sola línea de este código.

// TestValidaLosIdentificadores comprueba la única defensa entre un nombre
// escrito por el usuario y la SQL que se manda al motor.
//
// El bucle que valida y el mapa que lo alimenta estaban dados vuelta: el mapa
// guardaba descripción→nombre y el bucle lo leía como nombre→descripción, así
// que lo que se medía era el largo de la DESCRIPCIÓN. Un nombre de tabla de 300
// caracteres pasaba entero, y uno con un salto de línea adentro también.
func TestValidaLosIdentificadores(t *testing.T) {
	largo := strings.Repeat("x", maxIdent+1)

	malos := []struct {
		nombre string
		c      change.Change
	}{
		{"tabla demasiado larga", change.Change{Type: change.DropTable, Table: largo}},
		{"tabla con salto de línea", change.Change{Type: change.DropTable, Table: "con\nsalto"}},
		{"tabla con byte nulo", change.Change{Type: change.DropTable, Table: "con\x00nulo"}},
		{"columna demasiado larga", change.Change{
			Type: change.DropColumn, Table: "t", Column: &change.Column{Name: largo}}},
		{"nombre nuevo demasiado largo", change.Change{
			Type: change.RenameTable, Table: "t", NewName: largo}},
		{"índice con nombre demasiado largo", change.Change{
			Type: change.DropIndex, Table: "t", Name: largo}},
		{"columna de un índice demasiado larga", change.Change{
			Type: change.AddIndex, Table: "t", Names: []string{largo}}},
		{"tabla referenciada demasiado larga", change.Change{
			Type: change.AddForeignKey, Table: "t", Names: []string{"a"},
			RefTable: largo, RefNames: []string{"b"}}},
	}
	for _, m := range malos {
		t.Run(m.nombre, func(t *testing.T) {
			if _, err := RenderDDL(m.c, engine.MySQL); err == nil {
				t.Fatal("RenderDDL lo aceptó")
			}
		})
	}

	// Un nombre justo en el límite tiene que pasar: una validación que rechaza
	// de más es tan mala como una que no rechaza nada.
	enElLimite := change.Change{Type: change.DropTable, Table: strings.Repeat("y", maxIdent)}
	if _, err := RenderDDL(enElLimite, engine.MySQL); err != nil {
		t.Errorf("RenderDDL rechazó un nombre de exactamente %d caracteres: %v", maxIdent, err)
	}
}

// TestCitaLosIdentificadores: el acento invertido se duplica para escapar, y se
// cita SIEMPRE. Es lo único que separa un nombre de tabla de una inyección.
func TestCitaLosIdentificadores(t *testing.T) {
	casos := [][2]string{
		{"simple", "`simple`"},
		{"con espacio", "`con espacio`"},
		{"con`acento", "`con``acento`"},
		{"order", "`order`"},
	}
	for _, c := range casos {
		if got := QuoteIdent(c[0]); got != c[1] {
			t.Errorf("QuoteIdent(%q) = %q, se esperaba %q", c[0], got, c[1])
		}
	}
	if got := QualifiedName("base", "tabla"); got != "`base`.`tabla`" {
		t.Errorf("QualifiedName = %q", got)
	}
	if got := QualifiedName("", "tabla"); got != "`tabla`" {
		t.Errorf("QualifiedName sin base = %q", got)
	}
}

// TestElValorPorDefectoSaleDeColumnDefault: SetDefault leía c.Expression
// mientras Validate exige c.Column.Default, así que un cambio válido
// renderizaba «SET DEFAULT » y fallaba recién al aplicar.
func TestElValorPorDefectoSaleDeColumnDefault(t *testing.T) {
	st, err := RenderDDL(change.Change{
		Type: change.SetDefault, Schema: "b", Table: "t",
		Column: &change.Column{Name: "c", DataType: "int", Default: "7"},
	}, engine.MySQL)
	if err != nil {
		t.Fatalf("RenderDDL: %v", err)
	}
	if !strings.HasSuffix(st.SQL, "SET DEFAULT 7") {
		t.Errorf("la sentencia quedó %q", st.SQL)
	}
}

// TestVersionNum: los mínimos de engine se comparan con este número, así que
// leerlo mal deja entrar un servidor viejo o rechaza uno bueno.
func TestVersionNum(t *testing.T) {
	casos := []struct {
		version string
		num     int
		kind    engine.Kind
	}{
		{"9.7.2", 90702, engine.MySQL},
		{"8.4.0", 80400, engine.MySQL},
		{"12.3.3-MariaDB-ubu2404", 120303, engine.MariaDB},
		{"10.11.9-MariaDB", 101109, engine.MariaDB},
		{"9.7.2-log", 90702, engine.MySQL},
	}
	for _, c := range casos {
		if got := versionNum(c.version); got != c.num {
			t.Errorf("versionNum(%q) = %d, se esperaba %d", c.version, got, c.num)
		}
		if got := kindDe(c.version); got != c.kind {
			t.Errorf("kindDe(%q) = %q, se esperaba %q", c.version, got, c.kind)
		}
	}
}

// TestEncendido: MySQL contesta 0/1 y MariaDB OFF/ON a las mismas variables.
// Leerlo mal contra uno de los dos hacía que la conexión pareciera caída.
func TestEncendido(t *testing.T) {
	for _, v := range []string{"1", "ON", "on", "TRUE", "YES"} {
		if !encendido(v) {
			t.Errorf("encendido(%q) dio false", v)
		}
	}
	for _, v := range []string{"0", "OFF", "off", "", "NO"} {
		if encendido(v) {
			t.Errorf("encendido(%q) dio true", v)
		}
	}
}

// TestComentarUnaColumnaNoLeBorraElValorPorDefecto.
//
// MySQL no tiene COMMENT ON: el comentario es parte de la definición de la
// columna, así que hay que reescribirla entera con MODIFY. Y lo que no se
// vuelva a escribir se PIERDE. Agregarle un comentario a
// `estado varchar(20) NOT NULL DEFAULT 'nuevo'` le borraba el default, en
// silencio y sin nada en la vista previa que lo dijera.
func TestComentarUnaColumnaNoLeBorraElValorPorDefecto(t *testing.T) {
	st, err := RenderDDL(change.Change{
		Type: change.SetColumnComment, Schema: "b", Table: "t",
		Column: &change.Column{
			Name: "estado", DataType: "varchar(20)", Nullable: false, Default: "'nuevo'",
		},
		Comment: "el estado del pedido",
	}, engine.MySQL)
	if err != nil {
		t.Fatalf("RenderDDL: %v", err)
	}
	if !strings.Contains(st.SQL, "DEFAULT 'nuevo'") {
		t.Errorf("la sentencia no repite el valor por defecto, así que lo borra:\n%s", st.SQL)
	}
	if !strings.Contains(st.SQL, "NOT NULL") {
		t.Errorf("la sentencia no repite el NOT NULL:\n%s", st.SQL)
	}
	if st.Note == "" {
		t.Error("sin nota: quien revisa la vista previa no tiene cómo saber que MODIFY " +
			"reescribe la definición entera")
	}
}

// TestCitarUnLiteralRespetaElModoDelServidor.
//
// Con NO_BACKSLASH_ESCAPES la barra invertida no escapa nada, así que
// duplicarla guarda DOS barras: un comentario que dice `C:\ruta` se guardaba
// como `C:\ruta`. No es una inyección —la comilla simple, que es lo único que
// puede cerrar el literal, se duplica igual en los dos modos— pero sí
// corrupción silenciosa de un texto escrito por el usuario.
func TestCitarUnLiteralRespetaElModoDelServidor(t *testing.T) {
	const conBarra = `C:\ruta`
	// En el modo normal la barra invertida ESCAPA, así que hay que duplicarla
	// para que el servidor guarde una sola.
	if got := quoteString(conBarra, false); got != "'C:\\\\ruta'" {
		t.Errorf("en modo normal quedó %s y tendría que quedar 'C:\\\\ruta'", got)
	}
	// Con NO_BACKSLASH_ESCAPES no escapa nada, así que duplicarla guardaría dos.
	if got := quoteString(conBarra, true); got != `'C:\ruta'` {
		t.Errorf("con NO_BACKSLASH_ESCAPES quedó %s y tendría que quedar 'C:\\ruta'", got)
	}
	// La comilla simple se duplica SIEMPRE: es lo único que puede cerrar el
	// literal, y es lo que hace que esto no sea un agujero de inyección.
	for _, sinEscapes := range []bool{false, true} {
		got := quoteString(`no' DROP TABLE x --`, sinEscapes)
		if !strings.HasPrefix(got, `'no''`) {
			t.Errorf("sinEscapes=%v: la comilla no se duplicó: %s", sinEscapes, got)
		}
	}
}

// TestElErrorDeEscrituraEnSoloLecturaSeEntiende.
//
// El 1792 es el que devuelve una escritura en una conexión que Kaname abrió en
// modo solo lectura, así que es el más probable de toda la lista en una
// conexión de producción. Salía como «el motor rechazó la sentencia».
func TestElErrorDeEscrituraEnSoloLecturaSeEntiende(t *testing.T) {
	err := &sqldriver.MySQLError{
		Number:   1792,
		SQLState: [5]byte{'2', '5', '0', '0', '6'},
		Message:  "Cannot execute statement in a READ ONLY transaction.",
	}
	f := ClassifyStatement(err, "usuario@host/base")
	if f.Kind != engine.FailurePermission {
		t.Errorf("Kind = %q, se esperaba %q", f.Kind, engine.FailurePermission)
	}
	if !strings.Contains(strings.ToLower(f.Message), "solo lectura") {
		t.Errorf("el mensaje no dice de qué se trata: %q", f.Message)
	}
	if f.Hint == "" {
		t.Error("sin sugerencia: el usuario no sabe dónde se saca esa casilla")
	}
}
