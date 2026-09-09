import type { ChangeView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";

/** Una columna que se puede elegir, exista ya o esté preparada. */
export interface ColumnaElegible {
  name: string;
  /** Todavía no existe en la base: está en el changeset. */
  pendiente?: boolean;
}

/**
 * Lo que el changeset agrega o saca de UNA tabla, para la pantalla de
 * estructura.
 *
 * Existe por un problema concreto: se prepara una columna, se va a la pestaña de
 * claves foráneas para colgarla de otra tabla, y la columna no está en la lista.
 * El catálogo todavía no la tiene —y no la va a tener hasta que se aplique—, así
 * que la pantalla mostraba la verdad de la base y dejaba a alguien mirando una
 * lista donde falta lo que acaba de crear. Peor todavía si el orden es el
 * natural: primero la columna, después la clave que la usa. Kaname ya sabe
 * ordenar las sentencias para que eso funcione; lo que faltaba era poder
 * pedirlo.
 *
 * `erdStaged.ts` hace la misma traducción para el lienzo, que necesita otra
 * cosa: todas las tablas del esquema y con qué marca pintarlas. Acá alcanza con
 * una tabla y sin colores.
 */
export interface PendientesDeTabla {
  /** Columnas preparadas que todavía no existen. */
  columnasNuevas: string[];
  /** Columnas existentes que este changeset va a borrar. */
  columnasBorradas: Set<string>;
  /** Tablas del esquema preparadas que todavía no existen. */
  tablasNuevas: string[];
}

export const sinPendientes = (): PendientesDeTabla => ({
  columnasNuevas: [],
  columnasBorradas: new Set(),
  tablasNuevas: [],
});

/**
 * Lee el changeset para una tabla.
 *
 * Solo mira los cambios INCLUIDOS: uno destildado sigue en la lista pero no va a
 * correr, y ofrecer una columna que no se va a crear llevaría a preparar una
 * clave que no puede funcionar.
 */
export function pendientesDeTabla(
  cambios: ChangeView[],
  schema: string,
  table: string,
): PendientesDeTabla {
  const p = sinPendientes();

  for (const v of cambios) {
    const c = v.change;
    if (c.excluded) continue;
    if (c.schema !== schema) continue;

    if (c.type === "createTable") {
      p.tablasNuevas.push(c.table);
      continue;
    }
    if (c.table !== table) continue;
    if (c.type === "addColumn" && c.column) {
      p.columnasNuevas.push(c.column.name);
    } else if (c.type === "dropColumn" && c.column) {
      p.columnasBorradas.add(c.column.name);
    }
  }

  return p;
}

/**
 * Las columnas que se pueden elegir: las que están, menos las que se van, más
 * las que vienen.
 *
 * Las que se van se sacan y no se muestran tachadas: elegir una columna que el
 * mismo apply va a borrar no da ninguna sentencia que tenga sentido.
 */
export function columnasElegibles(
  existentes: readonly { name: string }[],
  p: PendientesDeTabla,
): ColumnaElegible[] {
  const out: ColumnaElegible[] = [];
  for (const c of existentes) {
    if (!p.columnasBorradas.has(c.name)) out.push({ name: c.name });
  }
  for (const n of p.columnasNuevas) {
    if (!out.some((c) => c.name === n)) out.push({ name: n, pendiente: true });
  }
  return out;
}

/** Las tablas a las que se puede apuntar: las del esquema más las preparadas. */
export function tablasElegibles(
  existentes: readonly string[],
  p: PendientesDeTabla,
): ColumnaElegible[] {
  const out: ColumnaElegible[] = existentes.map((name) => ({ name }));
  for (const n of p.tablasNuevas) {
    if (!out.some((t) => t.name === n)) out.push({ name: n, pendiente: true });
  }
  return out;
}
