import { Format } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import type { Options } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import * as ExportsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/exports";
import { textoDe } from "./dialogos";
import { opcionesPorDefecto } from "./exportar";

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

/**
 * Los formatos en que se pueden copiar filas.
 *
 * Son los mismos escritores que usa la exportación: copiar y exportar tienen
 * que dar exactamente el mismo texto, o el archivo y el portapapeles dirían
 * cosas distintas de las mismas filas y nadie sabría cuál creer.
 *
 * El orden es el del uso: TSV primero porque pegar en una planilla es lo más
 * común de largo.
 */
export interface FormatoDeCopia {
  key: string;
  label: string;
  nota: string;
  formato: Format;
  opciones: Partial<Options>;
}

export const FORMATOS_DE_COPIA: readonly FormatoDeCopia[] = [
  {
    key: "tsv",
    label: "TSV",
    nota: "para pegar en una planilla",
    formato: Format.CSV,
    // Separado por tabulación y con el NULL vacío: es lo que una planilla pega
    // en celdas, y la marca de nulo de las convenciones de COPY ahí es ruido.
    //
    // NO se encomilla todo, que es lo que hacen otras herramientas: Excel no
    // siempre saca las comillas al pegar y quedaría `"81758"` literal en la
    // celda. El escritor encomilla SOLO donde hace falta —un valor que
    // contenga una tabulación o un salto de línea— así que esos casos están
    // cubiertos igual.
    //
    // Lo que se pierde es distinguir un NULL de la cadena vacía. Es a
    // propósito: una planilla no tiene forma de mostrar esa diferencia.
    opciones: { delimiter: "\t", nullAsEmpty: true },
  },
  {
    key: "csv",
    label: "CSV",
    // El mismo CSV que escribe la exportación a archivo, con las convenciones
    // de `COPY … CSV`. Es el que sirve para volver a importar, y el único de
    // los cuatro que distingue un NULL de una cadena vacía.
    nota: "como el archivo, y distingue el NULL",
    formato: Format.CSV,
    opciones: { delimiter: "," },
  },
  {
    key: "json",
    label: "JSON",
    nota: "un array de objetos",
    formato: Format.$JSON,
    opciones: {},
  },
  {
    key: "markdown",
    label: "Markdown",
    nota: "alineado, para un ticket",
    formato: Format.Markdown,
    // Alineado: una tabla pegada en un ticket se lee antes de renderizarse.
    // Solo vale acá, donde las filas ya están juntas; al escribir un archivo
    // Go lo rechaza.
    opciones: { align: true },
  },
];

/**
 * textoDeFilas arma el texto de esas filas en ese formato.
 *
 * Lo formatea GO, con los mismos escritores que la exportación. Hacerlo en
 * TypeScript habría duplicado las reglas de escape —la barra vertical del
 * Markdown, las comillas del CSV, los numéricos exactos del JSON— y la segunda
 * copia se habría atrasado sin que nadie lo note.
 */
export async function textoDeFilas(
  columns: readonly Column[],
  rows: readonly ((string | null)[] | null)[],
  formato: FormatoDeCopia,
): Promise<string> {
  return ExportsSvc.Render({
    format: formato.formato,
    options: { ...opcionesPorDefecto(), ...formato.opciones },
    columns: [...columns],
    rows: rows.map((r) => r ?? []),
  });
}
