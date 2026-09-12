import { useEffect, useState } from "react";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Snapshot, Table } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { SearchInput, Spinner } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { BarraDeSesion } from "./Sesion";
import { Tabla } from "./Tabla";
import styles from "./mobile.module.css";

/**
 * S05 en el teléfono: el esquema como lista de tablas, con buscador. Tocar
 * una abre sus filas. Los objetos de texto —vistas, funciones— no entran:
 * son de escritorio.
 */
export function Tablas({
  sesion,
  onSesionCerrada,
  onAviso,
}: {
  sesion: SessionView;
  onSesionCerrada: (motivo: string) => void;
  onAviso: (t: ToastItem) => void;
}) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [error, setError] = useState("");
  const [filtro, setFiltro] = useState("");
  const [abierta, setAbierta] = useState<{ esquema: string; tabla: Table } | null>(null);

  useEffect(() => {
    let cancelado = false;
    SessionSvc.Schema(false)
      .then((s) => {
        if (!cancelado) setSnapshot(s);
      })
      .catch((err) => {
        if (!cancelado) setError(textoDe(err));
      });
    return () => {
      cancelado = true;
    };
  }, []);

  if (abierta) {
    return (
      <Tabla
        sesion={sesion}
        esquema={abierta.esquema}
        tabla={abierta.tabla}
        onVolver={() => setAbierta(null)}
        onSesionCerrada={onSesionCerrada}
        onAviso={onAviso}
      />
    );
  }

  const esquemas = (snapshot?.schemas ?? []).filter((e) => (e.tables ?? []).length > 0);
  const varios = esquemas.length > 1;
  const q = filtro.trim().toLowerCase();

  return (
    <>
      <BarraDeSesion sesion={sesion} />
      <main className={styles.cuerpo}>
        {error ? <div className={styles.error}>{error}</div> : null}
        {snapshot === null && !error ? (
          <div className={styles.vacio}>
            <Spinner />
            <span>Leyendo el esquema…</span>
          </div>
        ) : null}
        {snapshot ? (
          <SearchInput
            value={filtro}
            onChange={(e) => setFiltro(e.target.value)}
            placeholder="Buscar tabla"
            aria-label="Buscar tabla"
          />
        ) : null}
        {esquemas.map((e) => {
          const tablas = (e.tables ?? []).filter((t) => !q || t.name.toLowerCase().includes(q));
          if (tablas.length === 0) return null;
          return (
            <section key={e.name} className={styles.lista}>
              {varios ? (
                <div className={styles.seccion}>
                  <span>{e.name}</span>
                  <span>{tablas.length}</span>
                </div>
              ) : null}
              {tablas.map((t) => (
                <button
                  key={t.name}
                  type="button"
                  className={styles.tarjeta}
                  onClick={() => setAbierta({ esquema: e.name, tabla: t })}
                >
                  <div className={styles.fila1}>
                    <strong>{t.name}</strong>
                    <span className={styles.chip}>
                      {t.rowEstimate >= 0 ? `~${formatear(t.rowEstimate)} filas` : "filas: ?"}
                    </span>
                  </div>
                  <div className={styles.resumen}>
                    <span>{(t.columns ?? []).length} columnas</span>
                    {!t.hasPrimaryKey ? <span className={styles.chipMal + " " + styles.chip}>sin clave primaria</span> : null}
                    {t.comment ? <span>· {t.comment}</span> : null}
                  </div>
                </button>
              ))}
            </section>
          );
        })}
        {snapshot && esquemas.length === 0 ? <div className={styles.vacio}>No hay tablas.</div> : null}
      </main>
    </>
  );
}

function formatear(n: number): string {
  return new Intl.NumberFormat("es-AR").format(n);
}
