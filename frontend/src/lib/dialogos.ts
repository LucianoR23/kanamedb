/**
 * Lo que comparten los selectores de archivo del sistema operativo.
 *
 * Wails manda «cancelar» como rechazo de la promesa, no como un valor: adentro
 * es `cfd.ErrorCancelled`, de un paquete `internal/` que no se puede importar,
 * así que del lado del navegador solo queda el texto. Cancelar NO es un error,
 * y sin esto la acción más común de un selector terminaba en un cartel rojo en
 * inglés.
 */
export function cancelado(err: unknown): boolean {
  return textoDe(err).toLowerCase().includes("cancelled by user");
}

export function textoDe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
