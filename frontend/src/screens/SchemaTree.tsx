import { useState } from "react";
import type { Schema, Snapshot, Table } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { TreeRow } from "../components/ui";
import styles from "./SchemaTree.module.css";

/**
 * Árbol de esquema de S05. Iteración 1: solo tablas.
 *
 * Vistas, funciones, triggers, enums y secuencias llegan en la Iteración 8; el
 * modelo ya los contempla, así que sumarlos es agregar nodos, no rehacer esto.
 */
export function SchemaTree({
  snapshot,
  query,
  selected,
  onSelect,
  onContextMenu,
}: {
  snapshot: Snapshot;
  query: string;
  selected: string | null;
  onSelect: (schema: string, table: string) => void;
  onContextMenu?: (schema: string, table: string, e: React.MouseEvent) => void;
}) {
  // Todos los esquemas arrancan abiertos: con uno o dos, colapsarlos esconde
  // todo por nada. Quien tenga veinte los cierra.
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

  const q = query.trim().toLowerCase();
  const grupos = (snapshot.schemas ?? [])
    .map((sc) => ({ schema: sc, tables: matching(sc, q) }))
    // Con búsqueda activa, un esquema sin coincidencias no aporta nada.
    .filter((g) => q === "" || g.tables.length > 0);

  if (grupos.length === 0) {
    return (
      <p className={styles.empty}>
        {q === ""
          ? "Esta base no tiene tablas visibles para este usuario."
          : `Ninguna tabla coincide con “${query}”.`}
      </p>
    );
  }

  return (
    <div role="tree" aria-label="Esquema">
      {grupos.map(({ schema: sc, tables }) => {
        // Buscar expande: esconder una coincidencia detrás de un nodo cerrado
        // es lo contrario de lo que se pidió.
        const abierto = q !== "" || !collapsed.has(sc.name);
        return (
          <div key={sc.name} className={styles.group}>
            <TreeRow
              label={sc.name}
              kind="schema"
              depth={0}
              expanded={abierto}
              meta={metaEsquema(sc, tables.length, q !== "")}
              onClick={() =>
                setCollapsed((prev) => {
                  const next = new Set(prev);
                  if (next.has(sc.name)) next.delete(sc.name);
                  else next.add(sc.name);
                  return next;
                })
              }
            />
            {abierto
              ? tables.map((t) => {
                  const id = `${sc.name}.${t.name}`;
                  return (
                    <TreeRow
                      key={id}
                      label={t.name}
                      kind="table"
                      depth={1}
                      selected={id === selected}
                      meta={metaTabla(t)}
                      onClick={() => onSelect(sc.name, t.name)}
                      {...(onContextMenu
                        ? { onContextMenu: (e: React.MouseEvent) => onContextMenu(sc.name, t.name, e) }
                        : {})}
                    />
                  );
                })
              : null}
          </div>
        );
      })}
    </div>
  );
}

function matching(sc: Schema, q: string): Table[] {
  const tables = sc.tables ?? [];
  if (q === "") return tables;
  return tables.filter((t) => t.name.toLowerCase().includes(q));
}

function metaEsquema(sc: Schema, visibles: number, filtrando: boolean): string {
  const total = (sc.tables ?? []).length;
  if (filtrando && visibles !== total) return `${visibles} de ${total}`;
  return total === 1 ? "1 tabla" : `${total} tablas`;
}

/**
 * El conteo de filas es una ESTIMACIÓN del planificador, no un `count(*)`. Se
 * muestra con ~ para no mentir: un `count(*)` exacto por tabla recorrería cada
 * una entera cada vez que se abre el árbol.
 *
 * -1 significa que la tabla nunca fue analizada. No es lo mismo que cero filas,
 * y mostrarlo como cero sería mentir en la dirección peligrosa.
 */
function metaTabla(t: Table): string {
  if (!t.readable) return "sin permiso";
  if (t.rowEstimate < 0) return "sin analizar";
  if (t.rowEstimate === 0) return "vacía";
  return "~" + t.rowEstimate.toLocaleString("es");
}
