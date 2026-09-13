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
import { BarraDeSesion } from "./Sesion";
import type { Valor } from "./Tarjeta";
import { HojaDeValor } from "./HojaDeValor";
import type { CampoElegido } from "./HojaDeValor";
import { useMantener } from "./useMantener";
import { siLaSesionSeCerro } from "./sesionCerrada";
import styles from "./mobile.module.css";

/**
 * Una fila entera, y la edición por fila del teléfono.
 *
 * Ver es gratis, y mantener apretado un valor lo muestra entero con «Copiar».
 * Editar, agregar y borrar pasan por el mismo camino que la grilla de
 * escritorio, sin atajos: `StageGrid` convierte la edición en cambios del
 * changeset —Go decide la clave y el orden—, la palabra de producción se pide
 * si Go la pide, y después se muestra el SQL y se aplica con `Apply`, con la
 * huella de lo que se mostró. El changeset del teléfono es siempre esta fila y
 * nada más: se descarta antes de preparar y después de un fallo.
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
  const [error, setError] = useState("");
  // El valor que alguien mantuvo apretado, para verlo entero y copiarlo.
  const [campo, setCampo] = useState<CampoElegido | null>(null);
  const mantener = useMantener((el) => {
    const i = Number(el.dataset["mantener"]);
    const c = columnas[i];
    const v = valorDe(i);
    if (c && v !== undefined) setCampo({ columna: c.name, valor: v });
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
  const titulo = nueva ? `Nueva fila · ${tabla.name}` : tabla.name;

  return (
    <>
      <BarraDeSesion sesion={sesion} titulo={titulo} atras={onVolver} />
      <main className={styles.cuerpo}>
        {error ? <div className={styles.error}>{error}</div> : null}
        {stage.error ? <div className={styles.error}>{stage.error}</div> : null}

        <div className={styles.detalle} {...mantener}>
          {columnas.map((c, i) => {
            const m = meta.get(c.name);
            const esClave = clave.has(c.name);
            const v = valorDe(i);
            const editado = cambios.has(i);
            // La clave no se edita desde acá: cambiarla es otra fila. Y una
            // columna generada o identity la escribe la base.
            const bloqueada = (esClave && !nueva) || Boolean(m?.autoIncrement && nueva);
            return (
              <div key={c.name} className={styles.campo}>
                <label>
                  <span className={cx(esClave && styles.pk)} style={esClave ? { color: "var(--erd-pk)" } : undefined}>
                    {c.name}
                  </span>
                  <span>{c.dataType}</span>
                  {m && !m.nullable ? <span>NOT NULL</span> : null}
                </label>
                {editando && !bloqueada ? (
                  <>
                    <Textarea
                      rows={1}
                      value={v ?? ""}
                      disabled={v === null}
                      placeholder={v === undefined ? "(valor por defecto)" : v === null ? "NULL" : ""}
                      onChange={(e) => poner(i, e.target.value)}
                      spellCheck={false}
                      autoCapitalize="off"
                    />
                    <div className={styles.resumen}>
                      {m?.nullable !== false ? (
                        <Button size="sm" variant={v === null ? "secondary" : "ghost"} onClick={() => poner(i, v === null ? "" : null)}>
                          {v === null ? "NULL ✓" : "NULL"}
                        </Button>
                      ) : null}
                      {nueva && m?.hasDefault ? (
                        <Button size="sm" variant={v === undefined ? "secondary" : "ghost"} onClick={() => poner(i, undefined)}>
                          por defecto
                        </Button>
                      ) : null}
                      {editado && !nueva ? (
                        <Button size="sm" variant="ghost" onClick={() => poner(i, fila ? (fila[i] ?? null) : undefined)}>
                          deshacer
                        </Button>
                      ) : null}
                    </div>
                  </>
                ) : (
                  <div className={cx(styles.valor, editado && styles.valorEditado)} data-mantener={i}>
                    {v === undefined ? (
                      <span className={styles.nulo}>(por defecto)</span>
                    ) : v === null ? (
                      <span className={styles.nulo}>NULL</span>
                    ) : (
                      v
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
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
        etiqueta="Borrar…"
        onClose={() => setBorrando(false)}
        onConfirm={borrar}
      >
        Se va a preparar un <code>DELETE</code> de esta fila de <code>{tabla.name}</code>. Antes de
        aplicarlo vas a ver el SQL. Una fila borrada no se recupera desde Kaname.
      </ConfirmDialog>

      {previa ? (
        <Dialog
          open
          title={resultado ? "No se aplicó" : "Aplicar"}
          abrupto={previa.production}
          {...(aplicando ? {} : { onClose: cancelarPrevia })}
          footer={
            resultado ? (
              <Button onClick={() => setPrevia(null)}>Cerrar</Button>
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
          <div className={styles.detalle}>
            {previa.production ? (
              <div className={styles.error}>Esto es producción.</div>
            ) : null}
            <pre className={styles.script}>{previa.script}</pre>
            {(previa.warnings ?? []).map((w) => (
              <div key={w} className={styles.aviso}>
                {w}
              </div>
            ))}
            {previa.needsConfirmation && !resultado ? (
              <div className={styles.campo}>
                <label>
                  Escribí <code>{previa.confirmWord}</code> para aplicar
                </label>
                <Input value={palabra} onChange={(e) => setPalabra(e.target.value)} autoCapitalize="off" autoCorrect="off" />
              </div>
            ) : null}
            {resultado ? (
              <div className={styles.error}>
                {resultado.failure?.message ??
                  (resultado.results ?? []).find((r) => r.error)?.error ??
                  "La base rechazó el cambio."}
                {resultado.rolledBack ? " No quedó nada aplicado." : ""}
              </div>
            ) : null}
            {error ? <div className={styles.error}>{error}</div> : null}
          </div>
        </Dialog>
      ) : null}
    </>
  );
}
