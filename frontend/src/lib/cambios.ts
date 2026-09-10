import type { Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";

/**
 * Los tres tipos de cambio de DATOS. Todo lo demás del changeset es esquema.
 *
 * Vive acá y no en cada pantalla porque ya estaba escrito tres veces, y el
 * cuarto lugar que se olvidara de uno lo iba a tratar como esquema sin que se
 * note. Pasó justamente así en el diagrama: `erdStaged` decidía por descarte
 * —lo que no reconozco toca una columna— y editar una celda pintaba la tabla
 * como alterada en el ERD.
 */
export const TIPOS_DE_DATOS = ["insertRow", "updateRow", "deleteRow"] as const;

const CONJUNTO = new Set<string>(TIPOS_DE_DATOS);

/** Si el tipo es uno de los tres de datos. */
export function tipoEsDeDatos(tipo: string): boolean {
  return CONJUNTO.has(tipo);
}

/** Si el cambio toca filas y no estructura. */
export function esCambioDeDatos(c: Change): boolean {
  return tipoEsDeDatos(c.type);
}

/** La etiqueta de la operación, o null si el cambio es de esquema. */
export function etiquetaDeDatos(tipo: string): "INSERT" | "UPDATE" | "DELETE" | null {
  switch (tipo) {
    case "insertRow":
      return "INSERT";
    case "updateRow":
      return "UPDATE";
    case "deleteRow":
      return "DELETE";
    default:
      return null;
  }
}
