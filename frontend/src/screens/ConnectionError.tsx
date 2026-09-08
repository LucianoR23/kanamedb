import { useState } from "react";
import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Badge, Button, Dialog } from "../components/ui";
import styles from "./ConnectionError.module.css";

/** Un fallo de conexión ya interpretado, más el contexto para mostrarlo. */
export interface ConnectionFailure {
  connection: ConnectionView;
  kind: string;
  /** Qué pasó, en castellano. */
  message: string;
  /** Qué hacer al respecto. Vacío si no hay nada útil que decir. */
  hint: string;
  /** Lo que dijo el motor, redactado. Es la frase que otro va a reconocer. */
  detail: string;
  sqlState: string;
  /** Cuánto tardó en fallar. Un timeout de 10 s se ve distinto de un rechazo
   *  inmediato, y eso ya dice algo. */
  elapsedMs: number;
}

/** Qué hay que revisar según la causa. Cada una se arregla en un lugar distinto. */
const CULPRIT: Record<string, string> = {
  auth: "el usuario y la contraseña",
  database: "el nombre de la base",
  network: "el host y el puerto",
  timeout: "el host y el puerto",
  tls: "el modo SSL",
  permission: "los permisos del usuario en el servidor",
  tunnel: "el bastión SSH: el túnel se cerró y hay que reabrirlo",
};

/**
 * S24, variante "connection error".
 *
 * Sigue el orden del diseño: primero lo que dijo el motor, después qué
 * significa, después qué hacer. El texto crudo va arriba y en mono porque es la
 * única frase que alguien más va a reconocer, y la que se pega en un ticket.
 */
export function ConnectionError({
  failure,
  onClose,
  onRetry,
  onEdit,
}: {
  failure: ConnectionFailure;
  onClose: () => void;
  onRetry: () => void;
  onEdit: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const culprit = CULPRIT[failure.kind];

  function copyDetails() {
    const texto = [
      failure.connection.connection.name,
      failure.connection.uri,
      failure.sqlState ? `SQLSTATE ${failure.sqlState}` : "",
      failure.detail,
      failure.message,
    ]
      .filter(Boolean)
      .join("\n");
    void navigator.clipboard.writeText(texto).then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    });
  }

  return (
    <Dialog
      open
      title="No se pudo conectar"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={copyDetails}>
            {copied ? "Copiado" : "Copiar detalles"}
          </Button>
          <span style={{ flex: 1 }} />
          <Button onClick={onEdit}>Editar la conexión</Button>
          <Button variant="primary" onClick={onRetry}>
            Reintentar
          </Button>
        </>
      }
    >
      <div className={styles.target}>
        <span className={styles.name}>{failure.connection.connection.name}</span>
        <span className={styles.describe}>{failure.connection.uri}</span>
        <span className={styles.elapsed}>
          falló tras {(failure.elapsedMs / 1000).toFixed(1)} s
          {failure.sqlState ? " · " : ""}
          {failure.sqlState ? <Badge tone="danger">{failure.sqlState}</Badge> : null}
        </span>
      </div>

      {failure.detail ? <div className={styles.detail}>{failure.detail}</div> : null}

      <p className={styles.message}>{failure.message}</p>

      {failure.hint ? <p className={styles.hint}>{failure.hint}</p> : null}

      {culprit ? (
        <p className={styles.culprit}>
          Lo que hay que revisar es <strong>{culprit}</strong>.
        </p>
      ) : null}
    </Dialog>
  );
}
