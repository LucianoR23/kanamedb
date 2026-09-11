package service

import (
	"context"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/drift"
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// CompareRequest es qué se compara contra qué.
type CompareRequest struct {
	// SourceID y TargetID son las conexiones. El ORIGEN es el que manda: las
	// diferencias describen qué habría que hacerle al DESTINO para que se
	// parezca al origen.
	SourceID string `json:"sourceId"`
	TargetID string `json:"targetId"`
}

// CompareResult es el resultado de comparar dos esquemas.
//
// Struct con `ok` y no un par, por lo mismo que ConnectResult: un par se puede
// ignorar a medias, y una comparación fallida terminaría dibujando una lista
// vacía —«no hay diferencias»— que es la peor respuesta posible acá.
type CompareResult struct {
	OK bool `json:"ok"`

	Result *drift.Resultado `json:"result,omitempty"`

	// Source y Target son cómo describir cada lado en pantalla: nombre, entorno
	// y base. Sin ellos, la pantalla tendría que volver a buscarlos y podría
	// mostrar una conexión distinta de la que se comparó.
	Source CompareSide `json:"source"`
	Target CompareSide `json:"target"`

	Failure *engine.Failure `json:"failure,omitempty"`
}

// CompareSide identifica un lado de la comparación.
type CompareSide struct {
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	Environment  string `json:"environment"`
	// Engine es el motor. La pantalla lo muestra y la comparación lo usa: entre
	// motores distintos los tipos no se comparan.
	Engine   string `json:"engine"`
	Database string `json:"database"`
	// Describe es usuario@host:puerto/base, sin credenciales.
	Describe string `json:"describe"`
	Tables   int    `json:"tables"`
	// Production marca el lado que hay que mirar dos veces.
	Production bool `json:"production"`
}

// Compare lee el catálogo de dos conexiones y devuelve en qué se diferencian.
//
// # Esto ABRE las dos conexiones y no toca la que está abierta
//
// Ninguno de los dos lados se instala como la sesión en curso: se abren, se les
// lee el catálogo y se cierran. El changeset que cuelga de la sesión abierta
// —que puede tener horas de trabajo— no se toca, y la conexión del workspace
// sigue siendo la misma antes y después.
//
// # Y no ejecuta nada
//
// Lee catálogos. No corre una sola sentencia contra ninguna de las dos bases, ni
// siquiera contra el origen, y las sentencias que produce la comparación van al
// changeset o a un archivo: esta pantalla no aplica. Es la razón por la que
// comparar contra producción es seguro incluso sin marcarla de solo lectura.
func (s *Session) Compare(ctx context.Context, req CompareRequest) CompareResult {
	if req.SourceID == "" || req.TargetID == "" {
		return CompareResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "Faltan las dos conexiones que hay que comparar.",
		}}
	}
	if req.SourceID == req.TargetID {
		// Comparar algo consigo mismo siempre da cero diferencias, y esa
		// respuesta se lee como «están alineados». Es más honesto no dejarlo.
		return CompareResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "Las dos conexiones son la misma.",
			Hint:    "Elegí dos bases distintas: comparar una consigo misma no dice nada.",
		}}
	}

	origen, lado, failure := s.leerCatalogo(ctx, req.SourceID)
	if failure != nil {
		failure.Hint = anteponer("No se pudo leer el catálogo del origen.", failure.Hint)
		return CompareResult{Failure: failure}
	}

	destino, ladoDestino, failure := s.leerCatalogo(ctx, req.TargetID)
	if failure != nil {
		failure.Hint = anteponer("No se pudo leer el catálogo del destino.", failure.Hint)
		return CompareResult{Failure: failure, Source: lado}
	}

	// Si los dos lados no corren el mismo motor, la comparación deja los tipos
	// afuera y lo dice. Ver drift.Opciones.
	res := drift.Comparar(*origen, *destino, drift.Opciones{
		MismoMotor: lado.Engine == ladoDestino.Engine,
	})
	return CompareResult{OK: true, Result: &res, Source: lado, Target: ladoDestino}
}

// leerCatalogo abre una conexión, le lee el esquema y la cierra.
//
// El cierre va en un defer y no al final: entre medio hay un `Inspect` que puede
// fallar, y una conexión que queda abierta después de una comparación fallida es
// una sesión colgada en el servidor —y en el bastión, si hay túnel— que nadie va
// a cerrar hasta reiniciar la aplicación.
func (s *Session) leerCatalogo(ctx context.Context, id string) (*schema.Snapshot, CompareSide, *engine.Failure) {
	abierta, failure := s.abrirConexion(ctx, id, "")
	if failure != nil {
		return nil, CompareSide{}, failure
	}
	defer abierta.cerrar()

	snap, err := abierta.db.Introspect(ctx)
	if err != nil {
		return nil, CompareSide{}, &engine.Failure{
			Kind:    engine.FailureOther,
			Message: fmt.Sprintf("No se pudo leer el esquema de %q.", abierta.conn.Name),
			Detail:  engine.Redact(err.Error()),
		}
	}
	snap.Normalize()

	lado := CompareSide{
		ConnectionID: abierta.conn.ID,
		Name:         abierta.conn.Name,
		Environment:  string(abierta.conn.Environment),
		Engine:       string(abierta.conn.Engine),
		Database:     snap.Database,
		Describe:     abierta.conn.Describe(),
		Tables:       snap.TotalTables(),
		Production:   abierta.conn.Environment.NeedsWriteConfirmation(),
	}
	return snap, lado, nil
}

// anteponer pone un contexto adelante de una sugerencia que ya existía.
//
// Cuál de los dos lados falló es la mitad de la información: «no se pudo
// conectar» sin decir a cuál de las dos bases deja a la persona probando las dos.
func anteponer(contexto, hint string) string {
	if hint == "" {
		return contexto
	}
	return contexto + " " + hint
}
