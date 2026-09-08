import { useCallback, useEffect, useRef, useState } from "react";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Failure } from "../../bindings/github.com/LucianoR23/kanamedb/internal/postgres";
import type { Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Button, PillTabs } from "../components/ui";
import { DataGrid } from "../components/DataGrid";
import type { CellRef, SortState } from "../components/DataGrid";
import { CellViewer } from "./CellViewer";
import styles from "./TableDataScreen.module.css";

/** Cuántas filas trae cada página. */
const PAGINA = 500;

/**
 * S10 Table data.
 *
 * Iteración 2: leer, ordenar, cargar más y contar. Agregar, borrar y editar
 * filas llegan con la Iteración 7; los botones están y se ven deshabilitados,
 * porque la pestaña se diseñó con ellos.
 */
export function TableDataScreen({
  tabId,
  schema,
  table,
  readOnly,
  snapshot,
}: {
  tabId: string;
  schema: string;
  table: string;
  readOnly: boolean;
  snapshot: Snapshot | null;
}) {
  const [result, setResult] = useState<Result | null>(null);
  const [filas, setFilas] = useState<(string | null)[][]>([]);
  const [total, setTotal] = useState<number | null>(null);
  const [orden, setOrden] = useState<SortState | null>(null);
  const [orderedBy, setOrderedBy] = useState<string[]>([]);
  const [cargando, setCargando] = useState(false);
  const [hayMas, setHayMas] = useState(false);
  const [fallo, setFallo] = useState<Failure | null>(null);
  const [seleccion, setSeleccion] = useState<CellRef | null>(null);
  const [visor, setVisor] = useState<CellRef | null>(null);
  const [sub, setSub] = useState("data");

  const runID = useRef(`${tabId}:data`).current;

  const cargar = useCallback(
    async (offset: number, sort: SortState | null) => {
      setCargando(true);
      setFallo(null);
      const res = await QueriesSvc.TableData({
        runId: runID,
        schema,
        table,
        orderBy: sort ? [sort.column] : [],
        descending: sort?.descending ?? false,
        limit: PAGINA,
        offset,
      });
      setCargando(false);

      if (!res.ok || !res.result) {
        setFallo(res.failure ?? null);
        return;
      }
      const nuevas = (res.result.rows ?? []).map((f) => f ?? []);
      setResult(res.result);
      setOrderedBy(res.orderedBy ?? []);
      // Se pide una página completa: si vino menos, no hay más del otro lado.
      setHayMas(nuevas.length === PAGINA);
      setFilas((previas) => (offset === 0 ? nuevas : [...previas, ...nuevas]));
    },
    [runID, schema, table],
  );

  useEffect(() => {
    void cargar(0, null);
    // El conteo exacto va aparte y sin esperarlo: en una tabla grande recorre
    // todo, y la grilla tiene que poder mostrar las primeras filas ya.
    void QueriesSvc.TableCount(`${runID}:count`, schema, table).then((r) => {
      if (r.ok) setTotal(r.count);
    });
  }, [cargar, runID, schema, table]);

  function ordenarPor(columna: string) {
    const siguiente: SortState =
      orden?.column === columna
        ? { column: columna, descending: !orden.descending }
        : { column: columna, descending: false };
    setOrden(siguiente);
    setSeleccion(null);
    void cargar(0, siguiente);
  }

  // El resultado que ve la grilla: las columnas de la primera página más todas
  // las filas acumuladas por "cargar más".
  const acumulado: Result | null = result ? { ...result, rows: filas } : null;

  // Las claves salen del esquema ya introspectado, no de otra consulta: acá se
  // sabe qué tabla se está mirando, así que la información ya está en memoria.
  const claves: Record<string, "pk" | "fk"> = {};
  for (const esq of snapshot?.schemas ?? []) {
    if (esq.name !== schema) continue;
    for (const t of esq.tables ?? []) {
      if (t.name !== table) continue;
      for (const c of t.columns ?? []) {
        if (c.primaryKey) claves[c.name] = "pk";
        else if (c.foreignKey) claves[c.name] = "fk";
      }
    }
  }

  return (
    <div className={styles.screen}>
      <div className={styles.subtabs}>
        <PillTabs
          items={[
            { id: "data", label: "Datos" },
            { id: "structure", label: "Estructura", disabled: true, title: "Llega en la Iteración 8" },
            { id: "indexes", label: "Índices", disabled: true, title: "Llega en la Iteración 8" },
            { id: "fks", label: "Claves foráneas", disabled: true, title: "Llega en la Iteración 8" },
          ]}
          activeId={sub}
          onSelect={setSub}
          ariaLabel="Vistas de la tabla"
        />
        <span className={styles.divider} />
        <Button size="sm" disabled title="Llega en la Iteración 7">
          Agregar fila
        </Button>
        <Button size="sm" disabled title="Llega en la Iteración 7">
          Borrar fila
        </Button>
        <span className={styles.grow} />
        <span className={styles.count}>{conteo(filas.length, total, orden)}</span>
      </div>

      {orderedBy.length === 0 && filas.length > 0 ? (
        <div className={styles.avisoOrden}>
          Esta tabla no tiene clave primaria, así que el orden de las filas no está garantizado:
          «cargar más» puede repetir o saltear alguna. Ordená por una columna para fijarlo.
        </div>
      ) : null}

      {fallo ? (
        <div className={styles.error}>
          <p className={styles.errorMsg}>{fallo.message}</p>
          {fallo.detail ? <p className={styles.errorDetalle}>{fallo.detail}</p> : null}
          {fallo.hint ? <p className={styles.errorHint}>{fallo.hint}</p> : null}
        </div>
      ) : acumulado ? (
        <>
          <DataGrid
            result={acumulado}
            selection={seleccion}
            onSelect={setSeleccion}
            onOpenCell={setVisor}
            sort={orden}
            onSort={ordenarPor}
            keys={claves}
          />
          <div className={styles.foot}>
            {hayMas ? (
              <Button
                size="sm"
                loading={cargando}
                onClick={() => void cargar(filas.length, orden)}
              >
                Cargar {PAGINA} más
              </Button>
            ) : (
              <span className={styles.footNota}>
                {filas.length === 0 ? "La tabla está vacía." : "Se cargaron todas las filas."}
              </span>
            )}
            <span className={styles.grow} />
            {readOnly ? <span className={styles.roNote}>conexión de solo lectura</span> : null}
          </div>
        </>
      ) : (
        <p className={styles.vacio}>{cargando ? "Leyendo filas…" : "Sin datos."}</p>
      )}

      {visor && acumulado ? (
        <CellViewer
          open
          columns={acumulado.columns ?? []}
          row={filas[visor.row] ?? []}
          index={visor.col}
          rowNumber={visor.row + 1}
          source={`${schema}.${table}`}
          onIndexChange={(i) => setVisor({ row: visor.row, col: i })}
          onClose={() => setVisor(null)}
        />
      ) : null}
    </div>
  );
}

/**
 * "1.000 de 12.481 filas · id ▲".
 *
 * El total va aparte de las cargadas a propósito: sin el "de N", quien mira mil
 * filas no tiene forma de saber si son todas.
 */
function conteo(cargadas: number, total: number | null, orden: SortState | null): string {
  const n = (x: number) => x.toLocaleString("es", { useGrouping: true });
  const base =
    total === null
      ? `${n(cargadas)} filas cargadas`
      : cargadas >= total
        ? `${n(total)} filas`
        : `${n(cargadas)} de ${n(total)} filas`;
  if (!orden) return base;
  return `${base} · ${orden.column} ${orden.descending ? "▼" : "▲"}`;
}
