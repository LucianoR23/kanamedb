import type { ChangeView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { ErdColumna } from "./erd";
import { idDeTabla } from "./erd";

/** En qué estado dejó una edición a una columna o a una tabla. */
export type Estado = "agregada" | "cambiada" | "borrando";

/**
 * Lo que el diagrama tiene que pintar además del esquema real.
 *
 * El changeset vive en Go y es la única verdad de qué está pendiente. Esto no lo
 * duplica: lo traduce a lo que el lienzo necesita para marcar cada cosa, y se
 * recalcula cada vez que el changeset cambia.
 */
export interface Pendientes {
  /** "esquema.tabla" → estado, para las tablas que ya existen. */
  tablas: Map<string, Estado>;
  /** "esquema.tabla|columna" → estado. */
  columnas: Map<string, Estado>;
  /** Columnas agregadas que todavía no están en el catálogo, por tabla. */
  nuevasColumnas: Map<string, ErdColumna[]>;
  /** Tablas creadas que todavía no existen. */
  nuevasTablas: TablaNueva[];
  /** Claves foráneas nuevas, para dibujarlas antes de que existan. */
  nuevasClaves: ClaveNueva[];
  /** Nombres de restricciones que se van a borrar. */
  clavesBorradas: Set<string>;
  /** Cuántos cambios hay en total. */
  total: number;
}

export interface TablaNueva {
  id: string;
  schema: string;
  name: string;
  columnas: ErdColumna[];
}

export interface ClaveNueva {
  changeId: string;
  origen: string;
  destino: string;
  columna: string;
  refColumna: string;
  nombre: string;
}

export const vacio = (): Pendientes => ({
  tablas: new Map(),
  columnas: new Map(),
  nuevasColumnas: new Map(),
  nuevasTablas: [],
  nuevasClaves: [],
  clavesBorradas: new Set(),
  total: 0,
});

/** La clave de una columna dentro del mapa. */
export const claveColumna = (tabla: string, columna: string) => `${tabla}|${columna}`;

/**
 * Traduce el changeset a marcas para el lienzo.
 *
 * Solo mira los cambios INCLUIDOS: uno destildado sigue en la lista pero no va a
 * correr, y pintarlo en el diagrama diría que va a pasar algo que no va a pasar.
 */
export function pendientesDe(cambios: ChangeView[], esquema: string): Pendientes {
  const p = vacio();

  for (const v of cambios) {
    const c = v.change;
    if (c.excluded) continue;
    if (c.schema !== esquema) continue;
    p.total++;

    const tabla = idDeTabla(c.schema, c.table);

    switch (c.type) {
      case "createTable":
        p.nuevasTablas.push({
          id: tabla,
          schema: c.schema,
          name: c.table,
          columnas: (c.columns ?? []).map((col) => ({
            name: col.name,
            dataType: col.dataType,
            key: (c.names ?? []).includes(col.name) ? "PK" : "",
          })),
        });
        break;

      case "dropTable":
        p.tablas.set(tabla, "borrando");
        break;

      case "addColumn": {
        marcarTabla(p, tabla, "cambiada");
        const col = c.column;
        if (col) {
          p.columnas.set(claveColumna(tabla, col.name), "agregada");
          p.nuevasColumnas.set(tabla, [
            ...(p.nuevasColumnas.get(tabla) ?? []),
            { name: col.name, dataType: col.dataType, key: "" },
          ]);
        }
        break;
      }

      case "dropColumn":
        marcarTabla(p, tabla, "cambiada");
        if (c.column) p.columnas.set(claveColumna(tabla, c.column.name), "borrando");
        break;

      case "addForeignKey":
        marcarTabla(p, tabla, "cambiada");
        p.nuevasClaves.push({
          changeId: c.id,
          origen: tabla,
          destino: idDeTabla(c.refSchema || c.schema, c.refTable ?? ""),
          columna: (c.names ?? [])[0] ?? "",
          refColumna: (c.refNames ?? [])[0] ?? "",
          nombre: c.name ?? "",
        });
        break;

      case "dropConstraint":
      case "dropIndex":
        marcarTabla(p, tabla, "cambiada");
        if (c.name) p.clavesBorradas.add(c.name);
        break;

      default:
        // Todo lo demás toca una columna existente sin agregarla ni sacarla.
        marcarTabla(p, tabla, "cambiada");
        if (c.column) p.columnas.set(claveColumna(tabla, c.column.name), "cambiada");
        break;
    }
  }

  return p;
}

/** Marca la tabla, sin pisar un «borrando» con un «cambiada». */
function marcarTabla(p: Pendientes, tabla: string, estado: Estado) {
  if (p.tablas.get(tabla) === "borrando") return;
  p.tablas.set(tabla, estado);
}
