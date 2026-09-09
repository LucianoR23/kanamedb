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

	// Note es lo que hay que decirle a quien revisa antes de que aplique: por
	// qué esta sentencia va a tardar, qué bloquea, o qué garantía se pierde.
	// Vacía cuando no hay nada que avisar.
	Note string `json:"note,omitempty"`
}
