import { useEffect, useState } from "react";
import * as AppInfo from "../../bindings/github.com/LucianoR23/kanamedb/internal/appinfo/service";
import type { Info } from "../../bindings/github.com/LucianoR23/kanamedb/internal/appinfo";
import { Button, ShortcutChip } from "../components/ui";
import { DevSignature } from "../components/DevSignature";
import styles from "./About.module.css";

/** Motores y plataformas: `on` es lo que funciona hoy en el binario que estás
 *  corriendo, no lo que está planificado. Ver kaname-plan.md. */
const ENGINES = [
  { label: "PostgreSQL", on: true },
  { label: "MySQL · MariaDB", on: true },
  { label: "SQLite", on: true },
] as const;

const PLATFORMS = [
  { label: "Windows 10+ x64", on: true },
  { label: "Windows 11 arm64", on: true },
  { label: "Linux · macOS", on: false },
] as const;

/** Dependencias de terceros que están efectivamente en el binario hoy. La lista
 *  crece con cada iteración; no se adelanta. */
const THIRD_PARTY = [
  "Wails v3",
  "React 19",
  "pgx v5",
  "go-sql-driver/mysql",
  "modernc.org/sqlite",
  "x/crypto/ssh",
  "go-keyring",
  "CodeMirror 6",
  "xyflow",
  "dagre",
  "TanStack Virtual",
  "BurntSushi/toml",
  "Inter",
  "JetBrains Mono",
];

/**
 * Los atajos que EXISTEN, no los que estarían bien.
 *
 * Esta lista arrancó vacía con la promesa de que cada atajo se documenta cuando
 * la función que dispara existe de verdad. Los de acá abajo se pueden apretar
 * hoy; el resto no está.
 */
const SHORTCUTS = [
  { teclas: "Ctrl K", que: "Abrir la paleta de comandos" },
  { teclas: "Ctrl Enter", que: "Correr lo que hay en el editor SQL" },
  { teclas: "Ctrl C", que: "Copiar la celda seleccionada de la grilla" },
  { teclas: "Enter · F2", que: "Editar la celda seleccionada" },
  { teclas: "Esc", que: "Cerrar la paleta, un diálogo o la edición de una celda" },
  { teclas: "Tab", que: "Indentar en el editor SQL" },
] as const;

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
              Esta es una build temprana: los cuatro motores andan, con esquema,
              datos, diagrama y changeset, pero todavía no hay ninguna versión
              publicada ni builds de Linux y macOS.
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
                {/* Cada archivo por separado y no un directorio para varios.
                    Decían «conexiones y preferencias» sobre la ruta de las
                    preferencias —que hasta S23 ni siquiera se escribía— y
                    «historial y layouts» sobre una sola de las dos: los layouts
                    viajan con la libreta y el historial no, que es justamente lo
                    que hay que poder ver de un vistazo para saber qué se
                    sincroniza. */}
                <div>
                  <div className={styles.pathName}>Conexiones</div>
                  <div className={styles.pathValue}>{info?.paths.connections ?? "—"}</div>
                </div>
                <div>
                  <div className={styles.pathName}>Preferencias</div>
                  <div className={styles.pathValue}>{info?.paths.config ?? "—"}</div>
                </div>
                <div>
                  <div className={styles.pathName}>Consultas guardadas</div>
                  <div className={styles.pathValue}>{info?.paths.savedQueries ?? "—"}</div>
                </div>
                <div>
                  <div className={styles.pathName}>Historial (solo esta máquina)</div>
                  <div className={styles.pathValue}>{info?.paths.history ?? "—"}</div>
                </div>
                <div>
                  <div className={styles.pathName}>Diagramas</div>
                  <div className={styles.pathValue}>{info?.paths.layouts ?? "—"}</div>
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
          <div className={styles.shortcuts}>
            {SHORTCUTS.map((s) => (
              <div key={s.teclas} className={styles.shortcut}>
                <ShortcutChip>{s.teclas}</ShortcutChip>
                <span className={styles.shortcutQue}>{s.que}</span>
              </div>
            ))}
          </div>
        </div>

        <DevSignature variant="animada" />
      </div>
    </div>
  );
}
