import { useEffect, useState } from "react";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import type { HistoryEntry, SavedQuery, SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button, ConfirmDialog, PillTabs, SearchInput, Spinner } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cuando } from "../screens/HistoryPanel";
import { cx } from "../lib/cx";
import { BarraDeSesion } from "./Sesion";
import styles from "./mobile.module.css";

const LIMITE = 200;

type Modo = "corridas" | "guardadas";

const MODOS = [
  { id: "corridas", label: "Corridas" },
  { id: "guardadas", label: "Guardadas" },
] as const;

/**
 * S21 en el teléfono, en dos pestañas: las consultas que se corrieron en
 * este teléfono contra esta conexión, y las guardadas con nombre —las de esta
 * conexión primero, después las de otras—. Las guardadas llegan de la PC con
 * la libreta que se importa y se suman las que se guardan acá; no hay
 * sincronización de vuelta. Tocar una la lleva a la pestaña SQL. Se borran
 * desde acá, preguntando: es trabajo con nombre, no un rastro.
 */
export function Historial({
  sesion,
  onRepetir,
}: {
  sesion: SessionView;
  onRepetir: (sql: string) => void;
}) {
  const [modo, setModo] = useState<Modo>("corridas");
  const [entradas, setEntradas] = useState<HistoryEntry[] | null>(null);
  const [guardadas, setGuardadas] = useState<SavedQuery[] | null>(null);
  const [filtro, setFiltro] = useState("");
  const [error, setError] = useState("");
  const [borrando, setBorrando] = useState<SavedQuery | null>(null);

  useEffect(() => {
    let cancelado = false;
    setError("");
    const pedido =
      modo === "corridas"
        ? HistorySvc.List(LIMITE).then((l) => {
            if (!cancelado) setEntradas((l ?? []).filter((e) => e.connectionId === sesion.connectionId));
          })
        : HistorySvc.Saved().then((l) => {
            if (!cancelado) setGuardadas(l ?? []);
          });
    pedido.catch((err) => {
      if (!cancelado) setError(textoDe(err));
    });
    return () => {
      cancelado = true;
    };
  }, [modo, sesion.connectionId]);

  async function borrar(q: SavedQuery) {
    setBorrando(null);
    try {
      await HistorySvc.DeleteSaved(q.id);
      setGuardadas((prev) => (prev ?? []).filter((x) => x.id !== q.id));
    } catch (err) {
      setError(textoDe(err));
    }
  }

  const q = filtro.trim().toLowerCase();
  const lista = modo === "corridas" ? entradas : guardadas;
  const cargando = lista === null && !error;

  return (
    <>
      <BarraDeSesion sesion={sesion} titulo="Historial" />
      <main className={styles.cuerpo}>
        <PillTabs items={MODOS} activeId={modo} onSelect={(id) => setModo(id as Modo)} ariaLabel="Qué historial" />
        {error ? <div className={styles.error}>{error}</div> : null}
        {cargando ? (
          <div className={styles.vacio}>
            <Spinner />
          </div>
        ) : null}
        {lista && lista.length > 0 ? (
          <SearchInput
            value={filtro}
            onChange={(e) => setFiltro(e.target.value)}
            placeholder={modo === "corridas" ? "Buscar en el historial" : "Buscar una guardada"}
            aria-label={modo === "corridas" ? "Buscar en el historial" : "Buscar una guardada"}
          />
        ) : null}

        {modo === "corridas" ? (
          <>
            {entradas && entradas.length === 0 ? (
              <div className={styles.vacio}>Todavía no corriste nada contra esta conexión desde el teléfono.</div>
            ) : null}
            <div className={styles.lista}>
              {(entradas ?? [])
                .filter((e) => !q || e.sql.toLowerCase().includes(q))
                .map((e) => (
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
          </>
        ) : (
          <>
            {guardadas && guardadas.length === 0 ? (
              <div className={styles.vacio}>
                Todavía no hay consultas guardadas. En la pestaña SQL, «Guardar…» le pone nombre a la que tengas escrita.
              </div>
            ) : null}
            <div className={styles.lista}>
              {(guardadas ?? [])
                .filter((g) => !q || g.name.toLowerCase().includes(q) || g.sql.toLowerCase().includes(q))
                .map((g) => (
                  <div key={g.id} className={styles.tarjeta}>
                    <div className={styles.fila1}>
                      <strong>{g.name}</strong>
                      {g.connectionId && g.connectionId !== sesion.connectionId ? <span className={styles.chip}>de otra conexión</span> : null}
                    </div>
                    <div className={styles.mono}>{recortar(g.sql)}</div>
                    <div className={styles.acciones}>
                      <Button variant="dangerOutline" onClick={() => setBorrando(g)}>
                        Borrar…
                      </Button>
                      <Button variant="primary" onClick={() => onRepetir(g.sql)}>
                        Abrir en SQL
                      </Button>
                    </div>
                  </div>
                ))}
            </div>
          </>
        )}
      </main>

      <ConfirmDialog
        open={borrando !== null}
        title="Borrar la consulta guardada"
        severidad="aviso"
        etiqueta="Borrar"
        onClose={() => setBorrando(null)}
        onConfirm={() => {
          if (borrando) void borrar(borrando);
        }}
      >
        Se borra <strong>{borrando?.name}</strong> de las guardadas de este teléfono. El texto no se recupera desde
        Kaname.
      </ConfirmDialog>
    </>
  );
}

function recortar(sql: string): string {
  const una = sql.replace(/\s+/g, " ").trim();
  return una.length > 240 ? una.slice(0, 240) + "…" : una;
}
