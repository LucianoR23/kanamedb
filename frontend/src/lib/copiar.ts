import { textoDe } from "./dialogos";

/**
 * alPortapapeles escribe texto en el portapapeles del sistema.
 *
 * Lanza si no se pudo, y quien llama lo dice: un «Copiar» que falla en silencio
 * es la peor forma de fallar de una acción, porque el error es indistinguible
 * de haber copiado bien. Ya pasó una vez con el botón del editor —ver el
 * registro de la Iteración 7— y por eso esto no traga nada.
 */
export async function alPortapapeles(texto: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(texto);
  } catch (err) {
    throw new Error(`No se pudo escribir en el portapapeles. (${textoDe(err)})`);
  }
}

/**
 * textoDeCelda es lo que se copia de UNA celda: el valor tal cual.
 *
 * Sin comillas, sin formato y sin encabezado — copiar una celda es sacar su
 * contenido, no exportarla.
 *
 * NULL copia la cadena VACÍA, que es lo que una celda sin valor significa al
 * pegarla en cualquier otro lado. Copiar la palabra `NULL` metería un texto de
 * cuatro letras donde no había nada, y eso al pegarlo en una consulta o en una
 * planilla es un valor inventado. Lo que se pierde es poder distinguir NULL de
 * la cadena vacía, y para eso está el visor, que lo dice con todas las letras.
 */
export function textoDeCelda(valor: string | null): string {
  return valor ?? "";
}
