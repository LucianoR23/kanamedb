import { useEffect, useState } from "react";
import * as Connections from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type {
  ConnectionView,
  SessionView,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { ToastStack } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { cargar as cargarPreferencias } from "../lib/preferencias";
import { textoDe } from "../lib/dialogos";
import { useConectar } from "../lib/useConectar";
import { Conexiones } from "./Conexiones";
import { Sesion } from "./Sesion";
import styles from "./mobile.module.css";

/**
 * La interfaz del teléfono. Ver kaname-android.md: leer, consultar y
 * corregir filas; sin ERD, sin DDL, sin importar CSV.
 *
 * Dos pantallas de arriba: la lista de conexiones y la sesión abierta. Todo
 * lo demás —árbol, consulta, historial, ajustes— vive dentro de la sesión con
 * pestañas abajo. El backend es exactamente el mismo que el de escritorio;
 * lo que cambia es la forma.
 */
export function Mobile() {
  const [conexiones, setConexiones] = useState<ConnectionView[] | null>(null);
  const [sesion, setSesion] = useState<SessionView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [aviso, setAviso] = useState<ToastItem | null>(null);

  async function recargar() {
    const lista = (await Connections.List()) ?? [];
    setConexiones(lista);
    return lista;
  }

  async function refrescarSesion() {
    const v = await SessionSvc.Current();
    setSesion(v.connected ? v : null);
    return v;
  }

  useEffect(() => {
    let cancelado = false;
    (async () => {
      void cargarPreferencias().catch(() => {});
      try {
        const [lista, v] = await Promise.all([Connections.List(), SessionSvc.Current()]);
        if (cancelado) return;
        setConexiones(lista ?? []);
        setSesion(v.connected ? v : null);
      } catch (err) {
        if (cancelado) return;
        setError(textoDe(err));
        setConexiones([]);
      }
    })();
    return () => {
      cancelado = true;
    };
  }, []);

  // Al volver al frente se pregunta si la sesión sigue: el bloqueo en segundo
  // plano (movil_android.go) la cierra sola después de un rato, y la UI se
  // entera acá y no cuando falle el próximo toque. El motivo lo dice Go.
  useEffect(() => {
    if (!sesion) return;
    const alVolver = () => {
      if (document.visibilityState !== "visible") return;
      void SessionSvc.Current()
        .then((v) => {
          if (v.connected) return;
          setSesion(null);
          if (v.closedReason) {
            setAviso({
              id: `cierre-${Date.now()}`,
              tone: "info",
              title: "Sesión cerrada",
              detail: v.closedReason,
            });
          }
        })
        .catch(() => {});
    };
    document.addEventListener("visibilitychange", alVolver);
    return () => document.removeEventListener("visibilitychange", alVolver);
  }, [sesion]);

  const { connect, dialogos } = useConectar({
    onStart: () => setError(null),
    onConnected: () => void refrescarSesion(),
    onError: setError,
  });

  if (conexiones === null) {
    return (
      <div className={styles.app}>
        <div className={styles.vacio}>Cargando…</div>
      </div>
    );
  }

  return (
    <div className={styles.app}>
      {sesion ? (
        <Sesion
          sesion={sesion}
          onDesconectar={() => {
            void SessionSvc.Disconnect()
              .then(() => setSesion(null))
              .catch((err) => setError(textoDe(err)));
          }}
          onSesionCerrada={(motivo) => {
            setSesion(null);
            if (motivo) {
              setAviso({ id: `cierre-${Date.now()}`, tone: "info", title: "Sesión cerrada", detail: motivo });
            }
          }}
          onAviso={setAviso}
        />
      ) : (
        <Conexiones
          conexiones={conexiones}
          error={error}
          onConectar={(v) => void connect(v)}
          onRecargar={() => recargar().catch((err) => setError(textoDe(err)))}
          onAviso={setAviso}
          onError={setError}
        />
      )}
      {dialogos}
      {aviso ? <ToastStack toasts={[aviso]} onDismiss={() => setAviso(null)} /> : null}
    </div>
  );
}
