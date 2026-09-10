import { Dialogs } from "@wailsio/runtime";
import { Format } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import type { Options } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import { cancelado, textoDe } from "./dialogos";

/**
 * Lo que la interfaz sabe de cada formato: cómo se llama y qué es. La
 * extensión la dice Go (`Exports.Formats`), para que un cambio ahí no
 * necesite un cambio acá.
 */
export interface FormatoInfo {
  key: Format;
  tag: string;
  label: string;
  nota: string;
  /** Qué opciones de S19 tienen sentido en este formato. */
  opciones: readonly Opcion[];
}

export type Opcion = "delimiter" | "header" | "nullEmpty" | "quoteAll" | "bom" | "gzip";

export const FORMATOS: readonly FormatoInfo[] = [
  {
    key: Format.CSV,
    tag: "CSV",
    label: "CSV",
    nota: "planillas e importación",
    opciones: ["delimiter", "header", "nullEmpty", "quoteAll", "bom", "gzip"],
  },
  {
    key: Format.$JSON,
    tag: "JSON",
    label: "JSON",
    nota: "un array de objetos",
    opciones: ["gzip"],
  },
  {
    key: Format.JSONL,
    tag: "JSONL",
    label: "JSON Lines",
    nota: "un objeto por línea",
    opciones: ["gzip"],
  },
  {
    key: Format.Markdown,
    tag: "MD",
    label: "Tabla Markdown",
    nota: "documentos y tickets",
    opciones: ["nullEmpty", "gzip"],
  },
];

export const DELIMITADORES: readonly { valor: string; label: string }[] = [
  { valor: ",", label: ", coma" },
  { valor: ";", label: "; punto y coma" },
  { valor: "\t", label: "⇥ tabulación" },
];

/** Las opciones por defecto: las del valor cero de Go. */
export function opcionesPorDefecto(): Options {
  return {
    delimiter: ",",
    noHeader: false,
    nullAsEmpty: false,
    quoteAll: false,
    bom: false,
    gzip: false,
  };
}

/**
 * elegirDestino abre el selector de «guardar como» del sistema.
 *
 * Devuelve la cadena vacía si la persona cancela. La extensión viene de Go
 * para que el nombre sugerido y el filtro digan lo mismo.
 */
export async function elegirDestino(nombre: string, extension: string, gzip: boolean): Promise<string> {
  const ext = gzip ? `${extension}.gz` : extension;
  try {
    const ruta = await Dialogs.SaveFile({
      Title: "Guardar la exportación",
      Filename: `${nombre}${ext}`,
      CanCreateDirectories: true,
      AllowsOtherFiletypes: true,
      Filters: [
        { DisplayName: `Archivos ${ext}`, Pattern: `*${ext}` },
        { DisplayName: "Todos los archivos", Pattern: "*" },
      ],
    });
    return ruta ?? "";
  } catch (err) {
    if (cancelado(err)) return "";
    throw new Error(`No se pudo abrir el selector de archivos del sistema. (${textoDe(err)})`);
  }
}

/** Un nombre de archivo que no tenga lo que ningún sistema acepta. */
export function nombreDeArchivo(base: string): string {
  const limpio = base.replace(/[\\/:*?"<>|]+/g, "-").trim();
  return limpio || "exportacion";
}

/** «12,3 kB», con la coma del castellano. */
export function tamano(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const unidades = ["kB", "MB", "GB"];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < unidades.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toLocaleString("es", { maximumFractionDigits: 1 })} ${unidades[i]}`;
}
