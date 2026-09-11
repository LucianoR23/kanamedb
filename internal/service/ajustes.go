package service

import (
	"context"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/config"
	"github.com/LucianoR23/kanamedb/internal/history"
	"github.com/LucianoR23/kanamedb/internal/update"
)

// Settings es la pantalla de ajustes, S23.
//
// No necesita una conexión abierta: las preferencias son de la aplicación y se
// editan igual desde la pantalla de bienvenida.
type Settings struct {
	store     *config.Store
	updates   *update.Checker
	historial *history.Store

	// version es la del binario. Llega por parámetro y no se lee de appinfo
	// acá para que el test pueda fijarla: comparar contra "la que compiló esta
	// máquina" haría que el caso pase o falle según quién corre los tests.
	version   string
	buildDate string
}

// NewSettings arma el servicio.
func NewSettings(st *config.Store, ch *update.Checker, h *history.Store, version, buildDate string) *Settings {
	return &Settings{store: st, updates: ch, historial: h, version: version, buildDate: buildDate}
}

// SettingsView es lo que la pantalla recibe.
type SettingsView struct {
	Config config.Config `json:"config"`

	// Path es dónde está el archivo. Se muestra: una preferencia que no se sabe
	// dónde vive no se puede respaldar ni copiar a otra máquina.
	Path string `json:"path"`

	// Problem explica por qué lo que se está mostrando NO salió del archivo.
	//
	// Existe porque el caso silencioso es el peligroso: un `config.toml` roto a
	// mano devuelve los defaults, la pantalla los muestra como si fueran los
	// guardados, y el primer «Guardar» pisa el archivo original sin que nadie
	// se haya enterado de que había un problema.
	Problem string `json:"problem"`

	// LocalBuild dice si esto se compiló acá en vez de venir de una publicación.
	// Consultar versiones nuevas desde una build local no significa lo mismo.
	LocalBuild bool `json:"localBuild"`

	Version string `json:"version"`
}

// HistoryStats es cuánto historial hay guardado en esta máquina.
//
// La pantalla de ajustes ofrece borrarlo todo, y un botón de borrar que no dice
// cuánto va a borrar es un botón que no se aprieta o que se aprieta mal.
type HistoryStats struct {
	Entries int `json:"entries"`
	Saved   int `json:"saved"`
}

// Get devuelve las preferencias.
func (s *Settings) Get(_ context.Context) SettingsView {
	v := SettingsView{
		Path:       s.store.Path(),
		Version:    s.version,
		LocalBuild: s.buildDate == "" || s.buildDate == "dev",
	}
	c, err := s.store.Load()
	if err != nil {
		v.Problem = err.Error()
	}
	v.Config = c
	return v
}

// Save guarda las preferencias y devuelve lo que quedó escrito.
func (s *Settings) Save(_ context.Context, c config.Config) (SettingsView, error) {
	guardada, err := s.store.Save(c)
	if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{
		Config:     guardada,
		Path:       s.store.Path(),
		Version:    s.version,
		LocalBuild: s.buildDate == "" || s.buildDate == "dev",
	}, nil
}

// UpdateCheck es lo que devuelve la consulta de versiones.
//
// Trae el resultado Y las preferencias releídas. Las dos juntas y no solo la
// primera porque consultar ESCRIBE —queda anotado cuándo fue—, y la pantalla
// tiene en memoria una copia que acaba de quedar vieja. Sin esto, el siguiente
// cambio de cualquier ajuste guardaba esa copia y borraba la constancia de la
// consulta: `AnotarConsulta` relee el archivo justamente para no pisar lo que
// otro tocó, y lo pisaba la propia pantalla desde el otro lado.
type UpdateCheck struct {
	Result   update.Result `json:"result"`
	Settings SettingsView  `json:"settings"`
}

// CheckForUpdates consulta si hay una versión más nueva publicada.
//
// Es lo ÚNICO de la aplicación que sale a internet por su cuenta, y sale solo
// cuando alguien aprieta el botón: no hay temporizador, no corre al arrancar y
// no corre al conectar. Ver el paquete `internal/update`.
func (s *Settings) CheckForUpdates(ctx context.Context) UpdateCheck {
	res := s.updates.Check(ctx, s.version)
	// Se anota aunque haya fallado: «se consultó y no se pudo» es información,
	// y sin anotarlo la pantalla diría «nunca se consultó» después de haberlo
	// intentado. Un fallo al escribir la preferencia no toca el resultado.
	_ = s.store.AnotarConsulta(res.CheckedAt, res.Latest)
	return UpdateCheck{Result: res, Settings: s.Get(ctx)}
}

// HistorySize dice cuánto historial hay en esta máquina.
func (s *Settings) HistorySize(_ context.Context) (HistoryStats, error) {
	if s.historial == nil {
		return HistoryStats{}, fmt.Errorf("el historial no está disponible")
	}
	entradas, err := s.historial.List("", 0)
	if err != nil {
		return HistoryStats{}, err
	}
	guardadas, err := s.historial.Saved("")
	if err != nil {
		return HistoryStats{}, err
	}
	return HistoryStats{Entries: len(entradas), Saved: len(guardadas)}, nil
}

// ForgetHistory borra el historial ENTERO de esta máquina, de todas las
// conexiones.
//
// Es distinto del «Borrar» de la pestaña, que borra el de la conexión abierta.
// Acá no hay ninguna abierta necesariamente, y lo que se quiere es dejar la
// máquina sin rastro de qué se consultó.
//
// No toca las consultas guardadas: ésas son trabajo con nombre y viven en el
// otro archivo, el que se sincroniza. Borrarlas de arrastre sería perder algo
// que nadie pidió perder.
func (s *Settings) ForgetHistory(_ context.Context) error {
	if s.historial == nil {
		return fmt.Errorf("el historial no está disponible")
	}
	return s.historial.Clear("")
}
