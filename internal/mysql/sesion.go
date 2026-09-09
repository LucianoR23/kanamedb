package mysql

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"
)

// Este archivo resuelve un problema que no se ve mirando el código: **las
// opciones de sesión se pierden en el pool**.
//
// `SET SESSION …` vale para UNA conexión. Ejecutado con db.Exec después de
// abrir, se aplica a la que el pool haya entregado en ese momento, y la
// consulta siguiente puede salir por otra que nunca lo recibió. Con
// SetConnMaxLifetime encima, la única conexión configurada se retira a los
// treinta minutos y a partir de ahí NINGUNA lo tiene.
//
// Para el modo solo lectura eso no es un detalle: es la diferencia entre una
// conexión de producción protegida y una que parece protegida. Abrir una tabla
// —conexión 1— y después correr un DELETE en el editor —conexión 2— y que se
// ejecute.
//
// La solución es envolver el conector: cada conexión que el pool abre pasa por
// acá y recibe su configuración antes de entregarse.

// sentencia es algo que hay que correr en cada conexión nueva.
type sentencia struct {
	sql string
	// obligatoria hace fallar la conexión si la sentencia falla. Va en false
	// para las que dependen del motor, donde se prueban las dos variantes y
	// alcanza con que ande una.
	obligatoria bool
}

// conector envuelve al del driver para configurar cada conexión que abre.
type conector struct {
	base driver.Connector
	init []sentencia
	// alMenosUna exige que alguna de las opcionales haya funcionado. Sin esto,
	// un límite de tiempo que ningún motor entiende se convertiría en «sin
	// límite» sin que nadie se entere.
	alMenosUna bool
}

var _ driver.Connector = (*conector)(nil)

func (c *conector) Driver() driver.Driver { return c.base.Driver() }

func (c *conector) Connect(ctx context.Context) (driver.Conn, error) {
	cn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	ex, ok := cn.(driver.ExecerContext)
	if !ok {
		cn.Close()
		return nil, fmt.Errorf("el driver de MySQL no acepta ejecutar la configuración de sesión")
	}

	anduvoAlguna := false
	hayOpcionales := false
	for _, s := range c.init {
		_, err := ex.ExecContext(ctx, s.sql, nil)
		if !s.obligatoria {
			hayOpcionales = true
			if err == nil {
				anduvoAlguna = true
			}
			continue
		}
		if err != nil {
			cn.Close()
			return nil, fmt.Errorf("configurar la sesión: %w", err)
		}
	}
	if c.alMenosUna && hayOpcionales && !anduvoAlguna {
		cn.Close()
		return nil, fmt.Errorf(
			"el servidor no acepta ninguna de las formas de limitar el tiempo de una " +
				"sentencia; sin eso, una consulta pesada se queda corriendo aunque la " +
				"aplicación se cierre")
	}
	return cn, nil
}

// sesionDe arma las sentencias de configuración de una conexión.
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
