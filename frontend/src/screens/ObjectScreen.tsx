import { useEffect, useState } from "react";
import type { $Object as DBObject, ObjectDefinition } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { Badge, CopyButton, Glyph, Spinner } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { conArticulo, glifoDe, nombreDeClase } from "../lib/objetos";
import styles from "./ObjectScreen.module.css";

/**
 * S16, primera mitad: la definición de un objeto, para leer.
 *
 * Editarla —con la lista de dependientes y el aviso de DROP + CREATE— es lo que
 * falta. Se separa a propósito: poder VER qué hace una vista o una función ya
 * es la mitad del valor y no arriesga nada, mientras que guardarla implica
 * borrar y volver a crear el objeto, que es destructivo y tiene que pasar por
 * el changeset y su vista previa como todo lo demás.
 *
 * El texto se muestra tal como lo devuelve el motor. Es la promesa del editor
 * cuando exista: lo que se ve es lo que se ejecuta.
 */
export function ObjectScreen({ objeto, recarga }: { objeto: DBObject; recarga: number }) {
  const [def, setDef] = useState<ObjectDefinition | null>(null);
  const [error, setError] = useState("");
  const [cargando, setCargando] = useState(true);

  useEffect(() => {
    let vigente = true;
    setCargando(true);
    setError("");
    SessionSvc.ObjectDefinition(objeto)
      .then((d) => {
        if (!vigente) return;
        setDef(d);
      })
      .catch((err: unknown) => {
        if (!vigente) return;
        // El error se muestra ENTERO y no como «no se pudo». Lo que dice es
        // qué clase de objeto es y por qué Kaname todavía no la escribe —un
        // dominio, una política de RLS— y eso es la respuesta, no un detalle
        // técnico que convenga esconder.
        setError(textoDe(err));
        setDef(null);
      })
      .finally(() => {
        if (vigente) setCargando(false);
      });
    return () => {
      vigente = false;
    };
    // `recarga` es el contador de «Refrescar»: sin él, recrear una vista desde
    // el editor SQL y refrescar dejaba esta pestaña mostrando la definición
    // vieja, y la única salida era cerrarla y volver a abrirla. Es exactamente
    // la regresión para la que el contador se inventó en la grilla.
  }, [objeto, recarga]);

  const clase = nombreDeClase(objeto.kind);

  return (
    <div className={styles.pane} aria-busy={cargando}>
      <header className={styles.head}>
        <Glyph kind={glifoDe(objeto.kind)} />
        <h2 className={styles.nombre}>
          {objeto.schema ? <span className={styles.esquema}>{objeto.schema}.</span> : null}
          {objeto.name}
          {objeto.args ? <span className={styles.args}>({objeto.args})</span> : null}
        </h2>
        <Badge tone="neutral">{clase}</Badge>
        {objeto.table ? <span className={styles.sobre}>sobre {objeto.table}</span> : null}
        <span className={styles.spacer} />
        {def?.sql ? <CopyButton text={def.sql} label="Copiar" /> : null}
      </header>

      {cargando ? (
        <p className={styles.estado}>
          <Spinner size="sm" />
          Leyendo la definición…
        </p>
      ) : error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : (
        <>
          {def?.values && def.values.length > 0 ? (
            <section className={styles.valores}>
              <h3 className={styles.subtitulo}>
                Valores <span className={styles.cuantos}>{def.values.length}</span>
              </h3>
              {/* En orden de declaración, que en Postgres es el orden en que
                  ordenan y comparan: `'bajo' < 'alto'` si se declararon así.
                  Alfabetizarlos acá sería mostrar otro orden que el que la base
                  usa para un ORDER BY. */}
              <ol className={styles.lista}>
                {def.values.map((v) => (
                  <li key={v} className={styles.valor}>
                    {v}
                  </li>
                ))}
              </ol>
            </section>
          ) : null}
          <pre className={styles.sql}>{def?.sql ?? ""}</pre>
        </>
      )}

      {/* Solo cuando hay algo que mostrar: un cartel sobre un error sería
          ruido encima de una explicación. */}
      {!cargando && !error ? (
        <p className={styles.nota}>
          Editar {conArticulo(objeto.kind)} llega en la próxima unidad de esta iteración:
          guardar implica borrar y volver a crear el objeto, y eso pasa por la vista previa del
          changeset como cualquier otro cambio destructivo.
        </p>
      ) : null}
    </div>
  );
}
