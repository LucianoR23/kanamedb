//go:build windows

package tunnel

import (
	"fmt"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// pipeAgente es donde escucha el agente de OpenSSH en Windows.
//
// No es un socket de dominio Unix como en el resto de los sistemas: es un named
// pipe, y por eso hace falta go-winio. Sin él habría que pedirle al usuario que
// exporte la clave a un archivo, que es exactamente lo que el agente evita.
const pipeAgente = `\.\pipe\openssh-ssh-agent`

// agentSigners pide las claves al agente del sistema.
//
// La clave privada nunca sale del agente: se le manda lo que hay que firmar y
// devuelve la firma. La aplicación no la ve, no la guarda y no puede filtrarla
// ni en un log ni en un volcado de memoria.
func agentSigners() ([]ssh.Signer, error) {
	conn, err := winio.DialPipe(pipeAgente, plazoAgente())
	if err != nil {
		return nil, fmt.Errorf(
			"no se pudo hablar con el agente SSH de Windows en %s. "+
				"¿Está corriendo el servicio ssh-agent?: %w", pipeAgente, err)
	}
	// La conexión vive lo que viva el proceso de conexión: el agente firma cada
	// intento de autenticación, así que cerrarla acá dejaría al cliente sin con
	// qué firmar.
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("el agente SSH no devolvió ninguna clave: %w", err)
	}
	if len(signers) == 0 {
		conn.Close()
		return nil, fmt.Errorf(
			"el agente SSH está corriendo pero no tiene ninguna clave cargada. " +
				"Agregá una con `ssh-add`")
	}
	return signers, nil
}

func plazoAgente() *time.Duration {
	d := 5 * time.Second
	return &d
}
