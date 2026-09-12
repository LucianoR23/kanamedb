import { useEffect, useRef, useState } from "react";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { Batch, Column, Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Failure } from "../../bindings/github.com/LucianoR23/kanamedb/internal/engine";
import type { TransactionState } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import {
  Button,
  ConfirmDialog,
  ContextMenu,
  Dialog,
  Input,
  PillTabs,
  Spinner,
  DialogClose,
  Toggle,
} from "../components/ui";
import type { MenuAnchor } from "../components/ui";
import { DataGrid } from "../components/DataGrid";
import type { CellRef } from "../components/DataGrid";
import { SqlEditor } from "../components/SqlEditor";
import { textoDe } from "../lib/dialogos";
import { formatearSQL } from "../lib/formatear";
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

/** El plan de ejecución tiene su propio estado, aparte del de ejecutar: pedir
 *  el plan no toca los resultados que ya están en la pestaña de al lado. */
type Plan =
  | { fase: "vacio" }
  | { fase: "corriendo" }
  | { fase: "listo"; result: Result; elapsedMs: number }
  | { fase: "error"; failure: Failure };

const PANEL = { min: 220, max: 520, initial: 300 };

/**
 * S06 SQL editor.
 *
 * Escribir, ejecutar, cancelar y ver el resultado; guardar con nombre; pedir
 * el plan de ejecución de la sentencia bajo el cursor, que el motor da sin
 * correrla; y el control manual de transacciones: con «Auto-commit» sacado,
 * la pestaña retiene una conexión y BEGIN, COMMIT y ROLLBACK significan lo
 * que dicen. El estado vive en Go y muere con la conexión.
 */
export function SqlEditorScreen({
  tabId,
  active,
  snapshot,
  readOnly,
  statementTimeoutSeconds,
  timeoutSoloLecturas = false,
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
  /** MySQL: max_execution_time solo corta SELECT (K-16). */
  timeoutSoloLecturas?: boolean;
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

  // Control manual de transacciones. Go es el dueño del estado: cada
  // ejecución lo devuelve, y al montar la pestaña se pregunta por si la
  // conexión cambió por debajo. Al desmontar se cierra la pestaña en Go, que
  // revierte lo que tuviera abierto y suelta la conexión.
  const [tx, setTx] = useState<TransactionState | null>(null);
  const [errorTx, setErrorTx] = useState("");
  const [confirmando, setConfirmando] = useState(false);
  const [cerrandoTx, setCerrandoTx] = useState(false);
  // Se pregunta al montar Y cada vez que la conexión cambia por debajo —una
  // reconexión, otra base—: el estado vive en Go y muere con la conexión, y
  // sin esto la casilla seguía mostrando lo de antes. Cada ejecución también
  // lo trae, así que a lo sumo la casilla miente hasta la próxima.
  useEffect(() => {
    QueriesSvc.TransactionOf(tabId)
      .then(setTx)
      .catch(() => {});
  }, [tabId, connectionLabel]);
  useEffect(() => {
    return () => {
      void QueriesSvc.CloseTab(tabId).catch(() => {});
    };
  }, [tabId]);

  async function cambiarAutocommit(on: boolean) {
    setErrorTx("");
    try {
      setTx(await QueriesSvc.SetAutocommit(tabId, on));
    } catch (err) {
      setErrorTx(textoDe(err));
    }
  }

  async function cerrarTransaccion(como: "commit" | "rollback", palabra = "") {
    setErrorTx("");
    setCerrandoTx(true);
    try {
      const res =
        como === "commit"
          ? await QueriesSvc.Commit(tabId, palabra)
          : await QueriesSvc.Rollback(tabId);
      if (res.transaction) setTx(res.transaction);
      if (!res.ok) {
        setErrorTx(res.failure?.message ?? "No se pudo cerrar la transacción.");
        return;
      }
      setConfirmando(false);
      onHistorial?.();
    } catch (err) {
      setErrorTx(textoDe(err));
    } finally {
      setCerrandoTx(false);
    }
  }

  const manual = tx !== null && !tx.autocommit;
  const [estado, setEstado] = useState<Estado>({ fase: "vacio" });
  const [plan, setPlan] = useState<Plan>({ fase: "vacio" });
  const [seleccionPlan, setSeleccionPlan] = useState<CellRef | null>(null);
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
  // Por qué no se pudo formatear, un rato. El texto queda como estaba.
  const [errorFormato, setErrorFormato] = useState<string | null>(null);
  const formatoTimer = useRef<number | null>(null);
  const [menuCopiar, setMenuCopiar] = useState<MenuAnchor | null>(null);
  const copiadoTimer = useRef<number | null>(null);

  // El identificador de ejecución es por pestaña: cancelar acá no puede cortar
  // la consulta de otra pestaña.
  const runID = useRef(`${tabId}:${Math.random().toString(36).slice(2)}`).current;

  const corriendo = estado.fase === "corriendo";
  // Ejecutar y pedir el plan comparten el runID —es lo que permite cancelar
  // cualquiera de los dos con el mismo botón—, así que no van a la vez.
  const pidiendoPlan = plan.fase === "corriendo";

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
    // También con el plan en curso: Ctrl+↵ llega acá sin pasar por el botón,
    // y las dos llamadas comparten el runID. Sin esto, la consulta corría sin
    // registrarse y «Cancelar» no la encontraba.
    if (corriendo || pidiendoPlan || sql.trim() === "") return;
    setEstado({ fase: "corriendo", desde: Date.now() });
    setSeleccion(null);
    const res = await QueriesSvc.RunIn(tabId, runID, sql);
    if (res.transaction) setTx(res.transaction);
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

  /**
   * Ordena el texto con el dialecto del motor. Reemplaza el documento entero
   * en una sola transacción, así Ctrl+Z lo deshace de un golpe. Si no se puede
   * interpretar, se dice al lado del botón y no se toca nada.
   */
  function formatear() {
    if (sql.trim() === "") return;
    try {
      const ordenado = formatearSQL(sql, engine);
      if (ordenado !== sql) setSql(ordenado);
      setErrorFormato(null);
    } catch (err) {
      // La primera línea: dónde se trabó. La segunda dice qué dialecto usó,
      // que acá es siempre el del motor conectado.
      setErrorFormato(textoDe(err).split("\n")[0] ?? textoDe(err));
      if (formatoTimer.current !== null) window.clearTimeout(formatoTimer.current);
      formatoTimer.current = window.setTimeout(() => setErrorFormato(null), 6000);
    }
  }
  useEffect(() => {
    return () => {
      if (formatoTimer.current !== null) window.clearTimeout(formatoTimer.current);
    };
  }, []);

  /**
   * Pide el plan de la sentencia bajo el cursor. El motor no la ejecuta: es
   * EXPLAIN a secas —EXPLAIN QUERY PLAN en SQLite—, nunca ANALYZE. Un DELETE
   * bajo el cursor no borra nada. La línea la elige Go, con el texto entero
   * y la línea del cursor; si hay varias y el cursor no está sobre ninguna,
   * lo dice en vez de adivinar.
   */
  async function pedirPlan() {
    if (corriendo || pidiendoPlan || sql.trim() === "") return;
    setPlan({ fase: "corriendo" });
    setSeleccionPlan(null);
    setPanel("plan");
    const res = await QueriesSvc.Explain(runID, sql, cursor.linea);
    const result = (res.batch?.results ?? [])[0];
    if (res.ok && result) {
      setPlan({ fase: "listo", result, elapsedMs: res.batch?.elapsedMs ?? 0 });
      return;
    }
    if (res.failure?.kind === "canceled") {
      setPlan({ fase: "vacio" });
      return;
    }
    setPlan({
      fase: "error",
      failure: res.failure ?? ({ kind: "other", message: "El plan falló sin detalle." } as Failure),
    });
  }

  // Ctrl+C sobre la grilla copia LA CELDA seleccionada, no el resultado entero:
  // el resultado entero es lo que hace el botón «Copiar» de la barra. Son dos
  // cosas distintas y la tecla es la del sistema, que en cualquier grilla copia
  // lo que está seleccionado.
  function tecladoResultado(e: React.KeyboardEvent) {
    const { result: visible, seleccion: sel } = enPantalla;
    if (!sel || !visible?.returnsRows) return;
    if (!(e.target instanceof HTMLElement) || e.target.getAttribute("role") !== "grid") return;
    if (!((e.ctrlKey || e.metaKey) && (e.key === "c" || e.key === "C"))) return;
    e.preventDefault();
    const valor = (visible.rows ?? [])[sel.row]?.[sel.col] ?? null;
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
  // Lo que hay a la vista: el resultado de ejecutar, o el plan. Copiar una
  // celda con Ctrl+C y abrirla en el visor trabajan sobre lo que se está
  // mirando, no sobre la pestaña de al lado.
  const enPantalla: { result: Result | null; seleccion: CellRef | null } =
    panel === "plan"
      ? { result: plan.fase === "listo" ? plan.result : null, seleccion: seleccionPlan }
      : { result, seleccion };

  return (
    <div className={styles.screen}>
      <div className={styles.toolbar}>
        <Button
          variant="primary"
          size="sm"
          onClick={() => void ejecutar()}
          disabled={corriendo || pidiendoPlan}
        >
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
        {/* Dice en castellano lo que hace: lo que la persona quiere no es
            escribir EXPLAIN, es ver el plan. */}
        <button
          type="button"
          className={styles.link}
          disabled={corriendo || pidiendoPlan || sql.trim() === ""}
          title="Cómo va a correr el motor la sentencia bajo el cursor, sin correrla."
          onClick={() => void pedirPlan()}
        >
          {pidiendoPlan ? "Pidiendo el plan…" : "Plan de ejecución"}
        </button>
        <button
          type="button"
          className={styles.link}
          disabled={sql.trim() === ""}
          title="Ordenar espacios, saltos y sangría con el dialecto del motor. No cambia mayúsculas."
          onClick={formatear}
        >
          Formatear
        </button>
        {errorFormato ? (
          <span className={cx(styles.meta, styles.metaError)} role="alert">
            No se pudo formatear: {errorFormato}
          </span>
        ) : null}
        <span className={styles.divider} />
        {/* Con auto-commit sacado, la pestaña es dueña de una conexión y
            BEGIN/COMMIT/ROLLBACK significan lo que dicen. Se muestra el estado
            SIEMPRE que esté sacado —«sin transacción» también— para que nunca
            quede una abierta sin verse. */}
        <label className={styles.autocommit} title="Con auto-commit, cada sentencia se confirma sola. Sin él, la pestaña retiene una conexión y BEGIN, COMMIT y ROLLBACK valen de verdad.">
          <Toggle
            checked={!manual}
            onChange={(on) => void cambiarAutocommit(on)}
            label="Auto-commit"
            disabled={readOnly || corriendo}
          />
          <span>Auto-commit</span>
        </label>
        {manual ? (
          <span className={cx(styles.txEstado, tx?.inTransaction && styles.txAbierta)}>
            {tx?.inTransaction
              ? `Transacción abierta · ${tx.statements} ${plural(tx.statements, "sentencia", "sentencias")}`
              : "sin transacción"}
          </span>
        ) : null}
        {manual && tx?.inTransaction ? (
          <>
            <Button
              size="sm"
              variant={tx.needsConfirmation ? "danger" : "primary"}
              disabled={corriendo || cerrandoTx}
              onClick={() => {
                if (tx.needsConfirmation) setConfirmando(true);
                else void cerrarTransaccion("commit");
              }}
            >
              Confirmar
            </Button>
            <Button size="sm" disabled={corriendo || cerrandoTx} onClick={() => void cerrarTransaccion("rollback")}>
              Revertir
            </Button>
          </>
        ) : null}
        {errorTx ? (
          <span className={cx(styles.meta, styles.metaError)} role="alert">
            {errorTx}
          </span>
        ) : null}
        <span className={styles.grow} />
        {readOnly ? <span className={styles.roNote}>conexión de solo lectura</span> : null}
        <span className={styles.autoNote}>
          se traen hasta {rowLimit.toLocaleString("es", { useGrouping: true })}{" "}
          {plural(rowLimit, "fila", "filas")}
        </span>
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
              { id: "plan", label: "Plan" },
            ]}
            activeId={panel}
            onSelect={setPanel}
            ariaLabel="Panel de resultados"
          />
          <span className={styles.grow} />
          {panel === "plan" ? (
            <span className={cx(styles.meta, plan.fase === "error" && styles.metaError)}>
              {metaDelPlan(plan)}
            </span>
          ) : result ? (
            <span className={styles.meta}>
              {result.returnsRows
                ? `${(result.rows ?? []).length.toLocaleString("es", { useGrouping: true })} ${plural((result.rows ?? []).length, "fila", "filas")}`
                : result.command}
              {result.truncated ? " · cortado por el límite" : ""}
            </span>
          ) : null}
          {panel === "plan" ? null : (
            <span className={cx(styles.meta, estado.fase === "error" && styles.metaError)}>
              {metaDe(estado)}
            </span>
          )}
          {panel !== "plan" && result?.returnsRows ? (
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

        {panel !== "plan" && resultados.length > 1 ? (
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
          {/* Corriendo se muestra en cualquier pestaña: es lo único que dice
              que la app no se colgó, y el único lugar con «Cancelar». */}
          {corriendo ? (
            <div className={styles.corriendo}>
              <Spinner />
              <div className={styles.corriendoTitulo}>Ejecutando en {connectionLabel}…</div>
              <div className={styles.corriendoMeta}>
                {(transcurrido / 1000).toFixed(1)} s
                {statementTimeoutSeconds > 0
                  ? timeoutSoloLecturas
                    ? ` · el servidor corta un SELECT si supera ${statementTimeoutSeconds} s; una escritura no`
                    : ` · el servidor la corta si supera ${statementTimeoutSeconds} s`
                  : ""}
              </div>
              <button type="button" className={styles.cancel} onClick={cancelar}>
                Cancelar consulta
              </button>
            </div>
          ) : panel === "plan" ? (
            <PanelDelPlan
              plan={plan}
              seleccion={seleccionPlan}
              onSelect={setSeleccionPlan}
              onOpenCell={setVisor}
              onCancelar={cancelar}
            />
          ) : estado.fase === "cancelada" ? (
            <div className={styles.sinFilas}>
              <p className={styles.sinFilasTitulo}>Consulta cancelada</p>
              <p className={styles.sinFilasNota}>
                Se cortó la ejecución en el servidor. No quedó nada a medias: Postgres
                revierte la transacción implícita del lote.
              </p>
            </div>
          ) : panel === "messages" ? (
            <Mensajes estado={estado} />
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

      {visor && enPantalla.result ? (
        <CellViewer
          open
          columns={enPantalla.result.columns ?? []}
          row={(enPantalla.result.rows ?? [])[visor.row] ?? []}
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
      {confirmando && tx ? (
        <ConfirmDialog
          open
          severidad="produccion"
          title="Confirmar la transacción"
          confirmar
          palabra={tx.confirmWord ?? ""}
          etiqueta="Confirmar"
          onClose={() => setConfirmando(false)}
          onConfirm={(escrito) => void cerrarTransaccion("commit", escrito)}
        >
          Esta conexión pide confirmar las escrituras. Se van a confirmar{" "}
          <strong>
            {tx.statements} {plural(tx.statements, "sentencia", "sentencias")}
          </strong>{" "}
          que corrieron en esta pestaña desde el BEGIN. Lo que cambien no se deshace desde Kaname.
        </ConfirmDialog>
      ) : null}
      <Dialog
        open={guardando}
        title="Guardar la consulta"
        onClose={() => setGuardando(false)}
        footer={
          <>
            <DialogClose variant="ghost" onClose={() => setGuardando(false)}>
              Cancelar
            </DialogClose>
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

function metaDelPlan(p: Plan): string {
  switch (p.fase) {
    case "corriendo":
      return "pidiendo el plan…";
    case "error":
      return "no se pudo";
    case "listo":
      return `plan de la sentencia de la línea ${p.result.line} · no se ejecutó · ${p.elapsedMs} ms`;
    default:
      return "";
  }
}

/**
 * El plan de ejecución. Los cuatro motores lo devuelven como filas, pero no
 * con la misma forma: Postgres da UNA columna de texto donde la sangría es el
 * árbol —una grilla la pierde y corta cada línea en la primera palabra—, así
 * que eso va como texto tal cual. MySQL, MariaDB y SQLite dan una tabla, y
 * esa sí va en la grilla, con las columnas de texto más anchas que las de un
 * resultado común.
 */
function PanelDelPlan({
  plan,
  seleccion,
  onSelect,
  onOpenCell,
  onCancelar,
}: {
  plan: Plan;
  seleccion: CellRef | null;
  onSelect: (ref: CellRef | null) => void;
  onOpenCell: (ref: CellRef) => void;
  onCancelar: () => void;
}) {
  switch (plan.fase) {
    case "corriendo":
      return (
        <div className={styles.corriendo}>
          <Spinner />
          <div className={styles.corriendoTitulo}>Pidiendo el plan…</div>
          <button type="button" className={styles.cancel} onClick={onCancelar}>
            Cancelar
          </button>
        </div>
      );
    case "error":
      return <Mensajes estado={{ fase: "error", failure: plan.failure, batch: null }} />;
    case "listo": {
      const r = plan.result;
      if ((r.columns ?? []).length === 1) {
        return (
          <pre className={styles.planTexto}>
            {(r.rows ?? []).map((fila) => fila?.[0] ?? "").join("\n")}
          </pre>
        );
      }
      const anchos = anchosPorContenido(r);
      return (
        <DataGrid
          result={r}
          selection={seleccion}
          onSelect={onSelect}
          onOpenCell={onOpenCell}
          anchoDeColumna={(c, porDefecto) => anchos.get(c) ?? porDefecto}
        />
      );
    }
    default:
      return (
        <div className={styles.sinFilas}>
          <p className={styles.sinFilasTitulo}>Todavía no se pidió ningún plan</p>
          <p className={styles.sinFilasNota}>
            «Plan de ejecución» le pide al motor cómo va a correr la sentencia bajo el cursor,
            sin correrla. Un DELETE bajo el cursor no borra nada.
          </p>
        </div>
      );
  }
}

/**
 * Anchos de columna medidos por el contenido, para el plan tabular.
 *
 * La grilla los saca del tipo y no del contenido, porque una página de
 * resultados cambia al cargar más filas. Un plan no: son pocas filas y llegan
 * enteras, así que acá sí se mide lo que hay —el encabezado o el valor más
 * largo— y ni `select_type` ocupa media pantalla ni `Extra` se corta en la
 * primera palabra.
 */
function anchosPorContenido(r: Result): Map<Column, number> {
  const PX_POR_CARACTER = 7.4;
  const MARGEN = 28;
  // El encabezado lleva además la etiqueta del tipo («TXT») y el tirador.
  const MARGEN_ENCABEZADO = 64;
  const out = new Map<Column, number>();
  (r.columns ?? []).forEach((c, i) => {
    let px = c.name.length * PX_POR_CARACTER + MARGEN_ENCABEZADO;
    for (const fila of r.rows ?? []) {
      px = Math.max(px, (fila?.[i] ?? "[null]").length * PX_POR_CARACTER + MARGEN);
    }
    out.set(c, Math.min(640, Math.max(64, Math.ceil(px))));
  });
  return out;
}

/** «Corrieron 2 sentencias» / «No corrió ninguna», para el pie del error. */
function corridas(rs: readonly Result[] | null): string {
  const n = rs?.length ?? 0;
  if (n === 0) return "No corrió ninguna";
  return `${n === 1 ? "Corrió 1 sentencia" : `Corrieron ${n} sentencias`}`;
}
