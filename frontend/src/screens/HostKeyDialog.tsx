import { useState } from "react";
import { Verdict } from "../../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import type { Inspection } from "../../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import { Button, Checkbox, CopyButton, Dialog } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./HostKeyDialog.module.css";

/**
 * S04 · Verificación de la clave del host SSH.
 *
 * Aparece antes de que viaje ninguna credencial, y eso no es una promesa de la
 * interfaz: el protocolo SSH intercambia y verifica la clave del host antes de
 * la autenticación, y la inspección corta ahí. Cuando este diálogo se muestra,
 * el bastión todavía no recibió ni el usuario.
 */
export function HostKeyDialog({
  inspection,
  knownHostsPath,
  onCancel,
  onConnectOnce,
  onTrust,
}: {
  inspection: Inspection;
  knownHostsPath: string;
  onCancel: () => void;
  /** Conecta sin guardar la clave. */
  onConnectOnce: () => void;
  /** Guarda la clave y conecta. */
  onTrust: () => void;
}) {
  const cambiada = inspection.verdict === Verdict.VerdictChanged;
  // Por defecto se recuerda en el primer contacto y NO en un cambio de clave:
  // aceptar un cambio es una decisión más grande, y no debería quedar
  // consentida por una casilla que ya venía marcada.
  const [recordar, setRecordar] = useState(!cambiada);
  const [comoVerificar, setComoVerificar] = useState(false);

  const anterior = inspection.known;

  // Los dos caminos para verificar, ya armados con el host y el tipo de clave
  // reales. Se arman acá y no en el JSX para poder pasárselos al botón de
  // copiar sin repetirlos: un comando que se muestra distinto del que se copia
  // es peor que no tener botón.
  const comandoLocal = `ssh-keygen -lF ${hostSolo(inspection.address)}`;
  const comandoRemoto = `ssh-keygen -lf /etc/ssh/ssh_host_${tipoCorto(
    inspection.presented.algorithm,
  )}_key.pub`;

  return (
    <Dialog
      open
      title={
        cambiada
          ? `La clave de ${inspection.address} cambió`
          : `Verificá la clave de ${inspection.address}`
      }
      size="lg"
      production={cambiada}
      onClose={onCancel}
      footer={
        <>
          <Checkbox checked={recordar} onChange={setRecordar}>
            Recordar este host
          </Checkbox>
          <span className={styles.grow} />
          <Button onClick={onCancel}>Cancelar</Button>
          <Button onClick={onConnectOnce}>Conectar una vez</Button>
          <Button
            variant={cambiada ? "danger" : "primary"}
            onClick={recordar ? onTrust : onConnectOnce}
          >
            {cambiada ? "Reemplazar la clave y conectar" : "Confiar y conectar"}
          </Button>
        </>
      }
    >
      <div className={cx(styles.body, cambiada && styles.bodyDanger)}>
        <p className={styles.intro}>
          {cambiada
            ? "Este host ya era de confianza, pero ahora presenta una clave distinta. Kaname detuvo la conexión."
            : "Es la primera vez que se conecta a este host, así que no hay nada con qué comparar. Verificá que la huella de abajo sea la misma que informa el servidor antes de confiar en ella."}
        </p>

        <Huella
          etiqueta={cambiada ? "La que presenta ahora" : "Huella que presenta el host"}
          algoritmo={inspection.presented.algorithm}
          bits={inspection.presented.bits}
          valor={inspection.presented.fingerprint}
          tono={cambiada ? "danger" : "normal"}
        />

        {cambiada && anterior ? (
          <>
            <Huella
              etiqueta={`Confiada el ${fecha(anterior.addedAt)}`}
              algoritmo={anterior.algorithm}
              bits={anterior.bits}
              valor={anterior.fingerprint}
              tono="tachada"
            />
            <div className={styles.alerta}>
              <span className={styles.alertaMarca}>!</span>
              <p>
                Una clave que cambia puede significar que reinstalaron el servidor — o que algo
                está interceptando la conexión. Confirmá la huella nueva con quien administra el
                host antes de seguir. <strong>Todavía no se envió ninguna credencial.</strong>
              </p>
            </div>
          </>
        ) : null}

        <div className={styles.como}>
          <button
            type="button"
            className={styles.comoToggle}
            onClick={() => setComoVerificar((v) => !v)}
            aria-expanded={comoVerificar}
          >
            <span className={cx(styles.comoFlecha, comoVerificar && styles.comoFlechaAbierta)}>
              ▶
            </span>
            ¿Cómo verifico esta huella?
          </button>
          {comoVerificar ? (
            <div className={styles.comoCuerpo}>
              <p className={styles.comoAclara}>
                La huella identifica a <strong>la máquina a la que entrás por SSH</strong> — el
                servidor —, no a la base de datos ni al panel que corra encima. Si en ese servidor
                hay varias bases, o contenedores, o un panel de administración, la clave es una
                sola y es la del sistema operativo.
              </p>

              <div className={styles.opcion}>
                <div className={styles.opcionTitulo}>
                  Si ya te conectaste antes a este servidor desde esta máquina
                </div>
                <p>
                  Es lo más rápido: tu propio OpenSSH ya anotó su clave la primera vez. Corré esto
                  <strong> acá</strong>, en tu terminal:
                </p>
                <div className={styles.comandoFila}>
                  <code className={styles.comando}>{comandoLocal}</code>
                  <CopyButton text={comandoLocal} />
                </div>
                <p className={styles.comoNota}>
                  Si imprime una huella y coincide con la de arriba, es el mismo servidor de
                  siempre. Si no imprime nada, nunca te conectaste desde acá y hay que usar la
                  opción de abajo.
                </p>
              </div>

              <div className={styles.opcion}>
                <div className={styles.opcionTitulo}>Si es la primera vez</div>
                <p>
                  Corré esto <strong>en el servidor</strong>, entrando por la consola que te dé tu
                  proveedor — o pedíselo a quien lo administra:
                </p>
                <div className={styles.comandoFila}>
                  <code className={styles.comando}>{comandoRemoto}</code>
                  <CopyButton text={comandoRemoto} />
                </div>
              </div>

              <p className={styles.comoNota}>
                El valor SHA256 que imprima tiene que coincidir con el de arriba, carácter por
                carácter. Kaname guarda las claves aceptadas en{" "}
                <code className={styles.ruta}>{knownHostsPath}</code>.
              </p>
            </div>
          ) : null}
        </div>
      </div>
    </Dialog>
  );
}

function Huella({
  etiqueta,
  algoritmo,
  bits,
  valor,
  tono,
}: {
  etiqueta: string;
  algoritmo: string;
  bits: number;
  valor: string;
  tono: "normal" | "danger" | "tachada";
}) {
  return (
    <div className={styles.tarjeta}>
      <div className={styles.tarjetaHead}>
        <span className={styles.tarjetaLabel}>{etiqueta}</span>
        <span className={styles.grow} />
        <span className={styles.algoritmo}>
          {algoritmo}
          {bits > 0 ? ` · ${bits} bit` : ""}
        </span>
      </div>
      <div className={styles.tarjetaCuerpo}>
        {/* La huella se parte para que entre completa: se compara carácter por
            carácter, así que recortarla con puntos suspensivos la volvería
            inservible justo para lo único que sirve. */}
        <div
          className={cx(
            styles.huella,
            tono === "danger" && styles.huellaDanger,
            tono === "tachada" && styles.huellaTachada,
          )}
        >
          {valor}
        </div>
      </div>
    </div>
  );
}

/** `bastion.interna:2222` → `bastion.interna`.
 *
 * `ssh-keygen -F` busca por host, y con un puerto no estándar OpenSSH guarda la
 * entrada como `[host]:puerto`. Se pasa el host solo porque es lo que acierta en
 * el caso común; si el puerto no es el 22 la búsqueda puede no encontrarla, y
 * para eso está la segunda opción. */
function hostSolo(address: string): string {
  const i = address.lastIndexOf(":");
  return i > 0 ? address.slice(0, i) : address;
}

/** `ssh-ed25519` → `ed25519`, para armar la ruta del archivo en el host. */
function tipoCorto(algoritmo: string): string {
  const limpio = algoritmo.replace(/^ssh-/, "").replace(/-cert.*$/, "");
  if (limpio.startsWith("ecdsa")) return "ecdsa";
  return limpio || "ed25519";
}

/** La fecha en que se aceptó, en formato corto. Vacía si no la sabemos. */
function fecha(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return "una fecha desconocida";
  return d.toLocaleDateString("es", { day: "numeric", month: "long", year: "numeric" });
}
