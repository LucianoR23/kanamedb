import { useState } from "react";
import * as Connections from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import { PasswordAction } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button, Input, Textarea } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cx } from "../lib/cx";
import { Barra } from "./Barra";
import styles from "./mobile.module.css";

/**
 * M02: las credenciales de una conexión en el teléfono, a pantalla completa.
 * La contraseña de la base, el secreto del bastión y —con túnel por clave—
 * la clave privada como contenido, pegada. Es lo único que se escribe acá: el
 * resto de la conexión viene importado de la PC.
 *
 * Cada campo vacío se deja como está. Guardar pide la huella una vez por
 * secreto que cambie: es el vault (kaname-android.md). Mientras Go espera el
 * dedo, la pantalla lo dice con una hoja propia; el diálogo de la huella lo
 * pone el sistema encima.
 *
 * Lo que el diseño dibujaba como «sin biometría fuerte, se protege con el
 * PIN» no es lo que hace la app: sin biometría fuerte no guarda, y lo dice
 * con el error que devuelve Go. Acá se muestra ese error, no una promesa.
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
  // Cuál campo se rechazó, para marcarlo: la clave o la frase que no la abre.
  const [fallaClave, setFallaClave] = useState(false);
  // Lo que ya entró en un intento anterior que falló a medias: se vacían esos
  // campos para que «Reintentar» no vuelva a pedir el dedo por un secreto que
  // ya está guardado, y se suman al mensaje final.
  const [yaGuardados, setYaGuardados] = useState<string[]>([]);

  const hayAlgo = password !== "" || sshSecret !== "" || clave.trim() !== "";

  async function guardar() {
    setGuardando(true);
    setError("");
    setFallaClave(false);
    try {
      const cambios: string[] = [...yaGuardados];
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
        setYaGuardados([...cambios]);
        setPassword("");
        setSshSecret("");
      }
      if (clave.trim() !== "") {
        try {
          await Connections.SetSSHKey(c.id, clave);
        } catch (err) {
          // Lo anterior ya quedó guardado: se dice, en vez de dar a entender
          // que no entró nada. Cerrar recarga la lista.
          setFallaClave(true);
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
    <div className={styles.pantalla}>
      <Barra titulo="Credenciales" subtitulo={view.uri} atras={guardando ? undefined : onClose} prod={c.environment === "production"} />

      <main className={cx(styles.cuerpo, styles.cuerpoGap20)}>
        {error ? (
          <div className={cx(styles.aviso, styles.avisoMal)}>
            <div>
              <span className={styles.avisoTitulo}>No se pudo guardar</span>
              {error}
            </div>
          </div>
        ) : null}

        {!sinContrasena ? (
          <section className={styles.campo}>
            <span className={styles.rotulo}>Base de datos</span>
            <span className={styles.etiqueta}>Contraseña</span>
            <Secreto value={password} onChange={setPassword} placeholder={view.hasPassword ? "" : "sin guardar"} ariaLabel="Contraseña de la base" />
            <span className={styles.ayuda}>
              {view.hasPassword ? "Guardada. Dejala vacía para no cambiarla." : "Todavía no hay ninguna guardada."}
            </span>
          </section>
        ) : (
          <div className={styles.notaCaja}>SQLite abre un archivo: no lleva contraseña.</div>
        )}

        {ssh ? (
          <>
            <div className={styles.divisor} />
            <section className={cx(styles.campo, styles.campoGap8)}>
              <div className={styles.rotuloFila}>
                <span className={styles.rotulo}>Túnel SSH</span>
                <span className={styles.chip}>
                  {c.ssh.user}@{c.ssh.host}:{c.ssh.port}
                </span>
              </div>

              <span className={styles.etiqueta}>{porClave ? "Frase de paso de la clave" : "Contraseña del bastión"}</span>
              <Secreto
                value={sshSecret}
                onChange={setSshSecret}
                placeholder={view.hasSSHSecret ? "" : porClave ? "vacío si la clave no tiene" : "sin guardar"}
                ariaLabel={porClave ? "Frase de paso de la clave" : "Contraseña del bastión"}
                mal={fallaClave && porClave && sshSecret !== ""}
              />
              {view.hasSSHSecret ? <span className={styles.ayuda}>Guardada. Dejala vacía para no cambiarla.</span> : null}

              {porClave ? (
                <>
                  <span className={cx(styles.etiqueta, styles.margenArriba)}>Clave privada</span>
                  <Textarea
                    className={cx(styles.claveArea, fallaClave && clave.trim() !== "" && styles.editorMal)}
                    spellCheck={false}
                    autoCapitalize="off"
                    autoCorrect="off"
                    value={clave}
                    onChange={(e) => setClave(e.target.value)}
                    placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n…"}
                    aria-label="Clave privada"
                  />
                  <span className={styles.ayuda}>
                    {view.hasSSHKey ? "Hay una guardada. " : ""}Pegá el contenido del archivo, no la ruta
                    {c.ssh.keyPath ? ` (en la PC es ${c.ssh.keyPath})` : ""}.
                  </span>
                </>
              ) : null}
            </section>
          </>
        ) : null}

        <div className={styles.notaCaja}>
          Lo que dejes vacío queda como está. Todo se guarda cifrado en este teléfono, con una clave que solo se
          abre con tu huella o tu rostro, y nunca sale de acá.
        </div>
      </main>

      <div className={styles.pie}>
        <Button onClick={onClose} disabled={guardando}>
          Cancelar
        </Button>
        <Button variant="primary" onClick={() => void guardar()} disabled={!hayAlgo || guardando}>
          {error ? "Reintentar" : "Guardar"}
        </Button>
      </div>

      {guardando ? (
        <div className={styles.velo} role="status" aria-live="polite">
          <div className={styles.hoja}>
            <span className={styles.huella} aria-hidden="true">
              ⊛
            </span>
            <strong>Confirmá con tu huella</strong>
            <p>Kaname necesita desbloquear el almacén seguro para guardar estos secretos. El teléfono lo pide ahora.</p>
          </div>
        </div>
      ) : null}
    </div>
  );
}

/** Un campo de contraseña con el ojo para verla: 48 px, mono cuando se ve. */
function Secreto({
  value,
  onChange,
  placeholder,
  ariaLabel,
  mal = false,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  ariaLabel: string;
  mal?: boolean;
}) {
  const [visible, setVisible] = useState(false);
  return (
    <div className={cx(styles.secreto, mal && styles.secretoMal)}>
      <Input
        type={visible ? "text" : "password"}
        autoComplete="off"
        autoCapitalize="off"
        autoCorrect="off"
        spellCheck={false}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={ariaLabel}
      />
      <button
        type="button"
        className={styles.ojo}
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? "Ocultar" : "Mostrar"}
        aria-pressed={visible}
      >
        {visible ? "◉" : "◎"}
      </button>
    </div>
  );
}
