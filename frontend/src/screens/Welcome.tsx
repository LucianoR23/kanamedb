import { Button, ShortcutChip } from "../components/ui";
import { DevSignature } from "../components/DevSignature";
import styles from "./Welcome.module.css";

interface Props {
  onNew: () => void;
  /** Atajo: elegir un archivo de SQLite y abrirlo sin pasar por el formulario. */
  onOpenFile: () => void;
  onAbout: () => void;
  /** Ruta del archivo de conexiones, para la barra de estado. */
  connectionsPath: string;
}

/**
 * S01 Welcome.
 *
 * No están "Import connections" ni la detección de motores locales: el plan no
 * las pide y escanear puertos, aunque sea local, es alcance que nadie pidió.
 */
export function Welcome({ onNew, onOpenFile, onAbout, connectionsPath }: Props) {
  return (
    <div className={styles.screen}>
      <header className={styles.titlebar}>
        <span className={styles.wordmark}>KANAME</span>
        <span className={styles.divider} />
        <span className={styles.section}>Sin conexión</span>
        <span className={styles.spacer} />
        <ShortcutChip>Ctrl K</ShortcutChip>
      </header>

      <div className={styles.body}>
        <main className={styles.main}>
          <div className={styles.column}>
            <div className={styles.identity}>
              <span className={styles.mark} aria-hidden="true">
                要
              </span>
              <div>
                <div className={styles.name}>KANAME</div>
                <div className={styles.tagline}>
                  Edición de esquema y datos para PostgreSQL, MySQL, MariaDB y SQLite
                </div>
              </div>
            </div>

            <h1 className={styles.headline}>Conectate a una base para empezar</h1>
            <p className={styles.lede}>
              Las conexiones se guardan en esta máquina. Las credenciales van al keychain
              del sistema operativo, nunca a un archivo del proyecto y nunca por la red.
            </p>

            <div className={styles.actions}>
              <Button variant="primary" size="lg" onClick={onNew}>
                Nueva conexión
              </Button>
              <Button size="lg" onClick={onOpenFile}>
                Abrir archivo SQLite…
              </Button>
            </div>

            <div className={styles.howto}>
              <div className={styles.howtoLabel}>Cómo funciona</div>
              <Step n={1} title="Editás con libertad">
                Cambiás celdas, columnas, claves y enums como si el cambio ya estuviera
                hecho.
              </Step>
              <Step n={2} title="Leés el SQL">
                Cada edición se vuelve una sentencia en un changeset que podés leer,
                reordenar y copiar.
              </Step>
              <Step n={3} title="Aplicás a propósito">
                Nada llega a la base hasta que aplicás. Producción te pide que escribas su
                nombre.
              </Step>
            </div>
          </div>
        </main>

        <aside className={styles.side}>
          <div className={styles.sideBlock}>
            <div className={styles.sideLabel}>Recientes</div>
            <div className={styles.dashed} />
            <p className={styles.sideText}>
              Las conexiones que abras van a listarse acá, de la más reciente a la más
              vieja.
            </p>
          </div>

          <div className={styles.spacer} />

          <div className={styles.sideFoot}>
            <div className={styles.offline}>
              <span className={styles.offlineDot} />
              Sin conexión a internet. Sin telemetría, sin cuenta.
            </div>
            <button type="button" className={styles.link} onClick={onAbout}>
              Acerca de Kaname
            </button>
          </div>
        </aside>
      </div>

      <footer className={styles.statusbar}>
        <span>Sin conectar</span>
        <span className={styles.spacer} />
        <DevSignature />
        <span className={styles.path} title={connectionsPath}>
          {connectionsPath}
        </span>
      </footer>
    </div>
  );
}

function Step({
  n,
  title,
  children,
}: {
  n: number;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className={styles.step}>
      <span className={styles.stepNumber}>{n}</span>
      <div>
        <div className={styles.stepTitle}>{title}</div>
        <div className={styles.stepText}>{children}</div>
      </div>
    </div>
  );
}
