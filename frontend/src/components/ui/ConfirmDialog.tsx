import { useState } from "react";
import { Button } from "./Button";
import { Dialog } from "./Dialog";
import { Input } from "./Input";
import { cx } from "../../lib/cx";
import styles from "./ConfirmDialog.module.css";

/** Cuán grave es lo que se está por hacer. */
export type Severidad = "normal" | "aviso" | "produccion";

/**
 * Confirmación antes de algo que no se deshace.
 *
 * La severidad sube con el entorno, como en S00: normal para lo que se puede
 * rehacer, aviso para lo que toca datos, y producción para lo que además exige
 * escribir el nombre de la base a mano.
 *
 * Escribir el nombre no es una molestia decorativa: es lo único que distingue
 * «leí y decidí» de «apreté enter en el diálogo de siempre». Por eso el botón
 * queda deshabilitado hasta que coincida, en vez de solo avisar.
 */
export function ConfirmDialog({
  open,
  title,
  severidad = "normal",
  confirmar,
  palabra,
  etiqueta = "Confirmar",
  onConfirm,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  severidad?: Severidad;
  /** Exige escribir `palabra` para habilitar el botón. */
  confirmar?: boolean;
  palabra?: string;
  etiqueta?: string;
  onConfirm: (escrito: string) => void;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const [escrito, setEscrito] = useState("");
  const necesita = confirmar === true && (palabra ?? "") !== "";
  const listo = !necesita || escrito.trim() === palabra;

  return (
    <Dialog
      open={open}
      size="md"
      title={title}
      production={severidad === "produccion"}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" size="sm" onClick={onClose}>
            Cancelar
          </Button>
          <Button
            size="sm"
            variant={severidad === "normal" ? "primary" : "danger"}
            disabled={!listo}
            onClick={() => onConfirm(escrito.trim())}
          >
            {etiqueta}
          </Button>
        </>
      }
    >
      <div className={cx(styles.cuerpo, styles[`sev_${severidad}`])}>
        <div className={styles.texto}>{children}</div>

        {necesita ? (
          <div className={styles.confirmar}>
            <label className={styles.etiqueta} htmlFor="kn-confirmar">
              Escribí <code className={styles.palabra}>{palabra}</code> para habilitar
            </label>
            <Input
              id="kn-confirmar"
              value={escrito}
              autoFocus
              autoComplete="off"
              placeholder={palabra}
              className={listo ? styles.ok : styles.mal}
              onChange={(e) => setEscrito(e.currentTarget.value)}
            />
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}
