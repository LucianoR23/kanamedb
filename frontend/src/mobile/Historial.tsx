import { useEffect, useState } from "react";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import type { HistoryEntry, SavedQuery, SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button, ConfirmDialog, PillTabs, SearchInput } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cuando } from "../screens/HistoryPanel";
import { cx } from "../lib/cx";
import styles from "./mobile.module.css";

const LIMITE = 200;

type Modo = "corridas" | "guardadas";

const MODOS = [
  { id: "corridas", label: "Corridas" },
  { id: "guardadas", label: "Guardadas" },
] as const;

/**
 * M13: el historial en dos pestañas, con un control segmentado y buscador en
 * la cabecera. Corridas: lo que se corrió en este teléfono contra esta
 * conexión, una línea de SQL y la meta en mono; tocar repite en SQL.
 * Guardadas: las de nombre —las de esta conexión primero, después las de
 * otras—, que llegan de la PC con la libreta importada y se suman las de
 * acá; no hay sincronización de vuelta. Se borran desde acá, preguntando:
 * es trabajo con nombre, no un rastro.
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
  const prod = sesion.environment === "production";

  return (
    <>
      <div className={cx(styles.cabecera, prod && styles.cabeceraProd)}>
        <div className={styles.tituloCaja}>
          <span className={styles.titulo}>Historial</span>
          <span className={styles.subtitulo}>{sesion.describe}</span>
        </div>
        <div className={styles.segmentado}>
          <PillTabs items={MODOS} activeId={modo} onSelect={(id) => setModo(id as Modo)} ariaLabel="Qué historial" />
        </div>
        <SearchInput
          className={styles.busqueda}
          value={filtro}
          onChange={(e) => setFiltro(e.target.value)}
          placeholder={modo === "corridas" ? "Buscar en el historial" : "Buscar guardadas"}
          aria-label={modo === "corridas" ? "Buscar en el historial" : "Buscar guardadas"}
          disabled={!lista || lista.length === 0}
        />
      </div>

      <main className={cx(styles.cuerpo, styles.cuerpoGap10)}>
        {error ? (
          <div className={styles.errorTarjeta}>
            <div className={styles.errorTitulo}>No se pudo leer el historial</div>
            <div className={styles.errorTexto}>{error}</div>
          </div>
        ) : null}
        {cargando ? (
          <div className={styles.cargando}>
            <span className={cx(styles.aro, styles.aroChico)} />
            <span>leyendo…</span>
          </div>
        ) : null}

        {modo === "corridas" ? (
          <>
            {entradas && entradas.length === 0 ? (
              <div className={styles.vacio}>
                <span className={styles.vacioTitulo}>Nada todavía</span>
                <span className={styles.vacioTexto}>Lo que corras contra esta conexión desde el teléfono queda acá.</span>
              </div>
            ) : null}
            {(entradas ?? [])
              .filter((e) => !q || e.sql.toLowerCase().includes(q))
              .map((e) => (
                <button key={e.id} type="button" className={cx(styles.tarjeta, styles.tarjetaCorrida)} onClick={() => onRepetir(e.sql)}>
                  <span className={styles.sqlLinea}>{unaLinea(e.sql)}</span>
                  <div className={styles.meta}>
                    <span>
                      {cuando(e.ranAt)}
                      {e.failed ? "" : ` · ${e.elapsedMs} ms · ${e.rows} ${e.rows === 1 ? "fila" : "filas"}`}
                    </span>
                    {e.failed ? <span className={cx(styles.chip, styles.chipMal)}>falló</span> : null}
                    {e.runs > 1 ? <span className={styles.chip}>×{e.runs}</span> : null}
                  </div>
                </button>
              ))}
          </>
        ) : (
          <>
            {guardadas && guardadas.length === 0 ? (
              <div className={styles.vacio}>
                <span className={styles.vacioTitulo}>Ninguna guardada</span>
                <span className={styles.vacioTexto}>En la pestaña SQL, «Guardar…» le pone nombre a la que tengas escrita.</span>
              </div>
            ) : null}
            {(guardadas ?? [])
              .filter((g) => !q || g.name.toLowerCase().includes(q) || g.sql.toLowerCase().includes(q))
              .map((g) => (
                <div key={g.id} className={cx(styles.tarjeta, styles.tarjetaGuardada)}>
                  <div className={styles.fila1}>
                    <span className={cx(styles.nombre, styles.nombreGuardada)}>{g.name}</span>
                    {g.connectionId && g.connectionId !== sesion.connectionId ? <span className={cx(styles.chip, styles.chipSuave)}>de otra conexión</span> : null}
                  </div>
                  <span className={styles.sqlParrafo}>{unaLinea(g.sql)}</span>
                  <div className={styles.acciones}>
                    <Button className={cx(styles.fijo, styles.borrar)} onClick={() => setBorrando(g)}>
                      Borrar…
                    </Button>
                    <Button onClick={() => onRepetir(g.sql)}>Abrir en SQL</Button>
                  </div>
                </div>
              ))}
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

function unaLinea(sql: string): string {
  const una = sql.replace(/\s+/g, " ").trim();
  return una.length > 240 ? una.slice(0, 240) + "…" : una;
}
