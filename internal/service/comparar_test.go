package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/drift"
	"github.com/LucianoR23/kanamedb/internal/layout"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// Comparar algo consigo mismo siempre da cero diferencias, y cero diferencias
// se lee como «están alineados». Es la respuesta correcta a la pregunta
// equivocada, y por eso no se deja hacer.
func TestNoSeComparaUnaConexionConsigoMisma(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)

	res := sesion.Compare(context.Background(), CompareRequest{SourceID: id, TargetID: id})
	if res.OK {
		t.Fatal("se aceptó comparar una conexión consigo misma")
	}
	if res.Failure == nil || !strings.Contains(strings.ToLower(res.Failure.Message), "misma") {
		t.Errorf("el fallo no explica el problema: %+v", res.Failure)
	}
}

func TestCompararSinLasDosConexionesSeRechaza(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)

	for _, req := range []CompareRequest{
		{},
		{SourceID: id},
		{TargetID: id},
	} {
		res := sesion.Compare(context.Background(), req)
		if res.OK {
			t.Errorf("se aceptó %+v", req)
		}
		if res.Result != nil {
			t.Errorf("%+v devolvió un resultado: una comparación que no se hizo no puede traer diferencias", req)
		}
	}
}

// Comparar NO puede tocar la sesión abierta.
//
// Es la razón por la que existe `abrirConexion` separada de `ConnectAccepting`:
// las dos bases de la comparación se abren, se leen y se cierran, y la conexión
// del workspace —con su changeset, que puede tener horas de trabajo— tiene que
// seguir siendo exactamente la misma antes y después.
func TestCompararNoTocaLaConexionAbierta(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)

	res := sesion.Connect(context.Background(), id)
	saltearSinBase(t, res)
	if !res.OK {
		t.Fatalf("Connect(): %+v", res.Failure)
	}

	// Algo en el changeset, para poder comprobar que sigue ahí.
	antes := sesion.Current()
	if !antes.Connected {
		t.Fatal("la sesión no quedó conectada")
	}

	// Se compara la conexión abierta contra otra que NO existe: la comparación
	// va a fallar, y ése es el punto. Un fallo a mitad de camino es justamente
	// cuando algo podría quedar mal cerrado o mal instalado.
	segunda := sesion.Compare(context.Background(),
		CompareRequest{SourceID: id, TargetID: "no-existe"})
	if segunda.OK {
		t.Fatal("se comparó contra una conexión que no existe")
	}

	despues := sesion.Current()
	if !despues.Connected {
		t.Fatal("la comparación dejó la sesión desconectada")
	}
	if despues.ConnectionID != antes.ConnectionID {
		t.Errorf("la conexión abierta cambió: %q → %q", antes.ConnectionID, despues.ConnectionID)
	}
	if despues.OpenedAt != antes.OpenedAt {
		t.Errorf("la sesión se reabrió: abierta a las %q y ahora a las %q", antes.OpenedAt, despues.OpenedAt)
	}

	// Y la conexión abierta sigue sirviendo. Sin esto, cerrar por error el pool
	// de la sesión en el `defer` de la comparación pasaría desapercibido: la
	// vista sigue diciendo «conectado» porque es un struct en memoria.
	if _, err := sesion.Schema(context.Background(), true); err != nil {
		t.Errorf("después de comparar, la conexión abierta ya no sirve: %v", err)
	}
}

// El caso completo contra dos bases de verdad: dos conexiones al mismo servidor
// apuntando a bases distintas, una con una tabla que la otra no tiene.
func TestCompararDosBasesDeVerdad(t *testing.T) {
	dsn := os.Getenv("KANAME_TEST_POSTGRES")
	if dsn == "" {
		dsn = "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable"
	}
	parsed, err := connection.ParseURI(dsn)
	if err != nil {
		t.Fatalf("el DSN de pruebas no se pudo interpretar: %v", err)
	}

	st := store.New(filepath.Join(t.TempDir(), "connections.toml"))
	kr := newFakeKeyring()

	// Dos conexiones a la MISMA base. Las diferencias tienen que salir de los
	// esquemas, no de la base: así el test no necesita crear una segunda base y
	// sigue comparando dos catálogos leídos por separado.
	for _, id := range []string{"origen01", "destino01"} {
		c := parsed.Connection
		c.ID = id
		c.Name = id
		c.Environment = connection.Local
		if err := st.Add(c.Normalize()); err != nil {
			t.Fatalf("Add(): %v", err)
		}
		if parsed.Password != "" {
			if err := kr.Set(id, parsed.Password); err != nil {
				t.Fatalf("keyring: %v", err)
			}
		}
	}

	sesion := NewSession(st, kr,
		tunnel.NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts")),
		layout.New(filepath.Join(t.TempDir(), "layouts")))
	t.Cleanup(sesion.Disconnect)

	// Se comprueba primero que haya base, con el mismo criterio del resto.
	saltearSinBase(t, sesion.Connect(context.Background(), "origen01"))
	sesion.Disconnect()

	res := sesion.Compare(context.Background(),
		CompareRequest{SourceID: "origen01", TargetID: "destino01"})
	if !res.OK {
		t.Fatalf("Compare(): %+v", res.Failure)
	}
	if res.Result == nil {
		t.Fatal("Compare() dijo ok y no trajo resultado")
	}

	// Las dos leyeron la misma base, así que no puede haber diferencias. Lo que
	// importa es que igual diga qué NO comparó: cero diferencias sin esa lista
	// sería un «están alineados» que no se ganó.
	if len(res.Result.Diferencias) != 0 {
		t.Errorf("la misma base contra sí misma dio %d diferencias: %+v",
			len(res.Result.Diferencias), res.Result.Diferencias)
	}
	if len(res.Result.NoComparado) == 0 {
		t.Error("no se dijo qué quedó sin comparar")
	}
	if res.Source.Database == "" || res.Target.Database == "" {
		t.Errorf("los lados no traen la base: %+v / %+v", res.Source, res.Target)
	}
	if res.Source.ConnectionID != "origen01" || res.Target.ConnectionID != "destino01" {
		t.Errorf("los lados quedaron cruzados: %q / %q", res.Source.ConnectionID, res.Target.ConnectionID)
	}

	// Y después de comparar dos veces no quedan conexiones colgadas: si el
	// `defer` no cerrara, esto agotaría el pool del servidor bastante antes que
	// el test termine.
	for i := 0; i < 12; i++ {
		if r := sesion.Compare(context.Background(),
			CompareRequest{SourceID: "origen01", TargetID: "destino01"}); !r.OK {
			t.Fatalf("la comparación %d falló: %+v", i, r.Failure)
		}
	}
}

// El tipo que cruza el puente tiene que ser el del paquete, no una copia: dos
// definiciones del mismo dato se separan.
func TestElResultadoQueViajaEsElDelPaquete(t *testing.T) {
	var r CompareResult
	var _ *drift.Resultado = r.Result
}
