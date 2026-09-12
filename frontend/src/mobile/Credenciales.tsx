import { useState } from "react";
import * as Connections from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import { PasswordAction } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button, Dialog, Field, Input, Textarea } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import styles from "./mobile.module.css";

/**
 * Las credenciales de una conexión en el teléfono: la contraseña de la base,
 * el secreto del bastión y —con túnel por clave— la clave privada como
 * contenido, pegada. Es lo único que se escribe acá: el resto de la conexión
 * viene importado de la PC.
 *
 * Cada campo vacío se deja como está. Guardar pide la huella una vez por
 * secreto que cambie: es el vault (kaname-android.md).
 */
export function Credenciales({
  view,
  onClose,
  onSaved,
}: {
  view: ConnectionView;
  onClose: () => void;
  onSaved: (mensaje: string) => void;
}) {
  const c = view.connection;
  const ssh = c.ssh.enabled;
  const porClave = ssh && c.ssh.auth === "key";
  const sinContrasena = c.engine === "sqlite";

  const [password, setPassword] = useState("");
  const [sshSecret, setSshSecret] = useState("");
  const [clave, setClave] = useState("");
  const [guardando, setGuardando] = useState(false);
  const [error, setError] = useState("");

  const hayAlgo = password !== "" || sshSecret !== "" || clave.trim() !== "";

  async function guardar() {
    setGuardando(true);
    setError("");
    try {
      const cambios: string[] = [];
      if (password !== "" || sshSecret !== "") {
        await Connections.SaveWithSSH(
          c,
          password !== "" ? PasswordAction.PasswordSet : PasswordAction.PasswordKeep,
          password,
          sshSecret !== "" ? PasswordAction.PasswordSet : PasswordAction.PasswordKeep,
          sshSecret,
        );
        if (password !== "") cambios.push("contraseña");
        if (sshSecret !== "") cambios.push(porClave ? "frase de paso" : "contraseña del bastión");
      }
      if (clave.trim() !== "") {
        try {
          await Connections.SetSSHKey(c.id, clave);
        } catch (err) {
          // Lo anterior ya quedó guardado: se dice, en vez de dar a entender
          // que no entró nada. Cerrar recarga la lista.
          setError(
            (cambios.length > 0 ? `Se guardó ${cambios.join(" y ")}, pero la clave privada no: ` : "") +
              textoDe(err),
          );
          return;
        }
        cambios.push("clave privada");
      }
      onSaved(`Guardado: ${cambios.join(", ")}`);
    } catch (err) {
      setError(textoDe(err));
    } finally {
      setGuardando(false);
    }
  }

  return (
    <Dialog
      open
      title={c.name}
      {...(guardando ? {} : { onClose })}
      footer={
        <>
          <Button onClick={onClose} disabled={guardando}>
            Cancelar
          </Button>
          <Button variant="primary" onClick={() => void guardar()} loading={guardando} disabled={!hayAlgo}>
            Guardar
          </Button>
        </>
      }
    >
      <div className={styles.detalle}>
        <p className={styles.nota}>
          Lo que dejes vacío queda como está. Se guarda cifrado con una clave del equipo que solo
          se abre con tu huella o tu rostro.
        </p>

        {!sinContrasena ? (
          <Field label={view.hasPassword ? "Contraseña de la base (ya hay una guardada)" : "Contraseña de la base"}>
            <Input
              type="password"
              autoComplete="off"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={view.hasPassword ? "Dejar la actual" : "Sin guardar"}
            />
          </Field>
        ) : null}

        {ssh ? (
          <Field
            label={
              (porClave ? "Frase de paso de la clave" : "Contraseña del bastión") +
              (view.hasSSHSecret ? " (ya hay una guardada)" : "")
            }
          >
            <Input
              type="password"
              autoComplete="off"
              value={sshSecret}
              onChange={(e) => setSshSecret(e.target.value)}
              placeholder={view.hasSSHSecret ? "Dejar la actual" : porClave ? "Vacío si la clave no tiene" : "Sin guardar"}
            />
          </Field>
        ) : null}

        {porClave ? (
          <Field label={view.hasSSHKey ? "Clave privada (ya hay una guardada)" : "Clave privada"}>
            <Textarea
              rows={5}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              value={clave}
              onChange={(e) => setClave(e.target.value)}
              placeholder={"Pegá el contenido:\n-----BEGIN OPENSSH PRIVATE KEY-----\n…"}
              style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}
            />
            <p className={styles.nota}>
              En la PC la clave es un archivo (<code>{c.ssh.keyPath || "~/.ssh/…"}</code>); acá
              es contenido y va cifrada como una contraseña más.
            </p>
          </Field>
        ) : null}

        {error ? <div className={styles.error}>{error}</div> : null}
      </div>
    </Dialog>
  );
}
