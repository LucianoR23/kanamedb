import { useEffect, useState } from "react";
import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button } from "../components/ui";
import type { ConnectionFailure } from "../screens/ConnectionError";
import { cx } from "../lib/cx";
import styles from "./mobile.module.css";

/**
 * M04 A: la espera mientras se abre una conexión, a pantalla completa y
 * cancelable. El contador no es decoración: es lo que distingue «está
 * tardando» de «se colgó», y por un bastión son varios segundos.
 */
export function Conectando({ view, onCancelar }: { view: ConnectionView; onCancelar: () => void }) {
  const [transcurrido, setTranscurrido] = useState(0);
  useEffect(() => {
    const desde = Date.now();
    const id = window.setInterval(() => setTranscurrido(Date.now() - desde), 100);
    return () => window.clearInterval(id);
  }, []);
  const c = view.connection;
  const bastion = c.ssh.enabled ? `${c.ssh.user}@${c.ssh.host}:${c.ssh.port}` : "";

  return (
    <div className={styles.pantalla}>
      <div className={styles.centro}>
        <span className={styles.aro} />
        <div>
          {/* La región viva es solo el título: el contador cambia diez veces
              por segundo y TalkBack lo leería sin parar. */}
          <span className={styles.centroTitulo} role="status" aria-live="polite">
            Conectando a {c.name}
          </span>
          <span className={styles.centroMono}>{view.uri}</span>
          {bastion ? <span className={styles.centroMono}>por {bastion}</span> : null}
          <span className={styles.centroTiempo}>{(transcurrido / 1000).toFixed(1)} s</span>
        </div>
      </div>
      <div className={styles.pie}>
        <Button onClick={onCancelar}>Cancelar</Button>
      </div>
    </div>
  );
}

/** Qué hay que revisar según la causa. Cada una se arregla en un lugar distinto. */
const CULPABLE: Record<string, string> = {
  auth: "Revisá las credenciales guardadas para esta conexión.",
  database: "Revisá el nombre de la base en la conexión exportada.",
  network: "Puede ser que estés fuera de la VPN o que el servidor esté caído.",
  timeout: "Puede ser que estés fuera de la VPN o que el servidor esté caído.",
  tls: "El modo SSL de la conexión no coincide con lo que acepta el servidor.",
  permission: "El usuario no tiene permiso en el servidor.",
  tunnel: "El bastión SSH cerró el túnel; hay que reabrirlo.",
};

/**
 * M04 B/C: el fallo de conexión, a pantalla completa. Primero qué pasó en
 * castellano, después lo que dijo el motor en mono —la frase que otro
 * reconoce y la que se pega en un ticket—, y las tres salidas: arreglar las
 * credenciales, volver o reintentar. El botón principal cambia según la
 * causa: con un error de autenticación lo primero es ir a Credenciales; con
 * uno de red, reintentar.
 */
export function ErrorDeConexion({
  failure,
  onVolver,
  onReintentar,
  onCredenciales,
}: {
  failure: ConnectionFailure;
  onVolver: () => void;
  onReintentar: () => void;
  onCredenciales: () => void;
}) {
  const c = failure.connection.connection;
  const bastion = c.ssh.enabled ? `\nvía ${c.ssh.user}@${c.ssh.host}:${c.ssh.port}` : "";
  const detalle = [failure.detail, failure.sqlState ? `SQLSTATE ${failure.sqlState}` : "", `\n${failure.connection.uri}${bastion}`, `después de ${(failure.elapsedMs / 1000).toFixed(1)} s`]
    .filter(Boolean)
    .join("\n");
  const porCredenciales = failure.kind === "auth";
  const pista = failure.hint || CULPABLE[failure.kind] || "";

  return (
    <div className={styles.pantalla}>
      <main className={cx(styles.cuerpo, styles.cuerpoCentrado)}>
        <div className={styles.errorTarjeta}>
          <div className={styles.errorTitulo}>No se pudo conectar</div>
          <div className={styles.errorTexto}>
            {failure.message}
            {pista ? ` ${pista}` : ""}
          </div>
          <pre className={styles.detalleMotor}>{detalle}</pre>
        </div>

        <div className={styles.columna}>
          {porCredenciales ? (
            <Button variant="primary" className={styles.grande} onClick={onCredenciales}>
              Credenciales
            </Button>
          ) : (
            <Button variant="primary" className={styles.grande} onClick={onReintentar}>
              Reintentar
            </Button>
          )}
          <div className={styles.par}>
            <Button onClick={onVolver}>Volver</Button>
            {porCredenciales ? <Button onClick={onReintentar}>Reintentar</Button> : <Button onClick={onCredenciales}>Credenciales</Button>}
          </div>
        </div>
      </main>
    </div>
  );
}
