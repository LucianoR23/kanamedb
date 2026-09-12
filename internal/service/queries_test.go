package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/postgres"
	"github.com/LucianoR23/kanamedb/internal/query"

	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// Dos valores por defecto con el mismo número en paquetes distintos se separan
// solos en cuanto alguien toca uno. Este test es el que avisa.
func TestElLimiteDeFilasPorDefectoNoSeSepara(t *testing.T) {
	if postgres.DefaultRowLimit != connection.DefaultRowLimit {
		t.Errorf("postgres.DefaultRowLimit = %d y connection.DefaultRowLimit = %d: "+
			"el corte de lectura ya no coincide con lo que dice la conexión",
			postgres.DefaultRowLimit, connection.DefaultRowLimit)
	}
}

func queriesDePrueba(t *testing.T) (*Queries, *Session, string) {
	t.Helper()
	sesion, _, id := sesionDePrueba(t)
	return NewQueries(sesion), sesion, id
}

// Sin conexión no hay que reventar: hay que decirlo.
func TestRunSinConexionDevuelveFalloYNoRompe(t *testing.T) {
	q, _, _ := queriesDePrueba(t)

	res := q.Run(context.Background(), "r1", "select 1")
	if res.OK {
		t.Fatal("ejecutó una consulta sin conexión abierta")
	}
	if res.Failure == nil || res.Failure.Message == "" {
		t.Errorf("el fallo llegó vacío: %+v", res.Failure)
	}
	if res.Batch != nil {
		t.Error("vino un resultado junto con el fallo de no haber conexión")
	}
}

// Cancelar tiene que cortar la consulta que se pidió y solo esa. Con un único
// cancel compartido, apretar cancelar en una pestaña mataría la de otra — y de
// forma intermitente, que es la peor manera de tener un error.
func TestCancelCortaSoloLaEjecucionQueSePide(t *testing.T) {
	q, sesion, id := queriesDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	var wg sync.WaitGroup
	var largaRes, cortaRes RunResult

	wg.Add(2)
	go func() {
		defer wg.Done()
		largaRes = q.Run(context.Background(), "larga", "select pg_sleep(10)")
	}()
	go func() {
		defer wg.Done()
		cortaRes = q.Run(context.Background(), "corta", "select pg_sleep(1)")
	}()

	time.Sleep(300 * time.Millisecond)
	q.Cancel("larga")

	hecho := make(chan struct{})
	go func() { wg.Wait(); close(hecho) }()
	select {
	case <-hecho:
	case <-time.After(8 * time.Second):
		t.Fatal("las consultas no terminaron: la cancelación no cortó nada")
	}

	if largaRes.OK {
		t.Error("la consulta cancelada terminó bien")
	}
	if !cortaRes.OK {
		t.Errorf("se canceló la consulta equivocada: %+v", cortaRes.Failure)
	}
}

// El registro de cancelaciones no puede crecer para siempre: cada ejecución que
// termina tiene que sacarse, o cancelar una vieja alcanzaría a una nueva.
func TestElRegistroDeCancelacionesQuedaVacio(t *testing.T) {
	q, sesion, id := queriesDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	for i := 0; i < 5; i++ {
		if res := q.Run(context.Background(), "r", "select 1"); !res.OK {
			t.Fatalf("consulta %d falló: %+v", i, res.Failure)
		}
	}
	if n := q.running(); n != 0 {
		t.Errorf("quedaron %d ejecuciones registradas después de terminar todas", n)
	}
	// Cancelar algo que ya terminó no es un error.
	q.Cancel("r")
}

// Sin ORDER BY el paginado puede repetir y saltear filas. El servicio resuelve
// la clave primaria para evitarlo, y lo informa: OrderedBy vacío es la señal de
// que "cargar más" es aproximado.
func TestTableDataOrdenaPorLaClavePrimariaYLoInforma(t *testing.T) {
	q, sesion, id := queriesDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	esquema := "kn_svc_tabledata"
	crear(t, q, `drop schema if exists `+esquema+` cascade`)
	crear(t, q, `create schema `+esquema)
	t.Cleanup(func() { crear(t, q, `drop schema if exists `+esquema+` cascade`) })
	crear(t, q, `create table `+esquema+`.con_pk (id int primary key, n text)`)
	crear(t, q, `insert into `+esquema+`.con_pk select i, 'x' from generate_series(1, 5) as i`)
	crear(t, q, `create table `+esquema+`.sin_pk (n text)`)
	crear(t, q, `insert into `+esquema+`.sin_pk values ('a'), ('b')`)

	conPK := q.TableData(context.Background(), TableDataRequest{
		RunID: "t1", Schema: esquema, Table: "con_pk", Limit: 3,
	})
	if !conPK.OK {
		t.Fatalf("TableData falló: %+v", conPK.Failure)
	}
	if strings.Join(conPK.OrderedBy, ",") != "id" {
		t.Errorf("OrderedBy = %v, se esperaba [id]", conPK.OrderedBy)
	}
	if len(conPK.Result.Rows) != 3 {
		t.Errorf("filas = %d, se esperaban 3 por el LIMIT", len(conPK.Result.Rows))
	}

	sinPK := q.TableData(context.Background(), TableDataRequest{
		RunID: "t2", Schema: esquema, Table: "sin_pk", Limit: 10,
	})
	if !sinPK.OK {
		t.Fatalf("TableData sin clave primaria falló: %+v", sinPK.Failure)
	}
	if len(sinPK.OrderedBy) != 0 {
		t.Errorf("OrderedBy = %v: sin clave primaria tiene que quedar vacío", sinPK.OrderedBy)
	}
	// Y aun así muestra los datos: no tener orden no es motivo para no leer.
	if len(sinPK.Result.Rows) != 2 {
		t.Errorf("filas = %d, se esperaban 2", len(sinPK.Result.Rows))
	}
}

func TestTableCountEsExactoDesdeElServicio(t *testing.T) {
	q, sesion, id := queriesDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	esquema := "kn_svc_count"
	crear(t, q, `drop schema if exists `+esquema+` cascade`)
	crear(t, q, `create schema `+esquema)
	t.Cleanup(func() { crear(t, q, `drop schema if exists `+esquema+` cascade`) })
	crear(t, q, `create table `+esquema+`.t (id int)`)
	crear(t, q, `insert into `+esquema+`.t select generate_series(1, 41)`)

	res := q.TableCount(context.Background(), "c1", esquema, "t", nil)
	if !res.OK {
		t.Fatalf("TableCount falló: %+v", res.Failure)
	}
	if res.Count != 41 {
		t.Errorf("count = %d, se esperaban 41", res.Count)
	}
}

// crear ejecuta DDL de preparación a través del servicio.
func crear(t *testing.T, q *Queries, sql string) {
	t.Helper()
	if res := q.Run(context.Background(), "ddl", sql); !res.OK {
		t.Fatalf("no se pudo preparar con %q: %+v", sql, res.Failure)
	}
}

// Probar una conexión con túnel tiene que pasar por el túnel.
//
// Sin esto, la prueba intentaba llegar directo a la base: daba un error de red
// que no menciona el bastión, o —peor— funcionaba desde una red donde la base
// es alcanzable y daba por buena una configuración que en otra máquina no anda.
//
// Y si la clave del bastión no está aceptada, lo dice: es una condición previa
// y no un error de SSH cualquiera.
func TestProbarConTunelSinClaveAceptadaLoDice(t *testing.T) {
	sesion, conns, id := sesionDePrueba(t)
	_ = sesion

	c, err := conns.store.Get(id)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	c.SSH = tunnel.Config{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    52222,
		User:    "kaname",
		Auth:    tunnel.AuthPassword,
	}

	res := conns.Test(context.Background(), c, PasswordKeep, "")
	if res.OK {
		t.Fatal("probó con éxito una conexión cuyo bastión no está verificado")
	}
	if res.Failure == nil {
		t.Fatal("no vino el fallo")
	}
	if res.Failure.Kind != postgres.FailureTunnel {
		t.Errorf("Kind = %q, se esperaba %q", res.Failure.Kind, postgres.FailureTunnel)
	}
	// El mensaje tiene que decir qué hacer, no solo qué pasó.
	if !strings.Contains(res.Failure.Hint, "conectá") && !strings.Contains(res.Failure.Hint, "Revisá") {
		t.Errorf("el fallo no dice qué hacer: %+v", res.Failure)
	}
}

// TestLaFilaComoJSONSaleDelMismoEscritorQueLaExportacion.
//
// Si se armara aparte, el visor y el archivo dirían cosas distintas de la misma
// fila —uno con el número entre comillas y el otro sin— y nadie sabría cuál
// creer. Al usar el mismo escritor, coinciden por construcción.
func TestLaFilaComoJSONSaleDelMismoEscritorQueLaExportacion(t *testing.T) {
	q := NewQueries(NewSession(nil, nil, nil, nil))
	cols := []query.Column{
		{Name: "id", Class: query.ClassNumber},
		{Name: "nombre", Class: query.ClassText},
		{Name: "activo", Class: query.ClassBool},
		{Name: "nada", Class: query.ClassText},
	}
	s := func(v string) *string { return &v }
	fila := []*string{s("42"), s("Ana"), s("t"), nil}

	got, err := q.RowJSON(cols, fila)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":42,"nombre":"Ana","activo":true,"nada":null}` + "\n"
	if got != want {
		t.Fatalf("RowJSON:\n%s\nquería:\n%s", got, want)
	}

	// Y es exactamente lo que escribe la exportación con esa misma fila.
	deExport, err := export.Render(export.JSONL, export.Options{}, cols, [][]*string{fila}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Un numeric exacto sobrevive: indentar el JSON exigiría volver a parsearlo
	// y 12.50 se volvería 12.5.
	conNumeric, err := q.RowJSON(
		[]query.Column{{Name: "monto", Class: query.ClassNumber}},
		[]*string{s("12.50")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if conNumeric != `{"monto":12.50}`+"\n" {
		t.Errorf("el numeric se cambió: %s", conNumeric)
	}

	if got != deExport {
		t.Errorf("el visor dice:\n%s\ny la exportación:\n%s", got, deExport)
	}
}

// TestCancelarUnVolcadoSigueFuncionandoEntreTablas.
//
// El volcado se registra una vez y después llama a `volcar` por cada tabla, que
// se registra otra vez con el MISMO identificador. Registrarse encima y borrar
// la clave al terminar dejaba el volcado entero sin registrar en cuanto la
// primera tabla terminaba: «Cancelar» se volvía silenciosamente inútil justo
// en el volcado largo, que es el único donde alguien lo aprieta.
func TestCancelarUnVolcadoSigueFuncionandoEntreTablas(t *testing.T) {
	q, _, _ := queriesDePrueba(t)
	ctx := context.Background()

	afuera, cerrarAfuera := q.registrar(ctx, "vol")
	// Una tabla: se registra con el mismo identificador y termina.
	adentro, cerrarAdentro := q.registrar(afuera, "vol")
	cerrarAdentro()
	if adentro.Err() == nil {
		t.Error("el context de la tabla que terminó tendría que estar cerrado")
	}
	if n := q.running(); n != 1 {
		t.Fatalf("quedan %d ejecuciones registradas y el volcado sigue: se borró la de afuera", n)
	}

	// Y cancelar el volcado corta lo de afuera.
	q.Cancel("vol")
	if afuera.Err() == nil {
		t.Error("cancelar el volcado no cortó nada")
	}
	cerrarAfuera()
	if n := q.running(); n != 0 {
		t.Errorf("quedaron %d ejecuciones registradas al terminar", n)
	}
}

// TestUnaCancelacionAnidadaNoAlcanzaALaDeAfuera comprueba lo contrario: la de
// adentro NO puede cortar el volcado entero al terminar bien.
func TestUnaCancelacionAnidadaNoAlcanzaALaDeAfuera(t *testing.T) {
	q, _, _ := queriesDePrueba(t)
	afuera, cerrarAfuera := q.registrar(context.Background(), "vol2")
	defer cerrarAfuera()

	_, cerrarAdentro := q.registrar(afuera, "vol2")
	cerrarAdentro()
	if afuera.Err() != nil {
		t.Error("terminar una tabla cortó el volcado entero")
	}
}
