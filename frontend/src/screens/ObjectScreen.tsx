import { useEffect, useState } from "react";
import type { $Object as DBObject, Dependents, ObjectDefinition } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
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
  const [deps, setDeps] = useState<Dependents | null>(null);
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

    // Los dependientes van por su cuenta y su fallo NO rompe la pantalla: la
    // definición es lo que se vino a ver, y no poder listar lo que cuelga de un
    // objeto no es motivo para no mostrarla. Lo que SÍ importa es que un fallo
    // acá no se lea como «no depende nada»: por eso el estado arranca en null
    // —sin respuesta— y no en una lista vacía.
    setDeps(null);
    SessionSvc.ObjectDependents(objeto)
      .then((d) => {
        if (vigente) setDeps(d);
      })
      .catch((err: unknown) => {
        if (vigente) setDeps({ objects: [], unknown: true, reason: textoDe(err) } as Dependents);
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
          <Dependientes deps={deps} />
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

/**
 * Lo que se rompe si este objeto deja de existir.
 *
 * Son DOS preguntas y no una, y por eso hay dos bloques en vez de un `if`
 * encadenado: qué se encontró, y si se puede confiar en que eso es todo. Se
 * dan juntas más seguido de lo que parece — MySQL devuelve las vistas sobre las
 * que este usuario tiene permiso y esconde el resto SIN avisar, así que una
 * lista con tres nombres puede tener cinco. Un `else` entre las dos habría
 * tirado la lista para mostrar el aviso, o el aviso para mostrar la lista.
 *
 * De las cuatro combinaciones, la que este componente existe para separar es
 * «vacía y segura» de «vacía y no se sabe»: las dos son cero elementos y
 * significan lo contrario. MariaDB no registra qué vista usa qué, y SQLite no
 * guarda dependencias en absoluto.
 */
function Dependientes({ deps }: { deps: Dependents | null }) {
  if (!deps) {
    return (
      <p className={styles.estado}>
        <Spinner size="sm" />
        Buscando qué depende de esto…
      </p>
    );
  }

  const objetos = deps.objects ?? [];

  return (
    <>
      {objetos.length > 0 ? (
        <section className={styles.depsHay}>
          <h3 className={styles.subtitulo}>
            Qué depende de esto <span className={styles.cuantos}>{objetos.length}</span>
          </h3>
          <p className={styles.depsTexto}>
            Reemplazar este objeto donde el motor tenga que borrarlo y volver a crearlo rompe —o
            arrastra— lo que sigue:
          </p>
          <ul className={styles.lista}>
            {objetos.map((o) => (
              <li key={claveDep(o)} className={styles.dep}>
                <Glyph kind={glifoDe(o.kind)} />
                {o.schema ? <span className={styles.esquema}>{o.schema}.</span> : null}
                {o.name}
                {/* La tabla del trigger no es decoración: los nombres de
                    trigger son únicos POR TABLA, así que sin ella dos
                    «auditar» son el mismo renglón repetido — y la tabla es lo
                    único que dice dónde ir a arreglarlo. */}
                {o.table ? <span className={styles.sobre}>en {o.table}</span> : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {deps.unknown ? (
        <section className={styles.depsIncierto}>
          <h3 className={styles.subtitulo}>
            {objetos.length > 0 ? "Y puede haber más" : "Qué depende de esto"}
          </h3>
          <p className={styles.depsTexto}>
            <strong>{objetos.length > 0 ? "La lista puede estar incompleta." : "No se puede saber."}</strong>{" "}
            {deps.reason}
          </p>
        </section>
      ) : null}

      {objetos.length === 0 && !deps.unknown ? (
        <section className={styles.depsVacio}>
          <h3 className={styles.subtitulo}>Qué depende de esto</h3>
          <p className={styles.depsTexto}>Nada. Reemplazarlo no rompe ningún otro objeto.</p>
        </section>
      ) : null}
    </>
  );
}

/**
 * La clave de un dependiente en la lista.
 *
 * Lleva la TABLA además del nombre porque los nombres de trigger son únicos por
 * tabla y no por esquema: dos triggers `auditar`, sobre `pedidos` y sobre
 * `clientes`, que llamen a la misma función colisionaban en la misma clave y
 * React dibujaba dos renglones idénticos.
 */
function claveDep(o: DBObject): string {
  return `${o.kind}:${o.schema}.${o.name}@${o.table ?? ""}`;
}
