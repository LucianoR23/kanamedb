// Package history guarda qué consultas se corrieron y cuáles se dejaron
// guardadas con un nombre.
//
// Son DOS cosas con dos dueños distintos y por eso viven en dos archivos:
//
//   - El **historial** es de esta máquina. Qué corriste el martes a la tarde no
//     es algo que quieras ver replicado en la notebook del trabajo, y es el
//     mismo criterio que el plan fijó para la lista de recientes. Va al
//     directorio de estado.
//   - Las **consultas guardadas** son trabajo: escribir una consulta de
//     veinte líneas cuesta, y quien sincroniza su libreta de conexiones entre
//     máquinas no quiere volver a escribirla del otro lado. Van al lado de
//     `connections.toml`, como los diagramas del ERD y por la misma razón.
//
// # Qué NO se guarda
//
// Nunca un valor de fila. El historial es lo que vos escribiste, no lo que la
// base contestó: un resultado en disco sería una copia de los datos del
// servidor, sin su control de acceso y sin su cifrado.
//
// Y nunca una sentencia que lleve una contraseña escrita. CLAUDE.md es
// terminante con eso —los secretos van al keychain y a ningún otro lado— y un
// `ALTER USER … PASSWORD 'x'` en el editor SQL es un secreto que el historial
// pondría en un archivo de texto. Ver `LlevaSecreto`.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxEntradas es cuántas corridas se recuerdan.
//
// El archivo se reescribe entero en cada consulta, así que el tope no es un
// lujo: sin él, una sesión larga convierte cada Enter del editor en una
// escritura cada vez más cara. Quinientas alcanzan para «lo que hice hoy y
// ayer», que es para lo que se mira un historial; lo que se quiere conservar
// más allá de eso se guarda con nombre, que es la otra mitad de este paquete.
const maxEntradas = 500

// maxBytes es cuánto texto de consultas se guarda en total.
//
// El tope por cantidad no alcanza: quinientas entradas de ocho kilobytes cada
// una son cuatro megabytes, y el archivo se reescribe ENTERO en cada consulta
// que se corre. Sin este segundo tope, un rato pegando sentencias largas en el
// editor convierte cada Enter en una escritura de megabytes.
//
// Medio mega alcanza para cientos de consultas de las que uno escribe a mano.
const maxBytes = 512 << 10

// maxLargoSQL es hasta cuánto texto se guarda de una consulta.
//
// Un INSERT generado con diez mil filas adentro es una sola sentencia de
// megabytes. Recortarlo no pierde nada útil —nadie vuelve a correr eso desde el
// historial— y evita que una sola entrada se coma el archivo.
const maxLargoSQL = 8 << 10

const versionActual = 1

// Entry es una consulta que se corrió.
type Entry struct {
	ID string `json:"id"`

	// ConnectionID es contra qué conexión se corrió. El historial se muestra
	// filtrado por la conexión abierta: la misma consulta contra dos bases
	// distintas es otra cosa.
	ConnectionID string `json:"connectionId"`

	// SQL es el texto tal como se escribió, recortado a maxLargoSQL.
	SQL string `json:"sql"`

	// RanAt es la última vez que se corrió.
	RanAt time.Time `json:"ranAt"`

	// Runs es cuántas veces seguidas se corrió la misma consulta.
	//
	// Existe para que repetir un SELECT seis veces afinando nada no llene el
	// historial con seis renglones idénticos, que es lo que lo vuelve inútil
	// justo cuando más se lo mira.
	Runs int `json:"runs"`

	// ElapsedMs y Rows son de la ÚLTIMA corrida.
	ElapsedMs int64 `json:"elapsedMs"`
	Rows      int64 `json:"rows"`

	// Failed dice si la última corrida falló, y Error qué dijo el motor.
	//
	// Una consulta que falló se guarda igual: lo que uno busca en el historial
	// es a menudo justamente la que falló, para arreglarla.
	Failed bool   `json:"failed"`
	Error  string `json:"error,omitempty"`
}

// Saved es una consulta guardada con nombre.
type Saved struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	SQL  string `json:"sql"`

	// ConnectionID es contra qué conexión se guardó, o vacío si vale para
	// cualquiera.
	//
	// No se usa para filtrar sino para ORDENAR: una consulta escrita contra
	// otra base sigue sirviendo de punto de partida, y esconderla obligaría a
	// recordar dónde se la guardó para poder encontrarla.
	ConnectionID string `json:"connectionId,omitempty"`

	SavedAt time.Time `json:"savedAt"`
}

// ErrNombreVacio lo devuelve Save cuando la consulta no tiene nombre.
var ErrNombreVacio = errors.New("la consulta guardada necesita un nombre")

// ErrSQLVacia lo devuelven Add y Save cuando no hay nada que guardar.
var ErrSQLVacia = errors.New("no hay ninguna consulta que guardar")

// Store son los dos archivos.
type Store struct {
	historial string
	guardadas string

	mu sync.Mutex
}

// New arma el store con las dos rutas.
func New(historial, guardadas string) *Store {
	return &Store{historial: historial, guardadas: guardadas}
}

// Add registra una corrida.
//
// Devuelve false —sin error— cuando la consulta NO se guardó por llevar un
// secreto. No es un fallo: es el comportamiento correcto, y quien llama puede
// decírselo a la pantalla en vez de mostrarlo como un problema.
func (s *Store) Add(e Entry) (bool, error) {
	e.SQL = strings.TrimSpace(e.SQL)
	if e.SQL == "" {
		return false, ErrSQLVacia
	}
	if LlevaSecreto(e.SQL) {
		return false, nil
	}
	e.SQL = recortar(e.SQL)
	if e.RanAt.IsZero() {
		e.RanAt = time.Now()
	}
	if e.Runs <= 0 {
		e.Runs = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entradas, err := leer[Entry](s.historial)
	if err != nil {
		return false, err
	}

	// La misma consulta contra la misma conexión, otra vez: se actualiza la
	// que está en vez de agregar un renglón idéntico. Se compara contra la
	// MÁS NUEVA y no contra todas: repetir algo de hace una hora sí es una
	// corrida nueva y merece subir en la lista.
	if len(entradas) > 0 {
		u := &entradas[len(entradas)-1]
		if u.ConnectionID == e.ConnectionID && u.SQL == e.SQL {
			u.Runs++
			u.RanAt, u.ElapsedMs, u.Rows = e.RanAt, e.ElapsedMs, e.Rows
			u.Failed, u.Error = e.Failed, e.Error
			return true, escribir(s.historial, entradas)
		}
	}

	if e.ID == "" {
		e.ID = nuevoID()
	}
	entradas = append(entradas, e)
	return true, escribir(s.historial, podar(entradas))
}

// List devuelve el historial, lo más reciente primero.
//
// `conn` vacío trae todo; con un id trae solo el de esa conexión. `limit` en
// cero o menos trae todo lo que haya.
func (s *Store) List(conn string, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entradas, err := leer[Entry](s.historial)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entradas))
	for i := len(entradas) - 1; i >= 0; i-- {
		if conn != "" && entradas[i].ConnectionID != conn {
			continue
		}
		out = append(out, entradas[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Clear borra el historial de una conexión, o el entero si `conn` está vacío.
func (s *Store) Clear(conn string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if conn == "" {
		return escribir(s.historial, []Entry{})
	}
	entradas, err := leer[Entry](s.historial)
	if err != nil {
		return err
	}
	quedan := make([]Entry, 0, len(entradas))
	for _, e := range entradas {
		if e.ConnectionID != conn {
			quedan = append(quedan, e)
		}
	}
	return escribir(s.historial, quedan)
}

// Save guarda una consulta con nombre. Con un ID que ya existe, la reemplaza.
func (s *Store) Save(q Saved) (Saved, error) {
	q.Name = strings.TrimSpace(q.Name)
	q.SQL = strings.TrimSpace(q.SQL)
	if q.Name == "" {
		return Saved{}, ErrNombreVacio
	}
	if q.SQL == "" {
		return Saved{}, ErrSQLVacia
	}
	// Una consulta guardada SÍ se revisa igual que el historial: es texto que
	// va a un archivo, y el archivo se sincroniza entre máquinas — así que un
	// secreto ahí llega más lejos todavía.
	if LlevaSecreto(q.SQL) {
		return Saved{}, fmt.Errorf(
			"esta consulta lleva una contraseña escrita y Kaname no la guarda en un archivo: " +
				"los secretos van al keychain del sistema")
	}
	q.SQL = recortar(q.SQL)
	if q.SavedAt.IsZero() {
		q.SavedAt = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	guardadas, err := leer[Saved](s.guardadas)
	if err != nil {
		return Saved{}, err
	}
	for i := range guardadas {
		if q.ID != "" && guardadas[i].ID == q.ID {
			guardadas[i] = q
			return q, escribir(s.guardadas, guardadas)
		}
	}
	if q.ID == "" {
		q.ID = nuevoID()
	}
	guardadas = append(guardadas, q)
	return q, escribir(s.guardadas, guardadas)
}

// Saved devuelve las consultas guardadas.
//
// Primero las de la conexión abierta y después el resto, y dentro de cada grupo
// por nombre. No se filtran: una consulta escrita contra otra base sigue
// sirviendo de punto de partida, y esconderla obligaría a recordar dónde se la
// guardó para poder encontrarla.
func (s *Store) Saved(conn string) ([]Saved, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	guardadas, err := leer[Saved](s.guardadas)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(guardadas, func(i, j int) bool {
		ai := guardadas[i].ConnectionID == conn && conn != ""
		aj := guardadas[j].ConnectionID == conn && conn != ""
		if ai != aj {
			return ai
		}
		return strings.ToLower(guardadas[i].Name) < strings.ToLower(guardadas[j].Name)
	})
	return guardadas, nil
}

// DeleteSaved borra una consulta guardada.
func (s *Store) DeleteSaved(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	guardadas, err := leer[Saved](s.guardadas)
	if err != nil {
		return err
	}
	quedan := make([]Saved, 0, len(guardadas))
	for _, q := range guardadas {
		if q.ID != id {
			quedan = append(quedan, q)
		}
	}
	return escribir(s.guardadas, quedan)
}

// formasConSecreto son las formas de SQL que llevan una contraseña ESCRITA.
//
// La lista es corta a propósito: son las que existen. PostgreSQL usa `PASSWORD
// '…'` en CREATE/ALTER ROLE y USER, MySQL y MariaDB usan `IDENTIFIED BY '…'` y
// también `IDENTIFIED WITH … BY '…'`, y las dos familias tienen variantes con
// `ENCRYPTED`. `CREATE SUBSCRIPTION` de Postgres lleva un connection string
// entero, contraseña incluida.
var formasConSecreto = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bpassword\s+'`),
	regexp.MustCompile(`(?i)\bpassword\s*=\s*'`),
	regexp.MustCompile(`(?i)\bidentified\s+(by|with)\b`),
	regexp.MustCompile(`(?i)\bencrypted\s+password\b`),
	regexp.MustCompile(`(?i)\bcreate\s+subscription\b`),
	regexp.MustCompile(`(?i)\bconnection\s+'`),
}

// LlevaSecreto dice si esta sentencia tiene una contraseña escrita adentro.
//
// Reconoce FORMAS, no intención, y eso es todo lo que se puede hacer sin
// entender la SQL. Se equivoca hacia el lado seguro: un `SELECT * FROM
// passwords` no lleva ninguna contraseña y aun así no se guarda. El costo de
// ese error es una consulta que no queda en el historial; el del error contrario
// es una contraseña en un archivo de texto, que es exactamente lo que CLAUDE.md
// prohíbe —los secretos van al keychain del sistema y a ningún otro lado—.
//
// No reemplaza a nada: es la única defensa que hay acá, porque quien escribe la
// consulta no está pensando en el historial cuando la escribe.
func LlevaSecreto(sql string) bool {
	for _, re := range formasConSecreto {
		if re.MatchString(sql) {
			return true
		}
	}
	return false
}

/* ------------------------------------------------------------ el archivo */

type archivo[T any] struct {
	Version int `json:"version"`
	Items   []T `json:"items"`
}

func leer[T any](ruta string) ([]T, error) {
	b, err := os.ReadFile(ruta)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leer %s: %w", filepath.Base(ruta), err)
	}
	var a archivo[T]
	if err := json.Unmarshal(b, &a); err != nil {
		// Un archivo corrupto NO tira la aplicación ni se borra solo: se empieza
		// de cero en memoria y el que está en disco queda para que alguien lo
		// mire. Perder el historial es molesto; perderlo Y no poder abrir la
		// app sería peor, y borrarlo en silencio se lleva la evidencia.
		return nil, nil
	}
	return a.Items, nil
}

func escribir[T any](ruta string, items []T) error {
	if err := os.MkdirAll(filepath.Dir(ruta), 0o700); err != nil {
		return fmt.Errorf("crear %s: %w", filepath.Dir(ruta), err)
	}
	b, err := json.MarshalIndent(archivo[T]{Version: versionActual, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	// Temporal y rename, como el resto de lo que este proyecto escribe: un
	// corte de luz a mitad de la escritura no puede dejar medio archivo.
	tmp := ruta + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("escribir %s: %w", filepath.Base(ruta), err)
	}
	if err := os.Rename(tmp, ruta); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("guardar %s: %w", filepath.Base(ruta), err)
	}
	return nil
}

// podar deja el historial adentro de los dos topes, tirando lo más viejo.
//
// Los dos y no uno: el de cantidad evita una lista ingobernable, y el de bytes
// evita que unas pocas consultas enormes hagan cara cada escritura. Cualquiera
// de los dos solo deja abierta la mitad del problema.
func podar(entradas []Entry) []Entry {
	if len(entradas) > maxEntradas {
		entradas = entradas[len(entradas)-maxEntradas:]
	}
	total := 0
	for _, e := range entradas {
		total += len(e.SQL)
	}
	corte := 0
	for corte < len(entradas)-1 && total > maxBytes {
		total -= len(entradas[corte].SQL)
		corte++
	}
	return entradas[corte:]
}

func recortar(sql string) string {
	if len(sql) <= maxLargoSQL {
		return sql
	}
	return sql[:maxLargoSQL] + "\n-- (recortado por Kaname)"
}

func nuevoID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
