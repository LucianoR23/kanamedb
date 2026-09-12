import { useEffect, useState } from "react";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import type { HistoryEntry, SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { SearchInput, Spinner } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cuando } from "../screens/HistoryPanel";
import { cx } from "../lib/cx";
import { BarraDeSesion } from "./Sesion";
import styles from "./mobile.module.css";

const LIMITE = 200;

/**
 * S21 en el teléfono: las consultas que se corrieron en este teléfono contra
 * esta conexión, filtrables. Tocar una la lleva a la pestaña SQL.
 */
export function Historial({
  sesion,
  onRepetir,
}: {
  sesion: SessionView;
  onRepetir: (sql: string) => void;
}) {
  const [entradas, setEntradas] = useState<HistoryEntry[] | null>(null);
  const [filtro, setFiltro] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelado = false;
    HistorySvc.List(LIMITE)
      .then((l) => {
        if (!cancelado) setEntradas((l ?? []).filter((e) => e.connectionId === sesion.connectionId));
      })
      .catch((err) => {
        if (!cancelado) setError(textoDe(err));
      });
    return () => {
      cancelado = true;
    };
  }, [sesion.connectionId]);

  const q = filtro.trim().toLowerCase();
  const visibles = (entradas ?? []).filter((e) => !q || e.sql.toLowerCase().includes(q));

  return (
    <>
      <BarraDeSesion sesion={sesion} titulo="Historial" />
      <main className={styles.cuerpo}>
        {error ? <div className={styles.error}>{error}</div> : null}
        {entradas === null && !error ? (
          <div className={styles.vacio}>
            <Spinner />
          </div>
        ) : null}
        {entradas && entradas.length > 0 ? (
          <SearchInput value={filtro} onChange={(e) => setFiltro(e.target.value)} placeholder="Buscar en el historial" aria-label="Buscar en el historial" />
        ) : null}
        {entradas && entradas.length === 0 ? (
          <div className={styles.vacio}>Todavía no corriste nada contra esta conexión desde el teléfono.</div>
        ) : null}
        <div className={styles.lista}>
          {visibles.map((e) => (
            <button key={e.id} type="button" className={styles.tarjeta} onClick={() => onRepetir(e.sql)}>
              <div className={styles.mono} style={{ color: "var(--text-1)" }}>
                {recortar(e.sql)}
              </div>
              <div className={styles.resumen}>
                <span>{cuando(e.ranAt)}</span>
                <span>· {e.elapsedMs} ms</span>
                {e.failed ? (
                  <span className={cx(styles.chip, styles.chipMal)}>falló</span>
                ) : (
                  <span>· {e.rows} {e.rows === 1 ? "fila" : "filas"}</span>
                )}
                {e.runs > 1 ? <span>· ×{e.runs}</span> : null}
              </div>
            </button>
          ))}
        </div>
      </main>
    </>
  );
}

function recortar(sql: string): string {
  const una = sql.replace(/\s+/g, " ").trim();
  return una.length > 240 ? una.slice(0, 240) + "…" : una;
}
