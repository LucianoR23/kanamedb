import { useEffect, useState } from "react";
import * as Queries from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import type { RunResult, SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { Button, Dialog, Input, Textarea } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cx } from "../lib/cx";
import { Tarjeta } from "./Tarjeta";
import { HojaDeValor } from "./HojaDeValor";
import type { CampoElegido } from "./HojaDeValor";
import { resaltarSql } from "./sql";
import { siLaSesionSeCerro } from "./sesionCerrada";
import styles from "./mobile.module.css";

// La consulta escrita sobrevive al cambio de pestaña: vive en el módulo, no en
// el componente. Es una por sesión de la app; no se guarda en disco.
let borrador = "";

/**
 * M12: el editor SQL del teléfono. Un área de texto en la cabecera —sin
 * CodeMirror: sus atajos son de teclado— con Guardar… y Ejecutar, y el
 * resultado como tarjetas abajo. Sin pestañas ni transacciones manuales. Las
 * escrituras escritas a mano pasan por la misma confirmación de producción
 * que en escritorio: la hace Go.
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
  onCorriendo,
}: {
  sesion: SessionView;
  /** Una consulta que el historial pide repetir. */
  inicial: string | null;
  onTomada: () => void;
  onSesionCerrada: (motivo: string) => void;
  onAviso: (t: ToastItem) => void;
  /** Para el punto en la pestaña: hay una consulta corriendo. */
  onCorriendo: (corriendo: boolean) => void;
}) {
  const [sql, setSql] = useState(borrador);
  const [corriendo, setCorriendo] = useState<{ cancelar: () => void; desde: number } | null>(null);
  const [transcurrido, setTranscurrido] = useState(0);
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

  // El contador mientras corre: es lo único que dice que la app no se colgó.
  useEffect(() => {
    onCorriendo(corriendo !== null);
    if (!corriendo) {
      setTranscurrido(0);
      return;
    }
    const desde = corriendo.desde;
    const id = window.setInterval(() => setTranscurrido(Date.now() - desde), 100);
    return () => window.clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [corriendo]);

  // Al desmontar —cambio de pestaña— la pestaña deja de estar «corriendo»
  // aunque Go siga: el punto es de esta pantalla, no de la consulta.
  useEffect(() => () => onCorriendo(false), [onCorriendo]);

  async function correr() {
    const texto = sql.trim();
    if (!texto || corriendo) return;
    setError("");
    setResultado(null);
    const runId = crypto.randomUUID();
    const pedido = Queries.Run(runId, texto);
    setCorriendo({ cancelar: () => void Queries.Cancel(runId).catch(() => {}), desde: Date.now() });
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
      onAviso({ id: `guardada-${Date.now()}`, tone: "success", title: `Consulta guardada · ${g.name}` });
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
  const fallo = resultado && !resultado.ok ? resultado.failure : null;
  const prod = sesion.environment === "production";

  return (
    <>
      <div className={cx(styles.cabecera, prod && styles.cabeceraProd)}>
        <Textarea
          className={cx(styles.editor, fallo && styles.editorMal)}
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
          readOnly={corriendo !== null}
        />
        <div className={styles.botonera}>
          <Button
            onClick={() => {
              setErrorGuardar("");
              setGuardando(true);
            }}
            disabled={!sql.trim() || corriendo !== null}
          >
            Guardar…
          </Button>
          {corriendo ? (
            <Button variant="danger" onClick={corriendo.cancelar}>
              Cancelar
            </Button>
          ) : (
            <Button variant="primary" onClick={() => void correr()} disabled={!sql.trim()}>
              Ejecutar
            </Button>
          )}
        </div>
      </div>

      {corriendo ? (
        <div className={styles.corriendo} role="status">
          <span className={styles.aro} />
          <span>corriendo · {(transcurrido / 1000).toFixed(1).replace(".", ",")} s</span>
          <p>Podés cancelar. Si la base ya empezó a escribir, cancelar no deshace lo hecho.</p>
        </div>
      ) : (
        <main className={cx(styles.cuerpo, styles.cuerpoGap12, styles.cuerpoArriba)}>
          {error ? (
            <div className={styles.errorTarjeta}>
              <div className={styles.errorTitulo}>No se pudo ejecutar</div>
              <div className={styles.errorTexto}>{error}</div>
            </div>
          ) : null}

          {fallo ? (
            <div className={styles.errorTarjeta}>
              <div className={styles.errorTitulo}>La consulta falló</div>
              <div className={styles.errorTexto}>{fallo.message}</div>
              {fallo.detail || fallo.sqlState ? (
                <pre className={styles.detalleMotor}>
                  {[fallo.detail, fallo.sqlState ? `SQLSTATE ${fallo.sqlState}` : ""].filter(Boolean).join("\n")}
                </pre>
              ) : null}
              {fallo.hint ? <div className={styles.sugerencia}>{fallo.hint}</div> : null}
            </div>
          ) : null}

          {resultado?.ok && resultado.batch ? (
            <>
              <div className={styles.resumen}>
                {actual ? (
                  actual.returnsRows ? (
                    <>
                      {resultado.batch.elapsedMs} ms · {(actual.rows ?? []).length} {(actual.rows ?? []).length === 1 ? "fila" : "filas"}
                      {actual.truncated ? <span>(cortado en {actual.rowLimit})</span> : null}
                    </>
                  ) : (
                    <>
                      {resultado.batch.elapsedMs} ms · {actual.command || "OK"}, {actual.affectedRows}{" "}
                      {actual.affectedRows === 1 ? "fila afectada" : "filas afectadas"}
                    </>
                  )
                ) : (
                  <>{resultado.batch.elapsedMs} ms</>
                )}
                {resultados.length > 1 ? (
                  <Button size="sm" variant="ghost" onClick={() => setCual((c) => (c + 1) % resultados.length)}>
                    sentencia {cual + 1} de {resultados.length} · siguiente
                  </Button>
                ) : null}
              </div>
              {actual?.returnsRows
                ? (actual.rows ?? []).map((r, i) => {
                    const fila = (r ?? []).map((v) => v ?? null);
                    return (
                      <Tarjeta
                        key={i}
                        columnas={actual.columns ?? []}
                        fila={fila}
                        onMantener={(c) => {
                          const col = actual.columns?.[c];
                          if (col) setCampo({ columna: col.name, valor: fila[c] ?? null, tipo: col.dataType });
                        }}
                      />
                    );
                  })
                : null}
              {actual?.returnsRows && (actual.rows ?? []).length === 0 ? (
                <div className={styles.vacio}>
                  <span className={styles.vacioTitulo}>Sin filas</span>
                  <span className={styles.vacioTexto}>La consulta corrió y no devolvió ninguna.</span>
                </div>
              ) : null}
            </>
          ) : null}

          {!resultado && !error ? (
            <div className={styles.vacio}>
              <span className={styles.vacioTexto}>Escribí una consulta y tocá Ejecutar. El resultado aparece acá, una fila por tarjeta.</span>
            </div>
          ) : null}
        </main>
      )}

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
          <div className={styles.columna}>
            <div className={styles.campo}>
              <span className={styles.etiqueta}>Nombre</span>
              <Input
                className={cx(styles.palabraEntrada, errorGuardar && styles.editorMal)}
                value={nombre}
                onChange={(e) => setNombre(e.target.value)}
                placeholder="ventas del mes"
                autoFocus
                autoCapitalize="off"
                aria-label="Nombre de la consulta"
                onKeyDown={(e) => {
                  if (e.key === "Enter") void guardar();
                }}
              />
            </div>
            <pre className={cx(styles.sqlBloque, styles.sqlApagado)}>{resaltarSql(sql.trim())}</pre>
            {errorGuardar ? (
              <div className={cx(styles.aviso, styles.avisoMal)}>
                <span>No se guarda: {errorGuardar}</span>
              </div>
            ) : null}
            <span className={styles.ayuda}>Queda en Historial › Guardadas de este teléfono.</span>
          </div>
        </Dialog>
      ) : null}
    </>
  );
}
