package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/store"
)

// Exportar e importar conexiones: S02, «Exportar para compartir…»,
// «Exportar carpeta…» e «Importar…».
//
// El archivo tiene el formato de la libreta y **nunca lleva un secreto**: no
// porque se filtre algo al escribirlo, sino porque el modelo no tiene dónde
// ponerlo —la contraseña y la frase de paso viven en el keychain y nunca pasan
// por acá—. `TestExportarNuncaLlevaUnSecreto` guarda las dos y exige que el
// archivo no las contenga. La ruta de la clave privada SSH sí viaja: es una
// ruta, no la clave.

// SharedFile es lo que quedó escrito al exportar.
type SharedFile struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// ExportConnections escribe las conexiones pedidas en path, para compartirlas.
//
// No valida: exporta lo que hay, rota o no. Quien importa ve los problemas en
// la vista previa, que es donde corresponde decidir.
func (s *Connections) ExportConnections(ids []string, path string) (SharedFile, error) {
	if strings.TrimSpace(path) == "" {
		return SharedFile{}, errors.New("hace falta una ruta donde guardar el archivo")
	}
	if len(ids) == 0 {
		return SharedFile{}, errors.New("no hay ninguna conexión que exportar")
	}
	// El archivo exportado ES una libreta válida, así que elegir la propia en
	// el selector la reemplazaría por el subconjunto exportado: todo lo demás
	// desaparece y sus contraseñas quedan huérfanas en el keychain. Y además
	// se escribiría por fuera del cerrojo del store.
	if mismoArchivo(path, s.store.Path()) {
		return SharedFile{}, errors.New("esa es tu libreta de conexiones: elegí otro archivo para exportar")
	}
	todas, err := s.store.List()
	if err != nil {
		return SharedFile{}, err
	}
	porID := make(map[string]connection.Connection, len(todas))
	for _, c := range todas {
		porID[c.ID] = c
	}
	conns := make([]connection.Connection, 0, len(ids))
	for _, id := range ids {
		c, ok := porID[id]
		if !ok {
			return SharedFile{}, fmt.Errorf("%w: %s", store.ErrNotFound, id)
		}
		conns = append(conns, c)
	}
	data, err := store.Encode(conns, time.Now())
	if err != nil {
		return SharedFile{}, err
	}
	n, err := escribirAtomico(path, string(data))
	if err != nil {
		return SharedFile{}, err
	}
	return SharedFile{Path: path, Count: len(conns), Bytes: n}, nil
}

// ImportPreview es lo que trae un archivo, antes de agregar nada.
type ImportPreview struct {
	Path string `json:"path"`

	// Fingerprint identifica el contenido que se mostró. ImportConnections lo
	// exige de vuelta: si el archivo cambió entre la vista previa y el clic,
	// lo que se agrega no es lo que se vio.
	Fingerprint string `json:"fingerprint"`

	Connections []ImportCandidate `json:"connections"`

	// Notes son avisos sobre el archivo: claves que se ignoraron, y en
	// particular las que parecen un secreto, porque quien escribió
	// `password = "…"` a mano espera que se importe y no se importa.
	Notes []string `json:"notes"`
}

// ImportCandidate es una conexión del archivo, resumida para decidir.
type ImportCandidate struct {
	Index       int                    `json:"index"`
	Name        string                 `json:"name"`
	Engine      connection.Engine      `json:"engine"`
	Describe    string                 `json:"describe"`
	Environment connection.Environment `json:"environment"`
	Production  bool                   `json:"production"`
	Folder      string                 `json:"folder"`
	SSH         bool                   `json:"ssh"`

	// Existing es el nombre de una conexión que ya apunta al mismo lugar
	// —motor, host, puerto, base y usuario—, si la hay. Importar dos veces el
	// mismo archivo no tiene por qué duplicar la libreta sin avisar.
	Existing string `json:"existing"`

	// SessionSQL es lo que la entrada corre en cada conexión al abrirla, tal
	// cual viene en el archivo. Se muestra ANTES de importar y no después: es
	// SQL escrita por otra persona que va a correr con las credenciales de
	// esta, sin vista previa ni confirmación, y lo mínimo es verla.
	SessionSQL string `json:"sessionSql"`

	// Problems son los errores de validación. Una entrada rota se muestra
	// igual, con sus problemas, y no se puede incluir.
	Problems []connection.FieldError `json:"problems"`
}

// PreviewImport lee un archivo compartido y dice qué trae, sin agregar nada.
func (s *Connections) PreviewImport(path string) (ImportPreview, error) {
	data, huella, err := leerCompartido(path)
	if err != nil {
		return ImportPreview{}, err
	}
	dec, err := store.Decode(data)
	if err != nil {
		return ImportPreview{}, err
	}
	if len(dec.Connections) == 0 {
		return ImportPreview{}, errors.New("el archivo no trae ninguna conexión")
	}
	propias, err := s.store.List()
	if err != nil {
		return ImportPreview{}, err
	}

	out := ImportPreview{
		Path:        path,
		Fingerprint: huella,
		Connections: make([]ImportCandidate, 0, len(dec.Connections)),
		Notes:       notasDeClavesIgnoradas(dec.Ignored),
	}
	for i, c := range dec.Connections {
		cand := ImportCandidate{
			Index:       i,
			Name:        c.Name,
			Engine:      c.Engine,
			Describe:    c.Describe(),
			Environment: c.Environment,
			Production:  c.Environment == connection.Production,
			Folder:      c.Folder,
			SSH:         c.SSH.Enabled,
			Existing:    mismoDestino(propias, c),
			SessionSQL:  c.Advanced.SessionSQL,
			Problems:    []connection.FieldError{},
		}
		// El ID del archivo no cuenta —al importar se reemplaza—, así que la
		// validación no lo mira: sin esto, toda entrada sin id saldría rota
		// por un motivo que no le importa a nadie.
		c.ID = "vista-previa"
		var ve *connection.ValidationError
		if err := c.Validate(); errors.As(err, &ve) {
			cand.Problems = ve.Errors
		}
		out.Connections = append(out.Connections, cand)
	}
	return out, nil
}

// ImportConnections agrega a la libreta las entradas elegidas del archivo,
// cada una con un ID nuevo, o ninguna.
//
// El ID es nuevo aunque el archivo traiga uno: el ID es la clave del keychain,
// y heredarlo haría que una conexión importada tome la contraseña de otra que
// casualmente tenga el mismo. La contraseña se pide al conectar.
func (s *Connections) ImportConnections(path, fingerprint string, include []int) ([]ConnectionView, error) {
	if len(include) == 0 {
		return nil, errors.New("no se eligió ninguna conexión para importar")
	}
	data, huella, err := leerCompartido(path)
	if err != nil {
		return nil, err
	}
	if huella != fingerprint {
		return nil, errors.New("el archivo cambió desde la vista previa: volvé a abrirlo")
	}
	dec, err := store.Decode(data)
	if err != nil {
		return nil, err
	}

	nuevas := make([]connection.Connection, 0, len(include))
	vistos := make(map[int]bool, len(include))
	for _, i := range include {
		if i < 0 || i >= len(dec.Connections) {
			return nil, fmt.Errorf("el archivo no tiene una conexión número %d", i)
		}
		if vistos[i] {
			continue
		}
		vistos[i] = true
		c := dec.Connections[i]
		id, err := connection.NewID()
		if err != nil {
			return nil, err
		}
		c.ID = id
		nuevas = append(nuevas, c)
	}
	if err := s.store.AddAll(nuevas); err != nil {
		return nil, err
	}
	out := make([]ConnectionView, 0, len(nuevas))
	for _, c := range nuevas {
		out = append(out, s.view(c.Normalize()))
	}
	return out, nil
}

// leerCompartido lee el archivo entero, con el límite de tamaño puesto ANTES
// de leer: el archivo lo eligió una persona en un selector y puede ser
// cualquier cosa. Devuelve también la huella del contenido.
func leerCompartido(path string) ([]byte, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", errors.New("hace falta la ruta del archivo")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("no se pudo abrir el archivo: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("no se pudo leer el archivo: %w", err)
	}
	if info.Size() > store.MaxSharedFileBytes {
		return nil, "", store.ErrSharedFileTooBig
	}
	data, err := io.ReadAll(io.LimitReader(f, store.MaxSharedFileBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("no se pudo leer el archivo: %w", err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// mismoArchivo dice si las dos rutas son el mismo archivo. Compara las rutas
// limpias y, si los dos existen, el archivo de verdad: en Windows la misma
// ruta se escribe con mayúsculas o sin ellas, y `os.SameFile` lo sabe.
func mismoArchivo(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// mismoDestino devuelve el nombre de una conexión propia que apunta adonde
// apunta c, o vacío.
func mismoDestino(propias []connection.Connection, c connection.Connection) string {
	for _, p := range propias {
		if p.Engine != c.Engine || p.Database != c.Database {
			continue
		}
		if c.Engine == connection.SQLite {
			return p.Name
		}
		if p.Host == c.Host && p.Port == c.Port && p.User == c.User {
			return p.Name
		}
	}
	return ""
}

// notasDeClavesIgnoradas convierte las claves que Decode no leyó en avisos.
//
// Las que parecen un secreto llevan su propia frase: quien escribió
// `password = "…"` a mano está esperando que se importe, y hay que decirle
// que no y por qué antes de que se entere al conectar.
func notasDeClavesIgnoradas(claves []string) []string {
	notas := make([]string, 0, len(claves))
	for _, k := range claves {
		if pareceSecreto(k) {
			notas = append(notas, fmt.Sprintf(
				"El archivo trae «%s» y se ignoró: los secretos no viajan en archivos. La contraseña se pide al conectar y queda en el keychain.", k))
		} else {
			notas = append(notas, fmt.Sprintf("La clave «%s» no se conoce y se ignoró.", k))
		}
	}
	return notas
}

func pareceSecreto(clave string) bool {
	k := strings.ToLower(clave)
	for _, p := range []string{"pass", "secret", "token", "key", "credential", "contrase", "clave"} {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}
