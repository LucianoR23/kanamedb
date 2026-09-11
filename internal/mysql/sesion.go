package mysql

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// La configuración de sesión va en el conector y no en un Exec después de
// abrir, por lo que explica engine.WithInit: un `SET SESSION` vale para una
// sola conexión y el pool abre varias. Acá va lo propio de MySQL: qué
// sentencias, en qué orden, y cuáles pueden fallar.

// sentencia es algo que hay que correr en cada conexión nueva.
type sentencia struct {
	sql string
	// obligatoria hace fallar la conexión si la sentencia falla. Va en false
	// para las que dependen del motor, donde se prueban las dos variantes y
	// alcanza con que ande una.
	obligatoria bool
}

// initDe arma la configuración de cada conexión: las protecciones, la SQL de
// sesión de la persona, y las protecciones OTRA VEZ.
//
// Dos veces y no una, a propósito. En Postgres y SQLite el modo solo lectura
// viaja en el arranque —paquete de inicio, pragma del DSN—, así que la SQL de
// sesión ya corre protegida; en MySQL es un SET como cualquier otro, y si
// fuera solo después, un `DELETE` en la SQL de sesión de una conexión de solo
// lectura correría con escritura en cada conexión del pool —lo encontró el
// review—. Antes, para que la SQL de sesión corra protegida; después, para
// que una que las toque —`SET SESSION TRANSACTION READ WRITE`— no las gane.
// Lo último que se dice es lo que queda.
func initDe(opts engine.OpenOptions) engine.Init {
	sesion := engine.SessionStatements(opts.SessionSQL, engine.MySQL)
	protecciones, exigeUna := sesionDe(opts.ReadOnly, opts.StatementTimeout)

	proteger := func(ctx context.Context, ex driver.ExecerContext) error {
		anduvoAlguna := false
		hayOpcionales := false
		for _, s := range protecciones {
			_, err := ex.ExecContext(ctx, s.sql, nil)
			if !s.obligatoria {
				hayOpcionales = true
				if err == nil {
					anduvoAlguna = true
				}
				continue
			}
			if err != nil {
				return fmt.Errorf("configurar la sesión: %w", err)
			}
		}
		// alMenosUna exige que alguna de las opcionales haya funcionado. Sin
		// esto, un límite de tiempo que ningún motor entiende se convertiría
		// en «sin límite» sin que nadie se entere.
		if exigeUna && hayOpcionales && !anduvoAlguna {
			return fmt.Errorf(
				"el servidor no acepta ninguna de las formas de limitar el tiempo de una " +
					"sentencia; sin eso, una consulta pesada se queda corriendo aunque la " +
					"aplicación se cierre")
		}
		return nil
	}

	return func(ctx context.Context, ex driver.ExecerContext) error {
		if err := proteger(ctx, ex); err != nil {
			return err
		}
		if len(sesion) == 0 {
			return nil
		}
		if err := engine.RunSessionSQL(ctx, ex, sesion); err != nil {
			return err
		}
		return proteger(ctx, ex)
	}
}

// sesionDe arma las sentencias de protección de una conexión.
//
// El límite de tiempo se manda en las dos formas que existen porque el motor
// todavía no se conoce: se sabe recién después de conectar, y esto corre antes.
// MySQL tiene max_execution_time y lo mide en MILISEGUNDOS; MariaDB tiene
// max_statement_time y lo mide en SEGUNDOS, con decimales. Cada motor rechaza
// la variable del otro con «Unknown system variable», así que se marcan
// opcionales y se exige que una haya andado.
func sesionDe(soloLectura bool, timeout time.Duration) ([]sentencia, bool) {
	var out []sentencia
	if soloLectura {
		// Se lo pide AL SERVIDOR, no se comprueba acá: así se rechazan también
		// las escrituras que no pasen por nuestro código. Ver CLAUDE.md.
		out = append(out, sentencia{"SET SESSION TRANSACTION READ ONLY", true})
	}
	if timeout > 0 {
		ms := timeout.Milliseconds()
		if ms < 1 {
			ms = 1
		}
		out = append(out,
			sentencia{fmt.Sprintf("SET SESSION max_execution_time = %d", ms), false},
			sentencia{fmt.Sprintf("SET SESSION max_statement_time = %.3f", timeout.Seconds()), false},
		)
		return out, true
	}
	return out, false
}
