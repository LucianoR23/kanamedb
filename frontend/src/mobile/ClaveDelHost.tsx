import type { Inspection } from "../../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import { Verdict } from "../../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import { Button, Dialog } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./mobile.module.css";

/**
 * M03: la clave del bastión SSH, en el teléfono. Es el mismo TOFU que en la
 * PC —la decisión la toma `useConectar`, y hasta que se toma el bastión no
 * recibe ninguna credencial—; lo que cambia es la piel: una hoja con la
 * ficha host / tipo / SHA256 y dos botones grandes.
 *
 * Con la clave CAMBIADA se da vuelta todo: franja roja, sin transición, la
 * huella confiada tachada y la nueva en rojo, y el botón principal es
 * «Cancelar y no conectar». Confiar en la clave nueva es un botón de borde
 * rojo, y no se guarda: vale para este intento (`AcceptOnce`), igual que en
 * escritorio, donde reemplazar la confiada exige la casilla explícita.
 */
export function ClaveDelHost({
  inspection,
  onCancelar,
  onConectarUnaVez,
  onConfiar,
}: {
  inspection: Inspection;
  onCancelar: () => void;
  onConectarUnaVez: () => void;
  onConfiar: () => void;
}) {
  const cambiada = inspection.verdict === Verdict.VerdictChanged;
  const p = inspection.presented;

  if (cambiada) {
    return (
      <Dialog
        open
        title="La clave del host cambió"
        production
        abrupto
        onClose={onCancelar}
        footer={
          <div className={cx(styles.columna, styles.crece)}>
            <Button variant="primary" className={styles.grande} onClick={onCancelar}>
              Cancelar y no conectar
            </Button>
            <Button variant="dangerOutline" className={styles.grande} onClick={onConectarUnaVez}>
              Confiar en la clave nueva, solo esta vez
            </Button>
            <span className={cx(styles.ayuda, styles.centrado)}>Si no hablaste con quien administra el servidor, cancelá.</span>
          </div>
        }
      >
        <div className={styles.columna}>
          <p className={cx(styles.hojaTexto, styles.textoFuerte)}>
            El bastión está presentando una clave distinta a la que confiaste. Puede ser que el servidor se haya
            reinstalado, o que alguien se esté metiendo en el medio para leer tu contraseña y tus datos.
          </p>
          <div className={styles.ficha}>
            <div className={styles.fichaFila}>
              <span>Host</span>
              <span>{inspection.address}</span>
            </div>
            <div className={cx(styles.fichaFila, styles.fichaVieja)}>
              <span>Confiada</span>
              <span>{inspection.known?.fingerprint ?? "—"}</span>
            </div>
            <div className={cx(styles.fichaFila, styles.fichaMal)}>
              <span>Ahora</span>
              <span>{p.fingerprint}</span>
            </div>
          </div>
        </div>
      </Dialog>
    );
  }

  return (
    <Dialog
      open
      title="Host SSH desconocido"
      onClose={onCancelar}
      footer={
        <>
          <Button onClick={onCancelar}>Cancelar</Button>
          <Button variant="primary" onClick={onConfiar}>
            Confiar
          </Button>
        </>
      }
    >
      <div className={styles.columna}>
        <p className={styles.hojaTexto}>
          Es la primera vez que te conectás a este bastión. Confirmá la huella con quien administra el servidor
          antes de confiar.
        </p>
        <div className={styles.ficha}>
          <div className={styles.fichaFila}>
            <span>Host</span>
            <span>{inspection.address}</span>
          </div>
          <div className={styles.fichaFila}>
            <span>Tipo</span>
            <span>
              {p.algorithm}
              {p.bits ? ` · ${p.bits} bits` : ""}
            </span>
          </div>
          <div className={styles.fichaFila}>
            <span>SHA256</span>
            <span>{p.fingerprint}</span>
          </div>
        </div>
        <span className={cx(styles.ayuda, styles.centrado)}>Se guarda en este teléfono. No se vuelve a preguntar.</span>
      </div>
    </Dialog>
  );
}
