import { cx } from "../../lib/cx";
import styles from "./Glyph.module.css";

/** Tipos de objeto de base de datos que el árbol y las tabs saben mostrar.
 *  S00 los define como fichas mono de dos letras — sin fuente de íconos. */
export type ObjectKind =
  | "table"
  | "view"
  | "matview"
  | "function"
  | "procedure"
  | "trigger"
  | "enum"
  | "sequence"
  | "index"
  | "primaryKey"
  | "foreignKey"
  | "schema"
  | "query";

const GLYPHS: Record<ObjectKind, { label: string; tone: string | undefined }> = {
  table:      { label: "TB", tone: styles.relation },
  view:       { label: "VW", tone: styles.view },
  matview:    { label: "MV", tone: styles.view },
  function:   { label: "FN", tone: styles.routine },
  procedure:  { label: "PR", tone: styles.routine },
  trigger:    { label: "TG", tone: styles.trigger },
  enum:       { label: "EN", tone: styles.type },
  sequence:   { label: "SQ", tone: styles.neutral },
  index:      { label: "IX", tone: styles.neutral },
  primaryKey: { label: "PK", tone: styles.routine },
  foreignKey: { label: "FK", tone: styles.relation },
  schema:     { label: "SC", tone: styles.neutral },
  query:      { label: "SQ", tone: styles.neutral },
};

/** Nombre legible del tipo, para `title` y lectores de pantalla. */
const NAMES: Record<ObjectKind, string> = {
  table: "Tabla",
  view: "Vista",
  matview: "Vista materializada",
  function: "Función",
  procedure: "Procedimiento",
  trigger: "Trigger",
  enum: "Enum",
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
