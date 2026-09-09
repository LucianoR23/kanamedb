package engine

import "time"

// ServerInfo es lo que se sabe del servidor una vez conectado. Alimenta la
// barra de estado y las decisiones sobre qué se puede hacer con la conexión.
//
// Los campos son los mismos para los cuatro motores, aunque cada uno los
// averigüe de forma distinta y alguno no tenga qué contestar. Cuando un motor
// no tiene el concepto, el cero es la respuesta correcta y no una omisión:
// SQLite no tiene usuario, así que CurrentUser vacío es la verdad.
type ServerInfo struct {
	// Kind es qué motor hay del otro lado. Es lo primero que la interfaz
	// necesita saber para decidir qué ofrecer.
	Kind Kind `json:"engine"`

	// Version es el texto completo, como lo reporta el servidor.
	Version string `json:"version"`
	// VersionNum es la versión como número comparable. Cada motor la arma a su
	// manera —Postgres da server_version_num, 180006 para 18.6— pero la regla
	// es la misma: mayor*10000 + menor*100 + parche.
	VersionNum int `json:"versionNum"`
	// Display es la versión corta para mostrar: "PostgreSQL 18.6".
	Display string `json:"display"`

	CurrentUser string `json:"currentUser"`
	CurrentDB   string `json:"currentDatabase"`
	Encoding    string `json:"encoding"`
	TimeZone    string `json:"timeZone"`

	// IsSuperuser importa para explicar por qué una operación no se puede hacer.
	IsSuperuser bool `json:"isSuperuser"`

	// InRecovery es true si el servidor es una réplica. Escribir contra una
	// réplica falla con un mensaje poco claro, así que conviene avisar antes.
	// En MySQL y MariaDB el equivalente es un servidor con read_only puesto.
	InRecovery bool `json:"inRecovery"`

	// DefaultReadOnly es true si el servidor fuerza transacciones de solo
	// lectura, otra causa de escrituras que fallan sin explicación obvia.
	DefaultReadOnly bool `json:"defaultReadOnly"`

	// VisibleTables es cuántas tablas ve el usuario en esta base.
	//
	// Dice algo que la versión del servidor no dice: que las credenciales no
	// solo entran, sino que además alcanzan para ver algo. Conectar bien y ver
	// cero tablas casi siempre significa que faltan permisos o que la base no
	// es la que el usuario cree.
	VisibleTables int `json:"visibleTables"`

	// Latency es lo que tardó el ida y vuelta de la verificación.
	Latency time.Duration `json:"-"`
	// LatencyMS es lo mismo, en milisegundos, para el frontend.
	LatencyMS int64 `json:"latencyMs"`
}

// minimas son las versiones más viejas que la aplicación maneja, por motor.
//
// El criterio es el mismo en los cuatro: la más antigua con soporte oficial
// vigente. Sostener versiones que su propio fabricante abandonó es prometer
// algo que no se puede cumplir — no hay dónde reportar un bug del motor.
var minimas = map[Kind]int{
	// PostgreSQL 14, la más vieja con soporte a septiembre de 2026.
	Postgres: 140000,
	// MySQL 8.4 LTS. La 8.0 salió de soporte premier en abril de 2026.
	MySQL: 80400,
	// MariaDB 10.11 LTS, la más vieja de las cuatro series mantenidas.
	MariaDB: 101100,
	// SQLite 3.35, que es donde aparecen ALTER TABLE DROP COLUMN y RETURNING.
	SQLite: 33500,
}

// MinVersion es la versión más vieja soportada de un motor.
func MinVersion(k Kind) int { return minimas[k] }

// Supported dice si la versión del servidor está dentro de lo que la app maneja.
func (s ServerInfo) Supported() bool {
	min, ok := minimas[s.Kind]
	if !ok {
		return true
	}
	return s.VersionNum >= min
}
