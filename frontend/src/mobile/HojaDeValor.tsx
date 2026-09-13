import { useState } from "react";
import { Button, Dialog } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cx } from "../lib/cx";
import { copiar } from "./portapapeles";
import type { Valor } from "./Tarjeta";
import styles from "./mobile.module.css";

/** Un campo que alguien mantuvo apretado: de qué columna, de qué tipo y qué valor. */
export interface CampoElegido {
  columna: string;
  valor: Valor;
  tipo?: string;
}

/**
 * M09: el valor de un campo, entero y con «Copiar».
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
  const { columna, valor, tipo } = campo;

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
      onAviso({ id: `copiado-${Date.now()}`, tone: "success", title: `Copiado · ${columna}` });
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
      <div className={styles.columna}>
        {tipo ? <span className={styles.metaChica}>{tipo}</span> : null}
        {valor === null ? (
          <>
            <div className={cx(styles.valorEntero, styles.valorEnteroNulo, styles.nulo)}>NULL</div>
            <span className={styles.ayuda}>Sin valor. No hay nada que copiar.</span>
          </>
        ) : valor === "" ? (
          <>
            <div className={cx(styles.valorEntero, styles.valorEnteroNulo, styles.vacioValor)}>vacío</div>
            <span className={styles.ayuda}>La cadena vacía: se copia, y no es lo mismo que NULL.</span>
          </>
        ) : (
          <>
            <pre className={styles.valorEntero}>{valor}</pre>
            <span className={styles.metaChica}>
              {valor.length} {valor.length === 1 ? "carácter" : "caracteres"}
            </span>
          </>
        )}
        {error ? (
          <div className={cx(styles.aviso, styles.avisoMal)}>
            <span>No se pudo copiar. {error}</span>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}
