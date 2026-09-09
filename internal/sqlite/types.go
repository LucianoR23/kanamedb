package sqlite

import "github.com/LucianoR23/kanamedb/internal/schema"

// columnTypes son los tipos que se pueden elegir para una columna.
//
// Es una lista escrita a mano, y en SQLite eso necesita una aclaración más
// larga que en MySQL: acá el motor **acepta cualquier nombre de tipo**.
// `CREATE TABLE t (a noexistetipo)` no da error —comprobado— porque el tipo
// declarado no restringe nada: define una AFINIDAD, que es una preferencia de
// conversión, y cada valor guarda su propio tipo.
//
// Así que esta lista no es «los tipos que existen» sino «los que conviene
// escribir»: los cinco de la afinidad más los alias que todo el mundo usa y que
// SQLite reconoce con las reglas de afinidad. Ofrecer un selector libre sería
// fiel al motor y una fábrica de errores de tipeo que nadie ve hasta que una
// consulta compara texto con números.
func columnTypes() []schema.TypeOption {
	tipos := []schema.TypeOption{
		// Los cinco de verdad: son las cinco afinidades que tiene SQLite.
		{Name: "integer", Comment: "entero; con PRIMARY KEY es además el rowid de la fila"},
		{Name: "text", Comment: "texto de cualquier largo"},
		{Name: "real", Comment: "coma flotante de 8 bytes"},
		{Name: "blob", Comment: "bytes tal cual, sin conversión"},
		{Name: "numeric", Comment: "guarda entero o real según el valor"},

		// Alias que SQLite reconoce por las reglas de afinidad. Se ofrecen
		// porque son los que la gente escribe y porque un esquema pensado para
		// migrar a otro motor se lee mejor con ellos.
		{Name: "boolean", Comment: "no existe como tipo: se guarda 0 o 1 con afinidad numérica"},
		{Name: "date", Comment: "no existe como tipo: se guarda texto ISO-8601, o un número"},
		{Name: "datetime", Comment: "no existe como tipo: se guarda texto ISO-8601, o un número"},
		{Name: "varchar", AcceptsModifier: true, Comment: "alias de text; el largo NO se hace cumplir"},
		{Name: "char", AcceptsModifier: true, Comment: "alias de text; el largo NO se hace cumplir"},
		{Name: "decimal", AcceptsModifier: true, Comment: "afinidad numérica; NO es exacto, a diferencia de otros motores"},
		{Name: "bigint", Comment: "alias de integer; SQLite ya usa 8 bytes"},
		{Name: "smallint", Comment: "alias de integer; SQLite no guarda menos por pedirlo"},
		{Name: "double", Comment: "alias de real"},
		{Name: "float", Comment: "alias de real"},
		{Name: "json", Comment: "no existe como tipo: es texto, con funciones json_* para leerlo"},
	}
	for i := range tipos {
		tipos[i].BuiltIn = true
		tipos[i].Kind = "base"
	}
	return tipos
}
