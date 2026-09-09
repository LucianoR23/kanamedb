package service

import (
	"context"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/mysql"
	"github.com/LucianoR23/kanamedb/internal/postgres"
	"github.com/LucianoR23/kanamedb/internal/sqlite"
)

// El despacho por motor vive acá y no en `internal/engine`.
//
// No es una preferencia: sería un ciclo de importación. Los tres paquetes de
// motor importan `engine` para implementar su interfaz, así que `engine` no
// puede importarlos de vuelta. Alguien tiene que conocer a los cuatro, y ese
// alguien es el servicio — que es justamente el único que necesita elegir.
//
// La consecuencia buena es que el resto del servicio no vuelve a nombrar un
// motor: a partir de acá todo habla con `engine.Conn`.

// abrirMotor abre la conexión con el motor que corresponda.
func abrirMotor(
	ctx context.Context, c connection.Connection, dsn string, opts engine.OpenOptions,
) (engine.Conn, *engine.Failure) {
	switch c.Engine {
	case connection.Postgres:
		return conO(postgres.Open(ctx, dsn, c.Describe(), opts))
	case connection.MySQL, connection.MariaDB:
		return conO(mysql.Open(ctx, dsn, c.Describe(), opts))
	case connection.SQLite:
		return conO(sqlite.Open(ctx, dsn, c.Describe(), opts))
	}
	return nil, &engine.Failure{
		Kind:    engine.FailureOther,
		Message: "No se reconoce el motor «" + string(c.Engine) + "».",
		Hint: "El archivo de conexiones acepta postgres, mysql, mariadb y sqlite. " +
			"Puede estar mal escrito.",
	}
}

// probarMotor abre, lee los datos del servidor y cierra.
//
// Es lo que hay detrás del botón «Probar conexión». Va por el mismo camino que
// abrirMotor a propósito: probar con un código y conectar con otro es cómo se
// consigue que la prueba pase y la conexión falle.
func probarMotor(
	ctx context.Context, c connection.Connection, dsn string, dial engine.DialFunc,
) (*engine.ServerInfo, *engine.Failure) {
	cn, f := abrirMotor(ctx, c, dsn, engine.OpenOptions{MaxConns: 1, DialFunc: dial})
	if f != nil {
		return nil, f
	}
	defer cn.Close()
	return cn.Server(), nil
}

// conO adapta el `(*T, *Failure)` de cada motor a la interfaz.
//
// Hace falta por una trampa de Go que muerde cada vez: devolver directamente un
// `*postgres.Conn` nil como `engine.Conn` da una interfaz que NO es nil —tiene
// tipo y valor nil— así que un `if c == nil` de quien llama no la ataja y el
// primer método revienta.
func conO[T engine.Conn](c T, f *engine.Failure) (engine.Conn, *engine.Failure) {
	if f != nil {
		return nil, f
	}
	return c, nil
}
