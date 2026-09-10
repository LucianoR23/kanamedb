// Package dml escribe los cambios de DATOS del changeset —insertar, actualizar
// y borrar una fila— como SQL, una sola vez para los cuatro motores.
//
// El DDL se escribe en cada motor porque cada motor tiene su gramática: cómo se
// agrega una columna en SQLite no se parece a cómo se agrega en Postgres. Un
// UPDATE por clave primaria, en cambio, es la misma sentencia en los cuatro
// salvo tres detalles —cómo se cita un identificador, cómo se cita un literal
// y cómo se escribe un marcador de parámetro—, y esos tres son exactamente lo
// que Dialect pide. Escribirlo cuatro veces sería cuatro lugares donde
// equivocar la comprobación de filas.
//
// Cada sentencia sale en dos formas, y la diferencia es el punto del paquete:
// Statement.SQL con los valores escritos como literales, para leer, y
// Statement.Bound con marcadores y los valores aparte, para ejecutar. Un valor
// escrito en una celda nunca está adentro del texto que corre. Ver CLAUDE.md.
package dml

import (
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// Dialect es lo que un motor tiene que decir para que se pueda escribir su
// DML.
type Dialect struct {
	// Table arma el nombre calificado de la tabla, con la regla del motor
	// sobre qué hacer con el esquema.
	Table func(schema, table string) string
	// QuoteIdent cita el nombre de una columna.
	QuoteIdent func(string) string
	// QuoteLiteral cita un texto como literal, para la SQL que se LEE. Nunca
	// se ejecuta lo que sale de acá.
	QuoteLiteral func(string) string
	// Placeholder es el marcador del parámetro n, contando desde 1: "$1" en
	// Postgres, "?" en MySQL y SQLite.
	Placeholder func(n int) string
	// EmptyInsert es lo que sigue al nombre de la tabla para insertar una fila
	// con todos sus defaults: "DEFAULT VALUES" en Postgres y SQLite, "() VALUES
	// ()" en MySQL, que no acepta la otra forma.
	EmptyInsert string

	// InsertPrefix e InsertSuffix son cómo este motor pide que una fila que
	// choca con otra se saltee en vez de abortar. Son dos porque cada motor lo
	// pone en un lado: Postgres y SQLite al final —`ON CONFLICT DO NOTHING`,
	// `OR IGNORE`— y MySQL adelante, en `INSERT IGNORE INTO`.
	//
	// Nil se trata como «solo se puede insertar sin ignorar»: el prefijo por
	// defecto es "INSERT INTO " y el sufijo, nada.
	InsertPrefix func(ignorar bool) string
	InsertSuffix func(ignorar bool) string
}

// Render escribe un cambio de datos. Devuelve error para cualquier otro tipo.
func Render(c change.Change, d Dialect) (change.Statement, error) {
	if err := c.Validate(); err != nil {
		return change.Statement{}, err
	}
	if c.Kind() != change.KindData {
		return change.Statement{}, fmt.Errorf("dml: %q no es un cambio de datos", c.Type)
	}

	e := escritor{d: d}
	tabla := d.Table(c.Schema, c.Table)
	st := change.Statement{
		ChangeID:    c.ID,
		Impact:      change.ImpactData,
		Lock:        change.LockNone,
		Destructive: c.Destructive(),
	}

	switch c.Type {

	case change.InsertRow:
		if len(c.Values) == 0 {
			e.texto("INSERT INTO " + tabla + " " + d.EmptyInsert)
			break
		}
		cols := make([]string, len(c.Values))
		for i, v := range c.Values {
			cols[i] = d.QuoteIdent(v.Column)
		}
		e.texto("INSERT INTO " + tabla + " (" + strings.Join(cols, ", ") + ") VALUES (")
		for i, v := range c.Values {
			if i > 0 {
				e.texto(", ")
			}
			e.valor(v.Value)
		}
		e.texto(")")

	case change.UpdateRow:
		e.texto("UPDATE " + tabla + " SET ")
		for i, v := range c.Values {
			if i > 0 {
				e.texto(", ")
			}
			e.texto(d.QuoteIdent(v.Column) + " = ")
			e.valor(v.Value)
		}
		e.donde(c.Key)

	case change.DeleteRow:
		e.texto("DELETE FROM " + tabla)
		e.donde(c.Key)
		st.Note = "Se borra la fila. No se deshace."
	}

	st.SQL = e.legible.String()
	st.Bound = &change.Bound{SQL: e.ejecutable.String(), Args: e.args, Rows: 1, Op: c.Op()}
	return st, nil
}

// escritor arma las dos formas de la sentencia a la vez, para que no puedan
// diferir: cada pedazo de texto va a las dos, y cada valor va como literal a
// una y como marcador a la otra.
type escritor struct {
	d          Dialect
	legible    strings.Builder
	ejecutable strings.Builder
	args       []any
}

func (e *escritor) texto(s string) {
	e.legible.WriteString(s)
	e.ejecutable.WriteString(s)
}

// valor escribe un valor: literal en la forma legible, marcador en la
// ejecutable.
func (e *escritor) valor(v *string) {
	if v == nil {
		e.texto("NULL")
		return
	}
	e.legible.WriteString(e.d.QuoteLiteral(*v))
	e.args = append(e.args, v)
	e.ejecutable.WriteString(e.d.Placeholder(len(e.args)))
}

// donde escribe el WHERE que identifica la fila.
//
// Un valor nulo en la clave va como IS NULL y no como `= NULL`, que nunca es
// verdadero. No debería pasar —una clave primaria no admite nulos— salvo en
// SQLite, donde una PRIMARY KEY que no sea INTEGER puede tener NULL por una
// compatibilidad histórica que el propio manual lamenta.
func (e *escritor) donde(clave []change.Cell) {
	e.texto(" WHERE ")
	for i, k := range clave {
		if i > 0 {
			e.texto(" AND ")
		}
		if k.Value == nil {
			e.texto(e.d.QuoteIdent(k.Column) + " IS NULL")
			continue
		}
		e.texto(e.d.QuoteIdent(k.Column) + " = ")
		e.valor(k.Value)
	}
}

// CountWhere escribe la consulta que cuenta las filas que coinciden con
// `where`, con los valores como parámetros. Es lo que usan las comprobaciones
// de la revisión de filas: «¿la clave identifica una sola fila?», «¿existe el
// padre al que apunta esta clave foránea?», «¿cuántas hijas arrastra este
// borrado?».
//
// Sin condiciones no cuenta la tabla entera: devuelve una consulta que no
// coincide con nada. Contar todo por un descuido en quien llama sería la
// consulta más cara de la aplicación disparada sin querer.
func CountWhere(schema, table string, where []change.Cell, d Dialect) (string, []any) {
	e := escritor{d: d}
	e.texto("SELECT COUNT(*) FROM " + d.Table(schema, table))
	if len(where) == 0 {
		e.texto(" WHERE 1 = 0")
		return e.ejecutable.String(), nil
	}
	e.donde(where)
	return e.ejecutable.String(), e.args
}

// insertPrefix y insertSuffix aplican el default cuando el dialecto no los
// define, que es el caso de todo lo que no importa CSV.
func (d Dialect) prefijo(ignorar bool) string {
	if d.InsertPrefix == nil {
		return "INSERT INTO "
	}
	return d.InsertPrefix(ignorar)
}

func (d Dialect) sufijo(ignorar bool) string {
	if d.InsertSuffix == nil {
		return ""
	}
	return d.InsertSuffix(ignorar)
}

// InsertBatch escribe UN insert con varias filas y sus valores como parámetros.
//
// Es lo que usa la importación de CSV: mil filas por sentencia, porque una por
// fila hace un viaje al servidor por fila y todas juntas se pasa del límite de
// parámetros —Postgres admite 65535 por sentencia—.
//
// `ignorar` pide que las filas que chocan con una que ya está se salteen en vez
// de abortar. La cláusula es distinta en cada motor y por eso sale del dialecto.
func InsertBatch(
	schema, table string, columnas []string, filas [][]*string, d Dialect, ignorar bool,
) (string, []any) {
	var b strings.Builder
	var args []any

	b.WriteString(d.prefijo(ignorar))
	b.WriteString(d.Table(schema, table))
	b.WriteString(" (")
	for i, c := range columnas {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.QuoteIdent(c))
	}
	b.WriteString(") VALUES ")

	for i, fila := range filas {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for j, v := range fila {
			if j > 0 {
				b.WriteString(", ")
			}
			args = append(args, v)
			b.WriteString(d.Placeholder(len(args)))
		}
		b.WriteString(")")
	}
	b.WriteString(d.sufijo(ignorar))
	return b.String(), args
}
