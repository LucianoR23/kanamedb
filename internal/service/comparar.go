package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
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

	// ID identifica ESTA comparación. La migración se pide con él, y si la
	// comparación que hay en memoria ya es otra, se rechaza: el archivo tiene
	// que salir de lo que la persona está mirando, no de una corrida posterior.
	ID string `json:"id,omitempty"`

	Result *drift.Resultado `json:"result,omitempty"`

	// Statements es la sentencia de cada diferencia que tiene operación, por
	// ID de diferencia, escrita por el motor del DESTINO —que es el que la va a
	// correr—. La comparación no sabe escribir SQL, y la interfaz tampoco: ver
	// CLAUDE.md. Una diferencia con `change` y sin entrada acá no existe: si el
	// motor no supo escribirla, la diferencia se devuelve sin operación y con
	// el motivo en `noStatement`.
	Statements map[string]change.Statement `json:"statements,omitempty"`

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

	// El destino se queda abierto hasta el final: las sentencias las escribe
	// SU motor, y en SQLite escribir una reconstrucción de tabla necesita leer
	// la definición que hay. Se cierra en el defer pase lo que pase.
	abierta, destino, ladoDestino, failure := s.abrirYLeer(ctx, req.TargetID)
	if failure != nil {
		failure.Hint = anteponer("No se pudo leer el catálogo del destino.", failure.Hint)
		return CompareResult{Failure: failure, Source: lado}
	}
	defer abierta.cerrar()

	// Si los dos lados no corren el mismo motor, la comparación deja los tipos
	// afuera y lo dice. Ver drift.Opciones.
	res := drift.Comparar(*origen, *destino, drift.Opciones{
		MismoMotor: lado.Engine == ladoDestino.Engine,
		// Con la lista de objetos de un lado incompleta no se comparan
		// objetos: sobre una lista parcial, cada objeto del otro lado salía
		// como «existe solo en el destino» (C-22).
		SinObjetos: origen.ObjectsError != "" || destino.ObjectsError != "",
	})
	// Si la lista de objetos de un lado no se pudo leer, la comparación siguió
	// con las tablas, y eso se dice: sin esto, un lado con la lista vacía por
	// un error se ve igual que un lado sin vistas.
	if origen.ObjectsError != "" {
		res.NoComparado = append(res.NoComparado,
			"Las vistas, funciones y demás objetos del ORIGEN: no se pudo leer la lista. "+
				engine.Redact(origen.ObjectsError))
	}
	if destino.ObjectsError != "" {
		res.NoComparado = append(res.NoComparado,
			"Las vistas, funciones y demás objetos del DESTINO: no se pudo leer la lista. "+
				engine.Redact(destino.ObjectsError))
	}
	sentencias := renderizarDiferencias(ctx, abierta.db, &res)

	id, err := connection.NewID()
	if err != nil {
		return CompareResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "No se pudo identificar la comparación.",
			Detail:  engine.Redact(err.Error()),
		}}
	}
	out := CompareResult{
		OK: true, ID: id, Result: &res, Statements: sentencias,
		Source: lado, Target: ladoDestino,
	}

	// Se recuerda SOLO la última. La migración se genera desde acá y no desde
	// lo que mande la interfaz, así que la SQL que va al archivo es la que
	// escribió el motor y nada más.
	s.mu.Lock()
	s.comparacion = &out
	s.mu.Unlock()
	return out
}

// renderizarDiferencias le pide al motor del destino la sentencia de cada
// diferencia que tiene operación.
//
// Si el motor no la sabe escribir —no debería pasar: la comparación solo emite
// operaciones que `change.Validate` acepta, y todo motor las renderiza—, la
// diferencia se devuelve SIN operación y con el motivo, que es el contrato del
// paquete: o hay una operación que sirve, o hay un motivo. Una operación que
// no se puede escribir ofrecida como si se pudiera es la promesa falsa que el
// review de la comparación encontró cinco veces.
func renderizarDiferencias(ctx context.Context, db engine.Conn, res *drift.Resultado) map[string]change.Statement {
	out := make(map[string]change.Statement)
	for i := range res.Diferencias {
		d := &res.Diferencias[i]
		if d.Cambio == nil {
			continue
		}
		c := *d.Cambio
		// El ID del cambio es el de la diferencia: `Statement.ChangeID` vuelve
		// a apuntar a la fila de la pantalla.
		c.ID = d.ID
		st, err := db.RenderDDL(ctx, c)
		if err != nil {
			d.Cambio = nil
			d.SinSentencia = "El motor del destino no supo escribir esta operación: " +
				engine.Redact(err.Error())
			continue
		}
		out[d.ID] = st
	}
	suprimirReconstruccionesEnConflicto(res, out)
	return out
}

// suprimirReconstruccionesEnConflicto deja sin sentencia toda reconstrucción
// de tabla que en el archivo vendría DESPUÉS de otra sentencia sobre la misma
// tabla.
//
// Una reconstrucción —SQLite, que casi no tiene ALTER TABLE— se escribe desde
// la definición que la tabla tiene AHORA, al comparar. Si en el archivo la
// precede otra sentencia sobre esa tabla, la reconstrucción no la conoce: un
// `ADD COLUMN email` seguido del rebuild de un `SET NOT NULL` crea la copia sin
// `email`, la llena sin `email` y tira la original. La columna desaparece sin
// un solo error, y el archivo decía que se podía correr entero.
//
// La regla es la del orden del archivo, no la de la pantalla: se recorre en el
// orden en que las sentencias van a correr —ver rangoDeMigracion— y una
// reconstrucción cuya tabla ya fue tocada se convierte en una diferencia sin
// sentencia, con el motivo y el camino: aplicar esto y volver a comparar. La
// que va primero se queda, porque una sentencia común DESPUÉS de una
// reconstrucción sí es válida: opera sobre la tabla ya reconstruida.
func suprimirReconstruccionesEnConflicto(res *drift.Resultado, sentencias map[string]change.Statement) {
	orden := make([]int, 0, len(res.Diferencias))
	for i := range res.Diferencias {
		orden = append(orden, i)
	}
	sort.SliceStable(orden, func(a, b int) bool {
		return rangoDeMigracion(res.Diferencias[orden[a]]) < rangoDeMigracion(res.Diferencias[orden[b]])
	})

	tocada := make(map[string]bool)
	for _, i := range orden {
		d := &res.Diferencias[i]
		st, hay := sentencias[d.ID]
		if !hay || d.Cambio == nil {
			continue
		}
		tabla := d.Cambio.Schema + "." + d.Cambio.Table
		if st.RebuildsTable && tocada[tabla] {
			d.Cambio = nil
			d.SinSentencia = "Esta operación reconstruye la tabla desde lo que hay ahora, y " +
				"otra sentencia de esta misma migración ya la cambia antes: en el archivo " +
				"la reconstrucción desharía ese cambio sin avisar. Aplicá la migración y " +
				"volvé a comparar; entonces sale sola."
			delete(sentencias, d.ID)
			continue
		}
		tocada[tabla] = true
	}
}

// leerCatalogo abre una conexión, le lee el esquema y la cierra.
//
// El cierre va en un defer y no al final: entre medio hay un `Inspect` que puede
// fallar, y una conexión que queda abierta después de una comparación fallida es
// una sesión colgada en el servidor —y en el bastión, si hay túnel— que nadie va
// a cerrar hasta reiniciar la aplicación.
func (s *Session) leerCatalogo(ctx context.Context, id string) (*schema.Snapshot, CompareSide, *engine.Failure) {
	abierta, snap, lado, failure := s.abrirYLeer(ctx, id)
	if failure != nil {
		return nil, CompareSide{}, failure
	}
	abierta.cerrar()
	return snap, lado, nil
}

// abrirYLeer abre una conexión y le lee el esquema. Quien llama la cierra.
//
// Existe aparte de leerCatalogo porque el destino de una comparación tiene que
// seguir abierto después de leído: las sentencias las escribe su motor.
func (s *Session) abrirYLeer(ctx context.Context, id string) (*openSession, *schema.Snapshot, CompareSide, *engine.Failure) {
	abierta, failure := s.abrirConexion(ctx, id, "")
	if failure != nil {
		return nil, nil, CompareSide{}, failure
	}

	snap, err := abierta.db.Introspect(ctx)
	if err != nil {
		abierta.cerrar()
		return nil, nil, CompareSide{}, &engine.Failure{
			Kind:    engine.FailureOther,
			Message: fmt.Sprintf("No se pudo leer el esquema de %q.", abierta.conn.Name),
			Detail:  engine.Redact(err.Error()),
		}
	}
	// Los objetos que no son tablas —vistas, funciones, triggers, tipos— no
	// vienen en Introspect: los pega la sesión después, igual que al conectar.
	// Sin esta llamada la comparación decía que los miraba «por nombre» y no
	// los miraba en absoluto: una vista que faltaba en el destino no aparecía.
	// Se encontró probando a mano, no con un test — y ahora hay un test.
	conObjetos(ctx, abierta.db, snap)
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
	return abierta, snap, lado, nil
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
