//go:build android

package service

// soloDatosEnEstaPlataforma: en el teléfono el núcleo no acepta cambios de
// esquema, no solo la interfaz no los ofrece. Ver kaname-android.md, «Regla
// de partida».
const soloDatosEnEstaPlataforma = true
