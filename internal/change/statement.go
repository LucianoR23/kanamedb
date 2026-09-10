package change

// Impact es qué cuesta ejecutar una sentencia.
//
// La pantalla de revisión lo usa para avisar antes, no después. La diferencia
// entre «metadata» y «rewrite» sobre una tabla de cuarenta millones de filas es
// la diferencia entre un parpadeo y una ventana de mantenimiento.
type Impact string

const (
	// ImpactMetadata solo toca el catálogo. Es instantáneo pase lo que pase.
	ImpactMetadata Impact = "metadata"

	// ImpactScan lee todas las filas para verificar algo. Tarda en proporción
	// al tamaño de la tabla y puede fallar por los datos que ya están.
	ImpactScan Impact = "scan"

	// ImpactRewrite reescribe la tabla entera. Tarda, necesita espacio en disco
	// para las dos copias, y toma el bloqueo más fuerte que hay.
	ImpactRewrite Impact = "rewrite"

	// ImpactData modifica filas.
	ImpactData Impact = "data"
)

// Lock es el bloqueo que la sentencia toma sobre la tabla, en el vocabulario de
// quien la va a sufrir: qué deja de funcionar mientras corre.
type Lock string

const (
	// LockNone no molesta a nadie.
	LockNone Lock = "none"
	// LockWrites deja leer pero no escribir.
	LockWrites Lock = "writes"
	// LockAll no deja ni leer. Es ACCESS EXCLUSIVE.
	LockAll Lock = "all"
)

// Statement es una sentencia lista para ejecutar y lo que hay que saber antes
// de correrla.
//
// La SQL la arma siempre el motor, nunca la interfaz: los identificadores se
// citan por motor y la interfaz no tiene forma de saber cómo. Ver CLAUDE.md.
type Statement struct {
	// ChangeID es el cambio del que salió, para poder resaltarlo en la lista.
	ChangeID string `json:"changeId"`

	SQL    string `json:"sql"`
	Impact Impact `json:"impact"`
	Lock   Lock   `json:"lock"`

	// Destructive marca lo que puede perder datos de forma irreversible.
	Destructive bool `json:"destructive"`

	// RebuildsTable marca la sentencia que no MODIFICA la tabla sino que la
	// reconstruye: crea una nueva con la definición que se quiere, copia las
	// filas, tira la vieja y renombra.
	//
	// Es SQLite, que casi no tiene ALTER TABLE. Está acá y no deducido de
	// Impact porque decide dos cosas distintas: qué advierte la pantalla de
	// revisión —el costo es el tamaño de la tabla, no «solo metadatos»— y
	// cómo hay que ejecutarla, porque una reconstrucción necesita que las
	// claves foráneas estén apagadas mientras corre. Ver engine.TxOptions.
	RebuildsTable bool `json:"rebuildsTable,omitempty"`

	// Note es lo que hay que decirle a quien revisa antes de que aplique: por
	// qué esta sentencia va a tardar, qué bloquea, o qué garantía se pierde.
	// Vacía cuando no hay nada que avisar.
	Note string `json:"note,omitempty"`

	// Bound es la MISMA sentencia con los valores como parámetros, y es lo que
	// se ejecuta cuando la operación es de datos. Nil en un DDL.
	//
	// SQL lleva los valores escritos como literales, porque es lo que se lee,
	// se copia y se guarda como .sql: una vista previa con «$1» no se puede
	// revisar. Pero un valor escrito por alguien en una celda no puede
	// convertirse en SQL, y la única forma de garantizarlo es que nunca esté
	// adentro del texto que corre. Ver CLAUDE.md, «SQL siempre parametrizado».
	//
	// No cruza el puente: los valores ya están en Change, y el frontend no
	// ejecuta nada. El servicio vuelve a renderizar al aplicar, así que siempre
	// está donde hace falta.
	Bound *Bound `json:"-"`
}

// Bound es una sentencia lista para el driver: marcadores en el texto y los
// valores aparte.
type Bound struct {
	SQL string
	// Args van en el orden de los marcadores. Cada uno es un *string: nil es
	// NULL, y el texto lo interpreta el servidor según el tipo de la columna.
	Args []any

	// Op es qué hace la sentencia, para que un conteo que no cierra se
	// explique con las palabras justas: un INSERT que alcanzó cero filas no
	// es «la fila ya no está», es que un trigger o una regla la descartó.
	Op Op

	// Rows es cuántas filas TIENE que tocar la sentencia, y se comprueba
	// después de ejecutarla. Es la protección de fondo de la grilla: un UPDATE
	// por clave primaria que toca cero filas es una fila que otro borró, y uno
	// que toca dos es una clave que no era clave. Los dos casos revierten en
	// vez de seguir. Cero significa que no se comprueba.
	Rows int64
}
