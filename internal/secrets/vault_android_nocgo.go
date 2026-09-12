//go:build android && !cgo

package secrets

import "errors"

// Sin cgo no hay JNI y no hay vault. Un build así solo existe para compilar y
// analizar el código en una máquina de desarrollo (go vet, staticcheck); no es
// un APK. Si alguna vez corriera, cada operación falla en vez de degradar a
// un almacenamiento en claro.
func almacenDelSistema() almacen { return almacenMovil{s: sinVault{}} }

type sinVault struct{}

var errSinVault = errors.New("este build de Android no tiene el vault de contraseñas (compilado sin cgo)")

func (sinVault) SecureSet(string, string) error         { return errSinVault }
func (sinVault) SecureGet(string) (string, bool, error) { return "", false, errSinVault }
func (sinVault) SecureDelete(string) error              { return errSinVault }
func (sinVault) SecureHas(string) (bool, error)         { return false, errSinVault }
