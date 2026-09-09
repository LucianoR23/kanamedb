package layout

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nuevo(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "layouts"))
}

func TestGuardarYLeerUnDiagrama(t *testing.T) {
	s := nuevo(t)

	// Sin archivo no hay error: nunca haber abierto el diagrama es normal.
	vacio, err := s.Get("abc123", "public")
	if err != nil {
		t.Fatalf("Get() sobre un directorio vacío falló: %v", err)
	}
	if len(vacio) != 0 {
		t.Errorf("Get() devolvió %v", vacio)
	}

	quiero := Positions{
		"public.pedidos":  {X: 10, Y: 20.5},
		"public.clientes": {X: -30, Y: 0},
	}
	if err := s.Save("abc123", "public", quiero); err != nil {
		t.Fatalf("Save() falló: %v", err)
	}

	got, err := s.Get("abc123", "public")
	if err != nil {
		t.Fatalf("Get() falló: %v", err)
	}
	if !reflect.DeepEqual(got, quiero) {
		t.Errorf("Get() = %v, se esperaba %v", got, quiero)
	}
}

// Cada esquema es un diagrama distinto y guardar uno no puede pisar al otro.
func TestLosEsquemasNoSePisan(t *testing.T) {
	s := nuevo(t)

	if err := s.Save("c1", "public", Positions{"public.a": {X: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("c1", "ventas", Positions{"ventas.b": {X: 2}}); err != nil {
		t.Fatal(err)
	}

	pub, err := s.Get("c1", "public")
	if err != nil {
		t.Fatal(err)
	}
	if len(pub) != 1 || pub["public.a"].X != 1 {
		t.Errorf("public = %v: guardar «ventas» pisó el otro esquema", pub)
	}
	ven, err := s.Get("c1", "ventas")
	if err != nil {
		t.Fatal(err)
	}
	if len(ven) != 1 || ven["ventas.b"].X != 2 {
		t.Errorf("ventas = %v", ven)
	}
}

// Y cada conexión es un archivo aparte: dos conexiones a bases distintas con el
// mismo esquema «public» no comparten acomodado.
func TestLasConexionesNoSePisan(t *testing.T) {
	s := nuevo(t)

	if err := s.Save("c1", "public", Positions{"public.a": {X: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("c2", "public", Positions{"public.a": {X: 99}}); err != nil {
		t.Fatal(err)
	}

	uno, _ := s.Get("c1", "public")
	if uno["public.a"].X != 1 {
		t.Errorf("c1 = %v", uno)
	}
	dos, _ := s.Get("c2", "public")
	if dos["public.a"].X != 99 {
		t.Errorf("c2 = %v", dos)
	}
}

// Guardar vacío borra el acomodado: es cómo se vuelve al automático.
func TestGuardarVacioOlvidaElEsquema(t *testing.T) {
	s := nuevo(t)

	if err := s.Save("c1", "public", Positions{"public.a": {X: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("c1", "public", Positions{}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("c1", "public")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Get() = %v después de guardar vacío", got)
	}
}

// El identificador viene del proceso de la interfaz y termina siendo un nombre
// de archivo. Sin validarlo, escribiría donde quisiera.
func TestElIdentificadorNoPuedeSalirseDelDirectorio(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "layouts"))

	malos := []string{
		"../evasion",
		"..\\evasion",
		"a/b",
		"a\\b",
		"..",
		".",
		"",
		"con espacios",
		"con.punto",
		strings.Repeat("a", 65),
	}
	for _, id := range malos {
		t.Run(id, func(t *testing.T) {
			if err := s.Save(id, "public", Positions{"x": {}}); !errors.Is(err, ErrIDInvalido) {
				t.Errorf("Save(%q) error = %v, se esperaba ErrIDInvalido", id, err)
			}
			if _, err := s.Get(id, "public"); !errors.Is(err, ErrIDInvalido) {
				t.Errorf("Get(%q) error = %v, se esperaba ErrIDInvalido", id, err)
			}
		})
	}

	// Y nada quedó escrito fuera —ni dentro— del directorio.
	if entradas, err := os.ReadDir(dir); err == nil {
		for _, e := range entradas {
			if e.Name() != "layouts" {
				t.Errorf("apareció %q fuera del directorio de diagramas", e.Name())
			}
		}
	}
}

func TestElIdentificadorQueGeneraLaAppEsValido(t *testing.T) {
	s := nuevo(t)
	// 16 hexadecimales, que es lo que devuelve connection.NewID.
	if err := s.Save("9f3c1a7b2d8e4056", "public", Positions{"public.a": {X: 1}}); err != nil {
		t.Fatalf("Save() con un id real falló: %v", err)
	}
}

func TestUnArchivoCorruptoNoRompeElDiagrama(t *testing.T) {
	s := nuevo(t)
	if err := s.Save("c1", "public", Positions{"public.a": {X: 1}}); err != nil {
		t.Fatal(err)
	}

	ruta := filepath.Join(s.Dir(), "c1.json")
	if err := os.WriteFile(ruta, []byte("{esto no es json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Perder el acomodado molesta; no poder abrir el diagrama, más.
	got, err := s.Get("c1", "public")
	if err != nil {
		t.Fatalf("Get() sobre un archivo corrupto falló: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Get() = %v", got)
	}
	// Y el siguiente guardado lo deja sano otra vez.
	if err := s.Save("c1", "public", Positions{"public.b": {X: 5}}); err != nil {
		t.Fatalf("Save() sobre un archivo corrupto falló: %v", err)
	}
	got, err = s.Get("c1", "public")
	if err != nil || got["public.b"].X != 5 {
		t.Errorf("Get() = %v, err = %v", got, err)
	}
}

func TestSeRechazaUnDiagramaAbsurdamenteGrande(t *testing.T) {
	s := nuevo(t)
	enorme := Positions{}
	for i := 0; i <= maxTablas; i++ {
		enorme[string(rune('a'+i%26))+string(rune('a'+i/26))+string(rune('a'+i/676))] = Position{}
	}
	if len(enorme) <= maxTablas {
		t.Fatalf("la fixture generó %d entradas y el tope es %d: el test no probaría nada",
			len(enorme), maxTablas)
	}
	if err := s.Save("c1", "public", enorme); err == nil {
		t.Error("Save() aceptó un diagrama por encima del tope")
	}
}

func TestForgetBorraElArchivo(t *testing.T) {
	s := nuevo(t)
	if err := s.Save("c1", "public", Positions{"public.a": {X: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget("c1"); err != nil {
		t.Fatalf("Forget() falló: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "c1.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("el archivo sigue ahí: %v", err)
	}
	// Olvidar algo que no existe no es un error.
	if err := s.Forget("c1"); err != nil {
		t.Errorf("Forget() sobre nada falló: %v", err)
	}
}
