package service

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SaveCertificate guarda el certificado que presentó el servidor como archivo
// PEM, para cargarlo después como raíz de confianza.
//
// Es el camino para un servidor propio, que casi siempre tiene un certificado
// autofirmado: se prueba con `require`, se compara la huella con la que pasó
// quien administra el servidor, y se guarda. Con eso, `verify-ca` pasa a
// verificar contra ese certificado y un intermediario ya no puede meterse.
//
// Solo escribe un certificado, y solo uno que se pudo interpretar: el texto
// se decodifica y se vuelve a codificar, así que este binding no sirve para
// escribir cualquier cosa en cualquier ruta. La ruta la elige la persona en
// el diálogo del sistema.
func (s *Connections) SaveCertificate(pemText, path string) error {
	bloque, _ := pem.Decode([]byte(pemText))
	if bloque == nil || bloque.Type != "CERTIFICATE" {
		return errors.New("el texto no es un certificado en formato PEM")
	}
	if _, err := x509.ParseCertificate(bloque.Bytes); err != nil {
		return fmt.Errorf("el certificado no se pudo interpretar: %w", err)
	}
	path = filepath.Clean(path)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bloque.Bytes}), 0o600); err != nil {
		return fmt.Errorf("no se pudo escribir %s: %w", path, err)
	}
	return nil
}
