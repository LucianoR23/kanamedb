import { useEffect, useState } from "react";
import * as Queries from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import type { RunResult, SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { Button, Dialog, Input, Textarea } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { BarraDeSesion } from "./Sesion";
import { Tarjeta } from "./Tarjeta";
import { HojaDeValor } from "./HojaDeValor";
import type { CampoElegido } from "./HojaDeValor";
import { siLaSesionSeCerro } from "./sesionCerrada";
import styles from "./mobile.module.css";

// La consulta escrita sobrevive al cambio de pestaña: vive en el módulo, no en
// el componente. Es una por sesión de la app; no se guarda en disco.
let borrador = "";

/**
 * S06 en el teléfono: un área de texto y el resultado como tarjetas. Sin
 * CodeMirror —sus atajos son de teclado—, sin pestañas, sin transacciones
 * manuales. Las escrituras escritas a mano pasan por la misma confirmación de
 * producción que en escritorio: la hace Go.
 *
 * «Guardar…» le pone nombre a lo escrito y lo deja en las consultas guardadas
 * de este teléfono, las mismas que se ven en Historial › Guardadas junto a las
 * que llegaron de la PC. Go rechaza guardar una consulta con una contraseña
 * escrita adentro, igual que en escritorio.
 */
export function Consulta({
  sesion,
  inicial,
  onTomada,
  onSesionCerrada,
  onAviso,
}: {
  sesion: SessionView;
  /** Una consulta que el historial pide repetir. */
  inicial: string | null;
  onTomada: () => void;
  onSesionCerrada: (motivo: string) => void;
  onAviso: (t: ToastItem) => void;
}) {
  const [sql, setSql] = useState(borrador);
  const [corriendo, setCorriendo] = useState<{ cancelar: () => void } | null>(null);
  const [resultado, setResultado] = useState<RunResult | null>(null);
  const [cual, setCual] = useState(0);
  const [error, setError] = useState("");
  // El diálogo de «Guardar…»: el nombre, si está guardando y qué falló.
  const [guardando, setGuardando] = useState(false);
  const [nombre, setNombre] = useState("");
  const [enviando, setEnviando] = useState(false);
  const [errorGuardar, setErrorGuardar] = useState("");
  // El campo de una tarjeta que alguien mantuvo apretado.
  const [campo, setCampo] = useState<CampoElegido | null>(null);

  useEffect(() => {
    if (inicial === null) return;
    setSql(inicial);
    borrador = inicial;
    onTomada();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inicial]);

  async function correr() {
    const texto = sql.trim();
    if (!texto || corriendo) return;
    setError("");
    setResultado(null);
    const runId = crypto.randomUUID();
    const pedido = Queries.Run(runId, texto);
    setCorriendo({ cancelar: () => void Queries.Cancel(runId).catch(() => {}) });
    try {
      const res = await pedido;
      if (!res.ok && (await siLaSesionSeCerro(onSesionCerrada))) return;
      setResultado(res);
      const resultados = res.batch?.results ?? [];
      // Se muestra el último que devuelve filas; si ninguno, el último.
      let i = resultados.length - 1;
      for (let j = resultados.length - 1; j >= 0; j--) {
        if (resultados[j]?.returnsRows) {
          i = j;
          break;
        }
      }
      setCual(Math.max(0, i));
    } catch (err) {
      if (!(await siLaSesionSeCerro(onSesionCerrada))) setError(textoDe(err));
    } finally {
      setCorriendo(null);
    }
  }

  async function guardar() {
    const texto = sql.trim();
    if (!nombre.trim() || !texto || enviando) return;
    setEnviando(true);
    setErrorGuardar("");
    try {
      const g = await HistorySvc.Save({ id: "", name: nombre, sql: texto, savedAt: "" });
      setGuardando(false);
      setNombre("");
      onAviso({ id: `guardada-${Date.now()}`, tone: "success", title: "Consulta guardada", detail: g.name });
    } catch (err) {
      // Entero: el error que importa —«lleva una contraseña escrita»— es la
      // explicación, no un detalle técnico.
      setErrorGuardar(textoDe(err));
    } finally {
      setEnviando(false);
    }
  }

  const resultados = resultado?.batch?.results ?? [];
  const actual: Result | undefined = resultados[cual];

  return (
    <>
      <BarraDeSesion sesion={sesion} titulo="SQL" />
      <main className={styles.cuerpo}>
        <Textarea
          className={styles.sql}
          value={sql}
          onChange={(e) => {
            setSql(e.target.value);
            borrador = e.target.value;
          }}
          placeholder="select * from …"
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          aria-label="Consulta SQL"
        />
        <div className={styles.acciones}>
          <Button
            onClick={() => {
              setErrorGuardar("");
              setGuardando(true);
            }}
            disabled={!sql.trim()}
          >
            Guardar…
          </Button>
          {corriendo ? (
            <Button variant="dangerOutline" onClick={corriendo.cancelar}>
              Cancelar
            </Button>
          ) : (
            <Button variant="primary" onClick={() => void correr()} disabled={!sql.trim()}>
              Ejecutar
            </Button>
          )}
        </div>

        {error ? <div className={styles.error}>{error}</div> : null}

        {resultado && !resultado.ok ? (
          <div className={styles.error}>
            {resultado.failure?.message ?? "La consulta falló."}
            {resultado.failure?.detail ? <div className={styles.mono}>{resultado.failure.detail}</div> : null}
            {resultado.failure?.hint ? <div className={styles.nota}>{resultado.failure.hint}</div> : null}
          </div>
        ) : null}

        {resultado?.ok && resultado.batch ? (
          <>
            <div className={styles.resumen}>
              <span>{resultado.batch.elapsedMs} ms</span>
              {resultados.length > 1 ? (
                <span>
                  · sentencia {cual + 1} de {resultados.length}{" "}
                  <Button size="sm" variant="ghost" onClick={() => setCual((c) => (c + 1) % resultados.length)}>
                    siguiente
                  </Button>
                </span>
              ) : null}
              {actual ? (
                actual.returnsRows ? (
                  <span>
                    · {(actual.rows ?? []).length} {(actual.rows ?? []).length === 1 ? "fila" : "filas"}
                    {actual.truncated ? ` (cortado en ${actual.rowLimit})` : ""}
                  </span>
                ) : (
                  <span>
                    · {actual.command || "OK"}, {actual.affectedRows} {actual.affectedRows === 1 ? "fila afectada" : "filas afectadas"}
                  </span>
                )
              ) : null}
            </div>
            {actual?.returnsRows ? (
              <div className={styles.lista}>
                {(actual.rows ?? []).map((r, i) => {
                  const fila = (r ?? []).map((v) => v ?? null);
                  return (
                    <Tarjeta
                      key={i}
                      columnas={actual.columns ?? []}
                      fila={fila}
                      onMantener={(c) => {
                        const col = actual.columns?.[c];
                        if (col) setCampo({ columna: col.name, valor: fila[c] ?? null });
                      }}
                    />
                  );
                })}
              </div>
            ) : null}
          </>
        ) : null}
      </main>

      <HojaDeValor campo={campo} onCerrar={() => setCampo(null)} onAviso={onAviso} />

      {guardando ? (
        <Dialog
          open
          title="Guardar la consulta"
          {...(enviando ? {} : { onClose: () => setGuardando(false) })}
          footer={
            <>
              <Button onClick={() => setGuardando(false)} disabled={enviando}>
                Cancelar
              </Button>
              <Button variant="primary" onClick={() => void guardar()} loading={enviando} disabled={!nombre.trim()}>
                Guardar
              </Button>
            </>
          }
        >
          <div className={styles.detalle}>
            <div className={styles.campo}>
              <label>Nombre</label>
              <Input
                value={nombre}
                onChange={(e) => setNombre(e.target.value)}
                placeholder="ventas del mes"
                autoFocus
                autoCapitalize="off"
                onKeyDown={(e) => {
                  if (e.key === "Enter") void guardar();
                }}
              />
            </div>
            <pre className={styles.script}>{sql.trim()}</pre>
            <span className={styles.nota}>Queda en Historial › Guardadas de este teléfono.</span>
            {errorGuardar ? <div className={styles.error}>{errorGuardar}</div> : null}
          </div>
        </Dialog>
      ) : null}
    </>
  );
}
