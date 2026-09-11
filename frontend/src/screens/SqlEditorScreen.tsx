import { useEffect, useRef, useState } from "react";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { Batch, Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Failure } from "../../bindings/github.com/LucianoR23/kanamedb/internal/engine";
import { Button, ContextMenu, Dialog, Input, PillTabs, Spinner } from "../components/ui";
import type { MenuAnchor } from "../components/ui";
import { DataGrid } from "../components/DataGrid";
import type { CellRef } from "../components/DataGrid";
import { SqlEditor } from "../components/SqlEditor";
import { textoDe } from "../lib/dialogos";
import { CellViewer } from "./CellViewer";
import { ExportDialog } from "./ExportDialog";
import { Splitter } from "../components/Splitter";
import {
  FORMATOS_DE_COPIA,
  alPortapapeles,
  textoDeCelda,
  textoDeFilas,
} from "../lib/copiar";
import { cx } from "../lib/cx";
import { nombreDeMotor, plural } from "../lib/motor";
import styles from "./SqlEditorScreen.module.css";

type Estado =
  | { fase: "vacio" }
  | { fase: "corriendo"; desde: number }
  | { fase: "cancelada" }
  | { fase: "listo"; batch: Batch }
  | { fase: "error"; failure: Failure; batch: Batch | null };

const PANEL = { min: 220, max: 520, initial: 300 };

/**
 * S06 SQL editor.
 *
 * Iteración 2: escribir, ejecutar, cancelar y ver el resultado. Explain,
 * Format, guardar consultas y envolver en transacción están en la barra pero
 * deshabilitados: pertenecen a iteraciones posteriores y sacarlos dejaría una
 * barra que no se parece al diseño ni promete lo que va a venir.
 */
export function SqlEditorScreen({
  tabId,
  active,
  snapshot,
  readOnly,
  statementTimeoutSeconds,
  rowLimit,
  connectionLabel,
  engine,
  sqlInicial = "",
  onHistorial,
  onSucio,
}: {
  tabId: string;
  /** La pestaña está a la vista. CodeMirror necesita saberlo para volver a
   *  medirse: mientras estuvo escondida su alto era cero. */
  active: boolean;
  snapshot: Snapshot | null;
  readOnly: boolean;
  statementTimeoutSeconds: number;
  rowLimit: number;
  connectionLabel: string;
  /** El motor de la conexión abierta. Decide con qué reglas se resalta y se
   *  autocompleta, y qué dice la barra de estado. */
  engine: string;
  /** Con qué texto arranca la pestaña. Lo pone quien la abre desde el historial
   *  o desde las guardadas; vacío en una consulta nueva. */
  sqlInicial?: string;
  /** Avisar que el historial cambió, para que el panel del sidebar se relea. */
  onHistorial?: () => void;
  /** Avisar si hay texto que se perdería al cerrar la pestaña. */
  onSucio?: (sucio: boolean) => void;
}) {
  // El texto inicial lo elige quien abre la pestaña —el historial, las
  // guardadas— y de ahí en adelante el dueño es este editor. Por eso va como
  // estado inicial y no como prop controlada: una prop que siguiera mandando
  // pisaría lo que se esté escribiendo en cada render del Shell.
  const [sql, setSql] = useState(sqlInicial);
  // Una pestaña está «sucia» si tiene texto distinto del que trajo. Correrla no
  // la limpia: el historial guarda lo que CORRIÓ, pero lo que quedó escrito
  // después —la versión que se estaba afinando— no está en ningún lado.
  useEffect(() => {
    onSucio?.(sql.trim() !== "" && sql !== sqlInicial);
  }, [sql, sqlInicial, onSucio]);

  const [guardando, setGuardando] = useState(false);
  const [nombre, setNombre] = useState("");
  const [errorGuardar, setErrorGuardar] = useState("");
  const [estado, setEstado] = useState<Estado>({ fase: "vacio" });
  const [panel, setPanel] = useState("results");
  const [ancho, setAncho] = useState(PANEL.initial);
  const [cursor, setCursor] = useState({ linea: 1, columna: 1 });
  const [seleccion, setSeleccion] = useState<CellRef | null>(null);
  const [visor, setVisor] = useState<CellRef | null>(null);
  const [transcurrido, setTranscurrido] = useState(0);
  // Cuál de los resultados del lote se está mirando.
  const [cual, setCual] = useState(0);
  const [exportando, setExportando] = useState(false);
  // null mientras no se copió; "ok" o "error" un rato después de intentarlo.
  const [copia, setCopia] = useState<null | "ok" | "error">(null);
  const [menuCopiar, setMenuCopiar] = useState<MenuAnchor | null>(null);
  const copiadoTimer = useRef<number | null>(null);

  // El identificador de ejecución es por pestaña: cancelar acá no puede cortar
  // la consulta de otra pestaña.
  const runID = useRef(`${tabId}:${Math.random().toString(36).slice(2)}`).current;

  const corriendo = estado.fase === "corriendo";

  // El contador de la pantalla de "corriendo". Es lo único que dice que la app
  // no se colgó cuando una consulta tarda.
  useEffect(() => {
    if (!corriendo) {
      setTranscurrido(0);
      return;
    }
    const desde = estado.desde;
    const id = window.setInterval(() => setTranscurrido(Date.now() - desde), 100);
    return () => window.clearInterval(id);
  }, [corriendo, estado]);

  async function guardar() {
    setErrorGuardar("");
    try {
      await HistorySvc.Save({ name: nombre, sql } as never);
      setGuardando(false);
      setNombre("");
      onHistorial?.();
    } catch (err: unknown) {
      // El error se muestra entero: el que importa —«esta consulta lleva una
      // contraseña escrita y Kaname no la guarda en un archivo»— es la
      // explicación, no un detalle técnico.
      setErrorGuardar(textoDe(err));
    }
  }

  async function ejecutar() {
    if (corriendo || sql.trim() === "") return;
    setEstado({ fase: "corriendo", desde: Date.now() });
    setSeleccion(null);
    const res = await QueriesSvc.Run(runID, sql);
    // Corrió —bien o mal— así que el historial cambió. Se avisa siempre y no
    // solo cuando salió bien: la consulta que uno vuelve a buscar en el
    // historial es a menudo justamente la que falló.
    onHistorial?.();
    if (res.ok && res.batch) {
      // Se abre en el último resultado con filas: es lo que la persona acaba de
      // terminar de escribir. Los anteriores quedan a un clic de distancia.
      const ultimo = (res.batch.results ?? []).reduce((acc, r, i) => (r.returnsRows ? i : acc), 0);
      setCual(ultimo);
      setEstado({ fase: "listo", batch: res.batch });
      setPanel("results");
      return;
    }
    const f = res.failure;
    // Cancelar es lo que la persona pidió, no un error. Sin este caso, apretar
    // Cancelar devolvía un cartel rojo con "context canceled", que hace dudar de
    // si además pasó algo malo.
    if (f?.kind === "canceled") {
      setEstado({ fase: "cancelada" });
      setPanel("results");
      return;
    }
    setEstado({
      fase: "error",
      failure: f ?? ({ kind: "other", message: "La consulta falló sin detalle." } as Failure),
      batch: res.batch ?? null,
    });
    setPanel("messages");
  }

  function cancelar() {
    void QueriesSvc.Cancel(runID);
  }

  // Ctrl+C sobre la grilla copia LA CELDA seleccionada, no el resultado entero:
  // el resultado entero es lo que hace el botón «Copiar» de la barra. Son dos
  // cosas distintas y la tecla es la del sistema, que en cualquier grilla copia
  // lo que está seleccionado.
  function tecladoResultado(e: React.KeyboardEvent) {
    if (!seleccion || !result?.returnsRows) return;
    if (!(e.target instanceof HTMLElement) || e.target.getAttribute("role") !== "grid") return;
    if (!((e.ctrlKey || e.metaKey) && (e.key === "c" || e.key === "C"))) return;
    e.preventDefault();
    const valor = (result.rows ?? [])[seleccion.row]?.[seleccion.col] ?? null;
    void alPortapapeles(textoDeCelda(valor))
      .then(() => setCopia("ok"))
      .catch(() => setCopia("error"));
  }

  async function copiarResultado(formato: (typeof FORMATOS_DE_COPIA)[number]) {
    if (!result?.returnsRows) return;
    // Con try/catch: si el formateo o el portapapeles fallan, el botón lo dice.
    // Sin él la promesa se rechazaba sola y apretar «Copiar» no hacía nada
    // visible, que es la peor forma de fallar de un botón.
    try {
      await alPortapapeles(await textoDeFilas(result.columns ?? [], result.rows ?? [], formato));
      setCopia("ok");
    } catch {
      setCopia("error");
    }
    if (copiadoTimer.current !== null) window.clearTimeout(copiadoTimer.current);
    copiadoTimer.current = window.setTimeout(() => setCopia(null), 1800);
  }
  useEffect(() => {
    return () => {
      if (copiadoTimer.current !== null) window.clearTimeout(copiadoTimer.current);
    };
  }, []);

  const lote = estado.fase === "listo" ? estado.batch : null;
  const resultados = lote?.results ?? [];
  const result: Result | null = resultados[Math.min(cual, resultados.length - 1)] ?? null;
  const columnas = result?.columns ?? [];

  return (
    <div className={styles.screen}>
      <div className={styles.toolbar}>
        <Button variant="primary" size="sm" onClick={() => void ejecutar()} disabled={corriendo}>
          {corriendo ? "Ejecutando…" : "Ejecutar"}
          <span className={styles.shortcut}>Ctrl ↵</span>
        </Button>
        {corriendo ? (
          <button type="button" className={styles.cancel} onClick={cancelar}>
            Cancelar
          </button>
        ) : null}
        <span className={styles.divider} />
        <Button
          size="sm"
          onClick={() => setGuardando(true)}
          disabled={sql.trim() === ""}
          title="Guardar esta consulta con un nombre"
        >
          Guardar…
        </Button>
        <span className={styles.divider} />
        <button type="button" className={styles.link} disabled title="Llega en una iteración posterior">
          Explain
        </button>
        <button type="button" className={styles.link} disabled title="Llega en una iteración posterior">
          Formatear
        </button>
        <span className={styles.divider} />
        <span className={styles.grow} />
        {readOnly ? <span className={styles.roNote}>conexión de solo lectura</span> : null}
        <span className={styles.autoNote}>
          se traen hasta {rowLimit.toLocaleString("es", { useGrouping: true })}{" "}
          {plural(rowLimit, "fila", "filas")}
        </span>
        <button type="button" className={styles.link} disabled title="Llega en la Iteración 9">
          Guardar consulta
        </button>
      </div>

      <div className={styles.top}>
        <SqlEditor
          value={sql}
          onChange={setSql}
          snapshot={snapshot}
          active={active}
          onRun={() => void ejecutar()}
          onCursor={(linea, columna) => setCursor({ linea, columna })}
                  engine={engine}
        />
        <Splitter
          size={ancho}
          onResize={setAncho}
          min={PANEL.min}
          max={PANEL.max}
          side="right"
          label="Ancho del panel de esquema"
        />
        <aside className={styles.schema} style={{ width: ancho }}>
          <div className={styles.schemaHead}>Esquema</div>
          <div className={styles.schemaBody}>
            {(snapshot?.schemas ?? []).map((esq) => (
              <div key={esq.name}>
                <div className={styles.schemaName}>{esq.name}</div>
                {(esq.tables ?? []).map((t) => (
                  <details key={t.name} className={styles.tabla}>
                    <summary className={styles.tablaHead}>
                      <span className={styles.tablaTag}>TB</span>
                      <span className={styles.tablaName}>{t.name}</span>
                    </summary>
                    {(t.columns ?? []).map((c) => (
                      <div key={c.name} className={styles.col}>
                        <span className={styles.colName}>{c.name}</span>
                        <span className={styles.grow} />
                        <span className={styles.colType}>{c.dataType}</span>
                      </div>
                    ))}
                  </details>
                ))}
              </div>
            ))}
            {snapshot ? null : <p className={styles.vacio}>El esquema aparece al conectarse.</p>}
          </div>
        </aside>
      </div>

      <div className={styles.bottom}>
        <div className={styles.resultsBar}>
          <PillTabs
            items={[
              { id: "results", label: "Resultados" },
              { id: "messages", label: "Mensajes" },
            ]}
            activeId={panel}
            onSelect={setPanel}
            ariaLabel="Panel de resultados"
          />
          <span className={styles.grow} />
          {result ? (
            <span className={styles.meta}>
              {result.returnsRows
                ? `${(result.rows ?? []).length.toLocaleString("es", { useGrouping: true })} ${plural((result.rows ?? []).length, "fila", "filas")}`
                : result.command}
              {result.truncated ? " · cortado por el límite" : ""}
            </span>
          ) : null}
          <span className={cx(styles.meta, estado.fase === "error" && styles.metaError)}>
            {metaDe(estado)}
          </span>
          {result?.returnsRows ? (
            <>
              <button
                type="button"
                className={cx(styles.link, copia === "error" && styles.metaError)}
                onClick={(e) => {
                  const r = e.currentTarget.getBoundingClientRect();
                  setCopia(null);
                  setMenuCopiar({ x: r.left, y: r.bottom });
                }}
              >
                {copia === "ok" ? "Copiado" : copia === "error" ? "No se pudo copiar" : "Copiar…"}
              </button>
              <button type="button" className={styles.link} onClick={() => setExportando(true)}>
                Exportar…
              </button>
            </>
          ) : null}
        </div>

        {resultados.length > 1 ? (
          <div className={styles.resultTabs}>
            {resultados.map((r, i) => (
              <button
                type="button"
                key={i}
                className={cx(styles.resultTab, i === cual && styles.resultTabOn)}
                onClick={() => {
                  setCual(i);
                  setSeleccion(null);
                }}
              >
                <span className={styles.resultTabNo}>{i + 1}</span>
                {r.command}
                {r.line ? <span className={styles.resultTabLinea}>ln {r.line}</span> : null}
              </button>
            ))}
          </div>
        ) : null}

        <div className={styles.resultsBody} onKeyDown={tecladoResultado}>
          {estado.fase === "cancelada" ? (
            <div className={styles.sinFilas}>
              <p className={styles.sinFilasTitulo}>Consulta cancelada</p>
              <p className={styles.sinFilasNota}>
                Se cortó la ejecución en el servidor. No quedó nada a medias: Postgres
                revierte la transacción implícita del lote.
              </p>
            </div>
          ) : panel === "messages" ? (
            <Mensajes estado={estado} />
          ) : corriendo ? (
            <div className={styles.corriendo}>
              <Spinner />
              <div className={styles.corriendoTitulo}>Ejecutando en {connectionLabel}…</div>
              <div className={styles.corriendoMeta}>
                {(transcurrido / 1000).toFixed(1)} s
                {statementTimeoutSeconds > 0
                  ? ` · el servidor la corta si supera ${statementTimeoutSeconds} s`
                  : ""}
              </div>
              <button type="button" className={styles.cancel} onClick={cancelar}>
                Cancelar consulta
              </button>
            </div>
          ) : result && result.returnsRows ? (
            <DataGrid
              result={result}
              selection={seleccion}
              onSelect={setSeleccion}
              onOpenCell={setVisor}
            />
          ) : result ? (
            <div className={styles.sinFilas}>
              <p className={styles.sinFilasTitulo}>{result.command}</p>
              <p className={styles.sinFilasNota}>
                {result.affectedRows === 1
                  ? "1 fila afectada."
                  : `${result.affectedRows} ${plural(result.affectedRows, "fila afectada", "filas afectadas")}.`}{" "}
                Esta sentencia no devuelve resultados.
              </p>
            </div>
          ) : estado.fase === "error" ? (
            <Mensajes estado={estado} />
          ) : (
            <p className={styles.vacio}>Escribí una consulta y ejecutá con Ctrl ↵.</p>
          )}
        </div>
      </div>

      <footer className={styles.status}>
        <span className={styles.statusDim}>{connectionLabel}</span>
        <span className={styles.grow} />
        <span className={styles.statusDim}>
          ln {cursor.linea} · col {cursor.columna}
        </span>
        <span className={styles.statusDim}>dialecto {nombreDeMotor(engine)}</span>
      </footer>

      {menuCopiar && result?.returnsRows ? (
        <ContextMenu
          anchor={menuCopiar}
          entries={[
            {
              kind: "label",
              id: "lbl",
              label: `Copiar ${(result.rows ?? []).length.toLocaleString("es", {
                useGrouping: true,
              })} ${plural((result.rows ?? []).length, "fila", "filas")} como`,
            },
            ...FORMATOS_DE_COPIA.map((f) => ({
              id: f.key,
              label: f.label,
              hint: f.nota,
              onSelect: () => void copiarResultado(f),
            })),
          ]}
          onClose={() => setMenuCopiar(null)}
        />
      ) : null}

      {visor && result ? (
        <CellViewer
          open
          columns={columnas}
          row={(result.rows ?? [])[visor.row] ?? []}
          index={visor.col}
          rowNumber={visor.row + 1}
          source="resultado"
          onIndexChange={(i) => setVisor({ row: visor.row, col: i })}
          onClose={() => setVisor(null)}
        />
      ) : null}
      {exportando && result?.returnsRows ? (
        <ExportDialog
          open
          origen={{
            tipo: "resultado",
            columns: columnas,
            rows: result.rows ?? [],
            truncated: result.truncated,
          }}
          nombre="consulta"
          runID={runID}
          onClose={() => setExportando(false)}
        />
      ) : null}
      <Dialog
        open={guardando}
        title="Guardar la consulta"
        onClose={() => setGuardando(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setGuardando(false)}>
              Cancelar
            </Button>
            <Button variant="primary" onClick={() => void guardar()} disabled={nombre.trim() === ""}>
              Guardar
            </Button>
          </>
        }
      >
        <p className={styles.ayudaGuardar}>
          Las consultas guardadas viajan con la libreta de conexiones, así que las vas a encontrar
          en la otra máquina. El historial no: ese es de ésta.
        </p>
        <Input
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
          placeholder="Ventas del mes"
          aria-label="Nombre de la consulta"
          autoFocus
        />
        {errorGuardar ? <p className={styles.errorGuardar}>{errorGuardar}</p> : null}
      </Dialog>
    </div>
  );
}

/**
 * La pestaña Mensajes.
 *
 * Muestra el tag de CADA sentencia del lote, no solo la última. Es donde se ve
 * que `select 1; drop table x;` hizo dos cosas: la grilla sola mostraría la
 * fila del select y el drop pasaría desapercibido.
 */
function Mensajes({ estado }: { estado: Estado }) {
  if (estado.fase === "cancelada") {
    return <p className={styles.vacio}>La consulta se canceló.</p>;
  }
  if (estado.fase === "error") {
    const f = estado.failure;
    return (
      <div className={styles.mensajes}>
        <div className={styles.errorCard}>
          <div className={styles.errorHead}>
            <span className={styles.errorMark}>!</span>
            <span className={styles.errorTitulo}>
              {f.totalStatements && f.totalStatements > 1
                ? `Falló la sentencia ${f.statement} de ${f.totalStatements}, línea ${f.line}`
                : "La consulta falló"}
            </span>
            {f.sqlState ? <span className={styles.errorState}>{f.sqlState}</span> : null}
          </div>
          <div className={styles.errorBody}>
            {f.detail ? <div className={styles.errorDetalle}>{f.detail}</div> : null}
            <p className={styles.errorMensaje}>{f.message}</p>
            {f.hint ? <p className={styles.errorHint}>{f.hint}</p> : null}
          </div>
        </div>
        {f.totalStatements && f.totalStatements > 1 ? (
          <p className={styles.errorHint}>
            {corridas(estado.batch?.results ?? null)} y se cortó ahí: las que seguían no se
            ejecutaron.
          </p>
        ) : null}
        <Tags resultados={estado.batch?.results ?? null} />
      </div>
    );
  }
  if (estado.fase === "listo") {
    return (
      <div className={styles.mensajes}>
        <Tags resultados={estado.batch.results ?? null} />
      </div>
    );
  }
  return <p className={styles.vacio}>Todavía no se ejecutó nada.</p>;
}

/**
 * Una línea por sentencia del lote.
 *
 * Dice además cuántas filas movió cada una, porque el tag del motor solo no
 * alcanza: `select 1; select 2;` produce "SELECT 1" dos veces —las dos
 * devolvieron una fila— y leído sin más parece la misma sentencia repetida.
 */
function Tags({ resultados }: { resultados: Result[] | null }) {
  if (!resultados || resultados.length === 0) return null;
  return (
    <ol className={styles.tags}>
      {resultados.map((r, i) => (
        <li key={i} className={styles.tag}>
          <span className={styles.tagNo}>{i + 1}</span>
          <span className={styles.tagText}>{r.command}</span>
          <span className={styles.grow} />
          <span className={styles.tagMeta}>
            {r.returnsRows ? `${(r.rows ?? []).length} devueltas` : `${r.affectedRows} afectadas`}
          </span>
        </li>
      ))}
    </ol>
  );
}

function metaDe(e: Estado): string {
  switch (e.fase) {
    case "corriendo":
      return "ejecutando…";
    case "cancelada":
      return "cancelada";
    case "error":
      return "falló";
    case "listo": {
      const rs = e.batch.results ?? [];
      const partes: string[] = [`${e.batch.elapsedMs} ms`];
      if (rs.length > 1) partes.unshift(`${rs.length} sentencias`); // solo se muestra con >1
      return partes.join(" · ");
    }
    default:
      return "";
  }
}

/** «Corrieron 2 sentencias» / «No corrió ninguna», para el pie del error. */
function corridas(rs: readonly Result[] | null): string {
  const n = rs?.length ?? 0;
  if (n === 0) return "No corrió ninguna";
  return `${n === 1 ? "Corrió 1 sentencia" : `Corrieron ${n} sentencias`}`;
}
