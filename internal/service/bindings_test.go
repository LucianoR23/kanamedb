package service

import (
	"reflect"
	"testing"
)

// TestElCableadoDelServicioNoEsUnBinding.
//
// Wails expone como binding TODO método exportado de un servicio registrado.
// Eso incluía los setters con los que main cablea las piezas —`UsarHistorial`,
// `UsarPreferencias`— y dos métodos de diagnóstico: desde el webview se podía
// apagar el historial o hacer que las conexiones nuevas nacieran sin
// protecciones (K-14 de la auditoría del 2026-09-11). Ahora el cableado va por
// funciones del paquete, que no se bindean, y el diagnóstico no se exporta.
//
// El test mira los TIPOS por reflexión y no los bindings generados, porque
// los bindings no están en el repo: se generan en CI, y para cuando `tsc` los
// lee ya es tarde para preguntar.
func TestElCableadoDelServicioNoEsUnBinding(t *testing.T) {
	prohibidos := map[reflect.Type][]string{
		reflect.TypeOf(&Queries{}):     {"UsarHistorial", "Running"},
		reflect.TypeOf(&Connections{}): {"UsarPreferencias"},
		reflect.TypeOf(&Session{}):     {"TunnelDown", "UsarReloj"},
	}
	for tipo, nombres := range prohibidos {
		for _, nombre := range nombres {
			if _, existe := tipo.MethodByName(nombre); existe {
				t.Errorf("%s.%s está exportado: Wails lo expone al webview como binding", tipo, nombre)
			}
		}
	}
}
