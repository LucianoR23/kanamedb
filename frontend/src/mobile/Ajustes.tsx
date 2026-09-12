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
import { BarraDeSesion } from "./Sesion";
import styles from "./mobile.module.css";

/**
 * S23 mínimo: tema, borrar el historial, desconectar, y la versión. El
 * bloqueo, la captura bloqueada y la biometría no son ajustes: son cómo
 * funciona la app en el teléfono (kaname-android.md).
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
  const [borrando, setBorrando] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    AppInfo.Get()
      .then((i) => setVersion(`${i.version} · ${i.platform}`))
      .catch(() => {});
  }, []);

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
      onAviso({ id: `hist-${Date.now()}`, tone: "success", title: "Historial borrado" });
    } catch (err) {
      setError(textoDe(err));
    }
  }

  const tema = prefs?.theme || Theme.ThemeDark;

  return (
    <>
      <BarraDeSesion sesion={sesion} titulo="Ajustes" />
      <main className={styles.cuerpo}>
        {error ? <div className={styles.error}>{error}</div> : null}

        <div className={styles.opcion}>
          <div>
            <span>Tema</span>
            <small>El claro y el oscuro son los mismos que en la PC.</small>
          </div>
          <div className={styles.acciones} style={{ flex: "none" }}>
            {(
              [
                [Theme.ThemeDark, "Oscuro"],
                [Theme.ThemeLight, "Claro"],
                [Theme.ThemeSystem, "Sistema"],
              ] as const
            ).map(([t, nombre]) => (
              <Button key={t} size="sm" variant={tema === t ? "secondary" : "ghost"} onClick={() => void cambiarTema(t)}>
                {nombre}
              </Button>
            ))}
          </div>
        </div>

        <div className={styles.opcion}>
          <div>
            <span>Historial</span>
            <small>Lo que corriste contra esta conexión desde este teléfono. No sale del aparato.</small>
          </div>
          <Button size="sm" variant="dangerOutline" onClick={() => setBorrando(true)}>
            Borrar…
          </Button>
        </div>

        <div className={styles.opcion}>
          <div>
            <span>Sesión</span>
            <small>{sesion.describe}</small>
          </div>
          <Button size="sm" onClick={onDesconectar}>
            Desconectar
          </Button>
        </div>

        <p className={styles.nota}>
          Las contraseñas están cifradas con una clave del equipo que solo se abre con tu huella o tu
          rostro; la pantalla no sale en capturas; y si Kaname queda dos minutos en segundo plano,
          la sesión se cierra sola. Lo que esto no cubre: un teléfono rooteado, y una persona a la
          que se le fuerza el dedo.
        </p>
        <p className={styles.nota}>Kaname {version}</p>
      </main>

      <ConfirmDialog
        open={borrando}
        title="Borrar el historial"
        severidad="aviso"
        etiqueta="Borrar"
        onClose={() => setBorrando(false)}
        onConfirm={() => void borrarHistorial()}
      >
        Se borra el historial de consultas de <strong>esta conexión</strong> en este teléfono. Las
        de otras conexiones y las consultas guardadas con nombre quedan.
      </ConfirmDialog>
    </>
  );
}
