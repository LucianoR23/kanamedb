package service

import (
	"context"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// sufijoSSH es lo que se le agrega al ID de conexión para guardar el secreto
// del bastión en el keychain.
//
// Una conexión con túnel tiene DOS secretos —el de la base y el del salto— y el
// keychain se indexa por una sola clave. Con el sufijo, borrar la conexión borra
// los dos y ninguno queda huérfano.
const sufijoSSH = "#ssh"

// SSHSecretID es la clave del keychain donde vive el secreto del bastión: la
// contraseña de SSH, o la frase de paso de la clave privada.
func SSHSecretID(connectionID string) string { return connectionID + sufijoSSH }

// sufijoClaveSSH distingue la clave privada como contenido del secreto del
// bastión: son dos cosas —la clave y su frase de paso— y se guardan aparte.
const sufijoClaveSSH = "#ssh-key"

// SSHKeySecretID es la clave del keychain donde vive la clave privada SSH como
// contenido (PEM). Existe para el teléfono, donde no hay ~/.ssh: si está, se usa
// en lugar de leer KeyPath. En escritorio la clave sigue siendo una ruta.
func SSHKeySecretID(connectionID string) string { return connectionID + sufijoClaveSSH }

// Hosts resuelve la confianza en las claves de los servidores SSH.
//
// Es un servicio aparte de Connections porque responde a otra cosa: no
// administra la libreta, administra en qué hosts se confía. Y esa confianza no
// pertenece a una conexión sino a la máquina — dos conexiones al mismo bastión
// comparten la decisión.
type Hosts struct {
	known *tunnel.KnownHosts
}

func NewHosts(known *tunnel.KnownHosts) *Hosts { return &Hosts{known: known} }

// KnownHostsPath es dónde se guardan las claves aceptadas. La interfaz lo
// muestra para que se pueda ir a mirar el archivo.
func (h *Hosts) KnownHostsPath() string { return h.known.Path() }

// InspectResult es lo que necesita el diálogo de S04 para decidir.
type InspectResult struct {
	OK         bool               `json:"ok"`
	Inspection *tunnel.Inspection `json:"inspection,omitempty"`
	// Error es por qué no se pudo ni mirar: el bastión no responde, el puerto
	// está cerrado, no es un servidor SSH.
	Error string `json:"error,omitempty"`
}

// Inspect averigua qué clave presenta el bastión, sin autenticarse.
//
// Es el primer paso obligatorio antes de conectar con túnel. Devuelve un
// resultado con `ok` y no un par para que no se pueda ignorar a medias, igual
// que ConnectResult.
func (h *Hosts) Inspect(ctx context.Context, c connection.Connection) InspectResult {
	c = c.Normalize()
	if !c.SSH.Enabled {
		return InspectResult{Error: "Esta conexión no usa túnel SSH."}
	}
	insp, err := tunnel.Inspect(ctx, c.SSH, h.known)
	if err != nil {
		return InspectResult{Error: err.Error()}
	}
	return InspectResult{OK: true, Inspection: insp}
}

// Trust acepta una clave de host.
//
// Recibe la clave que se le mostró a la persona, no la vuelve a pedir al
// servidor. Así se guarda exactamente aquella cuya huella se aceptó: volver a
// preguntarle al servidor abriría una ventana en la que podría presentar otra.
func (h *Hosts) Trust(address, authorizedKey string) error {
	if address == "" || authorizedKey == "" {
		return fmt.Errorf("faltan la dirección o la clave a aceptar")
	}
	return h.known.Trust(address, authorizedKey)
}

// Forget saca un host de la lista de aceptados.
//
// Existe para poder deshacer un "confiar" sin editar el archivo a mano. Es la
// operación que hay que hacer cuando se descubre que se aceptó de más.
func (h *Hosts) Forget(address string) error {
	return h.known.Forget(address)
}
