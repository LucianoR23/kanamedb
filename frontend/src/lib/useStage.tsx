import { useState } from "react";
import type { ReactNode } from "react";
import type { Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { GridEdits } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { ConfirmDialog } from "../components/ui";

/** Una operación que prepara algo en Go y puede pedir la confirmación de
 *  producción. Recibe la palabra escrita, o "" en el primer intento. */
type Preparacion = (confirm: string) => Promise<unknown>;

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
 *
 * Sirve para un cambio suelto (`stage`) y para la tanda de la grilla
 * (`stageGrid`), que entra entera con una sola palabra.
 */
export function useStage(onHecho: () => void) {
  const [pendiente, setPendiente] = useState<{
    op: Preparacion;
    palabra: string;
    texto: ReactNode;
    onOk?: () => void;
  } | null>(null);
  const [error, setError] = useState("");

  // `onOk` es lo que quiere saber quien llamó a ESTA preparación, además del
  // aviso general: la grilla limpia sus ediciones solo si su tanda entró, y
  // no cuando entra un cambio de estructura de la misma pestaña. Se guarda con
  // lo pendiente para que el reintento con la palabra escrita también lo llame.
  async function intentar(op: Preparacion, texto: ReactNode, confirm = "", onOk?: () => void) {
    setError("");
    try {
      await op(confirm);
      setPendiente(null);
      onHecho();
      onOk?.();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      const palabra = palabraDeConfirmacion(msg);
      if (palabra) {
        setPendiente({ op, palabra, texto, ...(onOk ? { onOk } : {}) });
        return;
      }
      setError(msg);
    }
  }

  function stage(c: Change) {
    return intentar(
      (confirm) => SessionSvc.Stage(c, confirm),
      <>
        {/* Un cambio de objeto no tiene tabla —una vista y una función viven
            solas— así que se nombra lo que el cambio TOCA, no siempre una
            tabla. Sin esto, reemplazar una vista en producción preguntaba
            «destructivo sobre …» con el hueco vacío. */}
        Estás por preparar un cambio <strong>destructivo</strong> sobre{" "}
        <code>{c.table || c.name}</code> en
        una conexión marcada como producción. Todavía no se va a aplicar nada: esto lo suma a la
        lista de cambios pendientes, y el apply vuelve a preguntar. Lo que se pierda al aplicarlo
        no se recupera desde Kaname.
      </>,
    );
  }

  function stageGrid(e: GridEdits, onOk: () => void) {
    const borradas = (e.deletes ?? []).length;
    return intentar(
      (confirm) => SessionSvc.StageGrid(e, confirm),
      <>
        Estás por preparar el borrado de{" "}
        <strong>
          {borradas} {borradas === 1 ? "fila" : "filas"}
        </strong>{" "}
        de <code>{e.table}</code> en una conexión marcada como producción. Todavía no se va a
        aplicar nada: esto suma las ediciones a la lista de cambios pendientes, y el apply vuelve a
        preguntar. Una fila borrada no se recupera desde Kaname.
      </>,
      "",
      onOk,
    );
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
      onConfirm={(escrito) =>
        void intentar(pendiente.op, pendiente.texto, escrito, pendiente.onOk)
      }
    >
      {pendiente.texto}
    </ConfirmDialog>
  ) : null;

  return { stage, stageGrid, dialogo, error, limpiarError: () => setError("") };
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
