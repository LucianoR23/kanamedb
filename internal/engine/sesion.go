package engine

import (
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// DialectOf es el dialecto con el que se parte la SQL de un motor cuando
// todavía no hay conexión que lo diga.
//
// Cada `Conn` tiene su `Dialect()`, y MySQL lo ajusta con lo que el servidor
// contestó —NO_BACKSLASH_ESCAPES—. Esto es el default de cada motor, para lo
// que se parte ANTES de conectar: la SQL de sesión, que corre en cada
// conexión nueva, y los avisos de la libreta, que no conectan a nada.
func DialectOf(k Kind) query.Dialect {
	switch k {
	case Postgres:
		return query.Dialect{DollarQuotes: true}
	case MySQL, MariaDB:
		return query.Dialect{Backtick: true, HashComments: true, BackslashEscapes: true, Compound: true}
	case SQLite:
		return query.Dialect{Brackets: true, Compound: true}
	}
	return query.Dialect{}
}

// SessionStatements parte la SQL de sesión en sentencias, con el dialecto del
// motor. Vacía o solo comentarios da nil: no hay nada que correr.
func SessionStatements(sql string, k Kind) []query.Statement {
	var out []query.Statement
	for _, st := range query.Split(sql, DialectOf(k)) {
		if query.Command(st.SQL, DialectOf(k)) == "" {
			continue
		}
		out = append(out, st)
	}
	return out
}

// SessionSQLError es que una sentencia de la SQL de sesión falló al abrir una
// conexión. Lleva la línea para que el fallo diga cuál, y el error del motor
// adentro para que la clasificación siga funcionando sobre él.
type SessionSQLError struct {
	Line int
	Err  error
}

func (e *SessionSQLError) Error() string {
	return fmt.Sprintf("la SQL de sesión falló en la línea %d: %v", e.Line, e.Err)
}

func (e *SessionSQLError) Unwrap() error { return e.Err }

// SessionSQLFailure convierte el fallo de una sentencia de sesión —ya
// clasificado por el motor— en el que ve la persona: dice que es la SQL de
// sesión, en qué línea, y dónde se arregla. Sin esto, un error de sintaxis
// en un SET que se escribió hace meses aparece como «no se pudo conectar».
func SessionSQLFailure(e *SessionSQLError, clasificado *Failure) *Failure {
	if clasificado == nil {
		clasificado = &Failure{Kind: FailureOther, Message: e.Err.Error()}
	}
	return &Failure{
		Kind:     clasificado.Kind,
		Message:  fmt.Sprintf("La SQL de sesión falló en la línea %d: %s", e.Line, clasificado.Message),
		Hint:     "Corre en cada conexión que se abre, antes que nada. Se edita en la pestaña Advanced de la conexión.",
		SQLState: clasificado.SQLState,
		Detail:   clasificado.Detail,
	}
}
