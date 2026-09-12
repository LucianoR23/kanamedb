//go:build !android

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/LucianoR23/kanamedb/internal/service"
)

// bloquearEnSegundoPlano no hace nada en escritorio: una ventana detrás de
// otra no es un teléfono olvidado en la mesa. La versión de Android está en
// movil_android.go.
func bloquearEnSegundoPlano(*application.App, *service.Session) {}
