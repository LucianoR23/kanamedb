//go:build android

package appinfo

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// resolvePaths en Android: todo cuelga del directorio privado de la app
// (`Context.getFilesDir()`), que el sistema crea, protege por UID y borra al
// desinstalar. No hay %APPDATA% ni ~/.config: os.UserConfigDir devuelve
// /sdcard/.config, que es almacenamiento externo y desde API 30 ni siquiera
// se puede escribir.
//
// Es el único lugar de appinfo que depende de Wails —el directorio llega por
// el bridge JNI, que ya está inicializado cuando corre main— y por eso vive en
// un archivo con build tag: en escritorio appinfo no sabe de Wails.
//
// Una sola raíz, como en Windows: configuración y estado juntos. El reparto
// entre los dos lo decide PathsIn igual que en el resto de las plataformas.
func resolvePaths() (Paths, error) {
	dir := application.Mobile.StoragePath()
	if dir == "" {
		return Paths{}, errors.New("Android no informó el directorio privado de la aplicación")
	}
	return PathsIn(dir, dir), nil
}
