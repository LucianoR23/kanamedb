import { useEffect, useState } from "react";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Snapshot, Table } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Button, SearchInput } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cx } from "../lib/cx";
import { Tabla } from "./Tabla";
import styles from "./mobile.module.css";

/**
 * M06: el esquema como lista de tablas, con buscador en la cabecera. Cada
 * tabla es una tarjeta con su nombre en mono y chips de ~filas, columnas y
 * «sin clave primaria». Tocar una abre sus filas. Los objetos de texto
 * —vistas, funciones— no entran: son de escritorio.
 */
export function Tablas({
  sesion,
  onSesionCerrada,
  onAviso,
  onProfundidad,
}: {
  sesion: SessionView;
  onSesionCerrada: (motivo: string) => void;
  onAviso: (t: ToastItem) => void;
  /** Con una tabla abierta la pantalla es de ella: la navegación se esconde (M07). */
  onProfundidad: (abierta: boolean) => void;
}) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [error, setError] = useState("");
  const [filtro, setFiltro] = useState("");
  const [abierta, setAbierta] = useState<{ esquema: string; tabla: Table } | null>(null);
  // Cambia para volver a leer después de un error.
  const [intento, setIntento] = useState(0);

  useEffect(() => {
    onProfundidad(abierta !== null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [abierta]);

  useEffect(() => {
    let cancelado = false;
    setError("");
    setSnapshot(null);
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
  }, [intento]);

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
  const prod = sesion.environment === "production";

  return (
    <>
      <div className={cx(styles.cabecera, prod && styles.cabeceraProd)}>
        <div className={styles.tituloCaja}>
          <span className={styles.titulo}>Tablas</span>
          <span className={styles.subtitulo}>
            {sesion.describe}
            {sesion.readOnly ? " · solo lectura" : ""}
          </span>
        </div>
        {!error ? (
          <SearchInput
            className={styles.busqueda}
            value={filtro}
            onChange={(e) => setFiltro(e.target.value)}
            placeholder="Buscar tabla"
            aria-label="Buscar tabla"
            disabled={snapshot === null}
          />
        ) : null}
      </div>

      {error ? (
        <main className={cx(styles.cuerpo, styles.cuerpoCentrado)}>
          <div className={styles.errorTarjeta}>
            <div className={styles.errorTitulo}>No se pudo leer el esquema</div>
            <pre className={styles.detalleMotor}>{error}</pre>
          </div>
          <Button variant="primary" className={styles.grande} onClick={() => setIntento((n) => n + 1)}>
            Reintentar
          </Button>
        </main>
      ) : snapshot === null ? (
        <main className={cx(styles.cuerpo, styles.cuerpoGap10)} aria-busy="true">
          <div className={styles.cargando}>
            <span className={cx(styles.aro, styles.aroChico)} />
            <span>leyendo el esquema…</span>
          </div>
          <div className={cx(styles.esqueleto, styles.esqueletoTabla)} />
          <div className={cx(styles.esqueleto, styles.esqueletoTabla)} />
          <div className={cx(styles.esqueleto, styles.esqueletoTabla)} />
          <div className={cx(styles.esqueleto, styles.esqueletoTabla)} />
        </main>
      ) : (
        <main className={cx(styles.cuerpo, styles.cuerpoGap10)}>
          {esquemas.map((e) => {
            const tablas = (e.tables ?? []).filter((t) => !q || t.name.toLowerCase().includes(q));
            if (tablas.length === 0) return null;
            return (
              <section key={e.name} className={cx(styles.columna, styles.gap10)}>
                {varios ? (
                  <div className={styles.seccion}>
                    <strong>{e.name}</strong>
                    <span>
                      {tablas.length} {tablas.length === 1 ? "tabla" : "tablas"}
                    </span>
                  </div>
                ) : null}
                {tablas.map((t) => (
                  <button
                    key={t.name}
                    type="button"
                    className={cx(styles.tarjeta, styles.tarjetaTabla)}
                    onClick={() => setAbierta({ esquema: e.name, tabla: t })}
                  >
                    <span className={cx(styles.nombre, styles.nombreMono)}>{t.name}</span>
                    <div className={styles.chips}>
                      <span className={styles.chip}>{t.rowEstimate >= 0 ? `~${formatear(t.rowEstimate)} filas` : "filas: ?"}</span>
                      <span className={styles.chip}>{(t.columns ?? []).length} columnas</span>
                      {!t.hasPrimaryKey ? <span className={cx(styles.chip, styles.chipAviso)}>sin clave primaria</span> : null}
                    </div>
                    {t.comment ? <span className={styles.comentario}>{t.comment}</span> : null}
                  </button>
                ))}
              </section>
            );
          })}
          {esquemas.length === 0 ? (
            <div className={styles.vacio}>
              <span className={styles.vacioTitulo}>No hay tablas</span>
              <span className={styles.vacioTexto}>Esta base no tiene tablas visibles para este usuario.</span>
            </div>
          ) : null}
        </main>
      )}
    </>
  );
}

function formatear(n: number): string {
  return new Intl.NumberFormat("es-AR").format(n);
}
