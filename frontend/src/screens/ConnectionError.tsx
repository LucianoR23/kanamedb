import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Badge, Button, Dialog } from "../components/ui";
import styles from "./ConnectionError.module.css";

/** Un fallo de conexión ya interpretado, más la conexión que lo produjo. */
export interface ConnectionFailure {
  connection: ConnectionView;
  kind: string;
  message: string;
  hint: string;
  sqlState: string;
}

/** Qué campo del editor hay que arreglar según la causa. */
const CULPRIT: Record<string, string> = {
  auth: "el usuario y la contraseña",
  database: "el nombre de la base",
  network: "el host y el puerto",
  timeout: "el host y el puerto",
  tls: "el modo SSL",
  permission: "los permisos del usuario en el servidor",
};

/**
 * S24, variante "connection error".
 *
 * Existe para que el trabajo de clasificar el fallo no termine en un toast
 * genérico: un host mal escrito, una contraseña vieja y un firewall se arreglan
 * en lugares distintos, y el diálogo lleva directo al que corresponde.
 */
export function ConnectionError({
  failure,
  onClose,
  onEdit,
}: {
  failure: ConnectionFailure;
  onClose: () => void;
  onEdit: () => void;
}) {
  const culprit = CULPRIT[failure.kind];
  const arreglable = failure.kind !== "other";

  return (
    <Dialog
      open
      title="No se pudo conectar"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>Cerrar</Button>
          <Button variant="primary" onClick={onEdit}>
            {arreglable ? "Editar la conexión" : "Abrir el editor"}
          </Button>
        </>
      }
    >
      <div className={styles.target}>
        <span className={styles.name}>{failure.connection.connection.name}</span>
        <span className={styles.describe}>{failure.connection.uri}</span>
      </div>

      <p className={styles.message}>{failure.message}</p>

      {failure.hint ? <p className={styles.hint}>{failure.hint}</p> : null}

      {culprit ? (
        <p className={styles.culprit}>
          Lo que hay que revisar es <strong>{culprit}</strong>.
        </p>
      ) : null}

      {failure.sqlState ? (
        <div className={styles.code}>
          <span className={styles.codeLabel}>Código del motor</span>
          <Badge tone="danger">{failure.sqlState}</Badge>
        </div>
      ) : null}
    </Dialog>
  );
}
