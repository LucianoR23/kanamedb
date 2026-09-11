import { Dialogs } from "@wailsio/runtime";
import { cancelado, textoDe } from "./dialogos";

/**
 * Los selectores del sistema para compartir conexiones: dónde guardar el
 * archivo exportado, y cuál importar.
 *
 * El archivo tiene el formato de la libreta —`connections.toml`— y nunca lleva
 * un secreto; eso lo garantiza Go, no esta capa. Acá solo se elige la ruta.
 *
 * Los dos devuelven la cadena vacía si la persona cancela.
 */

/** Un nombre de archivo a partir de lo que se exporta, sin caracteres que un
 *  sistema de archivos rechace. */
export function nombreDeArchivoCompartido(base: string): string {
  const limpio = base
    .trim()
    .replace(/[\\/:*?"<>|]+/g, "-")
    .replace(/\s+/g, " ")
    .slice(0, 60);
  return `${limpio || "conexiones"}.toml`;
}

export async function elegirDestinoLibreta(nombre: string): Promise<string> {
  try {
    const ruta = await Dialogs.SaveFile({
      Title: "Exportar conexiones para compartir",
      Filename: nombre,
      CanCreateDirectories: true,
      AllowsOtherFiletypes: true,
      Filters: [
        { DisplayName: "Libreta de conexiones (TOML)", Pattern: "*.toml" },
        { DisplayName: "Todos los archivos", Pattern: "*" },
      ],
    });
    return ruta ?? "";
  } catch (err) {
    if (cancelado(err)) return "";
    throw new Error(`No se pudo abrir el selector de archivos del sistema. (${textoDe(err)})`);
  }
}

export async function elegirLibreta(): Promise<string> {
  try {
    const ruta = await Dialogs.OpenFile({
      Title: "Importar conexiones",
      CanChooseFiles: true,
      AllowsOtherFiletypes: true,
      Filters: [
        { DisplayName: "Libreta de conexiones (TOML)", Pattern: "*.toml" },
        { DisplayName: "Todos los archivos", Pattern: "*" },
      ],
    });
    return ruta ?? "";
  } catch (err) {
    if (cancelado(err)) return "";
    throw new Error(`No se pudo abrir el selector de archivos del sistema. (${textoDe(err)})`);
  }
}
