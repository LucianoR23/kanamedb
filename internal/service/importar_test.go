package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/csvimport"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

func csvDePrueba(t *testing.T, contenido string) string {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), "datos.csv")
	if err := os.WriteFile(ruta, []byte(contenido), 0o600); err != nil {
		t.Fatal(err)
	}
	return ruta
}

// TestImportarUnCSVEnLosCuatroMotores.
//
// Lo que se comprueba es lo que hace que una importación sirva: que entren
// todas las filas, que el mapeo respete el orden y saltee lo que no va, y —lo
// más importante— que un archivo con UNA fila mala no deje media tabla escrita.
func TestImportarUnCSVEnLosCuatroMotores(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_import", true)
			abierta, _ := sesion.abierta()
			// La tabla viene con dos filas; se vacía para contar lo importado.
			if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)); err != nil {
				t.Fatal(err)
			}

			imp := NewImports(NewQueries(sesion))
			// La tercera columna del archivo no va a ninguna parte.
			ruta := csvDePrueba(t, "id,nombre,sobra\n10,diez,x\n11,once,y\n12,,z\n")
			plan := ImportPlan{
				RunID:      "imp1",
				Path:       ruta,
				Schema:     esq,
				Table:      tabla,
				Options:    csvimport.Options{HasHeader: true, EmptyAsNull: true},
				Mapping:    []string{"id", "nombre", ""},
				OnConflict: ConflictFail,
			}

			// El ensayo no deja nada.
			ens := imp.DryRun(ctx, plan)
			if !ens.OK {
				t.Fatalf("el ensayo falló: %+v", ens.Failure)
			}
			if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 0 {
				t.Fatalf("el ensayo dejó %d filas: tiene que revertir", n)
			}

			res := imp.Run(ctx, plan)
			if !res.OK {
				t.Fatalf("la importación falló: %+v", res.Failure)
			}
			if res.Read != 3 || res.Inserted != 3 {
				t.Errorf("leídas=%d insertadas=%d, quería 3 y 3", res.Read, res.Inserted)
			}
			if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "10=diez 11=once 12=NULL" {
				t.Errorf("la tabla quedó con %q; el campo vacío tenía que ser NULL", got)
			}
		})
	}
}

// TestUnaFilaMalaNoDejaMediaTablaEscrita: es la promesa del diálogo —una
// transacción, todo o nada— y la que hace que se pueda volver a intentar sin
// mirar qué entró.
func TestUnaFilaMalaNoDejaMediaTablaEscrita(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_import_malo", true)
			abierta, _ := sesion.abierta()
			if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)); err != nil {
				t.Fatal(err)
			}

			imp := NewImports(NewQueries(sesion))
			// «no es un número» en una columna entera: lo rechaza el SERVIDOR,
			// que es quien sabe. Kaname no adivina tipos.
			ruta := csvDePrueba(t, "id,nombre\n1,uno\n2,dos\nno es un número,tres\n")
			res := imp.Run(ctx, ImportPlan{
				RunID: "imp2", Path: ruta, Schema: esq, Table: tabla,
				Options: csvimport.Options{HasHeader: true},
				Mapping: []string{"id", "nombre"},
			})
			if res.OK {
				t.Fatal("una fila con un entero inválido no debería haber entrado")
			}
			if res.Failure == nil || res.Failure.Message == "" {
				t.Errorf("el fallo no dice nada: %+v", res.Failure)
			}
			if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 0 {
				t.Errorf("quedaron %d filas de una importación que falló: tenía que revertir entera", n)
			}
		})
	}
}

// TestSaltearLasQueChocan comprueba la otra política, que en cada motor se
// escribe distinto.
func TestSaltearLasQueChocan(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_import_choque", true)
			// Quedan las filas 1 y 2 de tablaDeDatos.
			imp := NewImports(NewQueries(sesion))
			ruta := csvDePrueba(t, "id,nombre\n1,repetida\n3,tres\n")
			plan := ImportPlan{
				RunID: "imp3", Path: ruta, Schema: esq, Table: tabla,
				Options: csvimport.Options{HasHeader: true},
				Mapping: []string{"id", "nombre"},
			}

			// Con «fail», el choque aborta y no entra ni la 3.
			plan.OnConflict = ConflictFail
			if res := imp.Run(ctx, plan); res.OK {
				t.Fatal("un choque de clave tendría que abortar con la política por defecto")
			}
			if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos" {
				t.Fatalf("la tabla quedó con %q después de un fallo", got)
			}

			// Con «saltear», la 1 se deja como estaba y la 3 entra.
			plan.OnConflict = ConflictSkip
			res := imp.Run(ctx, plan)
			if !res.OK {
				t.Fatalf("saltear falló: %+v", res.Failure)
			}
			if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos 3=tres" {
				t.Errorf("la tabla quedó con %q: la 1 no se tenía que pisar", got)
			}
			if res.Inserted != 1 {
				t.Errorf("Inserted = %d: la salteada no se cuenta", res.Inserted)
			}

			// Y «saltear» saltea SOLO los choques de clave. Un valor inválido
			// sigue siendo un error: con INSERT IGNORE en MySQL 'abc' entraba
			// como 0 y se contaba como insertada, y con OR IGNORE en SQLite una
			// fila que viola NOT NULL desaparecía en silencio (C-04).
			plan.Path = csvDePrueba(t, "id,nombre\nabc,invalida\n4,cuatro\n")
			if res := imp.Run(ctx, plan); res.OK {
				t.Errorf("un id que no es un número entró con «saltear»: %+v", res)
			}
			if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos 3=tres" {
				t.Errorf("la tabla quedó con %q después de una fila inválida", got)
			}
			// Y con max_error_count = 0 —legal, y posible desde el SessionSQL—
			// SHOW WARNINGS no muestra nada: tampoco puede pasar (review del
			// 2026-09-12). Solo en MariaDB: MySQL 9 exige SESSION_VARIABLES_ADMIN
			// para tocarla y el usuario de pruebas no lo tiene. El código que
			// lo comprueba es el mismo para los dos.
			if caso.nombre == "mariadb" {
				// Por el SessionSQL de la conexión, que es justamente por
				// donde alguien lo pondría: llega a cada conexión del pool.
				guardada, err := sesion.store.Get(c.ID)
				if err != nil {
					t.Fatal(err)
				}
				guardada.Advanced.SessionSQL = "SET SESSION max_error_count = 0"
				if err := sesion.store.Update(guardada); err != nil {
					t.Fatal(err)
				}
				if res := sesion.Connect(ctx, c.ID); !res.OK {
					t.Fatal(res.Failure.Message)
				}
				imp = NewImports(NewQueries(sesion))
				if res := imp.Run(ctx, plan); res.OK {
					t.Errorf("con max_error_count = 0 la fila inválida entró: %+v", res)
				}
				if got := strings.Join(filas(t, sesion, c, esq, tabla), " "); got != "1=uno 2=dos 3=tres" {
					t.Errorf("con max_error_count = 0 la tabla quedó con %q", got)
				}
			}
		})
	}
}

func TestUnMapeoQueNoSePuedeAplicar(t *testing.T) {
	if _, err := columnasDestino([]string{"", ""}); err == nil {
		t.Error("se aceptó un mapeo sin ninguna columna")
	}
	if _, err := columnasDestino([]string{"id", "id"}); err == nil {
		t.Error("se aceptó la misma columna dos veces")
	}
	if got, err := columnasDestino([]string{"id", "", "nombre"}); err != nil ||
		strings.Join(got, "|") != "id|nombre" {
		t.Errorf("columnasDestino = %v, %v", got, err)
	}
}

// TestUnLoteGrandeSePartePorLaMitad comprueba que las filas se manden de a
// tandas y no todas en una sentencia, que se pasaría del límite de parámetros.
func TestUnLoteGrandeSePartePorLaMitad(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_import_lote", true)
	abierta, _ := sesion.abierta()
	if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	b.WriteString("id,nombre\n")
	const n = filasPorLote + 250
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%d,f%d\n", i, i)
	}
	imp := NewImports(NewQueries(sesion))
	res := imp.Run(ctx, ImportPlan{
		RunID: "imp4", Path: csvDePrueba(t, b.String()), Schema: esq, Table: tabla,
		Options: csvimport.Options{HasHeader: true},
		Mapping: []string{"id", "nombre"},
	})
	if !res.OK {
		t.Fatalf("falló: %+v", res.Failure)
	}
	if res.Inserted != int64(n) {
		t.Errorf("insertadas=%d, quería %d", res.Inserted, n)
	}
	if got, _ := abierta.db.Count(ctx, esq, tabla, nil); got != int64(n) {
		t.Errorf("la tabla tiene %d filas y quería %d", got, n)
	}
}

// TestUnLoteBuenoYDespuesUnoMaloNoDejanNada.
//
// Es el caso que de verdad podría dejar media tabla: el primer lote de mil
// filas entra bien y el segundo falla. Con una sola transacción no queda nada;
// sin ella quedarían mil filas y nadie sabría cuáles.
func TestUnLoteBuenoYDespuesUnoMaloNoDejanNada(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_import_2lotes", true)
	abierta, _ := sesion.abierta()
	if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	b.WriteString("id,nombre\n")
	for i := 1; i <= filasPorLote; i++ {
		fmt.Fprintf(&b, "%d,f%d\n", i, i)
	}
	// La primera del SEGUNDO lote es la que rompe.
	b.WriteString("no es un número,mala\n")

	imp := NewImports(NewQueries(sesion))
	res := imp.Run(ctx, ImportPlan{
		RunID: "imp5", Path: csvDePrueba(t, b.String()), Schema: esq, Table: tabla,
		Options: csvimport.Options{HasHeader: true},
		Mapping: []string{"id", "nombre"},
	})
	if res.OK {
		t.Fatal("la importación terminó bien con una fila inválida")
	}
	if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 0 {
		t.Errorf("quedaron %d filas: el primer lote no se revirtió", n)
	}
	// Y se dice en qué línea del ARCHIVO empieza el lote que rompió, que es lo
	// que deja ir a buscarla.
	if res.Line != filasPorLote+2 {
		t.Errorf("Line = %d, quería %d", res.Line, filasPorLote+2)
	}
}

// TestImportarContraProduccionExigeEscribirElNombreDeLaBase.
//
// Importar es escribir, y contra producción escribir exige tipear. El caso que
// hace falta cubrir no es el obvio —el botón deshabilitado— sino el que sí
// protege: que la comprobación esté en Go. Una que viva solo en el asistente
// no es una protección, es un cartel, y cualquiera que llame al binding
// directamente la saltea.
//
// El ENSAYO también lo pide, y no por simetría: inserta las filas de verdad
// antes de revertirlas, así que toma los mismos candados de la tabla y consume
// los valores de las secuencias, que un ROLLBACK no devuelve.
func TestImportarContraProduccionExigeEscribirElNombreDeLaBase(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_import_prod", true)
	abierta, _ := sesion.abierta()
	if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)); err != nil {
		t.Fatal(err)
	}
	abierta.conn.Environment = connection.Production

	imp := NewImports(NewQueries(sesion))
	base := ImportPlan{
		RunID: "impProd", Path: csvDePrueba(t, "id,nombre\n1,uno\n"),
		Schema: esq, Table: tabla,
		Options: csvimport.Options{HasHeader: true},
		Mapping: []string{"id", "nombre"},
	}

	// Sin confirmación no entra nada, ni importando ni ensayando.
	for _, caso := range []struct {
		nombre string
		correr func(ImportPlan) ImportResult
	}{
		{"importar", func(p ImportPlan) ImportResult { return imp.Run(ctx, p) }},
		{"ensayar", func(p ImportPlan) ImportResult { return imp.DryRun(ctx, p) }},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			res := caso.correr(base)
			if res.OK {
				t.Fatal("corrió contra producción sin confirmación")
			}
			if res.Failure == nil || !strings.Contains(res.Failure.Message, "producción") {
				t.Errorf("el fallo no dice que es producción: %+v", res.Failure)
			}
			// Una palabra que no es la de la base tampoco sirve.
			mal := base
			mal.Confirm = "no es el nombre"
			if caso.correr(mal).OK {
				t.Error("aceptó una confirmación que no coincide")
			}
			if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 0 {
				t.Fatalf("quedaron %d filas de un intento sin confirmar", n)
			}
		})
	}

	// Y con la palabra correcta sí: una puerta que no se puede abrir tampoco
	// sirve. Target es lo que el asistente usa para saber qué pedir, así que
	// tiene que decir exactamente la misma palabra que exige la importación.
	target, err := imp.Target()
	if err != nil {
		t.Fatalf("Target(): %v", err)
	}
	if !target.NeedsConfirmation || target.ConfirmWord != nombreDeLaBase(abierta) {
		t.Fatalf("Target() = %+v, quería la confirmación con %q", target, nombreDeLaBase(abierta))
	}
	bien := base
	bien.Confirm = target.ConfirmWord
	if res := imp.Run(ctx, bien); !res.OK {
		t.Fatalf("con la confirmación correcta falló: %+v", res.Failure)
	}
	if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 1 {
		t.Errorf("quedaron %d filas, quería 1", n)
	}
}

// TestElEnsayoAfirmaQueRevirtioSoloDespuesDeRevertir.
//
// `RolledBack` es la afirmación entera del ensayo —«no quedó nada»— y el punto
// del test es que sea una comprobación y no una suposición: se pone cuando el
// ROLLBACK devuelve bien, no al empezar. Un import de verdad no la pone nunca,
// porque ahí las filas se quedan a propósito.
func TestElEnsayoAfirmaQueRevirtioSoloDespuesDeRevertir(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_import_rb", true)
	abierta, _ := sesion.abierta()
	if err := abierta.db.Exec(ctx, "DELETE FROM "+califica(c, esq, tabla)); err != nil {
		t.Fatal(err)
	}

	imp := NewImports(NewQueries(sesion))
	plan := ImportPlan{
		RunID: "impRB", Path: csvDePrueba(t, "id,nombre\n7,siete\n"),
		Schema: esq, Table: tabla,
		Options: csvimport.Options{HasHeader: true},
		Mapping: []string{"id", "nombre"},
	}

	ens := imp.DryRun(ctx, plan)
	if !ens.OK || !ens.RolledBack {
		t.Fatalf("el ensayo no afirma haber revertido: %+v", ens)
	}
	if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 0 {
		t.Fatalf("el ensayo dejó %d filas mientras dice que revirtió", n)
	}

	// Un ensayo que falla NO afirma nada: no llegó al rollback explícito.
	malo := plan
	malo.Path = csvDePrueba(t, "id,nombre\nno es un número,mal\n")
	if fallo := imp.DryRun(ctx, malo); fallo.OK || fallo.RolledBack {
		t.Errorf("un ensayo que falló afirma haber revertido: %+v", fallo)
	}

	// Y una importación de verdad tampoco: sus filas quedan.
	res := imp.Run(ctx, plan)
	if !res.OK || res.RolledBack {
		t.Errorf("la importación afirma haber revertido: %+v", res)
	}
	if n, _ := abierta.db.Count(ctx, esq, tabla, nil); n != 1 {
		t.Errorf("quedaron %d filas, quería 1", n)
	}
}

// TestUnaConexionDeSoloLecturaLoDiceConLaRazon.
//
// Son TRES razones distintas y quien importa merece saber cuál: no es lo mismo
// haberlo elegido que descubrir que estás apuntando a una réplica. Antes se
// miraba solo el interruptor, así que contra una réplica la importación se iba
// a estrellar con el error crudo del driver.
func TestUnaConexionDeSoloLecturaLoDiceConLaRazon(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_import_ro", true)
	abierta, _ := sesion.abierta()
	abierta.server = &engine.ServerInfo{InRecovery: true}

	imp := NewImports(NewQueries(sesion))
	res := imp.Run(ctx, ImportPlan{
		RunID: "impRO", Path: csvDePrueba(t, "id,nombre\n1,uno\n"),
		Schema: esq, Table: tabla,
		Options: csvimport.Options{HasHeader: true},
		Mapping: []string{"id", "nombre"},
	})
	if res.OK {
		t.Fatal("importó contra una réplica")
	}
	if res.Failure == nil || !strings.Contains(res.Failure.Message, "réplica") {
		t.Errorf("el fallo no dice que es una réplica: %+v", res.Failure)
	}

	// Y Target dice lo mismo antes de dejar empezar.
	target, err := imp.Target()
	if err != nil {
		t.Fatalf("Target(): %v", err)
	}
	if !target.ReadOnly || !strings.Contains(target.Reason, "réplica") {
		t.Errorf("Target() = %+v, quería solo lectura por réplica", target)
	}
}

// TestUnaTablaAnchaNoSePasaDelLimiteDeParametros.
//
// «Mil filas por lote deja margen para tablas anchas» era exactamente al revés:
// el límite de 65535 marcadores es POR SENTENCIA, así que a más columnas entran
// MENOS filas. Con 66 columnas mapeadas, mil filas son 66.000 marcadores y el
// servidor rechaza el primer lote con un error crudo del driver.
func TestUnaTablaAnchaNoSePasaDelLimiteDeParametros(t *testing.T) {
	for _, n := range []int{1, 20, 65, 66, 200, 70000} {
		filas := filasDelLote(n)
		if filas < 1 {
			t.Errorf("con %d columnas el lote quedó en %d filas: no avanzaría nunca", n, filas)
		}
		if p := filas * n; p > maxParametros && filas > 1 {
			t.Errorf("con %d columnas van %d filas = %d marcadores, y el tope es %d",
				n, filas, p, maxParametros)
		}
	}
	// Y una tabla angosta sigue yendo de a mil: el tope no puede volverse el
	// caso normal.
	if got := filasDelLote(6); got != filasPorLote {
		t.Errorf("con 6 columnas el lote quedó en %d y tendría que ser %d", got, filasPorLote)
	}
}

// TestImportarEnUnaTablaAncha lo prueba contra el motor de verdad.
//
// El test de arriba comprueba la cuenta; este comprueba que el servidor la
// acepte, que es lo único que cierra el caso.
func TestImportarEnUnaTablaAncha(t *testing.T) {
	sesion, _ := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	abierta, _ := sesion.abierta()

	const columnas = 80
	esq := "kn_suite"
	var cols, cab strings.Builder
	for i := 0; i < columnas; i++ {
		if i > 0 {
			cols.WriteString(", ")
			cab.WriteString(",")
		}
		fmt.Fprintf(&cols, "c%d text", i)
		fmt.Fprintf(&cab, "c%d", i)
	}
	tabla := "kn_import_ancha"
	completo := fmt.Sprintf(`"%s"."%s"`, esq, tabla)
	if err := abierta.db.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+esq); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, "DROP TABLE IF EXISTS "+completo); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (%s)", completo, cols.String())); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+completo) })

	// Mil filas × 80 columnas son 80.000 marcadores si el lote no se achica.
	const filas = 1000
	var b strings.Builder
	b.WriteString(cab.String() + "\n")
	for f := 0; f < filas; f++ {
		for i := 0; i < columnas; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "v%d-%d", f, i)
		}
		b.WriteString("\n")
	}
	mapeo := make([]string, columnas)
	for i := range mapeo {
		mapeo[i] = fmt.Sprintf("c%d", i)
	}

	imp := NewImports(NewQueries(sesion))
	res := imp.Run(ctx, ImportPlan{
		RunID: "impAncha", Path: csvDePrueba(t, b.String()), Schema: esq, Table: tabla,
		Options: csvimport.Options{HasHeader: true},
		Mapping: mapeo,
	})
	if !res.OK {
		t.Fatalf("una tabla de %d columnas no se pudo importar: %+v", columnas, res.Failure)
	}
	if res.Inserted != filas {
		t.Errorf("entraron %d filas de %d", res.Inserted, filas)
	}
}
