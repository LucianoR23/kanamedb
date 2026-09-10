import { cx } from "../../lib/cx";
import styles from "./Glyph.module.css";

/** Tipos de objeto de base de datos que el árbol y las tabs saben mostrar.
 *  S00 los define como fichas mono de dos letras — sin fuente de íconos.
 *
 *  Los nombres son EXACTAMENTE los que manda el backend en `schema.ObjectKind`,
 *  no una lista paralela. Tenían su propio vocabulario —`matview` donde Go dice
 *  `materializedView`— y eso obliga a traducir en cada pantalla que muestre un
 *  objeto: una tabla de equivalencias que hay que acordarse de actualizar dos
 *  veces, y que en el último lugar que la copie va a estar desactualizada.
 *  `table`, `index`, `primaryKey`, `foreignKey`, `schema`, `query` y `erd` no
 *  vienen de ahí: son cosas de la interfaz, no del catálogo. */
export type ObjectKind =
  | "table"
  | "view"
  | "materializedView"
  | "function"
  | "procedure"
  | "trigger"
  | "enum"
  | "domain"
  | "composite"
  | "policy"
  | "event"
  | "sequence"
  | "index"
  | "primaryKey"
  | "foreignKey"
  | "schema"
  | "query"
  | "erd";

const GLYPHS: Record<ObjectKind, { label: string; tone: string | undefined }> = {
  table:      { label: "TB", tone: styles.relation },
  view:       { label: "VW", tone: styles.view },
  materializedView: { label: "MV", tone: styles.view },
  function:   { label: "FN", tone: styles.routine },
  procedure:  { label: "PR", tone: styles.routine },
  trigger:    { label: "TG", tone: styles.trigger },
  enum:       { label: "EN", tone: styles.type },
  domain:     { label: "DM", tone: styles.type },
  composite:  { label: "CT", tone: styles.type },
  policy:     { label: "PL", tone: styles.trigger },
  event:      { label: "EV", tone: styles.trigger },
  sequence:   { label: "SE", tone: styles.neutral },
  index:      { label: "IX", tone: styles.neutral },
  primaryKey: { label: "PK", tone: styles.routine },
  foreignKey: { label: "FK", tone: styles.relation },
  schema:     { label: "SC", tone: styles.neutral },
  // `query` es SQ de SQL. La secuencia decía «SQ» también, y desde que se
  // puede abrir como pestaña las dos eran indistinguibles en la barra: el
  // único dato que las separaba era un `title` sobre un span aria-hidden.
  query:      { label: "SQ", tone: styles.neutral },
  erd:        { label: "ER", tone: styles.view },
};

/** Nombre legible del tipo, para `title` y lectores de pantalla. */
const NAMES: Record<ObjectKind, string> = {
  table: "Tabla",
  erd: "Diagrama",
  view: "Vista",
  materializedView: "Vista materializada",
  function: "Función",
  procedure: "Procedimiento",
  trigger: "Trigger",
  enum: "Enum",
  domain: "Dominio",
  composite: "Tipo compuesto",
  policy: "Política de RLS",
  event: "Evento",
  sequence: "Secuencia",
  index: "Índice",
  primaryKey: "Clave primaria",
  foreignKey: "Clave foránea",
  schema: "Esquema",
  query: "Consulta",
};

interface GlyphProps {
  kind: ObjectKind;
  /** Atenuado, para filas en carga o deshabilitadas. */
  muted?: boolean;
}

export function Glyph({ kind, muted = false }: GlyphProps) {
  const g = GLYPHS[kind];
  return (
    <span
      className={cx(styles.glyph, muted ? styles.muted : g.tone)}
      title={NAMES[kind]}
      aria-hidden="true"
    >
      {g.label}
    </span>
  );
}
