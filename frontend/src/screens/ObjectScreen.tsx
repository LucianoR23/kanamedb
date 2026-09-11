import { useEffect, useRef, useState } from "react";
import { Type as ChangeType } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { $Object as DBObject, Dependents, ObjectDefinition, Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { SqlEditor } from "../components/SqlEditor";
import { Badge, Button, CopyButton, Glyph, Spinner } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { useStage } from "../lib/useStage";
import { glifoDe, nombreDeClase } from "../lib/objetos";
import styles from "./ObjectScreen.module.css";

/**
 * S16: la definición de un objeto, para leer y para editar.
 *
 * Guardar NO aplica nada: prepara un cambio en el changeset, que se revisa y se
 * aplica en la pantalla de pendientes como cualquier otro. Es lo mismo que hace
 * la grilla y el editor de estructura, y no es burocracia: reemplazar un objeto
 * puede ser destructivo, y la vista previa con la SQL exacta es la garantía de
 * fondo de todo el apply.
 *
 * El texto se muestra y se ejecuta TAL CUAL. Nada lo reescribe.
 */
export function ObjectScreen({
  objeto,
  recarga,
  snapshot,
  motor,
  soloLectura,
  onStaged,
}: {
  objeto: DBObject;
  recarga: number;
  snapshot: Snapshot | null;
  motor: string;
  soloLectura: boolean;
  onStaged: () => void;
}) {
  // El texto del editor y el que vino del motor, por separado. La comparación
  // entre los dos es lo único que decide si hay algo para guardar: un botón
  // habilitado sobre un texto idéntico prepara un cambio que no cambia nada y
  // ensucia el changeset.
  const [texto, setTexto] = useState("");
  // Lo último que trajo el motor. Compararlo con lo que hay en el editor es lo
  // que distingue «no lo toqué» de «estoy a medio escribir» cuando llega una
  // recarga.
  const textoAnterior = useRef("");
  const [recrear, setRecrear] = useState(false);
  const [guardando, setGuardando] = useState(false);
  const [aviso, setAviso] = useState("");

  // Se prepara con el MISMO camino que la grilla y el diagrama, no llamando a
  // Stage a mano. Es lo que hace aparecer la confirmación de producción cuando
  // Go la exige: sin esto, en una conexión marcada como producción el error de
  // «falta confirmar» caía como texto en el aviso y no había dónde escribir el
  // nombre de la base —así que reemplazar un objeto era imposible, y en SQLite
  // eso es CUALQUIER edición de objeto, porque todas son borrar y crear—.
  const staging = useStage(() => {
    setAviso("Listo. El cambio quedó en «Cambios pendientes» — todavía no se aplicó.");
    onStaged();
  });
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
        // El texto del editor NO se pisa si hay una edición sin guardar. Este
        // efecto también corre con «Refrescar» y después de cada apply, así
        // que sin esta guarda una vista a medio editar se perdía por aplicar un
        // cambio de otra pestaña, sin aviso y sin forma de recuperarla.
        setTexto((actual) => (actual === "" || actual === textoAnterior.current ? d.sql : actual));
        textoAnterior.current = d.sql;
        // Lo que el motor no sabe reemplazar en el lugar arranca —y queda— en
        // «borrar y volver a crear»: ofrecer la otra opción sería ofrecer algo
        // que el renderizador rechaza.
        setRecrear(!d.replaceable);
        setAviso("");
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
  // Se compara con lo que vino del motor y no se guarda un booleano aparte:
  // escribir y deshacer deja el texto igual, y un «sucio» pegajoso habilitaría
  // preparar un cambio que no cambia nada.
  const sucio = def !== null && texto !== def.sql;

  async function guardar() {
    if (!def || !sucio) return;
    setGuardando(true);
    setAviso("");
    try {
      await staging.stage(
        {
          type: ChangeType.ReplaceObject,
          objectKind: objeto.kind,
          schema: objeto.schema,
          name: objeto.name,
          args: objeto.args,
          // La tabla solo la lleva un trigger, y es lo único que permite
          // borrarlo: `DROP TRIGGER x` sin ella no es SQL válida en Postgres.
          table: objeto.table,
          definition: texto,
          recreate: recrear,
          source: "object",
        } as never,
      );
    } finally {
      setGuardando(false);
    }
  }

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
          <div className={styles.editor}>
            <SqlEditor
              value={texto}
              onChange={(v) => {
                setTexto(v);
                setAviso("");
              }}
              snapshot={snapshot}
              onRun={() => {}}
              readOnly={soloLectura}
              engine={motor}
            />
          </div>

          <div className={styles.acciones}>
            {/* El modo NO es un detalle técnico escondido: es la diferencia
                entre no poder romper nada y poder perder el objeto, así que
                está a la vista al lado del botón que lo usa. */}
            <label className={styles.modo} title={explicarModo(def, recrear)}>
              <input
                type="checkbox"
                checked={recrear}
                disabled={!def?.replaceable || soloLectura}
                onChange={(e) => setRecrear(e.target.checked)}
              />
              Borrar y volver a crear
            </label>
            <span className={styles.modoNota}>{explicarModo(def, recrear)}</span>
            <span className={styles.spacer} />
            {sucio ? (
              <Button variant="ghost" onClick={() => setTexto(def?.sql ?? "")} disabled={guardando}>
                Descartar
              </Button>
            ) : null}
            <Button onClick={guardar} disabled={!sucio || guardando || soloLectura}>
              {guardando ? "Preparando…" : "Preparar el cambio"}
            </Button>
          </div>
          {staging.error || aviso ? (
            <p className={styles.avisoGuardar}>{staging.error || aviso}</p>
          ) : null}

          <Dependientes deps={deps} />
        </>
      )}

      {!cargando && !error ? (
        <p className={styles.nota}>
          Preparar el cambio no lo aplica: queda en «Cambios pendientes», con la SQL exacta que se
          va a correr.
        </p>
      ) : null}
      {staging.dialogo}
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

/**
 * Qué va a pasar al aplicar, dicho antes de aplicarlo.
 *
 * Las dos frases NO son variantes de la misma: reemplazar en el lugar no puede
 * romper nada —si el CREATE falla, el objeto sigue como estaba— mientras que
 * borrar y crear rompe lo que dependa de él. Y en MySQL, donde el DDL hace
 * commit solo, además puede PERDER el objeto: no hay transacción que devuelva
 * el DROP si el CREATE falla. Esa última parte la dice la vista previa con el
 * motor en la mano; acá se dice la diferencia de fondo.
 */
function explicarModo(def: ObjectDefinition | null, recrear: boolean): string {
  if (!def) return "";
  if (!def.replaceable) {
    return "Este motor no sabe reemplazarlo en el lugar, así que hay que borrarlo y volver a crearlo.";
  }
  if (recrear) {
    return "Se borra y se vuelve a crear: rompe lo que dependa de él, y si el CREATE falla el objeto puede quedar perdido.";
  }
  return "Se reemplaza en el lugar: si la definición nueva falla, el objeto queda como estaba.";
}
