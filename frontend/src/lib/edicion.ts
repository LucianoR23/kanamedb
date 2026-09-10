import type { GridEdits, NewRow, RowEdit } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";

/** Un valor de celda: texto, o NULL. */
export type Valor = string | null;

/**
 * Lo que la grilla editó y todavía no preparó.
 *
 * Vive en la pantalla, no en Go, porque hasta que se prepara no es un cambio:
 * es texto en una celda que se puede deshacer con un clic. Recién al preparar
 * se convierte en cambios del changeset, y eso lo hace Go (`StageGrid`), que es
 * quien sabe en qué orden van las columnas y con qué clave se identifica la
 * fila.
 *
 * Las filas leídas se identifican por su ÍNDICE en la página cargada. Es
 * frágil a propósito: cualquier recarga que reordene la página —ordenar por
 * otra columna, refrescar— descarta las ediciones, y la pantalla lo pregunta
 * antes. Identificarlas por clave sería más robusto y también más mentiroso:
 * si la fila cambió del otro lado, lo que se está editando ya no es lo que se
 * ve.
 */
export interface Edicion {
  /** fila leída → columna → valor nuevo. */
  celdas: Map<number, Map<number, Valor>>;
  /** filas leídas marcadas para borrar. */
  borradas: Set<number>;
  /** filas agregadas: columna → valor, solo las que se cargaron. Las demás
   *  toman el valor por defecto de la tabla. */
  nuevas: Map<number, Valor>[];
}

export function sinEdicion(): Edicion {
  return { celdas: new Map(), borradas: new Set(), nuevas: [] };
}

export function hayEdiciones(e: Edicion): boolean {
  return e.celdas.size > 0 || e.borradas.size > 0 || e.nuevas.length > 0;
}

/** Cuántos cambios va a producir: una fila editada es uno, no uno por celda. */
export function cuantos(e: Edicion): number {
  return e.celdas.size + e.borradas.size + e.nuevas.length;
}

/** Copia con la celda (fila, col) puesta en `valor`. Si el valor es el que ya
 *  tenía la fila leída, la celda vuelve a estar sin editar. */
export function conCelda(
  e: Edicion,
  fila: number,
  col: number,
  valor: Valor,
  original: Valor,
): Edicion {
  const celdas = new Map(e.celdas);
  const deLaFila = new Map(celdas.get(fila) ?? []);
  if (valor === original) deLaFila.delete(col);
  else deLaFila.set(col, valor);
  if (deLaFila.size === 0) celdas.delete(fila);
  else celdas.set(fila, deLaFila);
  return { ...e, celdas };
}

/** Copia sin ninguna edición sobre la fila leída. */
export function sinFila(e: Edicion, fila: number): Edicion {
  const celdas = new Map(e.celdas);
  celdas.delete(fila);
  const borradas = new Set(e.borradas);
  borradas.delete(fila);
  return { ...e, celdas, borradas };
}

/** Copia con la fila marcada para borrar, o desmarcada. Borrar una fila
 *  descarta sus celdas editadas: no tiene sentido cambiarle el apodo a una
 *  fila que se va. */
export function conBorrada(e: Edicion, fila: number, borrar: boolean): Edicion {
  const borradas = new Set(e.borradas);
  const celdas = new Map(e.celdas);
  if (borrar) {
    borradas.add(fila);
    celdas.delete(fila);
  } else {
    borradas.delete(fila);
  }
  return { ...e, borradas, celdas };
}

export function conFilaNueva(e: Edicion): Edicion {
  return { ...e, nuevas: [...e.nuevas, new Map()] };
}

export function conCeldaNueva(e: Edicion, indice: number, col: number, valor: Valor): Edicion {
  const nuevas = e.nuevas.map((n, i) => {
    if (i !== indice) return n;
    const copia = new Map(n);
    copia.set(col, valor);
    return copia;
  });
  return { ...e, nuevas };
}

/** Copia con una celda de fila nueva vuelta a «por defecto». */
export function sinCeldaNueva(e: Edicion, indice: number, col: number): Edicion {
  const nuevas = e.nuevas.map((n, i) => {
    if (i !== indice) return n;
    const copia = new Map(n);
    copia.delete(col);
    return copia;
  });
  return { ...e, nuevas };
}

export function sinFilaNueva(e: Edicion, indice: number): Edicion {
  return { ...e, nuevas: e.nuevas.filter((_, i) => i !== indice) };
}

/**
 * Lo que se le manda a Go. Es un volcado, no una interpretación: las filas
 * leídas van enteras y lo editado va por nombre de columna. Qué identifica la
 * fila y en qué orden va cada valor lo decide Go.
 */
export function aGridEdits(
  e: Edicion,
  filas: readonly (readonly Valor[])[],
  columnas: readonly string[],
  clave: readonly string[],
  schema: string,
  table: string,
): GridEdits {
  const updates: RowEdit[] = [];
  for (const [fila, celdas] of e.celdas) {
    const before = filas[fila];
    if (!before) continue;
    const after: Record<string, Valor> = {};
    for (const [col, valor] of celdas) {
      const nombre = columnas[col];
      if (nombre !== undefined) after[nombre] = valor;
    }
    updates.push({ before: [...before], after });
  }
  const deletes: Valor[][] = [];
  for (const fila of e.borradas) {
    const before = filas[fila];
    if (before) deletes.push([...before]);
  }
  const inserts: NewRow[] = e.nuevas.map((n) => {
    const values: Record<string, Valor> = {};
    for (const [col, valor] of n) {
      const nombre = columnas[col];
      if (nombre !== undefined) values[nombre] = valor;
    }
    return { values };
  });
  return {
    schema,
    table,
    columns: [...columnas],
    key: [...clave],
    updates,
    deletes,
    inserts,
  };
}
