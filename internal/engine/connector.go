package engine

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// Este archivo resuelve un problema que no se ve mirando el código: **las
// opciones de sesión se pierden en el pool**.
//
// `SET SESSION …` vale para UNA conexión. Ejecutado con db.Exec después de
// abrir, se aplica a la que el pool haya entregado en ese momento, y la
// consulta siguiente puede salir por otra que nunca lo recibió. Para el modo
// solo lectura eso no es un detalle: es la diferencia entre una conexión de
// producción protegida y una que parece protegida.
//
// La solución es envolver el conector: cada conexión que el pool abre pasa
// por acá y recibe su configuración antes de entregarse. Lo usan MySQL y
// SQLite, los dos motores que van por database/sql; Postgres tiene lo mismo
// en `pgxpool.Config.AfterConnect`.

// Init configura una conexión recién abierta. Si falla, la conexión se
// descarta y el error sale por el Ping o la primera consulta.
type Init func(ctx context.Context, ex driver.ExecerContext) error

type initConnector struct {
	base driver.Connector
	init Init
}

// WithInit envuelve un conector para que cada conexión pase por `init`.
func WithInit(base driver.Connector, init Init) driver.Connector {
	return &initConnector{base: base, init: init}
}

func (c *initConnector) Driver() driver.Driver { return c.base.Driver() }

func (c *initConnector) Connect(ctx context.Context) (driver.Conn, error) {
	cn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	ex, ok := cn.(driver.ExecerContext)
	if !ok {
		cn.Close()
		return nil, errors.New("el driver no acepta ejecutar la configuración de sesión")
	}
	if err := c.init(ctx, ex); err != nil {
		cn.Close()
		return nil, err
	}
	return cn, nil
}

// RunSessionSQL corre las sentencias de la SQL de sesión, de a una, y
// devuelve un SessionSQLError con la línea de la que falló.
//
// De a una y no todas juntas: el driver de MySQL rechaza varias sentencias
// en un viaje —multiStatements está apagado a propósito— y, con cualquiera,
// «falló la de la línea 3» es lo que la persona necesita para arreglarla.
func RunSessionSQL(ctx context.Context, ex driver.ExecerContext, sentencias []query.Statement) error {
	for _, st := range sentencias {
		if _, err := ex.ExecContext(ctx, st.SQL, nil); err != nil {
			return &SessionSQLError{Line: st.Line, Err: err}
		}
	}
	return nil
}
