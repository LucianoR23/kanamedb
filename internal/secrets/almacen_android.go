//go:build android

package secrets

import "github.com/wailsapp/wails/v3/pkg/application"

// almacenDelSistema en Android es el almacenamiento cifrado de Wails:
// EncryptedSharedPreferences con una clave AES del Android Keystore. Lo que
// queda en disco es texto cifrado; la clave no sale del Keystore.
//
// Todavía no exige biometría para descifrar: eso es el paso 2b de
// kaname-android.md, y necesita Java propio en build/android.
func almacenDelSistema() almacen { return almacenMovil{s: application.Mobile} }
