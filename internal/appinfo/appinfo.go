// Package appinfo expone al frontend los datos de identidad y ubicación de la
// aplicación: versión, plataforma y dónde guarda sus archivos.
//
// Es el contrato de la pantalla S25 About. Todo lo que muestra sale de acá y no
// de constantes en el frontend, para que no pueda mentir sobre el binario que
// está corriendo.
package appinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
)

// Version y BuildDate se inyectan en el build con -ldflags. Los valores por
// defecto son los de una compilación local sin flags.
var (
	Version   = "0.1.0"
	BuildDate = "dev"
)

// Info es lo que la pantalla About recibe. Todos los campos son strings ya
// formateados: la UI muestra, no calcula.
type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
	Paths     Paths  `json:"paths"`
}

// Paths son los directorios donde la app guarda estado en esta máquina.
//
// Deliberadamente no hay ninguna ruta para credenciales: las contraseñas viven
// en el keychain del sistema operativo y nunca tocan el disco. Ver CLAUDE.md.
type Paths struct {
	// Connections es la libreta de conexiones. Sin secretos: es el archivo que
	// tiene sentido sincronizar entre máquinas.
	Connections string `json:"connections"`

	// Config son las preferencias de la aplicación. Sin secretos.
	Config string `json:"config"`
	// State es el historial, las posiciones del ERD y el resto del estado
	// local. SQLite. Sin secretos.
	State string `json:"state"`
	// Logs es donde van los diagnósticos. Nunca contienen credenciales,
	// connection strings ni valores de filas.
	Logs string `json:"logs"`

	// KnownHosts son las claves de servidores SSH que se aceptaron.
	//
	// Es un archivo propio y no `~/.ssh/known_hosts`: escribir en el de OpenSSH
	// es meterse con configuración que otras herramientas también usan, y tener
	// las decisiones de confianza de Kaname en un solo lugar permite auditarlas
	// de un vistazo. El formato sí es el de OpenSSH, así que se lee con
	// `ssh-keygen -F` sin aprender nada nuevo.
	//
	// Contiene claves PÚBLICAS de servidores. No es un secreto: es lo que
	// cualquiera obtiene conectándose a esos hosts. Pero sí es una lista de a
	// qué máquinas se conecta esta persona, así que va junto a la configuración
	// y no se sincroniza salvo que se quiera.
	KnownHosts string `json:"knownHosts"`
}

// Service es el servicio que se registra en Wails.
type Service struct{}

// New construye el servicio.
func New() *Service { return &Service{} }

// Get devuelve la información de la aplicación.
func (s *Service) Get() (Info, error) {
	paths, err := resolvePaths()
	if err != nil {
		return Info{}, fmt.Errorf("resolver rutas de la aplicación: %w", err)
	}
	return Info{
		Name:      "Kaname",
		Version:   Version,
		BuildDate: BuildDate,
		GoVersion: goVersion(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		Paths:     paths,
	}, nil
}

// goVersion prefiere la versión con la que se compiló el binario; si la
// información de build no está disponible cae a la del runtime.
func goVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.GoVersion != "" {
		return bi.GoVersion
	}
	return runtime.Version()
}

// appDirName es el subdirectorio bajo el directorio de datos del usuario.
const appDirName = "Kaname"

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

	return Paths{
		Connections: filepath.Join(base, "connections.toml"),
		Config:      filepath.Join(base, "config.toml"),
		State:       state,
		Logs:        filepath.Join(state, "logs"),
		KnownHosts:  filepath.Join(base, "known_hosts"),
	}, nil
}
