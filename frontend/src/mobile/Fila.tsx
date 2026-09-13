import { useState } from "react";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type {
  ApplyResult,
  ChangesetView,
  GridEdits,
  SessionView,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Table } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { Button, ConfirmDialog, Dialog, Input, Textarea } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { useStage } from "../lib/useStage";
import { cx } from "../lib/cx";
import { Barra } from "./Barra";
import type { Valor } from "./Tarjeta";
import { HojaDeValor } from "./HojaDeValor";
import type { CampoElegido } from "./HojaDeValor";
import { useMantener } from "./useMantener";
import { resaltarSql } from "./sql";
import { siLaSesionSeCerro } from "./sesionCerrada";
import styles from "./mobile.module.css";

/**
 * M10: una fila entera, y la edición por fila del teléfono. Un bloque por
 * columna: nombre, tipo y NOT NULL en la etiqueta; el valor en una caja mono.
 * La clave y las columnas que escribe la base se ven bloqueadas.
 *
 * Ver es gratis, y mantener apretado un valor lo muestra entero con «Copiar».
 * Editar, agregar y borrar pasan por el mismo camino que la grilla de
 * escritorio, sin atajos: `StageGrid` convierte la edición en cambios del
 * changeset —Go decide la clave y el orden—, la palabra de producción se pide
 * si Go la pide, y después se muestra el SQL (M11) y se aplica con `Apply`,
 * con la huella de lo que se mostró. El changeset del teléfono es siempre esta
 * fila y nada más: se descarta antes de preparar y después de un fallo.
 */
export function Fila({
  sesion,
  esquema,
  tabla,
  columnas,
  clave,
  fila,
  onVolver,
  onAplicado,
  onSesionCerrada,
  onAviso,
}: {
  sesion: SessionView;
  esquema: string;
  tabla: Table;
  columnas: Column[];
  clave: ReadonlySet<string>;
  /** null es una fila nueva. */
  fila: Valor[] | null;
  onVolver: () => void;
  onAplicado: (mensaje: string) => void;
  onSesionCerrada: (motivo: string) => void;
  onAviso: (t: ToastItem) => void;
}) {
  const nueva = fila === null;
  const puedeEditar = !sesion.readOnly && clave.size > 0;
  const [editando, setEditando] = useState(nueva);
  // Lo editado, por índice de columna. En una fila nueva, lo que no está toma
  // el valor por defecto de la tabla.
  const [cambios, setCambios] = useState<Map<number, Valor>>(new Map());
  const [borrando, setBorrando] = useState(false);
  const [previa, setPrevia] = useState<ChangesetView | null>(null);
  const [palabra, setPalabra] = useState("");
  const [aplicando, setAplicando] = useState(false);
  const [resultado, setResultado] = useState<ApplyResult | null>(null);
  // Después de un fallo, «Ver el SQL» vuelve a mostrar lo que se intentó.
  const [verSql, setVerSql] = useState(false);
  const [error, setError] = useState("");
  // El valor que alguien mantuvo apretado, para verlo entero y copiarlo.
  const [campo, setCampo] = useState<CampoElegido | null>(null);
  const mantener = useMantener((el) => {
    const i = Number(el.dataset["mantener"]);
    const c = columnas[i];
    const v = valorDe(i);
    if (c && v !== undefined) setCampo({ columna: c.name, valor: v, tipo: c.dataType });
  });

  const meta = new Map((tabla.columns ?? []).map((c) => [c.name, c]));
  const nombres = columnas.map((c) => c.name);

  function valorDe(i: number): Valor | undefined {
    if (cambios.has(i)) return cambios.get(i) ?? null;
    return fila ? (fila[i] ?? null) : undefined;
  }

  function poner(i: number, v: Valor | undefined) {
    setCambios((prev) => {
      const next = new Map(prev);
      const original = fila ? (fila[i] ?? null) : undefined;
      if (v === original || (v === undefined && !fila)) next.delete(i);
      else if (v === undefined) next.delete(i);
      else next.set(i, v);
      return next;
    });
  }

  const stage = useStage(() => {});

  /** Prepara la edición en Go y abre la vista previa del SQL. */
  async function preparar(e: GridEdits) {
    setError("");
    try {
      // El changeset del teléfono es esta fila: lo que hubiera es de un
      // intento anterior que no terminó.
      await SessionSvc.DiscardChanges();
    } catch (err) {
      if (!(await siLaSesionSeCerro(onSesionCerrada))) setError(textoDe(err));
      return;
    }
    await stage.stageGrid(e, () => {
      void SessionSvc.Changeset()
        .then((v) => {
          setPalabra("");
          setResultado(null);
          setVerSql(false);
          setPrevia(v);
        })
        .catch((err) => setError(textoDe(err)));
    });
  }

  function guardar() {
    const base: GridEdits = {
      schema: esquema,
      table: tabla.name,
      columns: nombres,
      key: [...clave],
    };
    if (nueva) {
      const values: Record<string, Valor> = {};
      for (const [i, v] of cambios) {
        const n = nombres[i];
        if (n !== undefined) values[n] = v;
      }
      void preparar({ ...base, inserts: [{ values }] });
      return;
    }
    const after: Record<string, Valor> = {};
    for (const [i, v] of cambios) {
      const n = nombres[i];
      if (n !== undefined) after[n] = v;
    }
    void preparar({ ...base, updates: [{ before: [...(fila ?? [])], after }] });
  }

  function borrar() {
    setBorrando(false);
    void preparar({
      schema: esquema,
      table: tabla.name,
      columns: nombres,
      key: [...clave],
      deletes: [[...(fila ?? [])]],
    });
  }

  async function aplicar() {
    if (!previa) return;
    setAplicando(true);
    setError("");
    try {
      const res = await SessionSvc.Apply({
        singleTransaction: true,
        confirm: palabra.trim(),
        // La huella de lo que se está mostrando: Go la recalcula sobre lo que
        // va a ejecutar y no aplica si difiere.
        fingerprint: previa.fingerprint,
      });
      if (res.ok) {
        setPrevia(null);
        onAplicado(nueva ? "Fila agregada" : previa.summary.destructive > 0 ? "Fila borrada" : "Fila actualizada");
        return;
      }
      setResultado(res);
      // Lo que falló no se queda esperando en el changeset: la próxima
      // edición arranca limpia.
      await SessionSvc.DiscardChanges().catch(() => {});
    } catch (err) {
      if (!(await siLaSesionSeCerro(onSesionCerrada))) setError(textoDe(err));
    } finally {
      setAplicando(false);
    }
  }

  function cancelarPrevia() {
    setPrevia(null);
    void SessionSvc.DiscardChanges().catch(() => {});
  }

  const hayCambios = cambios.size > 0;
  // «id = 10482»: la fila, dicha por su clave.
  const resumenClave = fila
    ? columnas
        .map((c, i) => (clave.has(c.name) ? `${c.name} = ${fila[i] ?? "NULL"}` : null))
        .filter((s): s is string => s !== null)
        .join(", ")
    : "";
  const prod = sesion.environment === "production";

  return (
    <div className={styles.pantalla}>
      {nueva ? (
        <Barra titulo="Nueva fila" subtitulo={tabla.name} atras={onVolver} prod={prod} />
      ) : (
        <Barra
          titulo={tabla.name}
          mono
          subtitulo={editando ? `editando · ${resumenClave}` : resumenClave}
          subtituloAcento={editando}
          atras={onVolver}
          prod={prod}
        />
      )}

      <main className={cx(styles.cuerpo, styles.cuerpoGap16)} {...mantener}>
        {error ? (
          <div className={cx(styles.aviso, styles.avisoMal)}>
            <span>{error}</span>
          </div>
        ) : null}
        {stage.error ? (
          <div className={cx(styles.aviso, styles.avisoMal)}>
            <span>{stage.error}</span>
          </div>
        ) : null}

        {columnas.map((c, i) => {
          const m = meta.get(c.name);
          const esClave = clave.has(c.name);
          const v = valorDe(i);
          const editado = cambios.has(i);
          const autoincremental = Boolean(m?.autoIncrement);
          // La clave no se edita desde acá: cambiarla es otra fila. Y una
          // columna generada o identity la escribe la base.
          const bloqueada = (esClave && !nueva) || (autoincremental && nueva);
          const notNull = m ? !m.nullable : false;
          return (
            <div key={c.name} className={styles.campo}>
              <div className={styles.campoEtiqueta}>
                <span className={cx(styles.campoNombre, esClave && styles.campoNombrePk)}>{c.name}</span>
                <span className={styles.campoTipo}>{c.dataType}</span>
                {editando && bloqueada ? (
                  <span className={styles.campoChip}>{autoincremental && nueva ? "autoincremental" : "bloqueada"}</span>
                ) : notNull ? (
                  <span className={cx(styles.campoNotNull, nueva && !m?.hasDefault && styles.campoRequerido)}>not null</span>
                ) : null}
              </div>

              {editando && !bloqueada ? (
                <>
                  <Textarea
                    rows={1}
                    className={cx(styles.entrada, editado && styles.entradaEditada, esLargo(c.dataType) && styles.entradaLarga)}
                    value={v ?? ""}
                    disabled={v === null}
                    placeholder={v === undefined ? (m?.hasDefault ? "por defecto" : "vacío") : v === null ? "NULL" : ""}
                    onChange={(e) => poner(i, e.target.value)}
                    spellCheck={false}
                    autoCapitalize="off"
                    aria-label={c.name}
                  />
                  <div className={styles.miniBotones}>
                    {m?.nullable !== false ? (
                      <Button size="sm" className={cx(styles.miniMono, v === null && styles.elegido)} aria-pressed={v === null} onClick={() => poner(i, v === null ? "" : null)}>
                        NULL
                      </Button>
                    ) : null}
                    {nueva && m?.hasDefault ? (
                      <Button size="sm" className={cx(v === undefined && styles.elegido)} aria-pressed={v === undefined} onClick={() => poner(i, undefined)}>
                        por defecto
                      </Button>
                    ) : null}
                    {editado && !nueva ? (
                      <Button size="sm" className={styles.deshacer} onClick={() => poner(i, fila ? (fila[i] ?? null) : undefined)}>
                        deshacer
                      </Button>
                    ) : null}
                  </div>
                </>
              ) : (
                <div
                  className={cx(
                    styles.valor,
                    esClave && styles.valorPk,
                    (editando && bloqueada) && styles.valorBloqueado,
                    editado && styles.valorEditado,
                    v === null && styles.nulo,
                    (v === "" || v === undefined) && styles.vacioValor,
                  )}
                  {...(editando ? {} : { "data-mantener": i })}
                >
                  {v === undefined ? (autoincremental ? "lo pone la base" : "por defecto") : v === null ? "NULL" : v === "" ? "vacío" : v}
                </div>
              )}
            </div>
          );
        })}
      </main>

      {puedeEditar ? (
        <div className={styles.pie}>
          {editando ? (
            <>
              <Button
                onClick={() => {
                  if (nueva) onVolver();
                  else {
                    setCambios(new Map());
                    setEditando(false);
                  }
                }}
              >
                Cancelar
              </Button>
              <Button variant="primary" onClick={guardar} disabled={!hayCambios}>
                {nueva ? "Agregar…" : "Guardar…"}
              </Button>
            </>
          ) : (
            <>
              <Button variant="dangerOutline" onClick={() => setBorrando(true)}>
                Borrar…
              </Button>
              <Button variant="primary" onClick={() => setEditando(true)}>
                Editar
              </Button>
            </>
          )}
        </div>
      ) : null}

      {stage.dialogo}
      <HojaDeValor campo={campo} onCerrar={() => setCampo(null)} onAviso={onAviso} />

      <ConfirmDialog
        open={borrando}
        title="Borrar esta fila"
        severidad="aviso"
        etiqueta="Continuar"
        onClose={() => setBorrando(false)}
        onConfirm={borrar}
      >
        Vas a borrar la fila <code>{resumenClave}</code> de <code>{tabla.name}</code>. En la pantalla siguiente vas
        a ver el SQL antes de aplicarlo. Una fila borrada no se recupera desde Kaname.
      </ConfirmDialog>

      {previa ? (
        <Dialog
          open
          title={resultado ? "No se aplicó" : "Aplicar"}
          production={previa.production}
          abrupto={previa.production}
          {...(aplicando ? {} : { onClose: cancelarPrevia })}
          footer={
            resultado ? (
              <>
                <Button onClick={() => setPrevia(null)}>Cerrar</Button>
                <Button variant="primary" onClick={() => setVerSql((v) => !v)}>
                  {verSql ? "Ver el error" : "Ver el SQL"}
                </Button>
              </>
            ) : (
              <>
                <Button onClick={cancelarPrevia} disabled={aplicando}>
                  Cancelar
                </Button>
                <Button
                  variant={previa.production ? "danger" : "primary"}
                  onClick={() => void aplicar()}
                  loading={aplicando}
                  disabled={previa.needsConfirmation && palabra.trim() !== (previa.confirmWord ?? "")}
                >
                  Aplicar
                </Button>
              </>
            )
          }
        >
          <div className={styles.columna}>
            {resultado && !verSql ? (
              <>
                <p className={styles.hojaTexto}>
                  La base rechazó el cambio.{resultado.rolledBack ? " No quedó nada aplicado." : ""}
                </p>
                <pre className={styles.detalleMotor}>
                  {resultado.failure?.message ?? (resultado.results ?? []).find((r) => r.error)?.error ?? "Sin detalle."}
                </pre>
              </>
            ) : (
              <>
                <p className={styles.hojaTexto}>
                  Se va a ejecutar esto contra <code>{sesion.describe}</code>.
                </p>
                <pre className={styles.sqlBloque}>{resaltarSql(previa.script)}</pre>
                {(previa.warnings ?? []).map((w) => (
                  <div key={w} className={styles.aviso}>
                    <span>{w}</span>
                  </div>
                ))}
                {previa.needsConfirmation && !resultado ? (
                  <div className={styles.campo}>
                    <span className={styles.palabra}>
                      Escribí <code>{previa.confirmWord}</code> para aplicar
                    </span>
                    <Input
                      className={styles.palabraEntrada}
                      value={palabra}
                      onChange={(e) => setPalabra(e.target.value)}
                      autoCapitalize="off"
                      autoCorrect="off"
                      aria-label="Palabra de confirmación"
                    />
                  </div>
                ) : null}
              </>
            )}
            {error ? (
              <div className={cx(styles.aviso, styles.avisoMal)}>
                <span>{error}</span>
              </div>
            ) : null}
          </div>
        </Dialog>
      ) : null}
    </div>
  );
}

/** Texto, JSON y compañía: la entrada arranca alta, para no escribir en una ranura. */
function esLargo(tipo: string): boolean {
  const t = tipo.toLowerCase();
  return t.includes("text") || t.includes("json") || t.includes("xml");
}
