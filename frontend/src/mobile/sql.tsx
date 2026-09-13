import type { ReactNode } from "react";
import styles from "./mobile.module.css";

/** Las palabras que se pintan. Una lista corta: es color, no un parser. */
const PALABRAS = new Set([
  "select", "from", "where", "and", "or", "not", "in", "is", "null", "as", "on",
  "join", "left", "right", "inner", "outer", "group", "by", "order", "limit",
  "offset", "insert", "into", "values", "update", "set", "delete", "returning",
  "distinct", "having", "union", "all", "case", "when", "then", "else", "end",
  "asc", "desc", "like", "between", "exists", "with", "count", "sum", "avg",
  "min", "max", "true", "false", "default",
]);

/**
 * Resalta SQL para mostrarlo: palabras clave, cadenas, números y signos, con
 * los tokens `--syn-*` de S00. Es lo mismo que hace CodeMirror en la PC, en
 * la medida justa para una vista previa: no entiende dialectos ni comenta
 * nada, solo colorea lo que cualquiera reconoce al leer.
 *
 * Devuelve nodos de React, nunca HTML: el texto es una consulta escrita por
 * alguien o generada con valores de la base, y va como texto.
 */
export function resaltarSql(sql: string): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /('(?:[^']|'')*'?)|(\b\d+(?:\.\d+)?\b)|(\b[a-zA-Z_]+\b)|([=<>!*+\-/(),;])/g;
  let ultimo = 0;
  let m: RegExpExecArray | null;
  let k = 0;
  while ((m = re.exec(sql)) !== null) {
    if (m.index > ultimo) out.push(sql.slice(ultimo, m.index));
    const [texto, cadena, numero, palabra, signo] = m;
    if (cadena) out.push(<span key={k++} className={styles.str}>{texto}</span>);
    else if (numero) out.push(<span key={k++} className={styles.num}>{texto}</span>);
    else if (palabra && PALABRAS.has(palabra.toLowerCase())) out.push(<span key={k++} className={styles.kw}>{texto}</span>);
    else if (signo) out.push(<span key={k++} className={styles.punc}>{texto}</span>);
    else out.push(texto);
    ultimo = m.index + texto.length;
  }
  if (ultimo < sql.length) out.push(sql.slice(ultimo));
  return out;
}
