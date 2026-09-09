import { useState } from "react";
import { Button, Dialog, Field, Input } from "../components/ui";
import styles from "./NewTableDialog.module.css";

/** Lo que hace falta para crear una tabla desde el diagrama. */
export interface TablaNueva {
  name: string;
  pkName: string;
  pkType: string;
}

/**
 * Crear una tabla desde el lienzo.
 *
 * Pide lo mínimo —nombre y clave primaria— y nada más. El resto de las columnas
 * se agregan con la herramienta de columna o desde la pantalla de estructura,
 * que es donde hay lugar para el tipo, el default y el comentario. Un formulario
 * de tabla completo acá sería una segunda pantalla de estructura, peor.
 *
 * La clave primaria no es opcional a propósito: una tabla sin clave no se puede
 * editar desde la grilla —no hay forma segura de identificar una fila— y crearla
 * así es fabricarse el problema.
 */
export function NewTableDialog({
  onCrear,
  onCerrar,
}: {
  onCrear: (t: TablaNueva) => void;
  onCerrar: () => void;
}) {
  const [nombre, setNombre] = useState("");
  const [pk, setPk] = useState("id");
  const [tipo, setTipo] = useState("bigint");

  const puede = nombre.trim() !== "" && pk.trim() !== "" && tipo.trim() !== "";

  return (
    <Dialog
      open
      size="md"
      title="Tabla nueva"
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
              onCrear({ name: nombre.trim(), pkName: pk.trim(), pkType: tipo.trim() })
            }
          >
            Preparar
          </Button>
        </>
      }
    >
      <div className={styles.cuerpo}>
        <Field label="Nombre">
          <Input value={nombre} autoFocus onChange={(e) => setNombre(e.currentTarget.value)} />
        </Field>
        <Field label="Clave primaria">
          <Input value={pk} onChange={(e) => setPk(e.currentTarget.value)} />
        </Field>
        <Field label="Tipo de la clave">
          <Input value={tipo} onChange={(e) => setTipo(e.currentTarget.value)} />
        </Field>
        <p className={styles.nota}>
          Las demás columnas se agregan después, con la herramienta de columna o desde la pantalla
          de estructura. La clave primaria se pide ahora porque una tabla sin clave no se puede
          editar por la grilla: no hay forma segura de identificar una fila.
        </p>
      </div>
    </Dialog>
  );
}
