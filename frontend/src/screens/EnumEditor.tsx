import { useEffect, useRef, useState } from "react";
import { Type as ChangeType } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { $Object as DBObject } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Button, Combobox, Input } from "../components/ui";
import styles from "./EnumEditor.module.css";

/**
 * S17: el editor de un enum.
 *
 * NO es un editor de texto, y esa es la decisión de fondo. Un enum se ve como
 * un `CREATE TYPE … AS ENUM ('a','b')` y editar ese texto sugiere que las tres
 * operaciones —agregar, renombrar, sacar— cuestan lo mismo. No es así:
 *
 *  - Agregar un valor es `ALTER TYPE … ADD VALUE`. No toca ninguna fila.
 *  - Renombrar es `ALTER TYPE … RENAME VALUE`. Tampoco, pero cambia lo que
 *    cada fila LEE, así que rompe lo que compare contra el texto viejo.
 *  - **Sacar un valor no existe.** PostgreSQL no tiene `DROP VALUE`
 *    —comprobado contra la 18: es un error de sintaxis— y hacerlo a mano es
 *    crear un tipo nuevo, reescribir cada columna que use el viejo y borrar el
 *    original. Eso falla a mitad de camino si alguna fila todavía tiene el
 *    valor que se quiere sacar.
 *
 * Un editor de texto dejaría borrar una línea y apretar guardar, y lo que sigue
 * sería un DROP TYPE que el servidor rechaza porque hay columnas usándolo. La
 * lista con un botón por operación no ofrece lo que no se puede hacer, que es
 * la misma regla que el resto del proyecto.
 *
 * Los enums son de PostgreSQL: en MySQL y MariaDB un ENUM es un TIPO DE COLUMNA
 * y no un objeto del catálogo, y SQLite no los tiene. Esta pantalla no aparece
 * ahí porque el árbol tampoco lista ninguno.
 */
export function EnumEditor({
  objeto,
  valores,
  preparados,
  renombrados,
  soloLectura,
  onStage,
}: {
  objeto: DBObject;
  /** Los valores que tiene el enum EN LA BASE, en orden de declaración. */
  valores: readonly string[];
  /** Los que ya se prepararon en el changeset y todavía no se aplicaron. */
  preparados: readonly string[];
  /** Los renombres preparados: valor viejo → valor nuevo. */
  renombrados: Readonly<Record<string, string>>;
  soloLectura: boolean;
  /** Prepara el cambio. Devuelve cuando terminó, haya entrado o no.
   *
   *  Tipado con `Change` y no con `unknown`: con `unknown` un `before` escrito
   *  `beforeValue` compilaba igual y el campo se perdía en silencio —la
   *  posición del valor, que es lo único que no se puede corregir después—. */
  onStage: (c: Change) => Promise<void>;
}) {
  const [nuevo, setNuevo] = useState("");
  const [antesDe, setAntesDe] = useState("");
  const [renombrando, setRenombrando] = useState<string | null>(null);
  const [nombreNuevo, setNombreNuevo] = useState("");
  const [trabajando, setTrabajando] = useState(false);

  // Lo que se escribió NO se limpia por haber apretado el botón: contra
  // producción `stage` abre un diálogo y devuelve sin haber preparado nada, así
  // que limpiar ahí borraba lo tipeado mientras el diálogo seguía arriba —y si
  // alguien cancelaba, sin decir nada—. Se limpia cuando el valor APARECE entre
  // los preparados, que es lo único que prueba que entró.
  // Se limpia lo que ESTE formulario envió y entró, no cualquier cosa que
  // coincida. La primera versión miraba solo si lo tipeado estaba entre los
  // preparados, y entonces volver a escribir un valor ya preparado —para ver
  // por qué no se podía— hacía desaparecer lo tipeado y el aviso no llegaba a
  // mostrarse nunca.
  const enviado = useRef<string | null>(null);
  const enviadoRename = useRef<string | null>(null);
  useEffect(() => {
    if (enviado.current !== null && preparados.includes(enviado.current)) {
      enviado.current = null;
      setNuevo("");
      setAntesDe("");
    }
    const r = enviadoRename.current;
    if (r !== null && renombrados[r] !== undefined) {
      enviadoRename.current = null;
      setRenombrando(null);
      setNombreNuevo("");
    }
  }, [preparados, renombrados]);

  // La comprobación mira los valores de la base Y los que ya se prepararon. Solo
  // con los de la base, agregar «medio» dos veces entraba dos cambios iguales
  // —la definición que se relee es la de la base, que no cambió— y el segundo
  // fallaba al aplicar con «enum label already exists», tirando el changeset
  // entero porque Postgres lo corre en una transacción.
  const todos = [...valores, ...preparados];
  const yaEsta = todos.some((v) => v === nuevo.trim());
  const puedeAgregar = nuevo.trim() !== "" && !yaEsta && !soloLectura && !trabajando;

  async function agregar() {
    if (!puedeAgregar) return;
    setTrabajando(true);
    enviado.current = nuevo.trim();
    try {
      await onStage({
        type: ChangeType.AddEnumValue,
        schema: objeto.schema,
        name: objeto.name,
        value: nuevo.trim(),
        before: antesDe,
        source: "enum",
      } as Change);
    } finally {
      setTrabajando(false);
    }
  }

  async function renombrar(viejo: string) {
    const destino = nombreNuevo.trim();
    if (destino === "" || destino === viejo) return;
    setTrabajando(true);
    enviadoRename.current = viejo;
    try {
      await onStage({
        type: ChangeType.RenameEnumValue,
        schema: objeto.schema,
        name: objeto.name,
        value: viejo,
        newName: destino,
        source: "enum",
      } as Change);
    } finally {
      setTrabajando(false);
    }
  }

  return (
    <section className={styles.pane}>
      <h3 className={styles.subtitulo}>
        Valores <span className={styles.cuantos}>{valores.length}</span>
      </h3>
      {/* El orden se dice explícitamente porque no es cosmético: en Postgres es
          el orden en que los valores comparan y ordenan, así que un ORDER BY
          sobre esta columna depende de él. */}
      <p className={styles.nota}>
        En orden de declaración, que es el orden en que comparan y ordenan.
      </p>

      <ol className={styles.lista}>
        {valores.map((v, i) => (
          <li key={v} className={styles.fila}>
            <span className={styles.indice}>{i + 1}</span>
            {renombrando === v ? (
              <>
                <Input
                  value={nombreNuevo}
                  onChange={(e) => setNombreNuevo(e.target.value)}
                  aria-label={`Nombre nuevo para ${v}`}
                  autoFocus
                />
                <Button
                  onClick={() => void renombrar(v)}
                  disabled={nombreNuevo.trim() === "" || nombreNuevo.trim() === v || trabajando}
                >
                  Renombrar
                </Button>
                <Button variant="ghost" onClick={() => setRenombrando(null)}>
                  Cancelar
                </Button>
              </>
            ) : (
              <>
                <code className={styles.valor}>{v}</code>
                {renombrados[v] ? (
                  <span className={styles.marca}>→ {renombrados[v]} (preparado)</span>
                ) : null}
                <span className={styles.spacer} />
                <Button
                  variant="ghost"
                  disabled={soloLectura || trabajando || Boolean(renombrados[v])}
                  onClick={() => {
                    setRenombrando(v);
                    setNombreNuevo(v);
                  }}
                >
                  Renombrar…
                </Button>
              </>
            )}
          </li>
        ))}
        {preparados.map((v) => (
          <li key={`pendiente:${v}`} className={styles.pendiente}>
            <span className={styles.indice}>+</span>
            <code className={styles.valor}>{v}</code>
            <span className={styles.spacer} />
            <span className={styles.marca}>preparado, sin aplicar</span>
          </li>
        ))}
      </ol>

      <div className={styles.agregar}>
        <Input
          value={nuevo}
          onChange={(e) => setNuevo(e.target.value)}
          placeholder="Valor nuevo"
          aria-label="Valor nuevo"
          disabled={soloLectura || trabajando}
        />
        {/* «Al final» es una OPCIÓN de la lista y no un placeholder. Con
            `estricto`, el valor solo sale de `options`: sin esta entrada, una
            vez elegido «antes de X» no había forma de volver atrás —ni
            borrando el texto, ni con Escape— y se preparaba una posición que
            ya no se quería. Y la posición de un enum no se puede corregir
            después de aplicarla. */}
        <Combobox
          value={antesDe}
          onChange={setAntesDe}
          ariaLabel="Dónde insertarlo"
          estricto
          placeholder="Al final"
          disabled={soloLectura || trabajando}
          options={[
            { value: "", label: "Al final" },
            ...valores.map((v) => ({ value: v, label: `Antes de ${v}` })),
          ]}
        />
        <Button onClick={() => void agregar()} disabled={!puedeAgregar}>
          Agregar valor
        </Button>
      </div>
      {yaEsta ? (
        <p className={styles.error}>
          «{nuevo.trim()}» ya está
          {valores.includes(nuevo.trim()) ? " en la lista" : " preparado, sin aplicar"}.
        </p>
      ) : null}

      {/* Decirlo acá, y no cuando alguien busque el botón que no existe. */}
      <p className={styles.nota}>
        <strong>Un valor no se puede sacar.</strong> PostgreSQL no tiene «DROP VALUE»: habría que
        crear un tipo nuevo, reescribir cada columna que use éste y borrar el original — y eso
        falla si alguna fila todavía tiene el valor. Tampoco se puede mover uno de lugar después de
        agregarlo, así que la posición se elige ahora.
      </p>
    </section>
  );
}
