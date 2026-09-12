import { useEffect, useState } from "react";
import * as Queries from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { RunResult, SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { Button, Textarea } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { BarraDeSesion } from "./Sesion";
import { Tarjeta } from "./Tarjeta";
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
 */
export function Consulta({
  sesion,
  inicial,
  onTomada,
  onSesionCerrada,
}: {
  sesion: SessionView;
  /** Una consulta que el historial pide repetir. */
  inicial: string | null;
  onTomada: () => void;
  onSesionCerrada: (motivo: string) => void;
}) {
  const [sql, setSql] = useState(borrador);
  const [corriendo, setCorriendo] = useState<{ cancelar: () => void } | null>(null);
  const [resultado, setResultado] = useState<RunResult | null>(null);
  const [cual, setCual] = useState(0);
  const [error, setError] = useState("");

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
                {(actual.rows ?? []).map((r, i) => (
                  <Tarjeta key={i} columnas={actual.columns ?? []} fila={(r ?? []).map((v) => v ?? null)} />
                ))}
              </div>
            ) : null}
          </>
        ) : null}
      </main>
    </>
  );
}
