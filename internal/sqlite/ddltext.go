package sqlite

import (
	"fmt"
	"strings"
	"unicode"
)

// Este archivo lee el texto de un CREATE TABLE.
//
// Hace falta porque SQLite no guarda la estructura de una tabla en ningún
// catálogo consultable: guarda la SENTENCIA con la que se creó, tal cual se
// escribió, en sqlite_schema.sql. Los pragmas —table_xinfo, index_list,
// foreign_key_list— cubren columnas, índices y claves, pero **no hay ningún
// pragma que devuelva los CHECK**. Solo están en ese texto.
//
// Y hace falta para reconstruir la tabla. Un rebuild tiene que escribir un
// CREATE TABLE nuevo, y armarlo desde los pragmas perdería en silencio todo lo
// que los pragmas no ven: los CHECK, las cláusulas ON CONFLICT, los COLLATE,
// las opciones WITHOUT ROWID y STRICT. Perder un CHECK en un cambio de tipo es
// exactamente la clase de error que este proyecto ya decidió no cometer con
// Atlas y las columnas VIRTUAL. Ver kaname-plan.md § 6.
//
// Lo que hay acá NO es un parser de SQL y no pretende serlo. Es un tokenizador
// que respeta comillas, paréntesis y comentarios, y con eso alcanza para
// separar la definición en partes y reescribir la que se pidió cambiar,
// dejando TODO lo demás byte por byte como estaba.

// tokenKind es qué clase de cosa es un token.
type tokenKind int

const (
	tokPalabra tokenKind = iota // identificador sin citar, o palabra clave
	tokCitado                   // "x", [x], `x`
	tokTexto                    // 'x'
	tokNumero
	tokSigno
)

type token struct {
	kind  tokenKind
	texto string // tal cual aparece, con las comillas si las tenía
	ini   int    // posición en la cadena original
	fin   int
	prof  int // profundidad de paréntesis en la que empieza
}

// tokenizar parte una cadena de SQL en tokens.
//
// Los comentarios se saltean pero no se pierden: al reescribir se cortan
// tramos del texto ORIGINAL por posición, así que lo que hay entre tokens
// —espacios, saltos de línea, comentarios— viaja intacto.
func tokenizar(s string) ([]token, error) {
	var out []token
	prof := 0
	i := 0
	for i < len(s) {
		c := s[i]

		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++

		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}

		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			fin := strings.Index(s[i+2:], "*/")
			if fin < 0 {
				return nil, fmt.Errorf("comentario sin cerrar")
			}
			i += 2 + fin + 2

		case c == '\'':
			ini := i
			i++
			for {
				if i >= len(s) {
					return nil, fmt.Errorf("literal de texto sin cerrar")
				}
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			out = append(out, token{tokTexto, s[ini:i], ini, i, prof})

		case c == '"' || c == '`':
			cierre := c
			ini := i
			i++
			for {
				if i >= len(s) {
					return nil, fmt.Errorf("identificador citado sin cerrar")
				}
				if s[i] == cierre {
					if i+1 < len(s) && s[i+1] == cierre {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			out = append(out, token{tokCitado, s[ini:i], ini, i, prof})

		case c == '[':
			ini := i
			j := strings.IndexByte(s[i:], ']')
			if j < 0 {
				return nil, fmt.Errorf("identificador entre corchetes sin cerrar")
			}
			i += j + 1
			out = append(out, token{tokCitado, s[ini:i], ini, i, prof})

		case c == '(':
			out = append(out, token{tokSigno, "(", i, i + 1, prof})
			prof++
			i++

		case c == ')':
			prof--
			if prof < 0 {
				return nil, fmt.Errorf("paréntesis de más")
			}
			out = append(out, token{tokSigno, ")", i, i + 1, prof})
			i++

		case c >= '0' && c <= '9':
			ini := i
			for i < len(s) && (s[i] == '.' || s[i] == 'e' || s[i] == 'E' ||
				(s[i] >= '0' && s[i] <= '9') ||
				((s[i] == '+' || s[i] == '-') && (s[i-1] == 'e' || s[i-1] == 'E'))) {
				i++
			}
			out = append(out, token{tokNumero, s[ini:i], ini, i, prof})

		case c == '_' || c == '$' || c >= 0x80 || unicode.IsLetter(rune(c)):
			ini := i
			for i < len(s) {
				d := s[i]
				if d == '_' || d == '$' || d >= 0x80 || d >= '0' && d <= '9' ||
					unicode.IsLetter(rune(d)) {
					i++
					continue
				}
				break
			}
			out = append(out, token{tokPalabra, s[ini:i], ini, i, prof})

		default:
			out = append(out, token{tokSigno, string(c), i, i + 1, prof})
			i++
		}
	}
	if prof != 0 {
		return nil, fmt.Errorf("faltan %d paréntesis de cierre", prof)
	}
	return out, nil
}

// desCitar saca las comillas de un identificador, cualquiera de las cuatro
// formas que acepta SQLite.
func desCitar(s string) string {
	if len(s) < 2 {
		return s
	}
	switch s[0] {
	case '"':
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	case '`':
		return strings.ReplaceAll(s[1:len(s)-1], "``", "`")
	case '[':
		return s[1 : len(s)-1]
	}
	return s
}

/* --------------------------------------------------- la tabla, en partes */

// clasePart dice qué es cada elemento de la lista del CREATE TABLE.
type clasePart string

const (
	partColumna clasePart = "columna"
	partPrimary clasePart = "primary"
	partUnique  clasePart = "unique"
	partCheck   clasePart = "check"
	partForanea clasePart = "foreign"
)

// parte es un elemento de la lista: una columna o una restricción de tabla.
type parte struct {
	Clase clasePart
	// Nombre es el de la columna, o el de la restricción si la tiene declarado
	// con CONSTRAINT. Vacío en una restricción anónima.
	Nombre string
	// Texto es el elemento tal cual estaba escrito. Se conserva para poder
	// devolverlo intacto cuando el cambio no lo toca.
	Texto string
}

// tablaDDL es un CREATE TABLE ya separado en sus pedazos.
type tablaDDL struct {
	// Encabezado es todo lo anterior al paréntesis de la lista, sin incluirlo:
	// "CREATE TABLE \"envios\"".
	Encabezado string
	Partes     []parte
	// Cola es lo que va después del paréntesis de cierre: " WITHOUT ROWID",
	// " STRICT", o nada. Se conserva porque cambia el comportamiento de la
	// tabla y perderla sería cambiarla sin decirlo.
	Cola string
}

// leerCreateTable separa un CREATE TABLE en sus partes.
func leerCreateTable(sql string) (*tablaDDL, error) {
	toks, err := tokenizar(sql)
	if err != nil {
		return nil, fmt.Errorf("leer la definición de la tabla: %w", err)
	}
	// El primer "(" de profundidad 0 abre la lista de columnas.
	abre, cierra := -1, -1
	for i, t := range toks {
		if t.kind == tokSigno && t.texto == "(" && t.prof == 0 {
			abre = i
			break
		}
	}
	if abre < 0 {
		return nil, fmt.Errorf("la definición no tiene lista de columnas")
	}
	for i := len(toks) - 1; i > abre; i-- {
		if toks[i].kind == tokSigno && toks[i].texto == ")" && toks[i].prof == 0 {
			cierra = i
			break
		}
	}
	if cierra < 0 {
		return nil, fmt.Errorf("la lista de columnas no cierra")
	}

	t := &tablaDDL{
		Encabezado: strings.TrimSpace(sql[:toks[abre].ini]),
		Cola:       strings.TrimSpace(sql[toks[cierra].fin:]),
	}

	// Las comas de profundidad 1 separan los elementos. Las de adentro de un
	// paréntesis —decimal(10,2), CHECK (a IN (1,2))— están más adentro.
	inicio := toks[abre].fin
	for i := abre + 1; i <= cierra; i++ {
		esCorte := i == cierra ||
			(toks[i].kind == tokSigno && toks[i].texto == "," && toks[i].prof == 1)
		if !esCorte {
			continue
		}
		texto := strings.TrimSpace(sql[inicio:toks[i].ini])
		if texto != "" {
			p, err := clasificarParte(texto)
			if err != nil {
				return nil, err
			}
			t.Partes = append(t.Partes, p)
		}
		inicio = toks[i].fin
	}
	if len(t.Partes) == 0 {
		return nil, fmt.Errorf("la tabla no tiene columnas")
	}
	return t, nil
}

// clasificarParte decide si un elemento es una columna o una restricción.
//
// La regla es la del propio SQLite: si empieza con una de las palabras que
// abren una restricción de tabla, es una restricción; si no, es una columna y
// lo primero es su nombre. Es la misma regla por la que una columna no se
// puede llamar `check` sin comillas.
func clasificarParte(texto string) (parte, error) {
	toks, err := tokenizar(texto)
	if err != nil {
		return parte{}, err
	}
	if len(toks) == 0 {
		return parte{}, fmt.Errorf("elemento vacío en la definición")
	}

	p := parte{Texto: texto}
	i := 0
	if esPalabra(toks[0], "CONSTRAINT") && len(toks) >= 2 {
		p.Nombre = desCitar(toks[1].texto)
		i = 2
	}
	if i >= len(toks) {
		return parte{}, fmt.Errorf("restricción sin cuerpo: %q", texto)
	}
	switch {
	case esPalabra(toks[i], "PRIMARY"):
		p.Clase = partPrimary
	case esPalabra(toks[i], "UNIQUE"):
		p.Clase = partUnique
	case esPalabra(toks[i], "CHECK"):
		p.Clase = partCheck
	case esPalabra(toks[i], "FOREIGN"):
		p.Clase = partForanea
	default:
		if p.Nombre != "" {
			// Empezaba con CONSTRAINT pero no sigue ninguna de las cuatro.
			return parte{}, fmt.Errorf("no se entiende la restricción %q", texto)
		}
		p.Clase = partColumna
		p.Nombre = desCitar(toks[i].texto)
	}
	return p, nil
}

func esPalabra(t token, palabra string) bool {
	return t.kind == tokPalabra && strings.EqualFold(t.texto, palabra)
}

// expresionDelCheck saca la expresión de adentro del CHECK(...).
func expresionDelCheck(texto string) string {
	i := strings.IndexByte(texto, '(')
	j := strings.LastIndexByte(texto, ')')
	if i < 0 || j <= i {
		return texto
	}
	return strings.TrimSpace(texto[i+1 : j])
}

/* ------------------------------------------------- la columna, en partes */

// palabras que abren una cláusula dentro de la definición de una columna.
// Es un conjunto CERRADO: la gramática de SQLite no tiene otras.
var abrenClausula = map[string]bool{
	"CONSTRAINT": true, "PRIMARY": true, "NOT": true, "NULL": true,
	"UNIQUE": true, "CHECK": true, "DEFAULT": true, "COLLATE": true,
	"REFERENCES": true, "GENERATED": true, "AS": true,
}

// abre dice si un token empieza una cláusula nueva, mirando también el
// anterior.
//
// El anterior hace falta por dos palabras: `ON DELETE SET NULL` y
// `ON DELETE SET DEFAULT`. NULL y DEFAULT abren cláusula en cualquier otro
// lado, y ahí no: son parte de la acción de la clave foránea. Sin esta
// comprobación, `REFERENCES padre(id) ON DELETE SET NULL` se parte en
// «REFERENCES padre(id) ON DELETE SET» y «NULL», y a partir de ahí todo lo
// que toque el default de esa columna escribe SQL rota — o peor, la borra sin
// error: sacar el default de una columna con ON DELETE SET DEFAULT dejaba
// «ON DELETE SET». En la gramática de SQLite, SET solo aparece ahí.
func abre(toks []token, i int) bool {
	t := toks[i]
	if t.prof != 0 || t.kind != tokPalabra || !abrenClausula[strings.ToUpper(t.texto)] {
		return false
	}
	if i > 0 && esPalabra(toks[i-1], "SET") {
		return false
	}
	return true
}

// columnaDDL es la definición de una columna, separada en lo que se puede
// cambiar y lo que hay que dejar como está.
type columnaDDL struct {
	Nombre string
	// NombreLiteral es el nombre tal cual estaba escrito, con sus comillas si
	// las tenía. Se conserva para no reescribir el estilo de quien lo creó.
	NombreLiteral string
	// Tipo es el tipo declarado, con modificadores. Puede ser vacío: SQLite
	// acepta una columna sin tipo.
	Tipo string
	// Clausulas son las que siguen al tipo, cada una entera y en orden.
	Clausulas []string
	// Generada dice que la columna se calcula. Ninguna de las operaciones de
	// rebuild puede tocarla, así que alcanza con detectarla.
	Generada bool
}

// leerColumnaDDL separa la definición de una columna.
func leerColumnaDDL(texto string) (*columnaDDL, error) {
	toks, err := tokenizar(texto)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("definición de columna vacía")
	}
	c := &columnaDDL{
		Nombre:        desCitar(toks[0].texto),
		NombreLiteral: toks[0].texto,
	}

	// El tipo va desde después del nombre hasta la primera palabra que abre
	// una cláusula, mirando solo la profundidad 0: `decimal(10,2)` lleva
	// paréntesis y adentro puede haber cualquier cosa.
	corte := len(toks)
	for i := 1; i < len(toks); i++ {
		if abre(toks, i) {
			corte = i
			break
		}
	}
	if corte > 1 {
		c.Tipo = strings.TrimSpace(texto[toks[1].ini:toks[corte-1].fin])
	}

	// Y las cláusulas, cada una desde su palabra hasta la siguiente.
	for i := corte; i < len(toks); {
		if !abre(toks, i) {
			i++
			continue
		}
		clave := strings.ToUpper(toks[i].texto)
		if clave == "GENERATED" || clave == "AS" {
			// Una columna generada no se puede reescribir sin entender su
			// expresión, así que no se intenta. Ver rebuild.go.
			c.Generada = true
			c.Clausulas = append(c.Clausulas, strings.TrimSpace(texto[toks[i].ini:]))
			break
		}
		j := i + 1
		// NOT NULL es una sola cláusula aunque NULL también abra una.
		if clave == "NOT" && j < len(toks) && esPalabra(toks[j], "NULL") {
			j++
		}
		for j < len(toks) {
			if abre(toks, j) {
				break
			}
			j++
		}
		fin := len(texto)
		if j < len(toks) {
			fin = toks[j].ini
		}
		c.Clausulas = append(c.Clausulas, strings.TrimSpace(texto[toks[i].ini:fin]))
		i = j
	}
	return c, nil
}

// String vuelve a armar la definición de la columna.
func (c *columnaDDL) String() string {
	partes := []string{c.NombreLiteral}
	if c.Tipo != "" {
		partes = append(partes, c.Tipo)
	}
	partes = append(partes, c.Clausulas...)
	return strings.Join(partes, " ")
}

// claseDeClausula dice con qué palabra arranca una cláusula, en mayúsculas.
func claseDeClausula(cl string) string {
	toks, err := tokenizar(cl)
	if err != nil || len(toks) == 0 {
		return ""
	}
	c := strings.ToUpper(toks[0].texto)
	if c == "NOT" && len(toks) > 1 && esPalabra(toks[1], "NULL") {
		return "NOT NULL"
	}
	return c
}

// sacarClausula saca las cláusulas de una clase. Devuelve si sacó alguna.
func (c *columnaDDL) sacarClausula(clase string) bool {
	var quedan []string
	sacó := false
	for _, cl := range c.Clausulas {
		if claseDeClausula(cl) == clase {
			sacó = true
			continue
		}
		quedan = append(quedan, cl)
	}
	c.Clausulas = quedan
	return sacó
}

// ponerClausula reemplaza la cláusula de una clase, o la agrega si no estaba.
func (c *columnaDDL) ponerClausula(clase, texto string) {
	for i, cl := range c.Clausulas {
		if claseDeClausula(cl) == clase {
			c.Clausulas[i] = texto
			return
		}
	}
	c.Clausulas = append(c.Clausulas, texto)
}

func (c *columnaDDL) tieneClausula(clase string) bool {
	for _, cl := range c.Clausulas {
		if claseDeClausula(cl) == clase {
			return true
		}
	}
	return false
}
