package secrets

// almacen es donde vive de verdad el secreto: el keychain del sistema en
// escritorio, el almacenamiento cifrado con clave del Keystore en Android.
//
// Keyring valida y envuelve errores; el almacén solo guarda, lee y borra. Las
// claves que recibe ya pasaron por validID, así que nunca están vacías ni
// traen caracteres de control. Está separado para que la parte que se puede
// probar en cualquier máquina —la lógica— no dependa de la que no —el
// backend—: el de Android se prueba en Windows con un bridge falso.
type almacen interface {
	// guardar reemplaza lo que hubiera para esa clave.
	guardar(service, id, secreto string) error
	// leer devuelve hay=false, sin error, cuando no hay nada guardado.
	leer(service, id string) (secreto string, hay bool, err error)
	// borrar no falla si no había nada: el resultado buscado ya se cumple.
	borrar(service, id string) error
	// hay dice si existe una entrada sin traer el secreto: en Android leer
	// pide la biometría, y la lista de conexiones no tiene por qué pedirla.
	hay(service, id string) (bool, error)
}
