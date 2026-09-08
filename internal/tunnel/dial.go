package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
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

	presentada, err := capturarClaveDelHost(ctx, cfg)
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
func capturarClaveDelHost(ctx context.Context, cfg Config) (ssh.PublicKey, error) {
	var presentada ssh.PublicKey
	conf := &ssh.ClientConfig{
		User: cfg.User,
		Auth: nil,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presentada = key
			return errSoloInspeccion
		},
		Timeout: DefaultConnectTimeout,
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
		Timeout:         DefaultConnectTimeout,
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
	return &Client{ssh: ssh.NewClient(c, chans, reqs), desc: cfg.Describe()}, nil
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
		datos, err := os.ReadFile(cfg.KeyPath)
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
