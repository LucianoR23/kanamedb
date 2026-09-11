package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// checkerContra arma un Checker apuntado a un servidor de prueba.
func checkerContra(t *testing.T, h http.HandlerFunc) *Checker {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := New()
	c.base = srv.URL
	c.ahora = func() time.Time { return time.Date(2026, 9, 11, 15, 4, 5, 0, time.UTC) }
	return c
}

func TestUnaVersionMasNuevaSeDetecta(t *testing.T) {
	c := checkerContra(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.4.0","html_url":"https://github.com/x/y/releases/v0.4.0"}`))
	})

	got := c.Check(context.Background(), "0.3.1")
	if got.Problem != "" {
		t.Fatalf("Problem = %q", got.Problem)
	}
	if !got.Newer {
		t.Error("0.4.0 es más nueva que 0.3.1 y Newer quedó en false")
	}
	if got.Latest != "v0.4.0" || got.URL == "" {
		t.Errorf("Latest = %q, URL = %q", got.Latest, got.URL)
	}
	if got.CheckedAt != "2026-09-11T15:04:05Z" {
		t.Errorf("CheckedAt = %q", got.CheckedAt)
	}
}

func TestLaMismaVersionNoEsMasNueva(t *testing.T) {
	c := checkerContra(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"https://github.com/x/y"}`))
	})
	if got := c.Check(context.Background(), "1.0.0"); got.Newer {
		t.Error("1.0.0 contra 1.0.0 dijo que hay una más nueva")
	}
}

// La petición no puede llevar nada de esta máquina. Es el punto entero del
// paquete: un botón que consulta versiones no es un lugar desde donde contar
// quién lo apretó ni qué está corriendo.
func TestLaConsultaNoLlevaNingunDatoDeEstaMaquina(t *testing.T) {
	var vista *http.Request
	c := checkerContra(t, func(w http.ResponseWriter, r *http.Request) {
		vista = r.Clone(context.Background())
		w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	})

	c.Check(context.Background(), "0.9.9-build-de-luciano")

	if vista == nil {
		t.Fatal("el servidor no recibió ninguna petición")
	}
	if q := vista.URL.RawQuery; q != "" {
		t.Errorf("la URL lleva query string: %q", q)
	}
	if ua := vista.Header.Get("User-Agent"); ua != "Kaname" {
		t.Errorf("User-Agent = %q: va la palabra sola, sin versión ni plataforma", ua)
	}
	if cs := vista.Header.Get("Cookie"); cs != "" {
		t.Errorf("la petición lleva cookies: %q", cs)
	}
	// Y lo más directo: la versión actual no aparece en ninguna parte de la
	// petición. Se compara acá, no del otro lado.
	crudo := vista.URL.String() + "\n"
	for k, vs := range vista.Header {
		crudo += k + ": " + strings.Join(vs, ",") + "\n"
	}
	if strings.Contains(crudo, "0.9.9") || strings.Contains(strings.ToLower(crudo), "luciano") {
		t.Errorf("la versión actual viajó en la petición:\n%s", crudo)
	}
}

func TestSinReleasesLoDiceEnVezDeFallar(t *testing.T) {
	c := checkerContra(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	})

	got := c.Check(context.Background(), "0.1.0")
	if got.Problem == "" {
		t.Fatal("un 404 tiene que explicarse, no pasar por «estás al día»")
	}
	if got.Newer || got.Latest != "" {
		t.Errorf("con un 404 se inventó un resultado: %+v", got)
	}
}

// Una respuesta que no se entiende NO puede terminar en «estás al día»: eso es
// exactamente la respuesta que esconde una versión nueva.
func TestUnaRespuestaRaraNoSeConfundeConEstarAlDia(t *testing.T) {
	casos := map[string]string{
		"no es json":     `<html>error</html>`,
		"sin tag":        `{"html_url":"https://github.com/x/y"}`,
		"es un borrador": `{"tag_name":"v9.9.9","draft":true}`,
	}
	for nombre, cuerpo := range casos {
		t.Run(nombre, func(t *testing.T) {
			c := checkerContra(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(cuerpo))
			})
			got := c.Check(context.Background(), "0.1.0")
			if got.Problem == "" {
				t.Errorf("se aceptó como buena: %+v", got)
			}
		})
	}
}

// Una versión que no se puede comparar tiene que decirlo. Si `Newer` fuera
// false y nada más, la pantalla mostraría «estás al día» sin haber comparado
// nada.
func TestUnaVersionQueNoSePuedeCompararLoDice(t *testing.T) {
	c := checkerContra(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":"nightly"}`))
	})

	got := c.Check(context.Background(), "0.1.0")
	if got.Comparable {
		t.Error("«nightly» no es un número de versión y se dijo que se pudo comparar")
	}
	if got.Newer {
		t.Error("sin poder comparar no se puede afirmar que hay una más nueva")
	}
	if got.Latest != "nightly" {
		t.Errorf("Latest = %q: lo publicado se muestra igual, aunque no se pueda comparar", got.Latest)
	}
}

// Una redirección a otro servidor convertiría el botón en «pedile lo que sea a
// donde diga la respuesta anterior».
func TestNoSeSigueUnaRedireccionAOtroServidor(t *testing.T) {
	otro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":"v99.0.0"}`))
	}))
	defer otro.Close()

	c := checkerContra(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, otro.URL, http.StatusFound)
	})

	got := c.Check(context.Background(), "0.1.0")
	if got.Latest == "v99.0.0" {
		t.Fatal("se siguió la redirección a otro host")
	}
	if got.Problem == "" {
		t.Error("cortar la redirección tiene que explicarse")
	}
}

// Un cuerpo infinito no puede comerse la memoria del proceso.
func TestUnaRespuestaEnormeSeCorta(t *testing.T) {
	c := checkerContra(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.0.0","notes":"`))
		basura := strings.Repeat("a", 64<<10)
		for i := 0; i < 100; i++ {
			if _, err := w.Write([]byte(basura)); err != nil {
				return
			}
		}
	})

	hecho := make(chan Result, 1)
	go func() { hecho <- c.Check(context.Background(), "0.1.0") }()
	select {
	case got := <-hecho:
		// Cortado a la mitad, el JSON no cierra: lo que importa es que terminó
		// y que no lo dio por bueno.
		if got.Problem == "" && got.Latest != "" {
			t.Errorf("una respuesta cortada se aceptó como buena: %+v", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Check no terminó: el cuerpo no se está limitando")
	}
}

func TestCompararEntiendeLasFormasQueUsamos(t *testing.T) {
	casos := []struct {
		a, b   string
		quiero int
	}{
		{"v1.0.0", "1.0.0", 0},
		{"1.2", "1.2.0", 0},
		{"1.2.1", "1.2.0", 1},
		{"1.10.0", "1.9.0", 1},
		{"0.9.0", "1.0.0", -1},
		{"2.0.0", "2.0.0-rc1", 1},
		{"2.0.0-rc1", "2.0.0", -1},
		{"2.0.0-rc2", "2.0.0-rc1", 1},

		// `rc10` contra `rc9`: comparados como texto, «1» < «9» y la rc10 sale
		// PERDIENDO. O sea que publicar una rc10 con una rc9 corriendo decía
		// «tenés la última» — el error que este paquete existe para no cometer.
		{"2.0.0-rc10", "2.0.0-rc9", 1},
		{"2.0.0-rc9", "2.0.0-rc10", -1},
		{"1.0.0-beta.11", "1.0.0-beta.2", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},

		// Los metadatos de build no cuentan para la precedencia: lo dice semver,
		// y si contaran, una build fechada quedaría por debajo de la misma
		// versión sin fecha.
		{"1.0.0+20260911", "1.0.0", 0},
		{"1.0.0+a", "1.0.0+b", 0},
		{"1.2.0+20260911", "1.1.0", 1},
	}
	for _, c := range casos {
		got, ok := Comparar(c.a, c.b)
		if !ok {
			t.Errorf("Comparar(%q, %q) no pudo comparar", c.a, c.b)
			continue
		}
		if got != c.quiero {
			t.Errorf("Comparar(%q, %q) = %d, se esperaba %d", c.a, c.b, got, c.quiero)
		}
	}
}

func TestCompararDiceQueNoPuedeEnVezDeAdivinar(t *testing.T) {
	for _, v := range []string{"nightly", "", "1.x.0", "v", "1.-2.0"} {
		if _, ok := Comparar(v, "1.0.0"); ok {
			t.Errorf("Comparar(%q, …) dijo que pudo comparar", v)
		}
	}
}
