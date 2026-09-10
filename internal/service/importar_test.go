package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/csvimport"
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
