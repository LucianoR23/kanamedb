import { useState } from "react";
import type { DetailColumn } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Button, Checkbox, Combobox, Dialog, Field, Input } from "../components/ui";
import { useColumnTypes } from "../lib/useColumnTypes";
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
 * Los tipos salen del catálogo de LA BASE CONECTADA, no de una lista en el
 * código. Los tipos de PostgreSQL no son un conjunto cerrado: cada extensión
 * agrega los suyos y cada enum o dominio definido en esa base es uno más, así
 * que una lista fija no podría ofrecer un tipo propio. Los de la base van
 * primero, que son justamente los que nadie recuerda de memoria.
 *
 * El modificador —el `(10,2)` de `numeric`— va en un campo aparte y solo para
 * los tipos que lo admiten. Es la parte que más se escribe mal, y separarla
 * permite que el nombre del tipo venga siempre de la lista.
 *
 * Se puede escribir algo que no esté listado: si no, un tipo instalado después
 * de conectar sería un callejón sin salida. Go rechaza lo que no sabe escribir y
 * el servidor rechaza lo que no existe, con su propio mensaje.
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
  const [modificador, setModificador] = useState("");
  const [arreglo, setArreglo] = useState(false);
  const [nullable, setNullable] = useState(true);
  const [porDefecto, setPorDefecto] = useState("");
  const [comentario, setComentario] = useState("");
  const { tipos, opciones } = useColumnTypes();

  const renombrar = modo === "renombrar";
  const elegido = tipos.find((t) => t.name === tipo);
  // Si el tipo no está en la lista —porque se escribió a mano— se ofrece el
  // modificador igual: no hay forma de saber si lo admite.
  const admiteModificador = elegido ? elegido.acceptsModifier : tipo.trim() !== "";
  const tipoCompleto =
    tipo.trim() + (modificador.trim() ? `(${modificador.trim()})` : "") + (arreglo ? "[]" : "");
  const nombreVacio = nombre.trim() === "";
  // Una columna NOT NULL nueva sobre una tabla con filas necesita qué poner en
  // las que ya están. Se dice acá y no en el error de Postgres, que no explica
  // qué hacer.
  const faltaDefault = !renombrar && !nullable && porDefecto.trim() === "";
  // Un default que es una palabra suelta es, casi siempre, el nombre de otra
  // columna. PostgreSQL lo rechaza —el default se evalúa sin ninguna fila a la
  // vista, así que no hay de dónde sacar el otro valor— y el error llega recién
  // al aplicar, cuando ya se armó todo el changeset.
  const defaultSospechoso = !renombrar && pareceNombreDeColumna(porDefecto);
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
            {...(puede ? {} : { title: razonDeBloqueo(nombreVacio, faltaDefault, tipo) })}
            onClick={() =>
              onGuardar({
                name: nombre.trim(),
                dataType: tipoCompleto,
                nullable,
                default: porDefecto.trim(),
                comment: comentario.trim(),
              })
            }
          >
            {renombrar ? "Preparar el renombrado" : "Preparar"}
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
              <Combobox
                value={tipo}
                options={opciones}
                ariaLabel="Tipo de la columna"
                placeholder="buscá un tipo…"
                onChange={(v) => {
                  setTipo(v);
                  setModificador("");
                }}
              />
            </Field>

            {admiteModificador ? (
              <Field label="Parámetros">
                <Input
                  value={modificador}
                  placeholder={placeholderDeModificador(tipo)}
                  onChange={(e) => setModificador(e.currentTarget.value)}
                />
              </Field>
            ) : null}

            <Checkbox checked={arreglo} onChange={setArreglo}>
              Es un arreglo
            </Checkbox>

            <p className={styles.resultado}>
              Queda como <code>{tipoCompleto || "…"}</code>
            </p>

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

            {defaultSospechoso ? (
              <p className={styles.error}>
                <code>{porDefecto.trim()}</code> se va a leer como el nombre de otra columna, y un
                valor por defecto <strong>no puede nombrar columnas</strong>: se calcula sin
                ninguna fila a la vista. Si querías el texto, va entre comillas simples; si el
                valor tiene que salir de otra columna, lo que hace falta es una columna generada,
                no un default.
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


/**
 * Las funciones del estándar SQL que se escriben sin paréntesis.
 *
 * Son la razón por la que esto avisa en vez de bloquear: `current_date` es una
 * palabra suelta y es un default perfectamente válido.
 */
const SIN_PARENTESIS = new Set([
  "true",
  "false",
  "null",
  "current_date",
  "current_time",
  "current_timestamp",
  "localtime",
  "localtimestamp",
  "current_user",
  "session_user",
  "user",
  "current_catalog",
  "current_schema",
]);

/** Si lo escrito es un identificador pelado, que PostgreSQL va a leer como columna. */
function pareceNombreDeColumna(v: string): boolean {
  const s = v.trim();
  if (!/^[A-Za-z_][A-Za-z0-9_$]*$/.test(s)) return false;
  return !SIN_PARENTESIS.has(s.toLowerCase());
}

/**
 * Por qué el botón está bloqueado.
 *
 * Va en el `title` porque un botón deshabilitado sin motivo es una pared: la
 * explicación de la columna sin default está más abajo en el formulario y puede
 * quedar fuera de la vista.
 */
function razonDeBloqueo(nombreVacio: boolean, faltaDefault: boolean, tipo: string): string {
  if (nombreVacio) return "Falta el nombre de la columna";
  if (tipo.trim() === "") return "Falta el tipo";
  if (faltaDefault) return "Una columna que no admite nulos necesita un valor por defecto";
  return "";
}

/** Qué se espera adentro de los paréntesis, según el tipo. */
function placeholderDeModificador(tipo: string): string {
  if (tipo.startsWith("numeric") || tipo.startsWith("decimal")) return "10,2  ·  precisión, escala";
  if (tipo.startsWith("character") || tipo.startsWith("bit")) return "255  ·  largo";
  if (tipo.startsWith("time") || tipo.startsWith("interval")) return "3  ·  dígitos de segundo";
  return "sin paréntesis si no hace falta";
}
