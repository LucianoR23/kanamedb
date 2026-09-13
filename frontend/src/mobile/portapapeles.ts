import { Clipboard } from "@wailsio/runtime";

/**
 * Copia un texto al portapapeles del teléfono.
 *
 * Primero por Wails, que en Android es el `ClipboardManager` del sistema y no
 * pide activación del usuario ni contexto seguro; si no hay puente —la vista
 * `?movil` en un navegador de la PC— cae al portapapeles del navegador.
 * Rechaza si ninguno de los dos pudo: quien llama no dice «Copiado» sin
 * saberlo.
 */
export async function copiar(texto: string): Promise<void> {
  try {
    await Clipboard.SetText(texto);
  } catch {
    await navigator.clipboard.writeText(texto);
  }
}
