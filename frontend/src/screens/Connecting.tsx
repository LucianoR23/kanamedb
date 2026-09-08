import { useEffect, useState } from "react";
import { Spinner } from "../components/ui";
import styles from "./Connecting.module.css";

/**
 * Lo que se ve mientras se abre una conexión.
 *
 * Existe porque el túnel lo hizo necesario: conectar directo tarda
 * milisegundos y nadie extraña una pantalla de carga, pero por un bastión son
 * varios segundos —TCP, handshake SSH, autenticación, y recién ahí Postgres— y
 * sin nada en pantalla la aplicación parece colgada.
 *
 * El contador no es decoración: es lo que distingue "está tardando" de "se
 * colgó". Sin él, a los diez segundos la única información disponible es la
 * paciencia de quien mira.
 */
export function Connecting({
  target,
  bastion,
  onCancel,
}: {
  /** A dónde se conecta, sin secretos: usuario@host:puerto/base. */
  target: string;
  /** El bastión, si la conexión usa túnel. Vacío si va directo. */
  bastion: string;
  onCancel: () => void;
}) {
  const [transcurrido, setTranscurrido] = useState(0);

  useEffect(() => {
    const desde = Date.now();
    const id = window.setInterval(() => setTranscurrido(Date.now() - desde), 100);
    return () => window.clearInterval(id);
  }, []);

  return (
    <div className={styles.scrim} role="status" aria-live="polite">
      <div className={styles.panel}>
        <Spinner />
        <p className={styles.titulo}>
          {bastion ? "Abriendo el túnel y conectando…" : "Conectando…"}
        </p>
        {bastion ? <p className={styles.paso}>por {bastion}</p> : null}
        <p className={styles.destino}>{target}</p>
        <p className={styles.tiempo}>{(transcurrido / 1000).toFixed(1)} s</p>
        <button type="button" className={styles.cancelar} onClick={onCancel}>
          Cancelar
        </button>
      </div>
    </div>
  );
}
