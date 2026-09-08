import { useEffect, useRef } from "react";
import { autocompletion, closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { PostgreSQL, sql } from "@codemirror/lang-sql";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap, highlightActiveLine, highlightActiveLineGutter, lineNumbers } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";
import type { Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import styles from "./SqlEditor.module.css";

/**
 * Resaltado por tokens del proyecto, no por colores literales.
 *
 * CodeMirror genera CSS a partir de este objeto, así que `var(--syn-kw)` llega
 * intacto a la hoja de estilos y el editor cambia de tema junto con el resto de
 * la aplicación. Con colores literales acá, el tema claro mostraría un editor
 * oscuro adentro de una app clara.
 */
const resaltado = HighlightStyle.define([
  { tag: [t.keyword, t.modifier, t.operatorKeyword], color: "var(--syn-kw)" },
  { tag: [t.string, t.special(t.string)], color: "var(--syn-str)" },
  { tag: [t.number, t.bool, t.null], color: "var(--syn-num)" },
  { tag: [t.comment, t.lineComment, t.blockComment], color: "var(--syn-com)", fontStyle: "italic" },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: "var(--syn-fn)" },
  { tag: [t.typeName, t.className, t.definition(t.variableName)], color: "var(--syn-type)" },
  { tag: [t.punctuation, t.separator, t.bracket, t.operator], color: "var(--syn-punc)" },
  { tag: t.invalid, color: "var(--danger)" },
]);

/** Tema del editor. Todo sale de tokens; ningún color literal. */
const tema = EditorView.theme({
  "&": {
    height: "100%",
    backgroundColor: "var(--bg-inset)",
    color: "var(--text-1)",
    font: "400 13px/20px var(--font-mono)",
  },
  ".cm-content": { padding: "10px 0", caretColor: "var(--text-1)" },
  ".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "20px" },
  ".cm-gutters": {
    backgroundColor: "var(--bg-app)",
    color: "var(--text-dim)",
    border: "none",
    borderRight: "1px solid var(--border-subtle)",
    font: "400 12px/20px var(--font-mono)",
  },
  ".cm-lineNumbers .cm-gutterElement": { padding: "0 9px 0 14px", minWidth: "48px" },
  ".cm-activeLine": { backgroundColor: "var(--bg-hover)" },
  ".cm-activeLineGutter": { backgroundColor: "var(--bg-hover)", color: "var(--text-2)" },
  "&.cm-focused": { outline: "none" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--text-1)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": {
    backgroundColor: "var(--bg-sel)",
  },
  ".cm-tooltip": {
    backgroundColor: "var(--bg-elev)",
    border: "1px solid var(--border)",
    borderRadius: "var(--r-md)",
    boxShadow: "var(--shadow-menu)",
  },
  ".cm-tooltip-autocomplete > ul > li": {
    padding: "0 9px",
    height: "24px",
    lineHeight: "24px",
    font: "400 12px var(--font-mono)",
    color: "var(--text-2)",
  },
  ".cm-tooltip-autocomplete > ul > li[aria-selected]": {
    backgroundColor: "var(--bg-sel)",
    color: "var(--text-1)",
  },
  ".cm-completionDetail": { color: "var(--text-3)", fontStyle: "normal", marginLeft: "10px" },
});

/**
 * Traduce el snapshot a lo que espera el autocompletado de lang-sql.
 *
 * Las tablas se registran dos veces a propósito: calificadas —`ventas.orders`—
 * y sueltas —`orders`—. Escribir el esquema adelante es lo correcto pero casi
 * nadie lo hace cuando la tabla está en `public`, y un autocompletado que
 * obliga a calificar se siente roto aunque sea técnicamente más preciso.
 */
function esquemaParaCompletado(snap: Snapshot | null): Record<string, string[]> {
  const salida: Record<string, string[]> = {};
  for (const esq of snap?.schemas ?? []) {
    for (const tabla of esq.tables ?? []) {
      const columnas = (tabla.columns ?? []).map((c) => c.name);
      salida[`${esq.name}.${tabla.name}`] = columnas;
      // Sin pisar: si dos esquemas tienen una tabla con el mismo nombre, gana
      // la primera y la otra sigue disponible calificada. Mezclar las columnas
      // de las dos sugeriría columnas que esa tabla no tiene.
      if (!(tabla.name in salida)) {
        salida[tabla.name] = columnas;
      }
    }
  }
  return salida;
}

export function SqlEditor({
  value,
  onChange,
  snapshot,
  onRun,
  onCursor,
  readOnly = false,
}: {
  value: string;
  onChange: (v: string) => void;
  snapshot: Snapshot | null;
  onRun: () => void;
  onCursor?: (line: number, col: number) => void;
  readOnly?: boolean;
}) {
  const host = useRef<HTMLDivElement | null>(null);
  const view = useRef<EditorView | null>(null);
  // Los callbacks se leen desde una ref porque las extensiones de CodeMirror se
  // crean una sola vez: si se capturaran directamente, el editor quedaría
  // llamando a la versión de la primera renderización para siempre.
  const cb = useRef({ onChange, onRun, onCursor });
  cb.current = { onChange, onRun, onCursor };

  const lenguaje = useRef(new Compartment());

  useEffect(() => {
    if (!host.current) return;

    const state = EditorState.create({
      doc: value,
      extensions: [
        lineNumbers(),
        highlightActiveLine(),
        highlightActiveLineGutter(),
        history(),
        closeBrackets(),
        autocompletion({ activateOnTyping: true, icons: false }),
        lenguaje.current.of(
          sql({ dialect: PostgreSQL, schema: esquemaParaCompletado(snapshot), upperCaseKeywords: false }),
        ),
        syntaxHighlighting(resaltado),
        tema,
        EditorView.lineWrapping,
        EditorState.readOnly.of(readOnly),
        keymap.of([
          // Va antes que defaultKeymap para ganarle a cualquier atajo que use
          // la misma combinación.
          {
            key: "Mod-Enter",
            preventDefault: true,
            run: () => {
              cb.current.onRun();
              return true;
            },
          },
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...historyKeymap,
          indentWithTab,
        ]),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) cb.current.onChange(u.state.doc.toString());
          if (u.selectionSet || u.docChanged) {
            const pos = u.state.selection.main.head;
            const linea = u.state.doc.lineAt(pos);
            cb.current.onCursor?.(linea.number, pos - linea.from + 1);
          }
        }),
      ],
    });

    const v = new EditorView({ state, parent: host.current });
    view.current = v;
    return () => {
      v.destroy();
      view.current = null;
    };
    // Se monta una sola vez. El documento y el esquema se sincronizan en los
    // efectos de abajo, porque recrear el editor perdería el cursor, el
    // historial de deshacer y el foco en cada tecla.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // El esquema cambia cuando se refresca o se cambia de conexión. Se reconfigura
  // el compartimento en vez de recrear el editor.
  useEffect(() => {
    view.current?.dispatch({
      effects: lenguaje.current.reconfigure(
        sql({ dialect: PostgreSQL, schema: esquemaParaCompletado(snapshot), upperCaseKeywords: false }),
      ),
    });
  }, [snapshot]);

  // Sincroniza el documento cuando el valor viene de afuera —cambiar de pestaña,
  // cargar una consulta guardada—. Se compara antes de despachar: sin eso,
  // cada tecla dispararía un reemplazo del documento entero y el cursor
  // saltaría al final.
  useEffect(() => {
    const v = view.current;
    if (!v) return;
    const actual = v.state.doc.toString();
    if (actual === value) return;
    v.dispatch({ changes: { from: 0, to: actual.length, insert: value } });
  }, [value]);

  return <div className={styles.host} ref={host} />;
}
