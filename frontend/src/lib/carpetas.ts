import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";

/** El valor de `folder` de una conexión que no tiene carpeta. */
export const SIN_CARPETA = "";

/**
 * Las carpetas que existen: las que alguna conexión tiene, en orden alfabético
 * sin distinguir mayúsculas.
 *
 * Una carpeta no es una entidad sino un nombre en la conexión, así que la
 * única lista de carpetas es esta, y la comparten el gestor —para el submenú
 * «Mover a carpeta»— y el editor —para el desplegable «Carpeta»—.
 */
export function carpetasDe(vistas: readonly ConnectionView[]): string[] {
  const nombres = new Set<string>();
  for (const v of vistas) {
    if (v.connection.folder !== SIN_CARPETA) nombres.add(v.connection.folder);
  }
  return [...nombres].sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }));
}
