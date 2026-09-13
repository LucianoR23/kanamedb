import { useEffect, useState } from "react";
import * as SettingsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/settings";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import * as AppInfo from "../../bindings/github.com/LucianoR23/kanamedb/internal/appinfo/service";
import { Theme } from "../../bindings/github.com/LucianoR23/kanamedb/internal/config";
import type { Config } from "../../bindings/github.com/LucianoR23/kanamedb/internal/config";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button, ConfirmDialog } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { aplicar, usarPreferencias } from "../lib/preferencias";
import { textoDe } from "../lib/dialogos";
import { cx } from "../lib/cx";
import { Selector } from "./Selector";
import styles from "./mobile.module.css";

const TEMAS = [
  { value: Theme.ThemeDark, label: "Oscuro" },
  { value: Theme.ThemeLight, label: "Claro" },
  { value: Theme.ThemeSystem, label: "Automático" },
];

/**
 * M14: tema, borrar el historial de esta conexión, desconectar, qué protege
 * y qué no, y la versión. El bloqueo, la captura bloqueada y la biometría no
 * son ajustes: son cómo funciona la app en el teléfono (kaname-android.md), y
 * por eso se explican en vez de ofrecerse.
 */
export function Ajustes({
  sesion,
  onDesconectar,
  onAviso,
}: {
  sesion: SessionView;
  onDesconectar: () => void;
  onAviso: (t: ToastItem) => void;
}) {
  const prefs = usarPreferencias();
  const [version, setVersion] = useState("");
  const [corridas, setCorridas] = useState<number | null>(null);
  const [borrando, setBorrando] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    AppInfo.Get()
      .then((i) => setVersion(`kaname ${i.version} · ${i.platform}`))
      .catch(() => {});
    HistorySvc.List(500)
      .then((l) => setCorridas((l ?? []).filter((e) => e.connectionId === sesion.connectionId).length))
      .catch(() => setCorridas(null));
  }, [sesion.connectionId]);

  async function cambiarTema(t: Theme) {
    if (!prefs) return;
    const next: Config = { ...prefs, theme: t };
    aplicar(next);
    try {
      const v = await SettingsSvc.Save(next);
      aplicar(v.config);
    } catch (err) {
      setError(textoDe(err));
    }
  }

  async function borrarHistorial() {
    setBorrando(false);
    try {
      await HistorySvc.Clear();
      setCorridas(0);
      onAviso({ id: `hist-${Date.now()}`, tone: "success", title: "Historial borrado" });
    } catch (err) {
      setError(textoDe(err));
    }
  }

  const tema = prefs?.theme || Theme.ThemeDark;
  const prod = sesion.environment === "production";
  // Cuenta consultas distintas, no corridas: la misma repetida tres veces es
  // una entrada con ×3, y es lo que se ve en Historial.
  const cuantas =
    corridas === null
      ? "las consultas corridas"
      : corridas === 0
        ? "nada: no hay consultas corridas"
        : corridas === 1
          ? "la única consulta corrida"
          : `las ${corridas >= 500 ? "500 o más" : corridas} consultas distintas corridas`;

  return (
    <>
      <div className={cx(styles.cabecera, prod && styles.cabeceraProd)}>
        <div className={styles.tituloCaja}>
          <span className={styles.titulo}>Ajustes</span>
          <span className={styles.subtitulo}>{sesion.describe}</span>
        </div>
      </div>

      <main className={styles.cuerpo}>
        {error ? (
          <div className={cx(styles.aviso, styles.avisoMal)}>
            <span>{error}</span>
          </div>
        ) : null}

        <div className={styles.opcion}>
          <div className={styles.opcionTexto}>
            <strong>Tema</strong>
            <span>Sigue el del sistema si lo dejás en automático.</span>
          </div>
          <div className={styles.opcionControl}>
            <Selector valor={tema} opciones={TEMAS} titulo="Tema" ariaLabel="Tema" ui onChange={(v) => void cambiarTema(v as Theme)} />
          </div>
        </div>

        <div className={styles.opcion}>
          <div className={styles.opcionTexto}>
            <strong>Borrar el historial de esta conexión</strong>
            <span>Se van {cuantas}. Las guardadas quedan.</span>
          </div>
          <Button className={styles.borrar} onClick={() => setBorrando(true)} disabled={corridas === 0}>
            Borrar…
          </Button>
        </div>

        <div className={styles.opcion}>
          <div className={styles.opcionTexto}>
            <strong>Desconectar</strong>
            <span>{sesion.name}: cierra la sesión{" "}y el túnel si hay, y vuelve a la lista.</span>
          </div>
          <Button onClick={onDesconectar}>Salir</Button>
        </div>

        <div className={styles.proteccion}>
          <span className={styles.rotulo}>Qué protege y qué no</span>
          <p>
            Las contraseñas y la clave privada se guardan cifradas en el Keystore del teléfono y se abren con tu
            huella o tu rostro, cada vez. Nunca salen del equipo.
          </p>
          <p>
            La pantalla no aparece en la vista de apps recientes ni se puede capturar. La sesión se cierra sola a
            los dos minutos en segundo plano.
          </p>
          <p>Nada de esto se copia a la nube: si perdés el teléfono, perdés las credenciales guardadas, no la base.</p>
          <div className={styles.aviso}>
            <span>No te cubre en un teléfono rooteado, ni de una persona a la que se le fuerza el dedo.</span>
          </div>
        </div>

        <div className={styles.opcion}>
          <div className={styles.opcionTexto}>
            <strong>Versión</strong>
            <span className={styles.monoChico}>{version || "…"}</span>
          </div>
        </div>
      </main>

      <ConfirmDialog
        open={borrando}
        title="Borrar el historial"
        severidad="aviso"
        etiqueta="Borrar"
        onClose={() => setBorrando(false)}
        onConfirm={() => void borrarHistorial()}
      >
        Se van {cuantas} en esta conexión, en este teléfono. Las de otras conexiones y las consultas guardadas con
        nombre quedan donde están.
      </ConfirmDialog>
    </>
  );
}
