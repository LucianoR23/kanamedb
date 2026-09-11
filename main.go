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
	"github.com/LucianoR23/kanamedb/internal/history"
	"github.com/LucianoR23/kanamedb/internal/layout"
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

	app := application.New(application.Options{
		Name:        "Kaname",
		Description: "Gestor de bases de datos con diagrama ERD editable",

		Services: servicios(info.Paths),

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

// servicios arma todo lo que el frontend puede llamar.
//
// Está aparte de main() por una razón concreta: un servicio que falte en esta
// lista NO rompe nada visible. Compila, el generador de bindings lo encuentra
// igual —recorre el código, no esta lista— y el frontend lo importa y lo llama
// con tipos correctos. Recién falla al apretar el botón, en tiempo de
// ejecución. Le pasó al historial, que estuvo escrito, testeado y con su
// pantalla hecha mientras el servicio no estaba registrado y el store nunca se
// construía: no se anotaba una sola consulta. Ver `main_test.go`, que compara
// esta lista contra lo que el frontend importa de verdad.
func servicios(rutas appinfo.Paths) []application.Service {
	connections := store.New(rutas.Connections)
	keyring := secrets.New()
	known := tunnel.NewKnownHosts(rutas.KnownHosts)
	diagramas := layout.New(rutas.Layouts)
	sesion := service.NewSession(connections, keyring, known, diagramas)
	// Las exportaciones se registran en el mismo lugar que las consultas, para
	// que «Cancelar» corte cualquiera de las dos con el mismo identificador.
	consultas := service.NewQueries(sesion)
	exportaciones := service.NewExports(consultas)

	// Dos archivos y dos dueños: el historial es de esta máquina, las
	// guardadas viajan con la libreta de conexiones. Ver internal/history.
	historial := history.New(rutas.History, rutas.SavedQueries)
	consultas.UsarHistorial(historial)

	return []application.Service{
		application.NewService(appinfo.New()),
		application.NewService(service.NewConnections(connections, keyring, known)),
		application.NewService(sesion),
		application.NewService(consultas),
		application.NewService(service.NewHosts(known)),
		application.NewService(exportaciones),
		application.NewService(service.NewImports(consultas)),
		application.NewService(service.NewDumps(exportaciones, consultas)),
		application.NewService(service.NewHistory(historial, sesion)),
	}
}
