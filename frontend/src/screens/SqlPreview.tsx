import { useEffect, useRef, useState } from "react";
import type {
  ApplyProgress,
  ApplyResult,
  ChangesetView,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { Button, CopyButton, Dialog, Input } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./SqlPreview.module.css";

/** Cada cuánto se pregunta por dónde va el apply. */
const CADENCIA_MS = 180;

/**
 * S15 SQL preview and apply.
 *
 * Muestra el guion completo —el mismo que se copia y se guarda— y lo aplica.
 * Nada de lo que se ve acá se arma en el frontend: la SQL, el orden y los avisos
 * los escribió Go, porque citar identificadores es específico del motor.
 *
 * El progreso se CONSULTA mientras el apply corre. No hay eventos en este
 * proyecto, y un `ALTER` que lee doce mil filas no puede ser una espera sin
 * información.
 */
export function SqlPreview({
  vista,
  singleTransaction,
  onClose,
  onApplied,
  onFalloParcial,
}: {
  vista: ChangesetView;
  singleTransaction: boolean;
  onClose: () => void;
  onApplied: () => void;
  /** Falló, pero alguna sentencia quedó aplicada. Sin transacción es lo normal
   *  y hay que releer igual: el esquema cambió aunque el apply no terminara. */
  onFalloParcial: () => void;
}) {
  const [confirmacion, setConfirmacion] = useState("");
  const [corriendo, setCorriendo] = useState(false);
  const [progreso, setProgreso] = useState<ApplyProgress | null>(null);
  const [resultado, setResultado] = useState<ApplyResult | null>(null);
  const [error, setError] = useState("");
  const timer = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (timer.current !== null) window.clearInterval(timer.current);
    };
  }, []);

  const r = vista.summary;
  const palabra = vista.confirmWord ?? "";
  const confirmado = !vista.needsConfirmation || confirmacion.trim() === palabra;
  const puedeAplicar = confirmado && !vista.readOnly && r.included > 0 && !corriendo;

  async function aplicar() {
    if (!puedeAplicar) return;
    setCorriendo(true);
    setError("");
    setResultado(null);

    timer.current = window.setInterval(() => {
      void SessionSvc.ApplyStatus().then(setProgreso);
    }, CADENCIA_MS);

    try {
      const res = await SessionSvc.Apply({
        singleTransaction,
        confirm: confirmacion.trim(),
      });
      setResultado(res);
      if (res.ok) {
        onApplied();
      } else if (!res.rolledBack && (res.results ?? []).some((r) => r.applied)) {
        onFalloParcial();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (timer.current !== null) window.clearInterval(timer.current);
      timer.current = null;
      setProgreso(null);
      setCorriendo(false);
    }
  }

  const destructivos = (vista.order ?? []).filter((v) => v.statement.destructive);
  const bloqueantes = (vista.order ?? []).filter(
    (v) => v.statement.lock === "all" && !v.statement.destructive,
  );

  return (
    <Dialog
      open
      size="xl"
      title={`Aplicar ${r.included} ${r.included === 1 ? "sentencia" : "sentencias"}`}
      onClose={corriendo ? () => {} : onClose}
    >
      <div className={cx(styles.marco, vista.needsConfirmation && styles.produccion)}>
        <div className={styles.izquierda}>
          <div className={styles.barraSql}>
            <span className={styles.meta}>
              {contarLineas(vista.script)} líneas · {r.included} sentencias
            </span>
            <span className={styles.grow} />
            <span className={styles.txn}>
              {singleTransaction ? "una transacción · revierte si algo falla" : "sin transacción"}
            </span>
            <CopyButton text={vista.script} />
          </div>

          <div className={styles.codigo}>
            {vista.script.split("\n").map((linea, i) => (
              <div key={i} className={cx(styles.linea, claseDeLinea(linea))}>
                <span className={styles.n}>{linea.trim() === "" ? "" : i + 1}</span>
                <span className={styles.texto}>{linea === "" ? " " : linea}</span>
              </div>
            ))}
          </div>

          {corriendo ? (
            <div className={styles.corriendo}>
              <div className={styles.corriendoFila}>
                <span className={styles.spinner} />
                <span className={styles.corriendoTitulo}>
                  Aplicando {(progreso?.completed ?? 0) + 1} de {progreso?.total ?? r.included}
                </span>
                <span className={styles.grow} />
                <span className={styles.meta}>
                  {((progreso?.elapsedMs ?? 0) / 1000).toFixed(1)} s
                </span>
              </div>
              <div className={styles.progreso}>
                <span
                  className={styles.progresoRelleno}
                  style={{
                    width: `${Math.round(
                      (100 * (progreso?.completed ?? 0)) / Math.max(1, progreso?.total ?? 1),
                    )}%`,
                  }}
                />
              </div>
              {progreso?.current ? (
                <div className={styles.actual}>{primeraLinea(progreso.current)}</div>
              ) : null}
            </div>
          ) : null}

          {resultado ? <Resultado res={resultado} transaccion={singleTransaction} /> : null}
          {error ? <p className={styles.error}>{error}</p> : null}
        </div>

        <aside className={styles.derecha}>
          <div className={styles.panelCuerpo}>
            <section>
              <div className={styles.seccionTitulo}>Qué hace</div>
              <dl className={styles.datos}>
                <dt>Sentencias</dt>
                <dd>{r.included}</dd>
                <dt>Cambios de esquema</dt>
                <dd>{r.schema}</dd>
                <dt>Filas que se leen</dt>
                <dd className={filasLeidas(vista) > 0 ? styles.aviso : undefined}>
                  {filasLeidas(vista) > 0
                    ? filasLeidas(vista).toLocaleString("es", { useGrouping: true })
                    : "ninguna"}
                </dd>
                <dt>Destructivas</dt>
                <dd className={r.destructive > 0 ? styles.peligro : undefined}>{r.destructive}</dd>
              </dl>
              <p className={styles.sinEstimacion}>
                No hay estimación de tiempo: depende del tamaño de las tablas y de qué más esté
                corriendo en el servidor. Inventar un número sería peor que no darlo.
              </p>
            </section>

            {destructivos.length > 0 || bloqueantes.length > 0 ? (
              <section>
                <div className={styles.seccionTitulo}>Lo que hay que mirar</div>
                <div className={styles.tarjetas}>
                  {destructivos.map((v) => (
                    <div key={v.change.id} className={cx(styles.tarjeta, styles.tarjetaPeligro)}>
                      <div className={styles.tarjetaCabeza}>
                        <span className={cx(styles.chip, styles.chipPeligro)}>DESTRUCTIVA</span>
                        <span className={styles.tarjetaObjeto}>
                          {v.change.table}
                          {v.change.column?.name ? `.${v.change.column.name}` : ""}
                        </span>
                      </div>
                      <p className={styles.tarjetaTexto}>{v.statement.note}</p>
                    </div>
                  ))}
                  {bloqueantes.map((v) => (
                    <div key={v.change.id} className={cx(styles.tarjeta, styles.tarjetaAviso)}>
                      <div className={styles.tarjetaCabeza}>
                        <span className={cx(styles.chip, styles.chipAviso)}>BLOQUEA</span>
                        <span className={styles.tarjetaObjeto}>
                          {v.change.table}
                          {v.rowEstimate >= 0
                            ? ` · ${v.rowEstimate.toLocaleString("es", { useGrouping: true })} filas`
                            : ""}
                        </span>
                      </div>
                      <p className={styles.tarjetaTexto}>{v.statement.note}</p>
                    </div>
                  ))}
                </div>
              </section>
            ) : null}

            {vista.needsConfirmation ? (
              <section className={styles.confirmar}>
                <div className={styles.confirmarTitulo}>
                  <span className={styles.bang}>!</span>
                  Esto es producción
                </div>
                <p className={styles.confirmarTexto}>
                  Escribí el nombre de la base para habilitar el botón. Los cambios de esquema de
                  este conjunto no se deshacen.
                </p>
                <Input
                  value={confirmacion}
                  placeholder={palabra}
                  aria-label={`Escribí ${palabra} para confirmar`}
                  onChange={(e) => setConfirmacion(e.currentTarget.value)}
                  className={confirmado ? styles.okInput : styles.malInput}
                />
              </section>
            ) : null}

            {(vista.warnings ?? []).length > 0 ? (
              <section>
                <div className={styles.seccionTitulo}>Antes de aplicar</div>
                <ul className={styles.avisos}>
                  {(vista.warnings ?? []).map((w) => (
                    <li key={w}>{w}</li>
                  ))}
                </ul>
              </section>
            ) : null}

            <p className={styles.sinRollback}>
              No se genera un guion para deshacer. Si querés dejar registro de lo que corrió,
              copiá la SQL antes de aplicar.
            </p>
          </div>

          <div className={styles.acciones}>
            <Button
              variant={vista.needsConfirmation ? "danger" : "primary"}
              disabled={!puedeAplicar}
              loading={corriendo}
              onClick={() => void aplicar()}
            >
              {vista.needsConfirmation ? `Aplicar en ${palabra}` : `Aplicar ${r.included}`}
            </Button>
            <Button variant="secondary" size="sm" disabled={corriendo} onClick={onClose}>
              Volver a los cambios
            </Button>
          </div>
        </aside>
      </div>
    </Dialog>
  );
}

/**
 * Cómo terminó el apply.
 *
 * Cuando falla, el orden de lo que se cuenta no es casual: primero QUÉ QUEDÓ
 * —revertido, aplicado a medias—, porque es lo que decide qué hacer ahora;
 * después por qué falló, después cómo arreglarlo, y al final la sentencia y el
 * texto original del motor, que es lo que se pega en un ticket.
 */
function Resultado({ res, transaccion }: { res: ApplyResult; transaccion: boolean }) {
  if (res.ok) {
    return (
      <div className={cx(styles.resultado, styles.resultadoOk)}>
        Se aplicaron {(res.results ?? []).length} sentencias en {(res.elapsedMs / 1000).toFixed(1)} s.
      </div>
    );
  }
  const hechas = (res.results ?? []).filter((r) => r.applied).length;
  const fallada = (res.results ?? []).find((r) => !r.applied);
  return (
    <div className={cx(styles.resultado, styles.resultadoMal)}>
      <p className={styles.resultadoTitulo}>
        {res.rolledBack
          ? "Falló y se revirtió todo: la base quedó como estaba."
          : hechas === 0
            ? "Falló en la primera sentencia: no quedó nada aplicado."
            : `Falló a la mitad. Las ${hechas} sentencias anteriores YA quedaron aplicadas y ` +
              "salieron de la lista."}
      </p>
      {!res.rolledBack && hechas > 0 && !transaccion ? (
        <p className={styles.resultadoParcial}>
          Con «Una sola transacción» esto no habría pasado: se habría revertido todo.
        </p>
      ) : null}
      {res.failure ? <p className={styles.resultadoMsg}>{res.failure.message}</p> : null}
      {res.failure?.hint ? <p className={styles.resultadoHint}>{res.failure.hint}</p> : null}
      {fallada ? <pre className={styles.resultadoSql}>{fallada.sql}</pre> : null}
      {res.failure?.detail ? (
        <p className={styles.resultadoDetalle}>
          {res.failure.sqlState ? (
            <span className={styles.resultadoCodigo}>{res.failure.sqlState}</span>
          ) : null}
          {res.failure.detail}
        </p>
      ) : null}
    </div>
  );
}

/** Los comentarios del guion llevan el aviso; se pintan según lo que dicen. */
function claseDeLinea(linea: string): string | undefined {
  if (!linea.startsWith("--")) return undefined;
  if (linea.includes("DESTRUCTIVA")) return styles.lineaPeligro;
  if (linea.includes("bloquea") || linea.includes("reescribe") || linea.includes("lee la tabla")) {
    return styles.lineaAviso;
  }
  return styles.lineaComentario;
}

function contarLineas(s: string): number {
  return s === "" ? 0 : s.trimEnd().split("\n").length;
}

function primeraLinea(s: string): string {
  const l = s.split("\n")[0] ?? "";
  return l.length > 90 ? l.slice(0, 90) + "…" : l;
}

/** Cuántas filas van a leerse en total, sumando las sentencias que las recorren. */
function filasLeidas(vista: ChangesetView): number {
  let n = 0;
  for (const v of vista.order ?? []) {
    if (v.rowEstimate > 0) n += v.rowEstimate;
  }
  return n;
}
