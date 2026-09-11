package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/config"
	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/history"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/update"
)

func ajustesDePrueba(t *testing.T) (*Settings, *config.Store, *history.Store) {
	t.Helper()
	dir := t.TempDir()
	prefs := config.New(filepath.Join(dir, "config.toml"))
	hist := history.New(filepath.Join(dir, "historial.json"), filepath.Join(dir, "consultas.json"))
	return NewSettings(prefs, update.New(), hist, "1.2.3", "2026-09-11"), prefs, hist
}

// Los ajustes son el punto de partida del formulario, no una regla sobre las
// conexiones que ya existen.
func TestUnaConexionNuevaNaceConLasProteccionesDeLosAjustes(t *testing.T) {
	dir := t.TempDir()
	prefs := config.New(filepath.Join(dir, "config.toml"))
	if _, err := prefs.Save(config.Config{NewConnection: connection.Safety{
		ReadOnly:          true,
		BlockDropTruncate: true,
		RowLimit:          250,
	}}); err != nil {
		t.Fatal(err)
	}

	s := &Connections{
		store:   store.New(filepath.Join(dir, "connections.toml")),
		keyring: newFakeKeyring(),
	}
	s.UsarPreferencias(prefs)

	v, err := s.Draft()
	if err != nil {
		t.Fatalf("Draft(): %v", err)
	}
	if !v.Connection.Safety.ReadOnly {
		t.Error("la conexión nueva no nació en solo lectura")
	}
	if !v.Connection.Safety.BlockDropTruncate {
		t.Error("la conexión nueva no nació bloqueando DROP y TRUNCATE")
	}
	if v.Connection.Safety.RowLimit != 250 {
		t.Errorf("RowLimit = %d, se esperaba 250", v.Connection.Safety.RowLimit)
	}
}

// Un archivo de preferencias que esta build NO entiende no puede terminar
// creando conexiones menos protegidas que las de fábrica.
//
// El caso elegido es el que de verdad puede filtrar: un archivo de una versión
// futura DECODIFICA bien —es TOML válido— y recién después se rechaza por el
// número de versión. Un archivo basura no sirve para probar esto, porque el
// decodificador parsea el documento entero antes de asignar nada y no deja
// medio poblada la estructura.
func TestConPreferenciasQueNoSeEntiendenLaConexionNuevaNaceProtegida(t *testing.T) {
	dir := t.TempDir()
	ruta := filepath.Join(dir, "config.toml")
	delFuturo := "version = 99\n\n[new_connection]\n" +
		"allow_apply_without_preview = true\n" +
		"allow_write_without_confirmation = true\n" +
		"read_only = false\n"
	if err := os.WriteFile(ruta, []byte(delFuturo), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Connections{
		store:   store.New(filepath.Join(dir, "connections.toml")),
		keyring: newFakeKeyring(),
	}
	s.UsarPreferencias(config.New(ruta))

	v, err := s.Draft()
	if err != nil {
		t.Fatalf("Draft(): %v", err)
	}
	if v.Connection.Safety.AllowApplyWithoutPreview {
		t.Error("con preferencias que no se entienden la conexión nació pudiendo aplicar sin vista previa")
	}
	if v.Connection.Safety.AllowWriteWithoutConfirmation {
		t.Error("con preferencias que no se entienden la conexión nació escribiendo sin confirmar")
	}
}

// El caso silencioso es el peligroso: unos defaults mostrados como si fueran
// los guardados, y el primer «Guardar» pisando el archivo que estaba roto.
func TestGetAvisaCuandoLasPreferenciasNoSePudieronLeer(t *testing.T) {
	s, prefs, _ := ajustesDePrueba(t)
	if err := os.WriteFile(prefs.Path(), []byte("{{{"), 0o600); err != nil {
		t.Fatal(err)
	}

	v := s.Get(context.Background())
	if v.Problem == "" {
		t.Fatal("no se avisó que el archivo no se pudo leer")
	}
	if v.Config.Editor.EffectiveFontSize() == 0 {
		t.Error("no se devolvieron preferencias usables")
	}
}

// Borrar el historial de la máquina NO puede llevarse las consultas guardadas:
// son el otro archivo, son trabajo con nombre, y nadie pidió perderlas.
func TestOlvidarElHistorialNoBorraLasConsultasGuardadas(t *testing.T) {
	s, _, hist := ajustesDePrueba(t)

	if _, err := hist.Add(history.Entry{ConnectionID: "a", SQL: "select 1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := hist.Add(history.Entry{ConnectionID: "b", SQL: "select 2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := hist.Save(history.Saved{Name: "el informe", SQL: "select 3", ConnectionID: "a"}); err != nil {
		t.Fatal(err)
	}

	antes, err := s.HistorySize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if antes.Entries != 2 || antes.Saved != 1 {
		t.Fatalf("HistorySize antes = %+v, se esperaban 2 y 1", antes)
	}

	if err := s.ForgetHistory(context.Background()); err != nil {
		t.Fatalf("ForgetHistory(): %v", err)
	}

	despues, err := s.HistorySize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if despues.Entries != 0 {
		t.Errorf("quedaron %d entradas de historial", despues.Entries)
	}
	if despues.Saved != 1 {
		t.Errorf("las consultas guardadas quedaron en %d: borrar el historial no las toca", despues.Saved)
	}
}

// Borra el de TODAS las conexiones y no solo el de la abierta: el punto de este
// botón es dejar la máquina sin rastro de qué se consultó.
func TestOlvidarElHistorialBorraElDeTodasLasConexiones(t *testing.T) {
	s, _, hist := ajustesDePrueba(t)
	for _, conn := range []string{"a", "b", "c"} {
		if _, err := hist.Add(history.Entry{ConnectionID: conn, SQL: "select " + conn}); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.ForgetHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []string{"a", "b", "c"} {
		quedan, err := hist.List(conn, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(quedan) != 0 {
			t.Errorf("la conexión %q conservó %d entradas", conn, len(quedan))
		}
	}
}

// Guardar devuelve lo que quedó ESCRITO y no lo que se pidió. Sin esto, pedir
// un cuerpo de 400 px deja el campo mostrando 400 hasta la próxima apertura,
// cuando de golpe dice 13.
func TestGuardarDevuelveLoQueQuedoEnElArchivo(t *testing.T) {
	s, _, _ := ajustesDePrueba(t)

	v, err := s.Save(context.Background(), config.Config{
		Theme:  config.ThemeLight,
		Editor: config.Editor{FontSize: 400},
	})
	if err != nil {
		t.Fatalf("Save(): %v", err)
	}
	if v.Config.Editor.FontSize != config.DefaultFontSize {
		t.Errorf("FontSize = %d: se devolvió lo pedido y no lo guardado", v.Config.Editor.FontSize)
	}
	if v.Config.Theme != config.ThemeLight {
		t.Errorf("Theme = %q", v.Config.Theme)
	}
	if !strings.HasSuffix(v.Path, "config.toml") {
		t.Errorf("Path = %q", v.Path)
	}
}

// Una build compilada acá no es una publicación, y decir «tenés la última» o
// «hay una más nueva» sobre una build local confunde.
func TestUnaBuildLocalSeReconoce(t *testing.T) {
	dir := t.TempDir()
	prefs := config.New(filepath.Join(dir, "config.toml"))
	hist := history.New(filepath.Join(dir, "h.json"), filepath.Join(dir, "c.json"))

	local := NewSettings(prefs, update.New(), hist, "0.1.0", "dev")
	if !local.Get(context.Background()).LocalBuild {
		t.Error("una build con fecha «dev» no se reconoció como local")
	}

	publicada := NewSettings(prefs, update.New(), hist, "0.1.0", "2026-09-11")
	if publicada.Get(context.Background()).LocalBuild {
		t.Error("una build con fecha de verdad se tomó por local")
	}
}
