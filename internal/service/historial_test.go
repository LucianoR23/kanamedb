package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/history"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// El historial es lo que escribiste vos, no lo que contestó la base — y el
// mensaje de un fallo de datos es lo que contestó la base, CON los valores
// adentro. Postgres traduce un `unique_violation` a «Ya hay filas con (email) =
// (ana@example.com) repetido»: guardar eso deja un pedazo de los datos del
// servidor en un archivo de texto de esta máquina, sin su control de acceso.
//
// El test no comprueba que el campo esté vacío: comprueba que el VALOR no esté
// en el archivo. Una versión que lo guardara en otro campo pasaría el primero.
func TestElHistorialNoGuardaLosValoresDeFilaQueVienenEnUnError(t *testing.T) {
	dir := t.TempDir()
	rutaHistorial := filepath.Join(dir, "historial.json")
	q := NewQueries(nil)
	q.UsarHistorial(history.New(rutaHistorial, filepath.Join(dir, "consultas.json")))

	const valor = "ana@example.com"
	q.anotar("conn-1", "insert into clientes(email) values ($1)", nil, &engine.Failure{
		Kind:     engine.FailureData,
		Message:  "Ya hay filas con (email) = (" + valor + ") repetido: no se puede exigir que sea único.",
		Hint:     "Para ver todos los repetidos: SELECT email, count(*) FROM clientes GROUP BY email.",
		SQLState: "23505",
		Detail:   "Key (email)=(" + valor + ") already exists.",
	})

	datos, err := os.ReadFile(rutaHistorial)
	if err != nil {
		t.Fatalf("el historial no se escribió: %v", err)
	}
	if strings.Contains(string(datos), valor) {
		t.Errorf("el valor de la fila quedó en %s:\n%s", filepath.Base(rutaHistorial), datos)
	}

	// Y la consulta sí se guardó, marcada como fallida: no guardar el mensaje
	// no puede significar perder la entrada, que es la que uno busca.
	entradas, err := q.historial.List("conn-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 1 {
		t.Fatalf("quedaron %d entradas, se esperaba 1", len(entradas))
	}
	if !entradas[0].Failed {
		t.Error("la entrada no quedó marcada como fallida")
	}
}

// El conteo de filas sí se guarda —es un número, no un dato— y viene del lote.
func TestElHistorialGuardaCuantasFilasVolvieron(t *testing.T) {
	dir := t.TempDir()
	q := NewQueries(nil)
	q.UsarHistorial(history.New(
		filepath.Join(dir, "historial.json"), filepath.Join(dir, "consultas.json")))

	q.anotar("conn-1", "select 1", &query.Batch{
		ElapsedMs: 12,
		Results:   []query.Result{{Rows: make([][]*string, 3)}},
	}, nil)

	entradas, err := q.historial.List("conn-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 1 {
		t.Fatalf("quedaron %d entradas", len(entradas))
	}
	if entradas[0].Rows != 3 || entradas[0].ElapsedMs != 12 {
		t.Errorf("Rows = %d, ElapsedMs = %d", entradas[0].Rows, entradas[0].ElapsedMs)
	}
}
