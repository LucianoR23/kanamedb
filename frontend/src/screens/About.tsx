import { useEffect, useState } from "react";
import * as AppInfo from "../../bindings/github.com/LucianoR23/kanamedb/internal/appinfo/service";
import type { Info } from "../../bindings/github.com/LucianoR23/kanamedb/internal/appinfo";
import { Button } from "../components/ui";
import { DevSignature } from "../components/DevSignature";
import styles from "./About.module.css";

/** Motores y plataformas: `on` es lo que funciona hoy en el binario que estás
 *  corriendo, no lo que está planificado. Ver kaname-plan.md. */
const ENGINES = [
  { label: "PostgreSQL", on: false },
  { label: "MySQL · MariaDB", on: false },
  { label: "SQLite", on: false },
] as const;

const PLATFORMS = [
  { label: "Windows 10+ x64", on: true },
  { label: "Windows 11 arm64", on: true },
  { label: "Linux · macOS", on: false },
] as const;

/** Dependencias de terceros que están efectivamente en el binario hoy. La lista
 *  crece con cada iteración; no se adelanta. */
const THIRD_PARTY = ["Wails v3", "React 19", "Inter", "JetBrains Mono"];

function StatusList({
  items,
}: {
  items: readonly { readonly label: string; readonly on: boolean }[];
}) {
  return (
    <div className={styles.statusList}>
      {items.map((it) => (
        <div key={it.label} className={styles.statusRow}>
          <span className={`${styles.statusTag} ${it.on ? styles.tagOn : styles.tagOff}`}>
            {it.on ? "sí" : "aún no"}
          </span>
          <span className={it.on ? styles.statusOn : styles.statusOff}>{it.label}</span>
        </div>
      ))}
    </div>
  );
}

/**
 * S25 About and shortcuts.
 *
 * Todo lo que muestra sobre el binario sale del servicio Go `appinfo`, no de
 * constantes del frontend: la pantalla no puede mentir sobre qué está
 * corriendo.
 */
export function About({ onBack }: { onBack: () => void }) {
  const [info, setInfo] = useState<Info | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    AppInfo.Get()
      .then((got) => {
        if (!cancelled) setInfo(got);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className={styles.page}>
      <div className={styles.topbar}>
        <span style={{ font: "700 13px/1 var(--font-ui)", letterSpacing: ".14em" }}>
          KANAME
        </span>
        <span className={styles.spacer} />
        <Button size="sm" onClick={onBack}>
          Volver
        </Button>
      </div>

      <div className={styles.inner}>
        <div className={styles.head}>
          <div>
            <div className={styles.identity}>
              <span className={styles.mark} aria-hidden="true">
                要
              </span>
              <div>
                <div className={styles.name}>KANAME</div>
                <div className={styles.build}>
                  {info
                    ? `v${info.version} · build ${info.buildDate} · ${info.platform} · ${info.goVersion}`
                    : error
                      ? "no se pudo leer la información del binario"
                      : "leyendo…"}
                </div>
              </div>
            </div>

            <p className={styles.prose}>
              Un cliente de escritorio para editar el esquema y los datos de
              PostgreSQL, MySQL y SQLite, donde cada cambio se prepara como SQL
              que leés antes de que corra. Nada llega a la base hasta que lo
              aplicás, y producción siempre pide que escribas su nombre.
            </p>
            <p className={styles.prose}>
              Las conexiones y los layouts viven en esta máquina. Las contraseñas
              van al keychain del sistema operativo y nunca al disco. No hay
              cuenta, ni sincronización, ni telemetría: el único tráfico de red
              es hacia las bases que vos configures.
            </p>

            <p className={styles.earlyNote}>
              Esta es una build temprana. Todavía no se conecta a ninguna base:
              lo que existe es el esqueleto de la ventana y el sistema de
              componentes. El gestor de conexiones es lo siguiente.
            </p>

            {error ? (
              <div className={styles.error} role="alert">
                No se pudo leer la información de la aplicación.
                <div className={styles.errorDetail}>{error}</div>
              </div>
            ) : null}

            <div className={styles.cards}>
              <div className={styles.card}>
                <div className={styles.cardLabel}>Motores</div>
                <StatusList items={ENGINES} />
              </div>
              <div className={styles.card}>
                <div className={styles.cardLabel}>Plataformas</div>
                <StatusList items={PLATFORMS} />
              </div>
              <div className={styles.card}>
                <div className={styles.cardLabel}>Build</div>
                <div className={styles.cardBody}>
                  {info ? (
                    <>
                      {info.version}
                      <br />
                      {info.buildDate}
                      <br />
                      {info.platform}
                    </>
                  ) : (
                    "—"
                  )}
                </div>
              </div>
            </div>
          </div>

          <div className={styles.side}>
            <div className={`${styles.card} ${styles.cardWide}`}>
              <div className={styles.sideLabel}>Dónde se guarda cada cosa</div>
              <div className={styles.paths}>
                <div>
                  <div className={styles.pathName}>Conexiones y preferencias</div>
                  <div className={styles.pathValue}>{info?.paths.config ?? "—"}</div>
                </div>
                <div>
                  <div className={styles.pathName}>Historial y layouts</div>
                  <div className={styles.pathValue}>{info?.paths.state ?? "—"}</div>
                </div>
                <div>
                  <div className={styles.pathName}>Diagnósticos</div>
                  <div className={styles.pathValue}>{info?.paths.logs ?? "—"}</div>
                </div>
                <div>
                  <div className={styles.pathName}>Contraseñas</div>
                  <div className={styles.pathValue}>
                    keychain del sistema, nunca en disco
                  </div>
                </div>
              </div>
            </div>

            <div className={`${styles.card} ${styles.cardWide}`}>
              <div className={styles.sideLabel}>Terceros</div>
              <div className={styles.thirdParty}>{THIRD_PARTY.join(" · ")}</div>
            </div>
          </div>
        </div>

        <div>
          <div className={styles.section}>
            <span className={styles.sectionTitle}>Atajos de teclado</span>
            <span className={styles.rule} />
            <span className={styles.sectionMeta}>Windows</span>
          </div>
          <p className={styles.shortcutsEmpty}>
            Todavía no hay ninguno. Cada atajo se documenta acá cuando la función
            que dispara existe de verdad, para que esta pantalla nunca prometa
            una tecla que no hace nada.
          </p>
        </div>

        <DevSignature variant="animada" />
      </div>
    </div>
  );
}
