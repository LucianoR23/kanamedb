import { useCallback, useEffect, useRef, useState } from "react";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Failure } from "../../bindings/github.com/LucianoR23/kanamedb/internal/postgres";
import type {
  Snapshot,
  TableDetail,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { Button, Glyph, PillTabs } from "../components/ui";
import { DataGrid } from "../components/DataGrid";
import type { CellRef, SortState } from "../components/DataGrid";
import { CellViewer } from "./CellViewer";
import { TableStructure, bytes } from "./TableStructure";
import type { StructureView } from "./TableStructure";
import styles from "./TableDataScreen.module.css";

/** Cuántas filas trae cada página. */
const PAGINA = 500;

/**
 * S10 Table data y S11 Table structure.
 *
 * Son la misma pestaña con sub-pestañas, como en el diseño: quien mira una
 * tabla alterna entre sus datos y su estructura todo el tiempo, y separarlas en
 * dos pestañas del árbol duplicaría la fila abierta.
 *
 * Iteración 2: leer, ordenar, cargar más y contar. Iteración 4: las cinco
 * vistas de estructura, solo lectura. Agregar, borrar y editar filas llegan con
 * la Iteración 7; los botones están y se ven deshabilitados, porque la pestaña
 * se diseñó con ellos.
 */
export function TableDataScreen({
  tabId,
  schema,
  table,
  readOnly,
  snapshot,
  onShowInErd,
}: {
  tabId: string;
  schema: string;
  table: string;
  readOnly: boolean;
  snapshot: Snapshot | null;
  onShowInErd: (schema: string, table: string) => void;
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
  const [sub, setSub] = useState<"data" | StructureView>("data");

  // La estructura se lee al abrir la tabla, junto con la primera página de
  // datos.
  //
  // Se probó perezosa —solo al entrar a una sub-pestaña de estructura— y era
  // peor: los contadores de las pestañas y el tamaño del encabezado aparecían
  // de golpe recién después del primer clic, y un número que aparece tarde
  // confunde más que un número que falta. Son siete consultas al catálogo en un
  // solo viaje; cuestan menos que la explicación.
  const [detalle, setDetalle] = useState<TableDetail | null>(null);
  const [detalleCargando, setDetalleCargando] = useState(false);
  const [detalleError, setDetalleError] = useState("");

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

  // Cuál es la lectura vigente. Dos llamadas superpuestas —doble clic en
  // «Actualizar», o el montaje más un refresco inmediato— se pisan: la primera
  // en resolver apagaba el spinner con la otra todavía en vuelo, y si la vieja
  // llegaba última dejaba datos anteriores con una hora de lectura posterior.
  const pedidoDetalle = useRef(0);

  const leerDetalle = useCallback(async () => {
    const mio = ++pedidoDetalle.current;
    setDetalleCargando(true);
    setDetalleError("");
    try {
      const d = await SessionSvc.TableDetail(schema, table);
      if (mio !== pedidoDetalle.current) return;
      setDetalle(d);
    } catch (err) {
      if (mio !== pedidoDetalle.current) return;
      setDetalleError(err instanceof Error ? err.message : String(err));
    } finally {
      if (mio === pedidoDetalle.current) setDetalleCargando(false);
    }
  }, [schema, table]);

  useEffect(() => {
    void leerDetalle();
  }, [leerDetalle]);

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
  // Las columnas se cuentan en el mismo recorrido: el encabezado las muestra
  // desde que se abre la pestaña, sin esperar a que se lea la estructura.
  // undefined mientras el snapshot no llegó o la tabla no está en él: la pastilla
  // esconde el contador cuando no hay número, y «Estructura 0» sería una cuenta
  // que ninguna tabla puede tener.
  let columnasDeLaTabla: number | undefined;
  for (const esq of snapshot?.schemas ?? []) {
    if (esq.name !== schema) continue;
    for (const t of esq.tables ?? []) {
      if (t.name !== table) continue;
      columnasDeLaTabla = (t.columns ?? []).length;
      for (const c of t.columns ?? []) {
        if (c.primaryKey) claves[c.name] = "pk";
        else if (c.foreignKey) claves[c.name] = "fk";
      }
    }
  }

  const enDatos = sub === "data";

  return (
    <div className={styles.screen}>
      <div className={styles.head}>
        <Glyph kind="table" />
        <span className={styles.titulo}>
          {schema}.{table}
        </span>
        <span className={styles.hechos}>{hechos(columnasDeLaTabla, total, detalle)}</span>
        <span className={styles.grow} />
        <Button size="sm" variant="ghost" onClick={() => onShowInErd(schema, table)}>
          Ver en el diagrama
        </Button>
        {!enDatos ? (
          <>
            {detalle ? (
              <span className={styles.leido}>leído a las {hora(detalle.capturedAt)}</span>
            ) : null}
            <Button
              size="sm"
              variant="ghost"
              loading={detalleCargando}
              onClick={() => void leerDetalle()}
            >
              Actualizar
            </Button>
          </>
        ) : null}
      </div>

      <div className={styles.subtabs}>
        <PillTabs
          items={[
            { id: "data", label: "Datos" },
            { id: "structure", label: "Estructura", count: columnasDeLaTabla },
            { id: "indexes", label: "Índices", count: detalle?.indexes?.length },
            {
              id: "keys",
              label: "Claves foráneas",
              count:
                detalle === null
                  ? undefined
                  : (detalle.foreignKeys?.length ?? 0) + (detalle.referencedBy?.length ?? 0),
            },
            { id: "constraints", label: "Restricciones", count: detalle?.checks?.length },
            { id: "triggers", label: "Triggers", count: detalle?.triggers?.length },
          ]}
          activeId={sub}
          onSelect={(id) => setSub(id as "data" | StructureView)}
          ariaLabel="Vistas de la tabla"
        />
        {enDatos ? (
          <>
            <span className={styles.divider} />
            <Button size="sm" disabled title="Llega en la Iteración 7">
              Agregar fila
            </Button>
            <Button size="sm" disabled title="Llega en la Iteración 7">
              Borrar fila
            </Button>
          </>
        ) : null}
        <span className={styles.grow} />
        {enDatos ? <span className={styles.count}>{conteo(filas.length, total, orden)}</span> : null}
      </div>

      {!enDatos ? (
        <TableStructure
          view={sub}
          detail={detalle}
          loading={detalleCargando}
          error={detalleError}
        />
      ) : (
        <>

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

        </>
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
 * "12.481 filas · 182 MB · 7 columnas".
 *
 * El tamaño aparece solo cuando la estructura ya se leyó: es lo único que no
 * está en el snapshot, y pedirlo al abrir la tabla sería una consulta al
 * catálogo por cada pestaña que nadie miró.
 */
function hechos(
  columnas: number | undefined,
  total: number | null,
  detalle: TableDetail | null,
): string {
  const n = (x: number) => x.toLocaleString("es", { useGrouping: true });
  const partes: string[] = [];

  if (total !== null) {
    partes.push(`${n(total)} filas`);
  } else if (detalle && detalle.rowEstimate >= 0) {
    // Es la estimación del planificador, no un conteo. El "≈" es lo que separa
    // "son 12.481" de "el planificador cree que son como 12.481".
    partes.push(`≈ ${n(detalle.rowEstimate)} filas`);
  }

  if (detalle) partes.push(bytes(detalle.totalBytes));
  if (columnas !== undefined && columnas > 0) partes.push(`${n(columnas)} columnas`);
  return partes.join(" · ");
}

/** 09:41. */
function hora(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? ""
    : d.toLocaleTimeString("es", { hour: "2-digit", minute: "2-digit" });
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
