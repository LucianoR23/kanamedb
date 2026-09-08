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
)

// Los assets del frontend se embeben en el binario: no hay archivos sueltos que
// distribuir ni servidor de estáticos.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "Kaname",
		Description: "Gestor de bases de datos con diagrama ERD editable",

		// Los servicios expuestos al frontend se registran acá. Cada iteración
		// define su contrato antes de la UI (ver kaname-plan.md).
		Services: []application.Service{
			application.NewService(appinfo.New()),
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
