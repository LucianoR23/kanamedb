import { useEffect, useState } from "react";
import type {
  DetailColumn,
  TypeOption,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { Button, Checkbox, Combobox, Dialog, Field, Input } from "../components/ui";
import type { ComboOption } from "../components/ui";
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
  const [tipos, setTipos] = useState<TypeOption[]>([]);

  useEffect(() => {
    void SessionSvc.ColumnTypes()
      .then((ts) => setTipos(ts ?? []))
      // Sin la lista el campo sigue sirviendo como texto libre: quedarse sin
      // poder agregar una columna porque no se pudo leer el catálogo sería
      // peor que perder la comodidad.
      .catch(() => setTipos([]));
  }, []);

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
                dataType: tipoCompleto,
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
              <Combobox
                value={tipo}
                options={opcionesDeTipo(tipos)}
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

/** Los tipos, con lo de esta base marcado. */
function opcionesDeTipo(tipos: TypeOption[]): ComboOption[] {
  return tipos.map((t) => ({
    value: t.name,
    ...(t.builtIn ? {} : { tag: t.kind === "base" ? "de esta base" : t.kind }),
    ...(t.comment ? { title: t.comment } : {}),
  }));
}

/** Qué se espera adentro de los paréntesis, según el tipo. */
function placeholderDeModificador(tipo: string): string {
  if (tipo.startsWith("numeric") || tipo.startsWith("decimal")) return "10,2  ·  precisión, escala";
  if (tipo.startsWith("character") || tipo.startsWith("bit")) return "255  ·  largo";
  if (tipo.startsWith("time") || tipo.startsWith("interval")) return "3  ·  dígitos de segundo";
  return "sin paréntesis si no hace falta";
}
