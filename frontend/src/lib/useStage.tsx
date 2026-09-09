import { useState } from "react";
import type { Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { ConfirmDialog } from "../components/ui";

/**
 * Mandar una edición al changeset, con la confirmación de producción si hace
 * falta.
 *
 * La regla de CUÁNDO hace falta vive en Go: contra producción, un cambio
 * destructivo no entra sin que alguien escriba el nombre de la base. Acá no se
 * decide nada — se intenta, y si Go dice que falta confirmar se pregunta y se
 * reintenta con lo escrito.
 *
 * Que la regla esté del otro lado es el punto: una pantalla nueva que prepare
 * cambios queda protegida sin acordarse de nada, y una comprobación que solo
 * viviera acá sería un cartel, no una protección.
 */
export function useStage(onHecho: () => void) {
  const [pendiente, setPendiente] = useState<{ cambio: Change; palabra: string } | null>(null);
  const [error, setError] = useState("");

  async function stage(c: Change, confirm = "") {
    setError("");
    try {
      await SessionSvc.Stage(c, confirm);
      setPendiente(null);
      onHecho();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      const palabra = palabraDeConfirmacion(msg);
      if (palabra) {
        setPendiente({ cambio: c, palabra });
        return;
      }
      setError(msg);
    }
  }

  const dialogo = pendiente ? (
    <ConfirmDialog
      open
      severidad="produccion"
      title="Esto es producción"
      confirmar
      palabra={pendiente.palabra}
      etiqueta="Preparar el cambio"
      onClose={() => setPendiente(null)}
      onConfirm={(escrito) => void stage(pendiente.cambio, escrito)}
    >
      Estás por preparar un cambio <strong>destructivo</strong> sobre{" "}
      <code>{pendiente.cambio.table}</code> en una conexión marcada como producción. Todavía no se
      va a aplicar nada: esto lo suma a la lista de cambios pendientes, y el apply vuelve a
      preguntar. Lo que se pierda al aplicarlo no se recupera desde Kaname.
    </ConfirmDialog>
  ) : null;

  return { stage, dialogo, error, limpiarError: () => setError("") };
}

/**
 * Saca de un mensaje de Go la palabra que hay que escribir.
 *
 * Go la manda entre comillas dobles al final del error, que es donde le sirve a
 * una persona leyéndolo en un log. Acá se recorta para poder ponerla en el
 * campo: pedirle a alguien que escriba «el nombre de la base» sin decirle cuál
 * es sería una adivinanza.
 */
function palabraDeConfirmacion(mensaje: string): string {
  if (!mensaje.includes("hay que escribir")) return "";
  const m = /hay que escribir "([^"]+)"/.exec(mensaje);
  return m?.[1] ?? "";
}
