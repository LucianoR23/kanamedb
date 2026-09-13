import { useState } from "react";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import * as HostsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/hosts";
import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Verdict } from "../../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import type { Inspection } from "../../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import { Connecting } from "../screens/Connecting";
import { HostKeyDialog } from "../screens/HostKeyDialog";
import { ConnectionError } from "../screens/ConnectionError";
import type { ConnectionFailure } from "../screens/ConnectionError";

/**
 * Conectar, con la verificación de la clave del bastión antes.
 *
 * Es un hook y no parte de App porque lo usan dos interfaces —la de
 * escritorio y la del teléfono— y esta es la parte que no se duplica: el
 * orden inspección → diálogo TOFU → conexión es lo que garantiza que el
 * bastión no reciba ninguna credencial hasta que la persona decidió confiar.
 *
 * El hook es dueño de los tres estados intermedios (conectando, clave del
 * host pendiente, fallo) y de sus diálogos; quien lo usa solo dice qué hacer
 * al conectar y qué hacer si la persona quiere editar la conexión desde el
 * error.
 *
 * Devuelve dos formas de lo mismo: `dialogos`, ya dibujados como los usa el
 * escritorio, y `estado` + `acciones`, para que el teléfono dibuje los suyos
 * —hojas a pantalla completa— sobre exactamente la misma máquina de estados.
 * Lo que no se duplica es la decisión; la piel sí puede ser otra.
 */
export function useConectar({
  onStart,
  onConnected,
  onEdit,
  onError,
}: {
  /** Al empezar a conectar: para limpiar un error anterior de la pantalla. */
  onStart?: () => void;
  onConnected: () => void;
  /** Sin editor —el teléfono no tiene uno completo— el botón no aparece. */
  onEdit?: (view: ConnectionView) => void;
  onError?: (mensaje: string) => void;
}) {
  const [failure, setFailure] = useState<ConnectionFailure | null>(null);

  // La verificación de la clave del bastión, esperando decisión.
  //
  // Se guarda la conexión junto con la inspección porque el diálogo puede
  // terminar en "conectar", y para eso hace falta saber a qué conexión volver.
  const [hostKey, setHostKey] = useState<{
    view: ConnectionView;
    inspection: Inspection;
  } | null>(null);
  const [knownHostsPath, setKnownHostsPath] = useState("");

  // Lo que se está conectando ahora, si hay algo.
  //
  // Guarda la promesa además de la vista porque el botón de cancelar la
  // necesita: las llamadas generadas por Wails se pueden cancelar, y cancelarlas
  // corta el contexto del lado de Go. Sin la referencia, el botón sería un
  // adorno que oculta la pantalla sin cortar nada.
  const [conectando, setConectando] = useState<{
    view: ConnectionView;
    cancelar: () => void;
  } | null>(null);

  /**
   * Conecta, verificando antes la clave del bastión si la conexión usa túnel.
   *
   * El orden importa y no es cosmético: se inspecciona ANTES de conectar, y la
   * inspección corta el handshake antes de la autenticación. Cuando aparece el
   * diálogo, el bastión todavía no recibió ninguna credencial.
   */
  async function connect(view: ConnectionView) {
    onStart?.();
    setFailure(null);

    if (view.connection.ssh.enabled) {
      const pedido = HostsSvc.Inspect(view.connection);
      setConectando({ view, cancelar: () => pedido.cancel() });
      let insp;
      try {
        insp = await pedido;
      } catch {
        // Cancelada por la persona, o el puente falló. En los dos casos no hay
        // nada que reportar: cancelar es lo que pidió.
        setConectando(null);
        return;
      }
      setConectando(null);
      if (!insp.ok || !insp.inspection) {
        setFailure({
          connection: view,
          kind: "network",
          message: "No se pudo contactar al bastión SSH.",
          hint: "Revisá el host, el puerto y que el servidor esté escuchando.",
          detail: insp.error ?? "",
          sqlState: "",
          elapsedMs: 0,
        });
        return;
      }
      if (insp.inspection.verdict !== Verdict.VerdictTrusted) {
        // Se corta acá: la decisión es de la persona, y hasta que la tome no
        // se manda nada.
        setKnownHostsPath(await HostsSvc.KnownHostsPath());
        setHostKey({ view, inspection: insp.inspection });
        return;
      }
    }
    await conectarDeVerdad(view);
  }

  async function conectarDeVerdad(view: ConnectionView, acceptOnce = "") {
    // El tiempo se mide acá y no en Go: un rechazo inmediato y un timeout de
    // diez segundos se ven distinto, y eso ya dice algo antes de leer nada.
    const inicio = performance.now();
    const pedido = SessionSvc.ConnectAccepting(view.connection.id, acceptOnce);
    setConectando({ view, cancelar: () => pedido.cancel() });
    try {
      // El fallo no es un error de Go: la promesa se resuelve igual. Por eso
      // Connect devuelve un resultado con `ok` en vez de un par, que se podía
      // ignorar a medias.
      const res = await pedido;
      if (!res.ok) {
        const f = res.failure;
        setFailure({
          connection: view,
          kind: f?.kind ?? "other",
          message: f?.message ?? "No se pudo conectar.",
          hint: f?.hint ?? "",
          detail: f?.detail ?? "",
          sqlState: f?.sqlState ?? "",
          elapsedMs: Math.round(performance.now() - inicio),
        });
        return;
      }
      onConnected();
    } catch (err) {
      // Cancelada por la persona: la promesa de Wails rechaza con CancelError
      // y eso no es un fallo que mostrar —en el teléfono terminaba en la
      // pantalla de error con «Reintentar», por haber apretado Cancelar—.
      if (err instanceof Error && err.name === "CancelError") return;
      // El servicio devuelve el fallo ya interpretado; si el puente falla, se
      // muestra lo que haya en vez de nada.
      const f = err as Partial<ConnectionFailure> | undefined;
      setFailure({
        connection: view,
        kind: f?.kind ?? "other",
        message: f?.message ?? (err instanceof Error ? err.message : String(err)),
        hint: f?.hint ?? "",
        detail: f?.detail ?? "",
        sqlState: f?.sqlState ?? "",
        elapsedMs: Math.round(performance.now() - inicio),
      });
    } finally {
      setConectando(null);
    }
  }

  const dialogos = (
    <>
      {conectando ? (
        <Connecting
          target={conectando.view.uri}
          bastion={
            conectando.view.connection.ssh.enabled
              ? `${conectando.view.connection.ssh.user}@${conectando.view.connection.ssh.host}:${conectando.view.connection.ssh.port}`
              : ""
          }
          onCancel={() => {
            conectando.cancelar();
            setConectando(null);
          }}
        />
      ) : null}

      {hostKey ? (
        <HostKeyDialog
          inspection={hostKey.inspection}
          knownHostsPath={knownHostsPath}
          onCancel={() => setHostKey(null)}
          onConnectOnce={() => {
            // Sin guardar: la aceptación vale para este intento y nada más.
            // El backend la recibe por AcceptOnce y no toca known_hosts.
            const { view, inspection } = hostKey;
            setHostKey(null);
            void conectarDeVerdad(view, inspection.presented.fingerprint);
          }}
          onTrust={() => {
            const { view, inspection } = hostKey;
            setHostKey(null);
            void HostsSvc.Trust(inspection.address, inspection.authorizedKey)
              .then(() => conectarDeVerdad(view))
              .catch((err) => onError?.(err instanceof Error ? err.message : String(err)));
          }}
        />
      ) : null}

      {failure ? (
        <ConnectionError
          failure={failure}
          onClose={() => setFailure(null)}
          onRetry={() => {
            const view = failure.connection;
            setFailure(null);
            void connect(view);
          }}
          {...(onEdit
            ? {
                onEdit: () => {
                  const view = failure.connection;
                  setFailure(null);
                  onEdit(view);
                },
              }
            : {})}
        />
      ) : null}
    </>
  );

  /** El estado crudo, para quien dibuja sus propias pantallas. */
  const estado = {
    conectando: conectando ? { view: conectando.view } : null,
    hostKey: hostKey ? { view: hostKey.view, inspection: hostKey.inspection } : null,
    failure,
  };

  /** Las mismas acciones que disparan los diálogos de arriba. */
  const acciones = {
    cancelar() {
      if (!conectando) return;
      conectando.cancelar();
      setConectando(null);
    },
    cancelarHostKey() {
      setHostKey(null);
    },
    conectarUnaVez() {
      if (!hostKey) return;
      const { view, inspection } = hostKey;
      setHostKey(null);
      void conectarDeVerdad(view, inspection.presented.fingerprint);
    },
    confiar() {
      if (!hostKey) return;
      const { view, inspection } = hostKey;
      setHostKey(null);
      void HostsSvc.Trust(inspection.address, inspection.authorizedKey)
        .then(() => conectarDeVerdad(view))
        .catch((err) => onError?.(err instanceof Error ? err.message : String(err)));
    },
    cerrarError() {
      setFailure(null);
    },
    reintentar() {
      if (!failure) return;
      const view = failure.connection;
      setFailure(null);
      void connect(view);
    },
  };

  return { connect, dialogos, conectando: conectando !== null, estado, acciones };
}
