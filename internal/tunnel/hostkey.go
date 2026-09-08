package tunnel

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKey es la clave pública que presenta un servidor SSH.
type HostKey struct {
	// Algorithm es el tipo tal como lo nombra el protocolo: ssh-ed25519,
	// ssh-rsa, ecdsa-sha2-nistp256.
	Algorithm string `json:"algorithm"`
	// Bits es el tamaño de la clave, para mostrarlo junto al algoritmo.
	Bits int `json:"bits"`
	// Fingerprint es la huella SHA256 en el mismo formato que imprime
	// `ssh-keygen -lf`, para que se puedan comparar carácter por carácter.
	Fingerprint string `json:"fingerprint"`
}

// TrustedKey es una clave que ya estaba en nuestro known_hosts.
type TrustedKey struct {
	HostKey
	// AddedAt es cuándo se confió en ella. El diálogo lo muestra —"confiada el
	// 5 de septiembre"— porque una clave que cambió ayer y una que cambió hace
	// un año son sospechas de distinto tamaño.
	AddedAt time.Time `json:"addedAt"`
}

// Verdict es el resultado de comparar lo que presenta el servidor con lo que
// teníamos guardado.
type Verdict string

const (
	// VerdictTrusted: coincide con la que ya habíamos aceptado.
	VerdictTrusted Verdict = "trusted"
	// VerdictUnknown: nunca vimos este host. Se puede aceptar, con confirmación.
	VerdictUnknown Verdict = "unknown"
	// VerdictChanged: teníamos otra. Nunca se acepta sin decisión explícita, y
	// el diálogo es bloqueante: puede ser un servidor reinstalado o alguien en
	// el medio, y desde acá no se puede distinguir.
	VerdictChanged Verdict = "changed"
)

// Inspection es lo que sabe la aplicación antes de mandar una sola credencial.
type Inspection struct {
	Verdict Verdict `json:"verdict"`
	// Address es host:puerto, como se guarda en known_hosts.
	Address   string  `json:"address"`
	Presented HostKey `json:"presented"`
	// Known es la que teníamos, solo cuando Verdict es changed.
	Known *TrustedKey `json:"known,omitempty"`

	// AuthorizedKey es la clave presentada, en el formato de una línea de
	// OpenSSH. Es la que se guarda si la persona acepta.
	//
	// Viaja hasta la interfaz y vuelve en vez de volver a pedírsela al servidor
	// por dos motivos. El primero es de corrección: garantiza que se guarda
	// exactamente la clave cuya huella se mostró. Reconectar para confiar abría
	// una ventana en la que el servidor podía presentar otra, y entonces la
	// persona aceptaba una huella y se guardaba otra.
	//
	// El segundo lo enseñó el servidor de pruebas: OpenSSH 9.8 penaliza a quien
	// se conecta sin intentar autenticarse. Inspeccionar y después reconectar
	// para confiar son dos conexiones sin autenticar seguidas, y un bastión con
	// PerSourcePenalties activado —que es el default— empieza a rechazar. Con
	// una sola, el primer contacto cuesta una conexión sin autenticar y no dos.
	//
	// No es un secreto: es la clave pública del servidor, la misma que cualquiera
	// obtiene conectándose.
	AuthorizedKey string `json:"authorizedKey"`
}

// KnownHosts es el archivo de claves aceptadas de Kaname.
//
// Es propio y no `~/.ssh/known_hosts`. Dos motivos: escribir en el archivo de
// OpenSSH del usuario es meterse con configuración que no es nuestra y que
// otras herramientas también usan; y tener las decisiones de confianza de
// Kaname en un solo lugar hace que se puedan auditar de un vistazo.
//
// El formato sí es el de OpenSSH, para que se pueda leer con `ssh-keygen -F` y
// no haya que aprender nada nuevo para revisarlo.
type KnownHosts struct {
	path string
	mu   sync.Mutex
}

func NewKnownHosts(path string) *KnownHosts { return &KnownHosts{path: path} }

// Path es dónde vive el archivo. La interfaz lo muestra para que se pueda ir a
// mirar.
func (k *KnownHosts) Path() string { return k.path }

// comentarioKaname marca las líneas que escribimos nosotros y lleva la fecha.
//
// known_hosts no tiene campo de fecha, y el diálogo de clave cambiada la
// necesita. El comentario es parte del formato de OpenSSH y las herramientas
// estándar lo ignoran, así que se puede guardar ahí sin romper nada.
const comentarioKaname = "kaname:"

// Lookup devuelve la clave que teníamos para esa dirección, si había alguna.
func (k *KnownHosts) Lookup(address string) (*TrustedKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	f, err := os.Open(k.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("abrir known_hosts: %w", err)
	}
	defer f.Close()

	buscado := knownhosts.Normalize(address)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		linea := sc.Bytes()
		_, hosts, pub, comentario, _, err := ssh.ParseKnownHosts(linea)
		if err != nil {
			// Una línea rota no invalida el archivo entero: se salta. Un
			// known_hosts se edita a mano y una línea mal pegada no debería
			// dejar sin conexión a todos los demás hosts.
			continue
		}
		for _, h := range hosts {
			if h != buscado {
				continue
			}
			return &TrustedKey{
				HostKey: describir(pub),
				AddedAt: fechaDeComentario(comentario),
			}, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("leer known_hosts: %w", err)
	}
	return nil, nil
}

// Trust guarda la clave como aceptada para esa dirección.
//
// Reemplaza la línea anterior si existía: es lo que hace "Reemplazar la clave y
// conectar" del diálogo de clave cambiada. Dejar las dos haría que el archivo
// aceptara ambas, que es exactamente lo que no se quiere después de un cambio.
func (k *KnownHosts) TrustKey(address string, pub ssh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return fmt.Errorf("crear el directorio de known_hosts: %w", err)
	}

	conservadas, err := k.lineasSinHost(address)
	if err != nil {
		return err
	}

	linea := fmt.Sprintf("%s %s %s%s\n",
		knownhosts.Normalize(address),
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
		comentarioKaname,
		time.Now().UTC().Format(time.RFC3339))

	// Escritura atómica: el archivo se reemplaza entero o no se toca. Un corte
	// a mitad de camino dejaría un known_hosts truncado, y un known_hosts
	// truncado no es un archivo con menos hosts: es uno que vuelve a preguntar
	// por hosts que ya estaban aceptados, y preguntar de más entrena a decir
	// que sí sin mirar.
	tmp, err := os.CreateTemp(filepath.Dir(k.path), ".known_hosts-*")
	if err != nil {
		return fmt.Errorf("crear el archivo temporal: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(strings.Join(conservadas, "") + linea); err != nil {
		tmp.Close()
		return fmt.Errorf("escribir known_hosts: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sincronizar known_hosts: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cerrar known_hosts: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("permisos de known_hosts: %w", err)
	}
	if err := os.Rename(tmp.Name(), k.path); err != nil {
		return fmt.Errorf("reemplazar known_hosts: %w", err)
	}
	return nil
}

// lineasSinHost devuelve el archivo actual sin las líneas de esa dirección.
func (k *KnownHosts) lineasSinHost(address string) ([]string, error) {
	f, err := os.Open(k.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("abrir known_hosts: %w", err)
	}
	defer f.Close()

	buscado := knownhosts.Normalize(address)
	var salida []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		linea := sc.Text()
		_, hosts, _, _, _, err := ssh.ParseKnownHosts([]byte(linea))
		if err == nil {
			salta := false
			for _, h := range hosts {
				if h == buscado {
					salta = true
					break
				}
			}
			if salta {
				continue
			}
		}
		salida = append(salida, linea+"\n")
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("leer known_hosts: %w", err)
	}
	return salida, nil
}

// describir arma la descripción mostrable de una clave pública.
func describir(pub ssh.PublicKey) HostKey {
	return HostKey{
		Algorithm:   pub.Type(),
		Bits:        bitsDe(pub),
		Fingerprint: ssh.FingerprintSHA256(pub),
	}
}

// bitsDe estima el tamaño de la clave para mostrarlo al lado del algoritmo.
//
// Es informativo: sirve para que quien mira reconozca "ed25519 de 256" como lo
// que espera. La comparación que decide es siempre la huella completa.
func bitsDe(pub ssh.PublicKey) int {
	if c, ok := pub.(ssh.CryptoPublicKey); ok {
		type conTamano interface{ Size() int }
		if k, ok := c.CryptoPublicKey().(conTamano); ok {
			return k.Size() * 8
		}
	}
	switch pub.Type() {
	case ssh.KeyAlgoED25519:
		return 256
	case ssh.KeyAlgoECDSA256:
		return 256
	case ssh.KeyAlgoECDSA384:
		return 384
	case ssh.KeyAlgoECDSA521:
		return 521
	}
	return 0
}

// fechaDeComentario saca la fecha del comentario que escribimos nosotros.
// Devuelve el cero si la línea la escribió otra herramienta.
func fechaDeComentario(comentario string) time.Time {
	c := strings.TrimSpace(comentario)
	if !strings.HasPrefix(c, comentarioKaname) {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimPrefix(c, comentarioKaname))
	if err != nil {
		return time.Time{}
	}
	return t
}

// Trust acepta una clave en el formato de una línea de OpenSSH, que es como
// viaja en Inspection.AuthorizedKey.
func (k *KnownHosts) Trust(address, authorizedKey string) error {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return fmt.Errorf("la clave a aceptar no se pudo interpretar: %w", err)
	}
	return k.TrustKey(address, pub)
}
