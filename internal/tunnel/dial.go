package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// DefaultConnectTimeout acota el handshake. Un bastión que no contesta tiene
// que fallar rápido: la alternativa es una interfaz colgada sin explicación.
const DefaultConnectTimeout = 10 * time.Second

// Secrets son los secretos de esta conexión, traídos del keychain en el momento
// de usarlos.
//
// Van en su propio tipo y no en Config para que no exista la posibilidad de
// guardarlos por accidente: Config se serializa a TOML, esto no se serializa a
// ningún lado.
type Secrets struct {
	// Password para AuthPassword.
	Password string
	// Passphrase descifra la clave privada de AuthKeyFile. Vacío si no tiene.
	Passphrase string
}

// errSoloInspeccion aborta el handshake justo después de recibir la clave del
// host. No es un fallo: es cómo se para el protocolo en el punto exacto en que
// ya sabemos con quién estamos hablando y todavía no dijimos quiénes somos.
var errSoloInspeccion = errors.New("inspección terminada")

// Inspect averigua qué clave presenta el servidor y qué opinamos de ella, sin
// autenticarse.
//
// Esto es lo que permite que el diálogo de S04 diga "no credentials sent yet" y
// que sea cierto. El protocolo SSH intercambia y verifica la clave del host
// ANTES de la autenticación, así que abortar en la devolución de llamada de la
// clave garantiza que ni la contraseña ni la firma de la clave privada llegaron
// a viajar.
//
// Hacerlo en un solo paso —conectar y preguntar después— mandaría las
// credenciales a un servidor sin verificar. Si ese servidor es un impostor, ya
// las tiene para cuando aparece el diálogo.
func Inspect(ctx context.Context, cfg Config, kh *KnownHosts) (*Inspection, error) {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	presentada, err := capturarClaveDelHost(ctx, cfg, algoritmosPreferidos(kh, cfg.Address()))
	if err != nil {
		return nil, fmt.Errorf("inspeccionar %s: %w", cfg.Describe(), err)
	}

	insp := &Inspection{
		Address:       cfg.Address(),
		Presented:     describir(presentada),
		AuthorizedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(presentada))),
	}

	conocida, err := kh.Lookup(cfg.Address())
	if err != nil {
		return nil, err
	}
	switch {
	case conocida == nil:
		insp.Verdict = VerdictUnknown
	case conocida.Fingerprint == insp.Presented.Fingerprint:
		insp.Verdict = VerdictTrusted
	default:
		insp.Verdict = VerdictChanged
		insp.Known = conocida
	}
	return insp, nil
}

// algoritmosPreferidos ordena la negociación para que el servidor presente el
// MISMO tipo de clave que ya tenemos guardado.
//
// known_hosts guarda una clave por dirección, y un servidor suele tener
// varias —ed25519, ecdsa, rsa—. Sin esto, la negociación elige el tipo por la
// lista por defecto del cliente y lo que ofrece el servidor: si el
// administrador agrega un tipo o cambia el orden, el servidor presenta OTRA
// clave legítima y el veredicto es «la clave cambió». Cada falso positivo
// entrena a apretar «Reemplazar la clave y conectar», que es justo el botón
// que un intermediario necesita (K-13 de la auditoría del 2026-09-11). Es lo
// que hace OpenSSH, y lo que hace `knownhosts.HostKeyAlgorithms` en otras
// librerías.
//
// Con una clave RSA se piden también las firmas rsa-sha2, que son las que un
// servidor moderno acepta para ese tipo. Sin clave conocida se deja la lista
// por defecto: no hay preferencia que respetar.
func algoritmosPreferidos(kh *KnownHosts, address string) []string {
	if kh == nil {
		return nil
	}
	conocida, err := kh.Lookup(address)
	if err != nil || conocida == nil {
		return nil
	}
	var primero []string
	switch conocida.Algorithm {
	case ssh.KeyAlgoRSA:
		primero = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	case "":
		return nil
	default:
		primero = []string{conocida.Algorithm}
	}
	// Los conocidos PRIMERO y los demás después, como OpenSSH. Solo los
	// conocidos dejaba afuera a un servidor que cambió de tipo de clave —RSA
	// a ed25519—: el handshake fallaba con «no common algorithm» y el diálogo
	// de «la clave cambió» ni aparecía, con la persona sin forma de entrar
	// salvo editar known_hosts a mano (review del 2026-09-12).
	vistos := make(map[string]bool, len(primero))
	for _, a := range primero {
		vistos[a] = true
	}
	for _, a := range ssh.SupportedAlgorithms().HostKeys {
		if !vistos[a] {
			primero = append(primero, a)
		}
	}
	return primero
}

// capturarClaveDelHost abre el handshake, se queda con la clave que presenta el
// servidor y lo aborta ahí mismo.
//
// El intercambio de claves y la verificación del host pasan ANTES de la
// autenticación, así que cortar en la devolución de llamada garantiza que no
// viajó ninguna credencial: ni contraseña, ni firma de la clave privada. De eso
// depende que el diálogo de S04 pueda decir "no credentials sent yet" y sea
// verdad.
//
// La configuración va sin métodos de autenticación además, para que no haya
// nada que mandar aunque el protocolo siguiera.
func capturarClaveDelHost(ctx context.Context, cfg Config, algoritmos []string) (ssh.PublicKey, error) {
	var presentada ssh.PublicKey
	conf := &ssh.ClientConfig{
		User: cfg.User,
		Auth: nil,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presentada = key
			return errSoloInspeccion
		},
		HostKeyAlgorithms: algoritmos,
		Timeout:           DefaultConnectTimeout,
	}

	conn, err := discar(ctx, cfg.Address())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_, _, _, err = ssh.NewClientConn(conn, cfg.Address(), conf)
	if presentada == nil {
		// No se llegó ni a la clave del host: el error es de red o de versión
		// de protocolo, y hay que devolverlo tal cual.
		if err == nil {
			err = errors.New("el servidor no presentó ninguna clave de host")
		}
		return nil, err
	}
	return presentada, nil
}

// Client es una conexión SSH abierta, lista para discar a través de ella.
type Client struct {
	ssh  *ssh.Client
	desc string
	// caido se enciende cuando el servidor cierra la conexión.
	//
	// Sin esto, un túnel que se murió se manifiesta como un error de red de la
	// base —"connection refused"— y la interfaz solo puede decir "el servidor
	// puede estar caído, o el túnel puede haberse cerrado". Con esto puede
	// decir cuál de las dos, que es la diferencia entre una pista y una
	// respuesta.
	caido atomic.Bool
}

// Closed dice si el túnel se cerró por su cuenta.
func (c *Client) Closed() bool {
	if c == nil {
		return true
	}
	return c.caido.Load()
}

// vigilar marca el cliente como caído cuando el servidor cierra.
//
// ssh.Client.Wait bloquea hasta que la conexión termina, así que va en su
// propia goroutine. Termina sola: cerrar el cliente hace que Wait vuelva.
func (c *Client) vigilar() {
	go func() {
		_ = c.ssh.Wait()
		c.caido.Store(true)
	}()
}

// Describe es usuario@host:puerto. Sin secretos.
func (c *Client) Describe() string { return c.desc }

// DialContext disca a través del túnel. Es la firma que espera pgx en DialFunc.
//
// Acá no hay ningún puerto local escuchando: el canal lo abre el servidor SSH y
// el net.Conn que se devuelve existe solo dentro del proceso.
func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// El cliente de x/crypto no toma contexto, así que se disca en una
	// goroutine y se respeta la cancelación cerrando lo que llegue tarde. Sin
	// esto, cancelar una conexión dejaría la goroutine esperando el timeout de
	// TCP con el usuario mirando una pantalla que no responde.
	type resultado struct {
		conn net.Conn
		err  error
	}
	hecho := make(chan resultado, 1)
	go func() {
		conn, err := c.ssh.Dial(network, addr)
		hecho <- resultado{conn, err}
	}()

	select {
	case <-ctx.Done():
		go func() {
			if r := <-hecho; r.conn != nil {
				r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-hecho:
		if r.err != nil {
			return nil, fmt.Errorf("abrir el canal hacia %s por %s: %w", addr, c.desc, r.err)
		}
		return r.conn, nil
	}
}

// Close cierra la conexión SSH.
func (c *Client) Close() error {
	if c == nil || c.ssh == nil {
		return nil
	}
	return c.ssh.Close()
}

// DialOptions ajusta cómo se conecta.
type DialOptions struct {
	// AcceptOnce es la huella que se aceptó para esta conexión sin guardarla.
	// Es el botón "Conectar una vez" de S04: sirve una vez y no deja rastro.
	AcceptOnce string
}

// Dial abre la conexión SSH verificando la clave del host.
//
// Nunca hay una ruta que ignore la verificación. No existe una opción para
// desactivarla, ni un valor de configuración que la saltee: si la clave no está
// aceptada, esto falla y quien llama tiene que pasar por Inspect y por una
// decisión de la persona.
func Dial(ctx context.Context, cfg Config, kh *KnownHosts, sec Secrets, opts DialOptions) (*Client, error) {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metodos, err := metodosDeAuth(cfg, sec)
	if err != nil {
		return nil, err
	}

	conf := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            metodos,
		HostKeyCallback: verificador(cfg, kh, opts.AcceptOnce),
		// Se pide primero el tipo de clave que ya se conoce: ver
		// algoritmosPreferidos.
		HostKeyAlgorithms: algoritmosPreferidos(kh, cfg.Address()),
		Timeout:           DefaultConnectTimeout,
		// El nombre queda en los logs del servidor. No lleva ningún dato del
		// usuario ni de la conexión.
		ClientVersion: "SSH-2.0-Kaname",
	}

	conn, err := discar(ctx, cfg.Address())
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, cfg.Address(), conf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("conectar a %s: %w", cfg.Describe(), err)
	}
	cli := &Client{ssh: ssh.NewClient(c, chans, reqs), desc: cfg.Describe()}
	cli.vigilar()
	return cli, nil
}

// verificador compara contra known_hosts y contra la aceptación de una sola vez.
func verificador(cfg Config, kh *KnownHosts, aceptadaUnaVez string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		huella := ssh.FingerprintSHA256(key)
		if aceptadaUnaVez != "" && huella == aceptadaUnaVez {
			return nil
		}
		conocida, err := kh.Lookup(cfg.Address())
		if err != nil {
			return err
		}
		if conocida == nil {
			return fmt.Errorf("la clave de %s no está aceptada (%s)", cfg.Address(), huella)
		}
		if conocida.Fingerprint != huella {
			return fmt.Errorf(
				"la clave de %s cambió: se esperaba %s y presentó %s",
				cfg.Address(), conocida.Fingerprint, huella)
		}
		return nil
	}
}

// metodosDeAuth arma los métodos de autenticación según la configuración.
func metodosDeAuth(cfg Config, sec Secrets) ([]ssh.AuthMethod, error) {
	switch cfg.Auth {
	case AuthPassword:
		return []ssh.AuthMethod{ssh.Password(sec.Password)}, nil

	case AuthKeyFile:
		ruta, err := ExpandHome(cfg.KeyPath)
		if err != nil {
			return nil, err
		}
		datos, err := os.ReadFile(ruta)
		if err != nil {
			// El error de os lleva la ruta, que no es secreta. El contenido de
			// la clave no aparece por ningún lado.
			return nil, fmt.Errorf("leer la clave privada: %w", err)
		}
		var signer ssh.Signer
		if sec.Passphrase == "" {
			signer, err = ssh.ParsePrivateKey(datos)
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(datos, []byte(sec.Passphrase))
		}
		if err != nil {
			// Se envuelve con un mensaje propio: el error de x/crypto puede
			// citar partes del archivo, y el archivo es la clave privada.
			var falta *ssh.PassphraseMissingError
			if errors.As(err, &falta) {
				return nil, fmt.Errorf("la clave privada %s está cifrada y necesita su frase de paso", cfg.KeyPath)
			}
			return nil, fmt.Errorf("no se pudo interpretar la clave privada %s", cfg.KeyPath)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil

	case AuthAgent:
		signers, err := agentSigners()
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, nil
	}
	return nil, fmt.Errorf("método de autenticación SSH desconocido: %q", cfg.Auth)
}

// discar abre el TCP respetando la cancelación del contexto.
func discar(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: DefaultConnectTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("conectar a %s: %w", addr, err)
	}
	return conn, nil
}

// ExpandHome resuelve un `~` inicial al directorio del usuario.
//
// Exportada por lo mismo que CleanPath: las rutas de los certificados TLS se
// guardan con el `~` sin resolver, para que la libreta sincronice, y se
// resuelven al armar la conexión.
//
// Se hace al USAR la ruta y no al guardarla, y esa es toda la gracia: el
// archivo de conexiones se sincroniza entre máquinas, y `~` significa algo
// distinto en cada una. Guardar la ruta ya expandida ataría la conexión a la
// máquina donde se configuró — que es exactamente lo que el plan quiere evitar
// cuando dice que las claves se referencian por path y viven en el ~/.ssh de
// cada máquina.
//
// Una ruta absoluta de Windows pasa intacta: no empieza con `~`.
//
// La forma `~\algo` se acepta en TODAS las plataformas, y por la misma razón de
// arriba: quien configuró la conexión en Windows escribió `~\.ssh\id_ed25519`, y
// ese mismo archivo de conexiones se abre en Linux. Ahí las barras invertidas no
// separan nada —son caracteres válidos de un nombre de archivo—, así que
// traducir solo el prefijo dejaba una ruta que apunta a un archivo llamado
// `.ssh\id_ed25519`, que no existe. Aceptar el prefijo sin traducir el resto es
// media función.
//
// La traducción se hace SOLO cuando la ruta empieza con `~\`, que es sintaxis de
// Windows inequívoca. Una ruta `~/carpeta\rara` conserva su barra invertida: en
// Linux es un nombre de archivo legítimo y cambiarlo sería romperlo.
func ExpandHome(ruta string) (string, error) {
	const tildeDeWindows = `~\`
	estiloWindows := strings.HasPrefix(ruta, tildeDeWindows)

	if ruta != "~" && !strings.HasPrefix(ruta, "~/") && !estiloWindows {
		return ruta, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no se pudo resolver %q: no se encontró el directorio del usuario: %w", ruta, err)
	}
	if ruta == "~" {
		return home, nil
	}

	resto := ruta[2:]
	if estiloWindows {
		// En Windows el separador YA es la barra invertida, así que esto no
		// cambia nada; en Linux y macOS es lo que hace que la ruta exista.
		resto = strings.ReplaceAll(resto, `\`, string(filepath.Separator))
	}
	return filepath.Join(home, resto), nil
}
