//go:build !android

package appinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// resolvePaths ubica los directorios de la app según la convención del sistema.
// En Windows los tres cuelgan de %APPDATA%\Kaname; en el resto, de las rutas
// que devuelve el runtime para config y datos de usuario.
func resolvePaths() (Paths, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("directorio de configuración del usuario: %w", err)
	}
	base := filepath.Join(config, appDirName)

	state := base
	if runtime.GOOS != "windows" {
		// En Linux y macOS el estado no va junto a la configuración.
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("directorio del usuario: %w", err)
		}
		if runtime.GOOS == "darwin" {
			state = filepath.Join(home, "Library", "Application Support", appDirName)
		} else {
			state = filepath.Join(home, ".local", "share", appDirName)
		}
	}

	return PathsIn(base, state), nil
}
