import { useState } from "react";
import { Button, Dialog } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { copiar } from "./portapapeles";
import type { Valor } from "./Tarjeta";
import styles from "./mobile.module.css";

/** Un campo que alguien mantuvo apretado: de qué columna y qué valor. */
export interface CampoElegido {
  columna: string;
  valor: Valor;
}

/**
 * El valor de un campo, entero y con «Copiar».
 *
 * Se abre al mantener apretado un campo de una tarjeta o de la fila. Hace dos
 * cosas: mostrar el valor completo —la tarjeta lo corta a tres líneas— y
 * copiarlo. El copiado va por un botón y no en el momento del gesto, para que
 * quien copia vea qué copió, y porque el aviso de «Copiado» es la mitad de la
 * función: sin él se vuelve a apretar. NULL no se copia: pegar la palabra
 * «NULL» en otro lado sería inventar un valor.
 */
export function HojaDeValor({
  campo,
  onCerrar,
  onAviso,
}: {
  campo: CampoElegido | null;
  onCerrar: () => void;
  onAviso: (t: ToastItem) => void;
}) {
  const [error, setError] = useState("");
  if (!campo) return null;
  const { columna, valor } = campo;

  // El error es de este intento: la próxima hoja arranca limpia.
  function cerrar() {
    setError("");
    onCerrar();
  }

  async function copiarYCerrar() {
    if (valor === null) return;
    setError("");
    try {
      await copiar(valor);
      onAviso({ id: `copiado-${Date.now()}`, tone: "success", title: "Copiado", detail: columna });
      cerrar();
    } catch (err) {
      setError(textoDe(err));
    }
  }

  return (
    <Dialog
      open
      title={columna}
      onClose={cerrar}
      footer={
        <>
          <Button onClick={cerrar}>Cerrar</Button>
          <Button variant="primary" onClick={() => void copiarYCerrar()} disabled={valor === null}>
            Copiar
          </Button>
        </>
      }
    >
      <div className={styles.detalle}>
        {valor === null ? (
          <div className={styles.valor}>
            <span className={styles.nulo}>NULL</span>
          </div>
        ) : valor === "" ? (
          <div className={styles.valor}>
            <span className={styles.nulo}>vacío</span>
          </div>
        ) : (
          <pre className={styles.valorEntero}>{valor}</pre>
        )}
        {valor !== null && valor !== "" ? (
          <span className={styles.nota}>
            {valor.length} {valor.length === 1 ? "carácter" : "caracteres"}
          </span>
        ) : null}
        {error ? <div className={styles.error}>{error}</div> : null}
      </div>
    </Dialog>
  );
}
