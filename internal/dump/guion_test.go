package dump

import (
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

func encabezado(t *testing.T, i Info) string {
	t.Helper()
	var b strings.Builder
	if err := Encabezado(&b, i); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func base() Info {
	return Info{
		Origen:     "kaname@127.0.0.1:5432/demo",
		Motor:      "PostgreSQL 18.3",
		Base:       "demo",
		Esquemas:   []string{"public"},
		Cuando:     time.Date(2026, 9, 10, 15, 4, 5, 0, time.UTC),
		Version:    "0.1.0",
		Estructura: true,
		Datos:      true,
	}
}

// TestElEncabezadoDiceQueNoLleva.
//
// Es la razón de ser del archivo: un volcado incompleto se ve idéntico a uno
// completo, y esta es la única diferencia visible. Tiene que estar arriba de
// todo, con nombre y apellido.
func TestElEncabezadoDiceQueNoLleva(t *testing.T) {
	i := base()
	i.Cobertura = Cobertura{Fuera: []schema.Object{
		{Kind: schema.ObjFunction, Schema: "public", Name: "tocar"},
		{Kind: schema.ObjView, Schema: "public", Name: "activos"},
	}}
	got := encabezado(t, i)

	for _, q := range []string{
		"LO QUE NO ESTÁ EN ESTE ARCHIVO",
		"public.activos",
		"public.tocar",
		"1 vista",
		"1 función",
	} {
		if !strings.Contains(got, q) {
			t.Errorf("el encabezado no dice %q:\n%s", q, got)
		}
	}
	// Todo el encabezado es comentario SQL: si una línea se escapara, el
	// archivo no correría.
	for _, l := range strings.Split(strings.TrimSpace(got), "\n") {
		if l != "" && !strings.HasPrefix(l, "--") {
			t.Errorf("una línea del encabezado no es un comentario: %q", l)
		}
	}
}

// TestSinNadaAfueraSeAfirmaQueNoFaltaNada.
//
// Y se dice que se comprobó, no que se supone: la diferencia entre las dos
// cosas es todo lo que este encabezado vale.
func TestSinNadaAfueraSeAfirmaQueNoFaltaNada(t *testing.T) {
	got := encabezado(t, base())
	if !strings.Contains(got, "NO DEJA NADA AFUERA") {
		t.Errorf("no se afirma la cobertura completa:\n%s", got)
	}
	if !strings.Contains(got, "no se está suponiendo") {
		t.Errorf("no se aclara que se comprobó contra el catálogo:\n%s", got)
	}
}

// TestElEncabezadoNoLlevaCredenciales es un requisito duro: el archivo se
// manda por correo y se sube a un ticket.
func TestElEncabezadoNoLlevaCredenciales(t *testing.T) {
	i := base()
	i.Origen = "kaname@127.0.0.1:5432/demo"
	got := encabezado(t, i)
	for _, prohibido := range []string{"password", "contraseña", "sslmode", "postgres://", "secreto"} {
		if strings.Contains(strings.ToLower(got), prohibido) {
			t.Errorf("el encabezado tiene %q:\n%s", prohibido, got)
		}
	}
	if !strings.Contains(got, "kaname@127.0.0.1:5432/demo") {
		t.Errorf("no está el origen sin credenciales:\n%s", got)
	}
}

// TestUnCicloSeAvisaEnElArchivo.
//
// Quien corre el script tiene que enterarse ANTES de que falle, y saber cuáles
// son: «va a fallar» no deja hacer nada, «pedidos ↔ clientes» sí.
func TestUnCicloSeAvisaEnElArchivo(t *testing.T) {
	i := base()
	i.Ciclos = [][]Ref{{{"public", "clientes"}, {"public", "pedidos"}}}
	got := encabezado(t, i)
	if !strings.Contains(got, "SE APUNTAN ENTRE SÍ") {
		t.Errorf("no se avisa del ciclo:\n%s", got)
	}
	if !strings.Contains(got, "public.clientes ↔ public.pedidos") {
		t.Errorf("no se nombran las tablas del ciclo:\n%s", got)
	}

	// Sin datos no hay aviso: el ciclo solo rompe al insertar filas, y las
	// restricciones del volcado de estructura van después de las tablas.
	soloEstructura := i
	soloEstructura.Datos = false
	if strings.Contains(encabezado(t, soloEstructura), "SE APUNTAN ENTRE SÍ") {
		t.Error("se avisa de un ciclo en un volcado que no lleva datos")
	}
}

// TestQueLlevaDiceLaVerdadEnLosTresCasos.
func TestQueLlevaDiceLaVerdadEnLosTresCasos(t *testing.T) {
	casos := []struct {
		estructura, datos bool
		quiero            string
	}{
		{true, true, "la estructura y los datos"},
		{true, false, "solo la estructura"},
		{false, true, "solo los datos"},
	}
	for _, c := range casos {
		i := base()
		i.Estructura, i.Datos = c.estructura, c.datos
		if got := encabezado(t, i); !strings.Contains(got, "Lleva:   "+c.quiero) {
			t.Errorf("con estructura=%v datos=%v no dice %q", c.estructura, c.datos, c.quiero)
		}
	}
}

// TestUnaListaLargaSeCortaParaQueSePuedaLeer.
//
// El encabezado se lee en una terminal: treinta funciones en una sola línea no
// se leen. Y ninguna palabra se puede partir al medio, porque son nombres.
func TestUnaListaLargaSeCortaParaQueSePuedaLeer(t *testing.T) {
	var fuera []schema.Object
	for i := 0; i < 30; i++ {
		fuera = append(fuera, schema.Object{
			Kind: schema.ObjFunction, Schema: "public",
			Name: strings.Repeat("f", 12) + string(rune('a'+i%26)),
		})
	}
	i := base()
	i.Cobertura = Cobertura{Fuera: fuera}
	got := encabezado(t, i)

	for _, l := range strings.Split(got, "\n") {
		if len([]rune(l)) > 78 {
			t.Errorf("una línea mide %d: %q", len([]rune(l)), l)
		}
	}
	// Y los nombres siguen enteros: cortar `public.fffffffffffa` al medio
	// haría ilegible justamente lo que hay que poder buscar.
	if !strings.Contains(got, "public."+strings.Repeat("f", 12)+"a") {
		t.Errorf("un nombre quedó partido:\n%s", got)
	}
}

func TestEnvolverNoPierdeNiInventaPalabras(t *testing.T) {
	texto := "uno dos tres cuatro cinco seis siete ocho nueve diez"
	lineas := envolver(texto, 20)
	if strings.Join(lineas, " ") != texto {
		t.Errorf("envolver cambió el texto: %q", lineas)
	}
	for _, l := range lineas {
		if len([]rune(l)) > 20 {
			t.Errorf("línea de %d: %q", len([]rune(l)), l)
		}
	}
	// Una palabra sola más larga que el ancho no se parte ni se pierde.
	larga := envolver(strings.Repeat("x", 40), 20)
	if len(larga) != 1 || larga[0] != strings.Repeat("x", 40) {
		t.Errorf("una palabra más larga que el ancho salió como %q", larga)
	}
}
