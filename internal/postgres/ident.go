package postgres

import "strings"

// QuoteIdent cita un identificador para Postgres.
//
// Se usa siempre, aunque el nombre parezca inofensivo. Citar solo "cuando hace
// falta" obliga a decidir en cada uso si un nombre necesita comillas, y esa
// decisión se equivoca: `order` es palabra reservada, `Mi Tabla` tiene espacio,
// `año` no es ASCII y `a"b` es un nombre válido en Postgres.
//
// Duplicar la comilla doble es todo el escape que define el estándar: dentro de
// un identificador citado, cualquier otro carácter —incluido `;`— es literal.
// De ahí que un nombre hostil no se convierta en SQL sino en un identificador
// raro que el servidor rechaza por inexistente.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QualifiedName arma esquema.tabla con las dos partes citadas.
func QualifiedName(schema, table string) string {
	if schema == "" {
		return QuoteIdent(table)
	}
	return QuoteIdent(schema) + "." + QuoteIdent(table)
}
