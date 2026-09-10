import { Dialogs } from "@wailsio/runtime";
import type { Options } from "../../bindings/github.com/LucianoR23/kanamedb/internal/csvimport";
import { cancelado, textoDe } from "./dialogos";

/**
 * elegirCSV abre el selector de archivos del sistema.
 *
 * El diálogo lo abre el sistema, no la página: el navegador da un `File` con
 * su contenido, y lo que la importación necesita es una RUTA — porque el
 * archivo lo lee Go de a una fila y nunca entra entero en memoria.
 *
 * Devuelve la cadena vacía si la persona cancela.
 */
export async function elegirCSV(): Promise<string> {
  try {
    const ruta = await Dialogs.OpenFile({
      Title: "Elegir el archivo a importar",
      CanChooseFiles: true,
      // Un archivo separado por tabulaciones suele llamarse `.tsv` o `.txt`, y
      // exportado de un sistema viejo puede no tener extensión.
      AllowsOtherFiletypes: true,
      Filters: [
        { DisplayName: "Archivos de texto separado", Pattern: "*.csv;*.tsv;*.txt" },
        { DisplayName: "Todos los archivos", Pattern: "*" },
      ],
    });
    return ruta ?? "";
  } catch (err) {
    if (cancelado(err)) return "";
    throw new Error(
      `No se pudo abrir el selector de archivos del sistema. (${textoDe(err)})`,
    );
  }
}

/** Los delimitadores que Go acepta. La tabulación se muestra con su símbolo
 *  porque un campo con un espacio ancho no se distingue de uno vacío. */
export const DELIMITADORES_CSV: readonly { valor: string; label: string }[] = [
  { valor: ",", label: ", coma" },
  { valor: ";", label: "; punto y coma" },
  { valor: "\t", label: "⇥ tabulación" },
  { valor: "|", label: "| barra" },
];

/** Las opciones por defecto de la lectura.
 *
 *  `hasHeader` arranca en true porque es lo que escribe cualquier planilla, y
 *  el paso de origen muestra las columnas detectadas: si la primera línea era
 *  un dato se ve enseguida y se destilda. */
export function opcionesDeLectura(): Options {
  return { delimiter: ",", hasHeader: true, emptyAsNull: false, trim: false };
}

/**
 * emparejar propone a qué columna de la tabla va cada columna del archivo.
 *
 * Empareja por nombre, sin distinguir mayúsculas ni espacios de los bordes, y
 * cada columna de la tabla se usa UNA sola vez: dos columnas del archivo que se
 * llamen igual no pueden ir las dos al mismo lado, y Go lo rechazaría. Lo que
 * no empareja queda sin elegir, que es más honesto que asignarlo por posición
 * —un archivo con las columnas en otro orden se importaría cruzado y en
 * silencio—.
 */
export function emparejar(
  columnasDelArchivo: readonly string[],
  columnasDeLaTabla: readonly string[],
): string[] {
  const porNombre = new Map<string, string>();
  for (const c of columnasDeLaTabla) porNombre.set(normalizar(c), c);
  const usadas = new Set<string>();
  return columnasDelArchivo.map((c) => {
    const destino = porNombre.get(normalizar(c));
    if (!destino || usadas.has(destino)) return "";
    usadas.add(destino);
    return destino;
  });
}

function normalizar(s: string): string {
  return s.trim().toLowerCase();
}

/** «4.182» con los puntos de miles del castellano. */
export function miles(n: number): string {
  return n.toLocaleString("es", { useGrouping: true });
}
