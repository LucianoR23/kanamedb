package query

import "strings"

// Partir un texto SQL en sentencias.
//
// Existe porque el editor mandaba el texto entero en una sola llamada y cada
// driver hacía una cosa distinta —comprobado contra los cuatro motores—:
//
//	Postgres        las tres corren   tres resultados
//	MySQL/MariaDB   ninguna corre     error de sintaxis
//	SQLite          las tres corren   UN resultado
//
// El tercero es el que obliga: contra SQLite, `INSERT; INSERT; INSERT` escribe
// tres filas y la pantalla muestra un resultado, así que quien las corre no
// tiene cómo saber que las demás escribieron.
//
// Partiendo del lado del cliente los cuatro se comportan igual, y además cada
// sentencia trae su tiempo, sus filas afectadas y su número de línea. La
// alternativa —encender `multiStatements` en el driver de MySQL— se descartó:
// vale para toda conexión y toda llamada, así que un `;` dejaría de ser el
// final de nada en un programa que maneja credenciales productivas, y encima no
// arreglaría SQLite. Ver § 6 del plan.

// Dialect es lo que hay que saber del motor para LEER el texto, que no es lo
// mismo que saber qué puede hacer.
//
// Son cinco banderas y no un `Kind` a propósito: lo que cambia es cómo se
// delimita una cadena y dónde empieza un comentario, y escribirlo así deja ver
// de un vistazo en qué se diferencian los motores.
type Dialect struct {
	// Backtick: `identificador`, en MySQL y MariaDB.
	Backtick bool
	// Brackets: [identificador], en SQLite.
	Brackets bool
	// DollarQuotes: $$ … $$ y $etiqueta$ … $etiqueta$, en Postgres. Es lo que
	// envuelve el cuerpo de una función, que está lleno de puntos y comas.
	DollarQuotes bool
	// HashComments: `# hasta el fin de la línea`, en MySQL y MariaDB.
	HashComments bool
	// BackslashEscapes: adentro de una cadena, `\'` no la cierra.
	//
	// En MySQL depende del modo del servidor —NO_BACKSLASH_ESCAPES lo apaga— y
	// por eso lo decide la conexión, no el motor.
	BackslashEscapes bool
	// Compound: el cuerpo de un trigger o un procedimiento va entre BEGIN y
	// END, con puntos y comas adentro que NO separan nada. Es SQLite, MySQL y
	// MariaDB; en Postgres el cuerpo va entre $$ y lo resuelve DollarQuotes.
	Compound bool
}

// Statement es una sentencia con de dónde salió.
type Statement struct {
	// SQL es la sentencia sin el punto y coma final.
	SQL string
	// Line es la línea donde empieza la SQL de verdad, contando desde 1.
	//
	// Los comentarios que la preceden viajan adentro de SQL —pueden ser una
	// indicación para el planificador— pero NO mueven este número: si una
	// sentencia falla, lo que hay que ir a mirar es la sentencia y no el
	// comentario que le pusieron arriba.
	//
	// Va acá porque es lo que permite decir «falló la sentencia de la línea 12»
	// en vez de «falló la tercera»: quien escribió el texto está mirando
	// números de línea, no sentencias.
	Line int
}

// Split parte el texto en sentencias ejecutables.
//
// Lo que NO devuelve: los comentarios sueltos y el espacio en blanco. Un texto
// que es solo un comentario devuelve una lista vacía, y eso es correcto — no
// hay nada que ejecutar, y mandarlo igual haría que un motor conteste un error
// por algo que no es un error.
//
// Un punto y coma de más tampoco produce una sentencia vacía: `SELECT 1;;` es
// una sola.
func Split(sql string, d Dialect) []Statement {
	var out []Statement
	var actual strings.Builder
	linea := 1        // en qué línea va el recorrido
	inicio := 0       // en qué línea empezó la sentencia que se está juntando
	hayAlgo := false  // si la sentencia tiene algo más que espacio
	profundidad := 0  // BEGIN … END anidados
	empezada := false // si ya se leyó la primera palabra de la sentencia
	compuesta := false
	// separador es lo que corta una sentencia: `;` salvo que un `DELIMITER x`
	// —la línea que pegan mysqldump y Workbench— diga otra cosa. La línea
	// misma no es SQL y no se manda al servidor (C-11 de la auditoría del
	// 2026-09-11).
	separador := []rune{';'}

	guardar := func() {
		if hayAlgo {
			out = append(out, Statement{SQL: strings.TrimSpace(actual.String()), Line: inicio})
		}
		actual.Reset()
		hayAlgo = false
		empezada = false
		compuesta = false
		profundidad = 0
	}

	r := []rune(sql)
	for i := 0; i < len(r); {
		c := r[i]

		// --- comentarios: se copian tal cual, no separan ni cuentan como algo
		if c == '-' && i+1 < len(r) && r[i+1] == '-' {
			j := finDeLinea(r, i)
			actual.WriteString(string(r[i:j]))
			i = j
			continue
		}
		if d.HashComments && c == '#' {
			j := finDeLinea(r, i)
			actual.WriteString(string(r[i:j]))
			i = j
			continue
		}
		if c == '/' && i+1 < len(r) && r[i+1] == '*' {
			j := finDeComentario(r, i, d.DollarQuotes)
			trozo := string(r[i:j])
			actual.WriteString(trozo)
			linea += strings.Count(trozo, "\n")
			i = j
			continue
		}

		// --- DELIMITER x, al principio de una sentencia: cambia el separador.
		if d.Compound && !hayAlgo && esLetra(c) && esDelimiter(r, i) {
			fin := finDeLinea(r, i)
			campos := strings.Fields(string(r[i:fin]))
			if len(campos) >= 2 {
				separador = []rune(campos[1])
			}
			i = fin
			continue
		}

		// --- cadenas e identificadores citados: adentro no hay separadores
		if fin, ok := cierreDeCita(r, i, d); ok {
			if !hayAlgo {
				inicio = linea
			}
			hayAlgo = true
			trozo := string(r[i:fin])
			actual.WriteString(trozo)
			// Se cuentan DESPUÉS de marcar el inicio: una cadena de varias
			// líneas empieza donde empieza, no donde termina.
			linea += strings.Count(trozo, "\n")
			i = fin
			continue
		}

		// --- BEGIN … END del cuerpo de un trigger o un procedimiento.
		//
		// En MySQL, MariaDB y SQLite el cuerpo va entre BEGIN y END (Compound).
		// En Postgres va entre $$, salvo desde la 14: `BEGIN ATOMIC … END` es
		// SQL puro con puntos y comas adentro, y se cuenta igual (C-27).
		if (d.Compound || d.DollarQuotes) && esLetra(c) {
			fin := finDePalabra(r, i)
			palabra := strings.ToUpper(string(r[i:fin]))
			if !hayAlgo {
				inicio = linea
			}
			hayAlgo = true
			if !empezada {
				empezada = true
				// Solo un CREATE/ALTER de rutina abre bloques. Sin esto, el
				// `BEGIN` que abre una TRANSACCIÓN contaría como bloque, nunca
				// encontraría su END, y a partir de ahí no se partiría nada.
				compuesta = abreRutina(r, i)
			}
			if compuesta {
				switch palabra {
				case "BEGIN":
					if d.Compound || palabraSiguiente(r, fin) == "ATOMIC" {
						profundidad++
					}
				case "CASE":
					profundidad++
				case "END":
					// `END IF`, `END LOOP`, `END WHILE` y `END REPEAT` cierran
					// construcciones que no se contaron al abrir —IF, LOOP,
					// WHILE y REPEAT no suman— así que tampoco restan. Sin esto,
					// el `END IF` de casi cualquier procedimiento de MySQL
					// bajaba a cero y el `;` siguiente partía el cuerpo (C-11).
					// `END CASE` sí resta: CASE sumó.
					switch palabraSiguiente(r, fin) {
					case "IF", "LOOP", "WHILE", "REPEAT":
					case "CASE":
						// `END CASE` cierra el CASE que sumó: resta una vez, y
						// la palabra CASE se consume acá para que no vuelva a
						// sumar al leerla suelta en la vuelta siguiente.
						if profundidad > 0 {
							profundidad--
						}
						j := saltarEspacio(r, fin)
						fin = finDePalabra(r, j)
					default:
						if profundidad > 0 {
							profundidad--
						}
					}
				}
			}
			actual.WriteString(string(r[i:fin]))
			i = fin
			continue
		}

		if empieza(r, i, separador) && profundidad == 0 {
			guardar()
			i += len(separador)
			continue
		}

		if c == '\n' {
			linea++
		} else if !esEspacio(c) {
			if !hayAlgo {
				inicio = linea
			}
			hayAlgo = true
		}
		actual.WriteRune(c)
		i++
	}
	guardar()
	return out
}

// abreRutina dice si la palabra en `i` empieza una sentencia cuyo cuerpo va
// entre BEGIN y END.
//
// Se mira la PRIMERA palabra de la sentencia y se busca TRIGGER, PROCEDURE o
// FUNCTION en las que siguen. `CREATE TABLE` no abre bloque, y `BEGIN;` a secas
// tampoco: es una transacción.
func abreRutina(r []rune, i int) bool {
	primera := strings.ToUpper(string(r[i:finDePalabra(r, i)]))
	if primera != "CREATE" && primera != "ALTER" && primera != "REPLACE" {
		return false
	}
	// Con mirar unas pocas palabras alcanza: entre CREATE y TRIGGER puede
	// haber OR REPLACE, TEMP, DEFINER = `app`@`10.0.0.1`, pero no veinte
	// palabras. Un identificador citado cuenta como UNA palabra: antes cada
	// carácter del DEFINER consumía un turno y el cuerpo se partía (C-27).
	j := saltarEspacio(r, finDePalabra(r, i))
	for n := 0; n < 12 && j < len(r); n++ {
		switch {
		case esLetra(r[j]):
			fin := finDePalabra(r, j)
			switch strings.ToUpper(string(r[j:fin])) {
			case "TRIGGER", "PROCEDURE", "FUNCTION":
				return true
			case "TABLE", "VIEW", "INDEX", "DATABASE", "SCHEMA", "USER", "ROLE":
				return false
			}
			j = fin
		case r[j] == '`' || r[j] == '"' || r[j] == '\'':
			j = hastaElCierre(r, j, r[j], true, false)
		default:
			j++
		}
		j = saltarEspacio(r, j)
	}
	return false
}

// esDelimiter dice si en `i` empieza una línea `DELIMITER x`.
func esDelimiter(r []rune, i int) bool {
	fin := finDePalabra(r, i)
	return strings.EqualFold(string(r[i:fin]), "DELIMITER") && fin < len(r) && (r[fin] == ' ' || r[fin] == '\t')
}

// empieza dice si en `i` está el separador.
func empieza(r []rune, i int, sep []rune) bool {
	if i+len(sep) > len(r) {
		return false
	}
	for k, c := range sep {
		if r[i+k] != c {
			return false
		}
	}
	return true
}

// palabraSiguiente es la palabra que sigue a la posición `i`, saltando el
// espacio, en mayúsculas; vacía si lo que sigue no es una palabra.
func palabraSiguiente(r []rune, i int) string {
	j := saltarEspacio(r, i)
	if j >= len(r) || !esLetra(r[j]) {
		return ""
	}
	return strings.ToUpper(string(r[j:finDePalabra(r, j)]))
}

// finDeComentario devuelve dónde termina el comentario de bloque que empieza
// en `i`. En Postgres los comentarios se anidan y el primer `*/` no cierra
// necesariamente (C-27); en los demás, sí.
func finDeComentario(r []rune, i int, anidados bool) int {
	nivel := 1
	j := i + 2
	for j < len(r) {
		switch {
		case anidados && r[j] == '/' && j+1 < len(r) && r[j+1] == '*':
			nivel++
			j += 2
		case r[j] == '*' && j+1 < len(r) && r[j+1] == '/':
			nivel--
			j += 2
			if nivel == 0 {
				return j
			}
		default:
			j++
		}
	}
	return len(r)
}

// cierreDeCita devuelve dónde termina la cadena o el identificador que empieza
// en `i`, y si de verdad empezaba uno.
func cierreDeCita(r []rune, i int, d Dialect) (int, bool) {
	switch c := r[i]; {
	case (c == 'E' || c == 'e') && d.DollarQuotes && i+1 < len(r) && r[i+1] == '\'' &&
		(i == 0 || !esLetra(r[i-1]) && r[i-1] != '_'):
		// E'…' de Postgres: adentro, `\'` no cierra (C-27). Solo si la E no es
		// el final de un identificador —`WHERE nombre'x'` no existe, pero
		// `CASE'a'` tampoco, así que alcanza con mirar el carácter anterior—.
		return hastaElCierre(r, i+1, '\'', true, true), true
	case c == '\'' || c == '"':
		return hastaElCierre(r, i, c, true, d.BackslashEscapes), true
	case c == '`' && d.Backtick:
		return hastaElCierre(r, i, '`', true, false), true
	case c == '[' && d.Brackets:
		// SQLite no tiene escape adentro de los corchetes: cierra en el primero.
		return hastaElCierre(r, i, ']', false, false), true
	case c == '$' && d.DollarQuotes:
		if fin, ok := dolar(r, i); ok {
			return fin, true
		}
	}
	return 0, false
}

// hastaElCierre avanza hasta el delimitador de cierre.
//
// `duplica` es el escape de casi todo SQL: dos comillas seguidas son una
// comilla y no un cierre.
func hastaElCierre(r []rune, i int, cierre rune, duplica, barra bool) int {
	j := i + 1
	for j < len(r) {
		if barra && r[j] == '\\' && j+1 < len(r) {
			j += 2
			continue
		}
		if r[j] == cierre {
			if duplica && j+1 < len(r) && r[j+1] == cierre {
				j += 2
				continue
			}
			return j + 1
		}
		j++
	}
	// Sin cerrar: se devuelve todo lo que queda. El motor dirá qué pasa; lo que
	// no puede pasar es que un literal a medio escribir haga partir el texto
	// por un punto y coma que está adentro de la cadena.
	return len(r)
}

// dolar reconoce $$ … $$ y $etiqueta$ … $etiqueta$ de Postgres.
//
// Es lo que envuelve el cuerpo de una función, que está lleno de puntos y
// comas: sin esto, crear una función se parte en pedazos que no compilan.
func dolar(r []rune, i int) (int, bool) {
	j := i + 1
	for j < len(r) && (esLetra(r[j]) || r[j] == '_' || (j > i+1 && r[j] >= '0' && r[j] <= '9')) {
		j++
	}
	if j >= len(r) || r[j] != '$' {
		return 0, false
	}
	etiqueta := string(r[i : j+1])
	resto := string(r[j+1:])
	k := strings.Index(resto, etiqueta)
	if k < 0 {
		return len(r), true
	}
	return j + 1 + len([]rune(resto[:k])) + len([]rune(etiqueta)), true
}

func finDeLinea(r []rune, i int) int {
	for i < len(r) && r[i] != '\n' {
		i++
	}
	return i
}

func finDePalabra(r []rune, i int) int {
	for i < len(r) && (esLetra(r[i]) || r[i] == '_' || (r[i] >= '0' && r[i] <= '9')) {
		i++
	}
	return i
}

func saltarEspacio(r []rune, i int) int {
	for i < len(r) && esEspacio(r[i]) {
		i++
	}
	return i
}

func esLetra(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func esEspacio(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// Command saca el comando de una sentencia: SELECT, INSERT, ALTER…
//
// Saltea los comentarios de adelante, y eso no es un detalle de prolijidad:
// desde que el editor parte el texto, los comentarios que preceden a una
// sentencia viajan ADENTRO de ella —pueden ser una indicación para el
// planificador, así que sacarlos sería peor—, y tomar la primera palabra a
// secas hacía que la pestaña del resultado dijera «--» en vez de «SELECT».
//
// Es una función y no un método porque la usan dos motores; Postgres no la
// necesita, que recibe el comando del propio servidor.
func Command(sql string, d Dialect) string {
	r := []rune(Trim(sql, d))
	if len(r) == 0 {
		return ""
	}
	return strings.ToUpper(string(r[:finDePalabra(r, 0)]))
}

// Trim devuelve la sentencia desde su primera palabra, sin los comentarios
// ni el espacio de adelante. Es el MISMO escaneo que hace Command, expuesto
// para quien tenga que mirar más que la primera palabra —una expresión
// anclada al principio, por ejemplo—. Volver a buscar la palabra en el texto
// entero no sirve: la encontraría antes, adentro del comentario.
func Trim(sql string, d Dialect) string {
	r := []rune(sql)
	i := 0
	for i < len(r) {
		switch {
		case esEspacio(r[i]):
			i++
		case r[i] == '-' && i+1 < len(r) && r[i+1] == '-':
			i = finDeLinea(r, i)
		case d.HashComments && r[i] == '#':
			i = finDeLinea(r, i)
		case r[i] == '/' && i+1 < len(r) && r[i+1] == '*':
			i = finDeComentario(r, i, d.DollarQuotes)
		default:
			return string(r[i:])
		}
	}
	return ""
}
