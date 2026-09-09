import { useState } from "react";
import type { DetailColumn } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Button, Checkbox, Dialog, Field, Input } from "../components/ui";
import styles from "./ColumnEditor.module.css";

/** Lo que el diálogo devuelve. La SQL la escribe Go, acá solo se junta el dato. */
export interface ColumnaNueva {
  name: string;
  dataType: string;
  nullable: boolean;
  default: string;
  comment: string;
}

/**
 * Alta de columna y renombrado.
 *
 * No valida el tipo contra el catálogo: Go rechaza lo que no sabe escribir y el
 * servidor rechaza lo que no existe, con su propio mensaje. Adivinar acá qué
 * tipos son válidos sería una lista que envejece mal —cada extensión agrega los
 * suyos— y que además impediría escribir uno legítimo.
 */
export function ColumnEditor({
  modo,
  columna,
  tabla,
  onGuardar,
  onCerrar,
}: {
  modo: "agregar" | "renombrar";
  /** La columna que se renombra. Vacío al agregar. */
  columna?: DetailColumn;
  tabla: string;
  onGuardar: (v: ColumnaNueva) => void;
  onCerrar: () => void;
}) {
  const [nombre, setNombre] = useState(modo === "renombrar" ? (columna?.name ?? "") : "");
  const [tipo, setTipo] = useState("text");
  const [nullable, setNullable] = useState(true);
  const [porDefecto, setPorDefecto] = useState("");
  const [comentario, setComentario] = useState("");

  const renombrar = modo === "renombrar";
  const nombreVacio = nombre.trim() === "";
  // Una columna NOT NULL nueva sobre una tabla con filas necesita qué poner en
  // las que ya están. Se dice acá y no en el error de Postgres, que no explica
  // qué hacer.
  const faltaDefault = !renombrar && !nullable && porDefecto.trim() === "";
  const puede = !nombreVacio && (renombrar || tipo.trim() !== "") && !faltaDefault;

  return (
    <Dialog
      open
      size="md"
      title={renombrar ? `Renombrar ${columna?.name}` : `Agregar una columna a ${tabla}`}
      onClose={onCerrar}
      footer={
        <>
          <Button variant="secondary" size="sm" onClick={onCerrar}>
            Cancelar
          </Button>
          <Button
            size="sm"
            disabled={!puede}
            onClick={() =>
              onGuardar({
                name: nombre.trim(),
                dataType: tipo.trim(),
                nullable,
                default: porDefecto.trim(),
                comment: comentario.trim(),
              })
            }
          >
            {renombrar ? "Renombrar" : "Agregar"}
          </Button>
        </>
      }
    >
      <div className={styles.cuerpo}>
        <Field label={renombrar ? "Nombre nuevo" : "Nombre"}>
          <Input
            value={nombre}
            autoFocus
            onChange={(e) => setNombre(e.currentTarget.value)}
          />
        </Field>

        {renombrar ? (
          <p className={styles.nota}>
            El nombre cambia en la base. Todo lo que la nombre —consultas guardadas, vistas,
            código— deja de encontrarla.
          </p>
        ) : (
          <>
            <Field label="Tipo">
              <Input
                value={tipo}
                placeholder="text, bigint, numeric(10,2), timestamptz…"
                onChange={(e) => setTipo(e.currentTarget.value)}
              />
            </Field>

            <Checkbox checked={nullable} onChange={setNullable}>
              Admite nulos
            </Checkbox>

            <Field label="Valor por defecto">
              <Input
                value={porDefecto}
                placeholder="una expresión: 0, 'EUR', now()"
                onChange={(e) => setPorDefecto(e.currentTarget.value)}
              />
            </Field>
            <p className={styles.nota}>
              Es una <strong>expresión</strong>, no un valor: <code>now()</code> llama a la
              función y <code>&apos;now()&apos;</code> guarda ese texto. Las cadenas van entre
              comillas simples.
            </p>

            {faltaDefault ? (
              <p className={styles.error}>
                Una columna que no admite nulos necesita un valor por defecto: las filas que ya
                están tienen que tener qué poner.
              </p>
            ) : null}

            <Field label="Comentario">
              <Input
                value={comentario}
                onChange={(e) => setComentario(e.currentTarget.value)}
              />
            </Field>
          </>
        )}
      </div>
    </Dialog>
  );
}
