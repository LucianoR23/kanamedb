// Kaname — gestor de bases de datos de escritorio con diagrama ERD editable.
//
// La aplicación no expone ningún socket. Toda la comunicación entre el frontend
// y Go va por los bindings de Wails v3. Ver CLAUDE.md.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/LucianoR23/kanamedb/internal/appinfo"
	"github.com/LucianoR23/kanamedb/internal/secrets"
	"github.com/LucianoR23/kanamedb/internal/service"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// Los assets del frontend se embeben en el binario: no hay archivos sueltos que
// distribuir ni servidor de estáticos.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	info, err := appinfo.New().Get()
	if err != nil {
		// Sin saber dónde guardar la configuración no hay nada que hacer, y
		// arrancar igual dejaría la libreta de conexiones en cualquier lado.
		log.Fatalf("no se pudo ubicar el directorio de la aplicación: %v", err)
	}

	connections := store.New(info.Paths.Connections)
	keyring := secrets.New()
	known := tunnel.NewKnownHosts(info.Paths.KnownHosts)
	sesion := service.NewSession(connections, keyring, known)

	app := application.New(application.Options{
		Name:        "Kaname",
		Description: "Gestor de bases de datos con diagrama ERD editable",

		Services: []application.Service{
			application.NewService(appinfo.New()),
			application.NewService(service.NewConnections(connections, keyring)),
			application.NewService(sesion),
			application.NewService(service.NewQueries(sesion)),
			application.NewService(service.NewHosts(known)),
		},

		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Kaname",
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 640,
		// Coincide con --color-bg del tema oscuro para evitar el flash blanco
		// del webview durante el arranque.
		BackgroundColour: application.NewRGB(15, 17, 21),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
