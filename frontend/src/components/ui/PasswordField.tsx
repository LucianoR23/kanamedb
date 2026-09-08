import { useState } from "react";
import { PasswordAction } from "../../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button } from "./Button";
import { Input } from "./Input";
import { cx } from "../../lib/cx";
import styles from "./PasswordField.module.css";

export type PasswordState =
  /** Hay una contraseña guardada y no se tocó. */
  | { kind: "stored" }
  /** El usuario escribió una nueva. */
  | { kind: "typed"; value: string }
  /** El usuario pidió quitarla. */
  | { kind: "removed" }
  /** No hay ninguna guardada y no se escribió nada. */
  | { kind: "empty" };

interface PasswordFieldProps {
  state: PasswordState;
  onChange: (next: PasswordState) => void;
  /** Trae la contraseña guardada del keychain. Se llama solo al pedirlo. */
  onReveal: () => Promise<string>;
  disabled?: boolean;
}

/**
 * Campo de contraseña del editor de conexiones.
 *
 * Dos decisiones que no se ven pero importan:
 *
 * Dejar el campo en blanco NO borra la contraseña. El estado distingue "no la
 * toqué" de "la quiero sacar", porque si no, abrir el editor para cambiarle el
 * nombre a una conexión y guardar borraría la credencial.
 *
 * La contraseña guardada no viaja al frontend al abrir el editor: se pide al
 * keychain solo cuando el usuario aprieta Ver. Abrir la conexión de producción
 * no puede meter esa contraseña en la memoria del webview sin que nadie lo pida.
 */
export function PasswordField({
  state,
  onChange,
  onReveal,
  disabled = false,
}: PasswordFieldProps) {
  const [revealed, setRevealed] = useState<string | null>(null);
  const [revealing, setRevealing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const stored = state.kind === "stored";
  const typed = state.kind === "typed";

  async function reveal() {
    if (revealed !== null) {
      setRevealed(null);
      return;
    }
    setRevealing(true);
    setError(null);
    try {
      setRevealed(await onReveal());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRevealing(false);
    }
  }

  return (
    <div className={styles.wrap}>
      <div className={styles.row}>
        {stored ? (
          <Input
            readOnly
            value={revealed ?? "••••••••••••"}
            className={cx(styles.control, revealed === null && styles.masked)}
            aria-label="Contraseña guardada"
          />
        ) : (
          <Input
            type={revealed === null ? "password" : "text"}
            value={typed ? state.value : ""}
            placeholder={state.kind === "removed" ? "se va a quitar" : "sin guardar"}
            disabled={disabled}
            aria-label="Contraseña"
            onChange={(e) => {
              const value = e.currentTarget.value;
              onChange(value === "" ? { kind: "empty" } : { kind: "typed", value });
            }}
          />
        )}

        {stored ? (
          <Button size="sm" loading={revealing} onClick={reveal} disabled={disabled}>
            {revealed === null ? "Ver" : "Ocultar"}
          </Button>
        ) : typed ? (
          <Button
            size="sm"
            onClick={() => setRevealed(revealed === null ? state.value : null)}
            disabled={disabled}
          >
            {revealed === null ? "Ver" : "Ocultar"}
          </Button>
        ) : null}
      </div>

      <div className={styles.hint}>
        {stored ? (
          <>
            <span>Guardada en el keychain del sistema.</span>
            <button
              type="button"
              className={styles.action}
              disabled={disabled}
              onClick={() => {
                setRevealed(null);
                onChange({ kind: "typed", value: "" });
              }}
            >
              Reemplazar
            </button>
            <button
              type="button"
              className={cx(styles.action, styles.danger)}
              disabled={disabled}
              onClick={() => {
                setRevealed(null);
                onChange({ kind: "removed" });
              }}
            >
              Quitar
            </button>
          </>
        ) : state.kind === "removed" ? (
          <>
            <span className={styles.warn}>Se va a quitar del keychain al guardar.</span>
            <button
              type="button"
              className={styles.action}
              onClick={() => onChange({ kind: "stored" })}
            >
              Cancelar
            </button>
          </>
        ) : (
          <span>Se guarda en el keychain del sistema, nunca en un archivo.</span>
        )}
      </div>

      {error ? (
        <div className={styles.error} role="alert">
          No se pudo leer la contraseña del keychain. {error}
        </div>
      ) : null}
    </div>
  );
}

/** Traduce el estado del campo a lo que espera el servicio al guardar.
 *
 *  Un campo vacío es "keep" y no "remove": dejarlo en blanco no puede borrar la
 *  credencial. Quitarla es una acción propia. */
export function passwordAction(state: PasswordState): {
  action: PasswordAction;
  password: string;
} {
  switch (state.kind) {
    case "typed":
      return state.value === ""
        ? { action: PasswordAction.PasswordKeep, password: "" }
        : { action: PasswordAction.PasswordSet, password: state.value };
    case "removed":
      return { action: PasswordAction.PasswordRemove, password: "" };
    default:
      return { action: PasswordAction.PasswordKeep, password: "" };
  }
}
