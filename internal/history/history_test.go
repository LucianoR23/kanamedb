package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func store(t *testing.T) *Store {
	t.Helper()
	d := t.TempDir()
	return New(filepath.Join(d, "historial.json"), filepath.Join(d, "guardadas.json"))
}

// TestUnaContrasenaEscritaNoLlegaAlArchivo.
//
// Es la regla dura de CLAUDE.md —los secretos van al keychain del sistema y a
// ningún otro lado— aplicada al único lugar donde este paquete la puede violar:
// alguien escribe `ALTER USER … PASSWORD 'x'` en el editor, lo corre, y el
// historial lo pone en un archivo de texto sin cifrar.
//
// Lo que se comprueba NO es que la función devuelva false sino que el archivo
// no tenga la contraseña adentro. Es la diferencia entre probar la decisión y
// probar el efecto: una versión que dijera «no guardado» y escribiera igual
// pasaría el primer test.
func TestUnaContrasenaEscritaNoLlegaAlArchivo(t *testing.T) {
	s := store(t)

	peligrosas := []string{
		"ALTER USER ana WITH PASSWORD 'topsecret'",
		"CREATE ROLE bot LOGIN PASSWORD 'topsecret'",
		"create user x identified by 'topsecret'",
		"CREATE USER y IDENTIFIED WITH mysql_native_password BY 'topsecret'",
		"ALTER ROLE z ENCRYPTED PASSWORD 'topsecret'",
		"CREATE SUBSCRIPTION s CONNECTION 'host=x password=topsecret' PUBLICATION p",
		"SET password = 'topsecret'",
	}
	for _, sql := range peligrosas {
		guardada, err := s.Add(Entry{ConnectionID: "c1", SQL: sql})
		if err != nil {
			t.Fatalf("Add(%q): %v", sql, err)
		}
		if guardada {
			t.Errorf("se guardó una sentencia con contraseña: %q", sql)
		}
	}

	// Y el archivo tampoco la tiene. Es lo que de verdad importa.
	if crudo := archivoCrudo(t, s.historial); strings.Contains(crudo, "topsecret") {
		t.Fatalf("la contraseña quedó escrita en disco:\n%s", crudo)
	}

	// Guardarla con nombre tampoco se puede, y ahí llega MÁS lejos: ese archivo
	// va al lado de la libreta de conexiones, que es la que se sincroniza.
	if _, err := s.Save(Saved{Name: "crear bot", SQL: peligrosas[0]}); err == nil {
		t.Error("se dejó guardar una consulta con contraseña")
	}
	if crudo := archivoCrudo(t, s.guardadas); strings.Contains(crudo, "topsecret") {
		t.Fatalf("la contraseña quedó en las guardadas:\n%s", crudo)
	}
}

// TestLoQueNoLlevaSecretoSiSeGuarda: el filtro tiene que dejar pasar lo normal,
// o el historial no sirve para nada.
func TestLoQueNoLlevaSecretoSiSeGuarda(t *testing.T) {
	for _, sql := range []string{
		"SELECT * FROM clientes WHERE id = 1",
		"UPDATE pedidos SET estado = 'listo' WHERE id = 7",
		// Una columna que se LLAMA password no es una contraseña escrita.
		"SELECT password_hash FROM usuarios",
		"ALTER TABLE t ADD COLUMN password_changed_at timestamptz",
	} {
		if LlevaSecreto(sql) {
			t.Errorf("se rechazó una consulta normal: %q", sql)
		}
	}
}

// TestCorrerLoMismoDosVecesNoLlenaElHistorial.
//
// Repetir un SELECT afinando nada es lo que uno hace todo el tiempo, y seis
// renglones idénticos vuelven inútil la lista justo cuando más se la mira.
func TestCorrerLoMismoDosVecesNoLlenaElHistorial(t *testing.T) {
	s := store(t)
	const sql = "SELECT 1"
	for i := 0; i < 3; i++ {
		if _, err := s.Add(Entry{ConnectionID: "c1", SQL: sql, ElapsedMs: int64(i)}); err != nil {
			t.Fatalf("Add(): %v", err)
		}
	}
	lista, err := s.List("c1", 0)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(lista) != 1 {
		t.Fatalf("quedaron %d entradas y se corrió la misma consulta tres veces", len(lista))
	}
	if lista[0].Runs != 3 {
		t.Errorf("Runs = %d, se esperaba 3", lista[0].Runs)
	}
	// Y los datos son los de la ÚLTIMA corrida, no los de la primera.
	if lista[0].ElapsedMs != 2 {
		t.Errorf("quedaron los tiempos de la primera corrida: %d ms", lista[0].ElapsedMs)
	}

	// Otra consulta en el medio corta la racha: volver a la primera es una
	// corrida nueva y sube en la lista.
	if _, err := s.Add(Entry{ConnectionID: "c1", SQL: "SELECT 2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Entry{ConnectionID: "c1", SQL: sql}); err != nil {
		t.Fatal(err)
	}
	lista, _ = s.List("c1", 0)
	if len(lista) != 3 {
		t.Fatalf("quedaron %d entradas, se esperaban 3: %+v", len(lista), lista)
	}
	if lista[0].SQL != sql {
		t.Errorf("la más reciente es %q", lista[0].SQL)
	}
}

// TestElHistorialSeFiltraPorConexion: la misma consulta contra dos bases es
// otra cosa, y mezclarlas hace que abrir el historial de una muestre lo de la
// otra — que contra una producción es peor que inútil.
func TestElHistorialSeFiltraPorConexion(t *testing.T) {
	s := store(t)
	if _, err := s.Add(Entry{ConnectionID: "prod", SQL: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Entry{ConnectionID: "dev", SQL: "SELECT 2"}); err != nil {
		t.Fatal(err)
	}

	solo, err := s.List("dev", 0)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(solo) != 1 || solo[0].SQL != "SELECT 2" {
		t.Fatalf("el filtro por conexión no anduvo: %+v", solo)
	}

	// Y borrar el de una NO se lleva el de la otra.
	if err := s.Clear("dev"); err != nil {
		t.Fatalf("Clear(): %v", err)
	}
	todo, _ := s.List("", 0)
	if len(todo) != 1 || todo[0].ConnectionID != "prod" {
		t.Errorf("borrar una conexión se llevó lo de otra: %+v", todo)
	}
}

// TestElHistorialTieneTope: el archivo se reescribe entero en cada consulta,
// así que sin tope cada Enter del editor sale más caro que el anterior.
func TestElHistorialTieneTope(t *testing.T) {
	s := store(t)
	const sobran = 20
	for i := 0; i < maxEntradas+sobran; i++ {
		if _, err := s.Add(Entry{
			ConnectionID: "c1",
			SQL:          fmt.Sprintf("SELECT %d", i),
		}); err != nil {
			t.Fatalf("Add(): %v", err)
		}
	}
	lista, _ := s.List("", 0)
	if len(lista) != maxEntradas {
		t.Fatalf("quedaron %d entradas y el tope es %d", len(lista), maxEntradas)
	}

	// Y sobrevive lo MÁS NUEVO. Contar entradas no alcanza: podar desde el otro
	// extremo deja exactamente la misma cantidad y tira todo lo reciente, que es
	// justo lo único que alguien va a buscar en un historial. Con la cuenta sola,
	// esa inyección pasaba en verde.
	if lista[0].SQL != fmt.Sprintf("SELECT %d", maxEntradas+sobran-1) {
		t.Errorf("la más reciente es %q: se podó por el lado equivocado", lista[0].SQL)
	}
	ultima := lista[len(lista)-1].SQL
	if ultima != fmt.Sprintf("SELECT %d", sobran) {
		t.Errorf("la más vieja que quedó es %q, se esperaba la número %d", ultima, sobran)
	}
}

// TestUnaConsultaEnormeSeRecorta: un INSERT generado con diez mil filas adentro
// es una sola sentencia de megabytes, y nadie la vuelve a correr desde acá.
func TestUnaConsultaEnormeSeRecorta(t *testing.T) {
	s := store(t)
	grande := "SELECT '" + strings.Repeat("x", maxLargoSQL*2) + "'"
	if _, err := s.Add(Entry{ConnectionID: "c1", SQL: grande}); err != nil {
		t.Fatalf("Add(): %v", err)
	}
	lista, _ := s.List("c1", 0)
	if len(lista[0].SQL) > maxLargoSQL+64 {
		t.Errorf("la consulta quedó con %d bytes", len(lista[0].SQL))
	}
	if !strings.Contains(lista[0].SQL, "recortado") {
		t.Error("se recortó sin decirlo: quien la copie del historial no sabría que le falta un pedazo")
	}
}

// TestUnArchivoCorruptoNoTiraLaAplicacion.
//
// Un JSON roto —un corte de luz de una versión anterior, un editor de texto— no
// puede impedir abrir la aplicación. Se empieza de cero en memoria y el archivo
// queda en disco: borrarlo en silencio se llevaría la evidencia de qué pasó.
func TestUnArchivoCorruptoNoTiraLaAplicacion(t *testing.T) {
	s := store(t)
	if err := os.WriteFile(s.historial, []byte("{esto no es json"), 0o600); err != nil {
		t.Fatal(err)
	}
	lista, err := s.List("", 0)
	if err != nil {
		t.Fatalf("List() con el archivo roto: %v", err)
	}
	if len(lista) != 0 {
		t.Errorf("salió algo de un archivo corrupto: %+v", lista)
	}
	// Y se puede seguir usando.
	if _, err := s.Add(Entry{ConnectionID: "c1", SQL: "SELECT 1"}); err != nil {
		t.Fatalf("Add() después del archivo roto: %v", err)
	}

	// Y el archivo roto SIGUE ESTANDO. Antes esto era una promesa del comentario
	// y nada más: la app empezaba de cero en memoria y la primera consulta que
	// se corriera después pisaba el archivo. La evidencia duraba hasta el
	// siguiente Enter.
	apartado, err := os.ReadFile(s.historial + ".corrupto")
	if err != nil {
		t.Fatalf("el archivo corrupto no quedó apartado: %v", err)
	}
	if string(apartado) != "{esto no es json" {
		t.Errorf("lo apartado no es lo que estaba roto: %q", apartado)
	}
}

// Dos IDs seguidos no se repiten aunque el reloj no haya avanzado.
//
// Se generan en un bucle cerrado a propósito. Guardar dos consultas «seguidas»
// desde afuera no prueba nada: entre una y otra hay una escritura a disco, y
// para cuando vuelve, el reloj ya avanzó — así que un `UnixNano` pelado pasaría
// ese test en esta máquina y fallaría en otra. Acá no hay nada en el medio, y el
// reloj de Windows avanza de a ~15 ms: con la hora sola, esto se repite seguro.
func TestNuevoIDNoSeRepiteAunqueElRelojNoAvance(t *testing.T) {
	vistos := make(map[string]bool, 20000)
	for i := 0; i < 20000; i++ {
		id := nuevoID()
		if vistos[id] {
			t.Fatalf("el ID %q salió dos veces en %d intentos", id, i+1)
		}
		vistos[id] = true
	}
}

// Y la consecuencia de lo anterior, del lado de lo que se ve: guardar dos
// consultas seguidas deja DOS. Con IDs repetidos, `Save` reemplazaba —el ID que
// ya existe pisa— y `DeleteSaved` borraba las dos de una.
func TestDosGuardadasSeguidasNoSePisan(t *testing.T) {
	s := store(t)

	primera, err := s.Save(Saved{Name: "una", SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	segunda, err := s.Save(Saved{Name: "otra", SQL: "SELECT 2"})
	if err != nil {
		t.Fatal(err)
	}
	if primera.ID == segunda.ID {
		t.Fatalf("las dos salieron con el ID %q", primera.ID)
	}

	guardadas, err := s.Saved("")
	if err != nil {
		t.Fatal(err)
	}
	if len(guardadas) != 2 {
		t.Fatalf("quedaron %d consultas guardadas, se esperaban 2: una pisó a la otra", len(guardadas))
	}

	// Y borrar una deja la otra.
	if err := s.DeleteSaved(primera.ID); err != nil {
		t.Fatal(err)
	}
	quedan, err := s.Saved("")
	if err != nil {
		t.Fatal(err)
	}
	if len(quedan) != 1 || quedan[0].Name != "otra" {
		t.Errorf("después de borrar una quedaron %d: %+v", len(quedan), quedan)
	}
}

// TestLasGuardadasPonenPrimeroLasDeEstaConexion, sin esconder las otras: una
// consulta escrita contra otra base sigue sirviendo de punto de partida.
func TestLasGuardadasPonenPrimeroLasDeEstaConexion(t *testing.T) {
	s := store(t)
	for _, q := range []Saved{
		{Name: "zeta ajena", SQL: "SELECT 1", ConnectionID: "otra"},
		{Name: "beta propia", SQL: "SELECT 2", ConnectionID: "esta"},
		{Name: "alfa ajena", SQL: "SELECT 3", ConnectionID: "otra"},
	} {
		if _, err := s.Save(q); err != nil {
			t.Fatalf("Save(%q): %v", q.Name, err)
		}
	}
	lista, err := s.Saved("esta")
	if err != nil {
		t.Fatalf("Saved(): %v", err)
	}
	if len(lista) != 3 {
		t.Fatalf("se escondió alguna: %+v", lista)
	}
	if lista[0].Name != "beta propia" {
		t.Errorf("la de esta conexión no quedó primera: %v", nombres(lista))
	}
	if lista[1].Name != "alfa ajena" || lista[2].Name != "zeta ajena" {
		t.Errorf("las ajenas no quedaron por nombre: %v", nombres(lista))
	}

	// Sin nombre no se guarda: una lista de consultas sin nombre no se puede
	// usar, y el nombre es lo único que las distingue.
	if _, err := s.Save(Saved{SQL: "SELECT 1"}); err == nil {
		t.Error("se guardó una consulta sin nombre")
	}
}

// TestGuardarConElMismoIDReemplaza: editar una consulta guardada no puede dejar
// dos con el mismo nombre y distinto cuerpo.
func TestGuardarConElMismoIDReemplaza(t *testing.T) {
	s := store(t)
	q, err := s.Save(Saved{Name: "ventas", SQL: "SELECT 1", SavedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	q.SQL = "SELECT 2"
	if _, err := s.Save(q); err != nil {
		t.Fatal(err)
	}
	lista, _ := s.Saved("")
	if len(lista) != 1 {
		t.Fatalf("quedaron %d: %+v", len(lista), lista)
	}
	if lista[0].SQL != "SELECT 2" {
		t.Errorf("no se reemplazó: %q", lista[0].SQL)
	}

	if err := s.DeleteSaved(q.ID); err != nil {
		t.Fatalf("DeleteSaved(): %v", err)
	}
	if lista, _ := s.Saved(""); len(lista) != 0 {
		t.Errorf("no se borró: %+v", lista)
	}
}

func archivoCrudo(t *testing.T, ruta string) string {
	t.Helper()
	b, err := os.ReadFile(ruta)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("leer %s: %v", ruta, err)
	}
	return string(b)
}

func nombres(qs []Saved) []string {
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, q.Name)
	}
	return out
}

// TestElHistorialTambienTieneTopeDeBytes.
//
// El tope por cantidad no alcanza: el archivo se reescribe ENTERO en cada
// consulta que se corre, así que quinientas entradas de ocho kilobytes serían
// cuatro megabytes por cada Enter del editor. Los dos topes cubren mitades
// distintas del mismo problema.
func TestElHistorialTambienTieneTopeDeBytes(t *testing.T) {
	s := store(t)
	// Cada entrada ya se recorta a maxLargoSQL, así que para pasar el tope de
	// bytes hacen falta más de maxBytes/maxLargoSQL de ellas. El cálculo va acá
	// y no un número a ojo: si alguno de los dos topes cambia, el caso sigue
	// probando lo que dice en vez de pasar por casualidad.
	const cuantas = maxBytes/maxLargoSQL + 16
	grande := strings.Repeat("x", maxLargoSQL)
	for i := 0; i < cuantas; i++ {
		if _, err := s.Add(Entry{
			ConnectionID: "c1",
			SQL:          "SELECT " + string(rune('a'+i)) + " -- " + grande,
		}); err != nil {
			t.Fatalf("Add(): %v", err)
		}
	}
	lista, _ := s.List("", 0)
	if len(lista) >= cuantas {
		t.Fatalf("no se podó por bytes: quedaron las %d entradas grandes", len(lista))
	}
	total := 0
	for _, e := range lista {
		total += len(e.SQL)
	}
	if total > maxBytes {
		t.Errorf("el historial pesa %d bytes y el tope es %d", total, maxBytes)
	}
	// Lo más nuevo es lo que sobrevive: podar desde el otro lado dejaría un
	// historial que solo tiene lo que ya no interesa.
	if !strings.HasPrefix(lista[0].SQL, "SELECT "+string(rune('a'+cuantas-1))) {
		t.Errorf("la más reciente no sobrevivió: %.20q", lista[0].SQL)
	}
}
