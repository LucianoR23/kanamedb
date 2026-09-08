//go:build !windows

package tunnel

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentSigners pide las claves al agente del sistema.
//
// La clave privada nunca sale del agente: se le manda lo que hay que firmar y
// devuelve la firma. La aplicación no la ve, no la guarda y no puede filtrarla
// ni en un log ni en un volcado de memoria.
func agentSigners() ([]ssh.Signer, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, fmt.Errorf(
			"no hay agente SSH: SSH_AUTH_SOCK está vacío. " +
				"Arrancá uno con `eval $(ssh-agent)` y cargá la clave con `ssh-add`")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("no se pudo hablar con el agente SSH: %w", err)
	}
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
