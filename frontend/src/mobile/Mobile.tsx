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
import { Conectando, ErrorDeConexion } from "./Conectar";
import { ClaveDelHost } from "./ClaveDelHost";
import { Credenciales } from "./Credenciales";
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
 *
 * La raíz lleva `data-movil`: es lo que convierte los diálogos y los toasts
 * de `components/ui` en hojas del teléfono (M01–M15).
 */
export function Mobile() {
  const [conexiones, setConexiones] = useState<ConnectionView[] | null>(null);
  const [sesion, setSesion] = useState<SessionView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [aviso, setAviso] = useState<ToastItem | null>(null);
  // Credenciales abiertas desde el error de conexión, para arreglar y volver.
  const [credenciales, setCredenciales] = useState<ConnectionView | null>(null);

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

  // El hook es dueño del orden inspección → clave del host → conexión; acá
  // solo se dibujan sus tres estados con las pantallas del teléfono.
  const { connect, estado, acciones } = useConectar({
    onStart: () => setError(null),
    onConnected: () => void refrescarSesion(),
    onError: setError,
  });

  function contenido() {
    if (conexiones === null) {
      return (
        <div className={styles.centro}>
          <span className={styles.aro} />
        </div>
      );
    }
    if (sesion) {
      return (
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
      );
    }
    if (estado.conectando) {
      return <Conectando view={estado.conectando.view} onCancelar={acciones.cancelar} />;
    }
    if (estado.failure) {
      return (
        <ErrorDeConexion
          failure={estado.failure}
          onVolver={acciones.cerrarError}
          onReintentar={acciones.reintentar}
          onCredenciales={() => {
            const view = estado.failure?.connection ?? null;
            acciones.cerrarError();
            setCredenciales(view);
          }}
        />
      );
    }
    return (
      <Conexiones
        conexiones={conexiones}
        error={error}
        onConectar={(v) => void connect(v)}
        onRecargar={() => recargar().catch((err) => setError(textoDe(err)))}
        onAviso={setAviso}
        onError={setError}
      />
    );
  }

  return (
    <div className={styles.app} data-movil="">
      {credenciales ? (
        <Credenciales
          view={credenciales}
          onClose={() => {
            setCredenciales(null);
            void recargar().catch((err) => setError(textoDe(err)));
          }}
          onSaved={(mensaje) => {
            setCredenciales(null);
            void recargar().catch((err) => setError(textoDe(err)));
            setAviso({ id: `cred-${Date.now()}`, tone: "success", title: mensaje });
          }}
        />
      ) : (
        contenido()
      )}

      {estado.hostKey ? (
        <ClaveDelHost
          inspection={estado.hostKey.inspection}
          onCancelar={acciones.cancelarHostKey}
          onConectarUnaVez={acciones.conectarUnaVez}
          onConfiar={acciones.confiar}
        />
      ) : null}

      {aviso ? <ToastStack toasts={[aviso]} onDismiss={() => setAviso(null)} /> : null}
    </div>
  );
}
