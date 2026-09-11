// Package config son las preferencias de la aplicación: lo que vale para todo
// Kaname y no para una conexión en particular.
//
// Es el contrato de S23 Settings. Hasta ahora `config.toml` era una ruta que la
// pantalla About mostraba y que nadie escribía nunca.
//
// # Dos reglas que ordenan todo el paquete
//
// **Acá no entra ningún secreto.** Este archivo se lee y se escribe en claro, y
// las contraseñas viven en el keychain del sistema. Ver CLAUDE.md, y ver
// `TestNingunCampoDeConfigSuenaAUnSecreto`, que recorre la estructura por
// reflexión para que agregar un campo llamado `token` falle en CI.
//
// **El valor cero es lo que la aplicación ya hacía.** El archivo se edita a
// mano y una versión futura va a leer archivos escritos por una anterior: a la
// clave que falta le toca el comportamiento de siempre, nunca uno nuevo y nunca
// una protección apagada. Por eso los campos se llaman `HideLineNumbers` y
// `NoWrap` en vez de `LineNumbers` y `Wrap`, que con el cero apagarían dos
// cosas que hoy están prendidas.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/LucianoR23/kanamedb/internal/connection"
)

// versionActual es el formato de este archivo. Sube cuando un cambio no se
// puede leer con la versión anterior.
const versionActual = 1

// Theme es el tema de la interfaz.
//
// El cero es la cadena vacía y significa «lo que la aplicación hizo siempre»,
// que es el tema oscuro. Es la misma decisión que `EffectiveSSLMode`: el campo
// ausente no puede cambiar el aspecto de la app por su cuenta.
type Theme string

const (
	ThemeDark   Theme = "dark"
	ThemeLight  Theme = "light"
	ThemeSystem Theme = "system"
)

// Effective es el tema que se va a usar de verdad.
func (t Theme) Effective() Theme {
	switch t {
	case ThemeLight, ThemeSystem, ThemeDark:
		return t
	default:
		return ThemeDark
	}
}

// Editor son las preferencias del editor SQL.
type Editor struct {
	// FontSize es el tamaño del texto del editor, en píxeles. Cero es el
	// default.
	FontSize int `toml:"font_size" json:"fontSize"`

	// HideLineNumbers apaga la regleta de números.
	//
	// Nombrado en negativo a propósito: hoy los números están prendidos, así
	// que el cero tiene que dejarlos prendidos. Con `LineNumbers bool`, un
	// archivo viejo —o uno al que le falta la clave— apagaría la regleta sin
	// que nadie lo haya pedido.
	HideLineNumbers bool `toml:"hide_line_numbers" json:"hideLineNumbers"`

	// NoWrap corta el ajuste de línea y deja que el editor scrollee a lo ancho.
	// En negativo por lo mismo que el anterior.
	NoWrap bool `toml:"no_wrap" json:"noWrap"`
}

// Defaults del editor. El rango no es decorativo: abajo de 10 px el texto
// monoespaciado deja de ser legible y arriba de 24 entran cuatro líneas en
// pantalla, y los dos extremos se alcanzan editando el archivo a mano.
const (
	DefaultFontSize = 13
	MinFontSize     = 10
	MaxFontSize     = 24
)

// EffectiveFontSize es el tamaño que se va a usar de verdad.
func (e Editor) EffectiveFontSize() int {
	if e.FontSize < MinFontSize || e.FontSize > MaxFontSize {
		return DefaultFontSize
	}
	return e.FontSize
}

// Updates es lo que quedó de la última consulta manual de versiones.
//
// Se guarda para poder DECIR cuándo fue: un botón que no deja rastro obliga a
// apretarlo para saber si hacía falta. No habilita ninguna consulta automática
// —eso no existe en esta aplicación, ver CLAUDE.md—: son dos strings que la
// pantalla muestra.
type Updates struct {
	// LastCheck es cuándo se consultó, en RFC 3339. Vacío: nunca se consultó.
	//
	// Es texto y no time.Time porque el único que lo mira es la pantalla, y
	// porque así el archivo dice algo legible al abrirlo con un editor.
	LastCheck string `toml:"last_check,omitempty" json:"lastCheck"`

	// LastSeen es la última versión publicada que se vio. Vacío: ninguna.
	LastSeen string `toml:"last_seen,omitempty" json:"lastSeen"`
}

// Config son las preferencias completas.
type Config struct {
	Version int `toml:"version" json:"version"`

	// Theme es el tema de la interfaz. Vacío: oscuro.
	Theme Theme `toml:"theme,omitempty" json:"theme"`

	Editor Editor `toml:"editor" json:"editor"`

	// NewConnection son las protecciones con las que NACE una conexión nueva.
	//
	// No toca ninguna conexión existente: es el punto de partida del formulario
	// y nada más. Sirve para quien trabaja casi siempre contra bases que no son
	// suyas y quiere que lo primero que exista sea una conexión de solo lectura,
	// en vez de acordarse de marcarla cada vez.
	//
	// El tipo es el mismo `connection.Safety` que edita la pestaña Safety de
	// S03, así que la pantalla también es la misma. Dos estructuras con los
	// mismos campos se separan; un default que se separa del valor real es un
	// default que empieza a mentir.
	NewConnection connection.Safety `toml:"new_connection" json:"newConnection"`

	Updates Updates `toml:"updates,omitempty" json:"updates"`
}

// Normalize deja la configuración en un estado que el resto del código puede
// usar sin volver a preguntarse nada.
//
// Un valor que esta build no entiende vuelve al default en vez de ser un error:
// el archivo se edita a mano, y lo que está en juego es una preferencia, no un
// dato. Negarse a arrancar porque alguien escribió `theme = "violeta"` sería
// desproporcionado.
func (c Config) Normalize() Config {
	c.Version = versionActual
	c.Theme = c.Theme.Effective()
	c.Editor.FontSize = c.Editor.EffectiveFontSize()
	c.Updates.LastCheck = strings.TrimSpace(c.Updates.LastCheck)
	c.Updates.LastSeen = strings.TrimSpace(c.Updates.LastSeen)
	return c
}

// Default es la configuración de una instalación nueva.
func Default() Config { return Config{}.Normalize() }

// Store es el archivo de preferencias.
type Store struct {
	path string
	mu   sync.Mutex
}

// New arma el store sobre una ruta.
func New(path string) *Store { return &Store{path: path} }

// Path es dónde está el archivo. La pantalla lo muestra: una preferencia que no
// se sabe dónde vive no se puede respaldar ni copiar a otra máquina.
func (s *Store) Path() string { return s.path }

// Load lee las preferencias. Un archivo ausente es una instalación nueva.
func (s *Store) Load() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leer()
}

func (s *Store) leer() (Config, error) {
	datos, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("leer %s: %w", s.path, err)
	}

	var c Config
	if _, err := toml.Decode(string(datos), &c); err != nil {
		// El contenido NO va en el mensaje. Acá no debería haber secretos
		// —ésa es la regla del paquete— pero un error tampoco es lugar para
		// volcar un archivo entero.
		return Default(), fmt.Errorf("el archivo de preferencias %s está corrupto: %w", s.path, err)
	}
	if c.Version > versionActual {
		return Default(), fmt.Errorf(
			"el archivo de preferencias %s es de la versión %d y esta build entiende hasta la %d: actualizá Kaname",
			s.path, c.Version, versionActual)
	}
	return c.Normalize(), nil
}

// Save guarda las preferencias y devuelve lo que quedó escrito.
//
// Devuelve la configuración normalizada y no un error vacío porque la pantalla
// tiene que mostrar lo que se guardó: si pidió un cuerpo de 40 px y quedó en 13,
// el campo tiene que decirlo en vez de mentir hasta la próxima apertura.
func (s *Store) Save(c Config) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Un archivo de una versión que esta build no entiende NO se pisa.
	//
	// `leer` ya se negaba a leerlo, y eso solo cubría la mitad: quien tenga dos
	// versiones de Kaname instaladas abría la vieja, veía el cartel de que no se
	// pudo leer, tocaba cualquier cosa —los controles seguían vivos— y el primer
	// guardado reemplazaba el archivo de la nueva con uno viejo. Perder las
	// preferencias así es exactamente lo que el número de versión existe para
	// evitar.
	if v, err := s.versionEnDisco(); err == nil && v > versionActual {
		return c.Normalize(), fmt.Errorf(
			"no se guardó: %s es de la versión %d y esta build entiende hasta la %d. "+
				"Abrilo con la versión nueva de Kaname, o borrá el archivo para empezar de cero",
			s.path, v, versionActual)
	}
	return s.escribir(c.Normalize())
}

// versionEnDisco lee SOLO el número de versión del archivo.
//
// Decodifica en una estructura con un campo y no en `Config` a propósito: un
// archivo de una versión futura puede tener formas que esta build no sepa
// decodificar —una clave que pasó de número a tabla— y fallar ahí haría que el
// guardado siguiera adelante justo en el caso que hay que frenar.
func (s *Store) versionEnDisco() (int, error) {
	datos, err := os.ReadFile(s.path)
	if err != nil {
		return 0, err
	}
	var solo struct {
		Version int `toml:"version"`
	}
	// El error se ignora: un decodificado parcial igual deja `version` puesta si
	// estaba, y lo que se busca es exactamente eso.
	_, _ = toml.Decode(string(datos), &solo)
	return solo.Version, nil
}

// Escritura atómica: temporal en el mismo directorio y rename encima. Un corte
// a mitad de escritura no puede dejar el archivo truncado. Es lo mismo que hace
// la libreta de conexiones, por la misma razón.
func (s *Store) escribir(c Config) (Config, error) {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return c, fmt.Errorf("crear %s: %w", dir, err)
	}

	var buf strings.Builder
	buf.WriteString("# Preferencias de Kaname.\n")
	buf.WriteString("# Acá NO hay contraseñas ni ningún otro secreto: viven en el keychain del sistema.\n")
	buf.WriteString("# Se puede editar a mano. Una clave que falta usa el valor de siempre.\n\n")
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return c, fmt.Errorf("serializar las preferencias: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return c, fmt.Errorf("crear archivo temporal en %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(buf.String()); err != nil {
		tmp.Close()
		return c, fmt.Errorf("escribir %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return c, fmt.Errorf("sincronizar %s a disco: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return c, fmt.Errorf("cerrar %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return c, fmt.Errorf("ajustar permisos de %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return c, fmt.Errorf("reemplazar %s: %w", s.path, err)
	}
	return c, nil
}

// AnotarConsulta deja registrado que se consultó por versiones nuevas.
//
// Es una operación aparte y no un Save entero porque quien consulta versiones
// no está editando preferencias: pisar el archivo con lo que la pantalla tenía
// en pantalla borraría cualquier cambio hecho a mano mientras tanto.
func (s *Store) AnotarConsulta(cuando, ultimaVista string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, err := s.leer()
	if err != nil {
		return err
	}
	c.Updates.LastCheck = cuando
	c.Updates.LastSeen = ultimaVista
	_, err = s.escribir(c)
	return err
}
