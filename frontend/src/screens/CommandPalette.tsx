import { useEffect, useRef, useState } from "react";
import type { $Object as DBObject, Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Glyph } from "../components/ui";
import type { ObjectKind as GlyphKind } from "../components/ui";
import { puntaje, resalta } from "../lib/buscar";
import { glifoDe, idDe, metaDe } from "../lib/objetos";
import styles from "./CommandPalette.module.css";

/**
 * Una acción que la paleta puede correr.
 *
 * Las arma el Shell a partir de las MISMAS funciones que usan sus botones. No
 * es una lista paralela de comandos: si lo fuera, agregar un botón y olvidarse
 * de la paleta —o al revés— sería la forma normal de que las dos se separen, y
 * la que queda desactualizada es siempre la que menos se mira.
 */
export interface Accion {
  id: string;
  label: string;
  /** Lo que se muestra a la derecha: un atajo, o de dónde sale. */
  meta?: string;
  kind: GlyphKind;
  correr: () => void;
}

type Entrada =
  | { tipo: "accion"; accion: Accion; puntos: number }
  | { tipo: "tabla"; schema: string; name: string; puntos: number }
  | { tipo: "objeto"; objeto: DBObject; puntos: number };

/**
 * S22: la paleta de comandos.
 *
 * Existe porque el chip «Ctrl K» estaba en la barra de título desde la
 * Iteración 1 y no había NINGÚN handler de teclado en el frontend: prometía
 * algo que no existía. Eso es peor que no tener el atajo, porque quien lo
 * prueba concluye que la aplicación está rota.
 *
 * Lo que ofrece son tres cosas y en este orden: las acciones del workspace, las
 * tablas y los demás objetos. El orden no es alfabético ni por relevancia
 * calculada: las acciones van primero porque son cinco y las tablas pueden ser
 * doscientas, así que sin eso escribir una letra sepulta «Nueva consulta» bajo
 * la primera tabla que la contenga.
 *
 * Solo aparece lo que se PUEDE hacer ahora. Una paleta llena de entradas
 * deshabilitadas es la misma promesa vacía que el chip, repetida veinte veces.
 */
export function CommandPalette({
  abierta,
  acciones,
  snapshot,
  onCerrar,
  onAbrirTabla,
  onAbrirObjeto,
}: {
  abierta: boolean;
  acciones: readonly Accion[];
  snapshot: Snapshot | null;
  onCerrar: () => void;
  onAbrirTabla: (schema: string, tabla: string) => void;
  onAbrirObjeto: (o: DBObject) => void;
}) {
  const [texto, setTexto] = useState("");
  const [resaltado, setResaltado] = useState(0);
  const campo = useRef<HTMLInputElement>(null);
  const lista = useRef<HTMLDivElement>(null);
  // El handler de teclado vive en un efecto que se monta UNA vez por apertura,
  // así que lee lo de cada render por referencia. Volver a suscribirlo en cada
  // tecla sería re-registrar el listener con cada letra que se escribe.
  const entradasRef = useRef<Entrada[]>([]);
  const activoRef = useRef(0);
  const correrRef = useRef<(e: Entrada) => void>(() => {});
  // A dónde volvía el foco. Sin esto, cerrar la paleta lo dejaba en `body` y la
  // grilla y el editor —que manejan sus propias teclas— quedaban sordos hasta
  // que alguien les clickeara encima.
  const foco = useRef<HTMLElement | null>(null);
  // La última posición real del puntero. Ver el onMouseMove de la fila.
  const puntero = useRef({ x: -1, y: -1 });

  // Cada apertura arranca limpia. Guardar lo último escrito parece una
  // comodidad y no lo es: la paleta se abre para hacer OTRA cosa, y encontrarla
  // con el filtro de la vez pasada obliga a borrarlo antes de empezar.
  useEffect(() => {
    if (!abierta) return;
    foco.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setTexto("");
    setResaltado(0);
    puntero.current = { x: -1, y: -1 };
    // El foco va al campo en el frame siguiente: el diálogo todavía no está
    // montado cuando este efecto corre.
    const t = window.setTimeout(() => campo.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, [abierta]);

  const entradas = abierta ? buscar(texto, acciones, snapshot) : [];
  const activo = Math.min(resaltado, Math.max(0, entradas.length - 1));

  // El teclado se escucha en `window` y no en el campo. Estaba en el campo, y
  // un solo clic adentro de la caja —en el pie, en el relleno de la lista, en el
  // borde— lo desenfocaba: desde ahí las flechas, Enter y Escape quedaban
  // muertos y la única salida era clickear afuera.
  useEffect(() => {
    if (!abierta) return;
    function alTeclado(ev: KeyboardEvent) {
      switch (ev.key) {
        case "ArrowDown":
          ev.preventDefault();
          // El tope de abajo es 0 y no `length - 1`: con la lista vacía eso
          // daba −1, no quedaba ninguna fila activa y Enter no hacía nada
          // hasta escribir otra letra.
          setResaltado((n) => Math.max(0, Math.min(n + 1, entradasRef.current.length - 1)));
          break;
        case "ArrowUp":
          ev.preventDefault();
          setResaltado((n) => Math.max(n - 1, 0));
          break;
        case "Enter": {
          ev.preventDefault();
          const e = entradasRef.current[activoRef.current];
          if (e) correrRef.current(e);
          break;
        }
        case "Escape":
          ev.preventDefault();
          onCerrar();
          break;
      }
    }
    window.addEventListener("keydown", alTeclado, true);
    return () => window.removeEventListener("keydown", alTeclado, true);
  }, [abierta, onCerrar]);

  useEffect(() => {
    lista.current?.querySelector('[data-activo="1"]')?.scrollIntoView({ block: "nearest" });
  }, [activo, texto]);

  entradasRef.current = entradas;
  activoRef.current = activo;
  correrRef.current = correr;

  if (!abierta) return null;

  function correr(e: Entrada) {
    cerrar();
    if (e.tipo === "accion") e.accion.correr();
    else if (e.tipo === "tabla") onAbrirTabla(e.schema, e.name);
    else onAbrirObjeto(e.objeto);
  }

  // Cerrar devuelve el foco a donde estaba. La grilla y el editor SQL manejan
  // sus propias teclas mirando el foco; sin esto quedaban sordos hasta que
  // alguien les clickeara encima.
  function cerrar() {
    onCerrar();
    foco.current?.focus();
  }

  return (
    // Capa propia y no el <dialog> del design system: la paleta se cierra al
    // tocar fuera, no lleva título ni pie, y se pega arriba en vez de
    // centrarse. Reusar Dialog obligaría a apagarle casi todo.
    <div className={styles.capa} onMouseDown={cerrar}>
      <div
        className={styles.caja}
        role="dialog"
        aria-modal="true"
        aria-label="Paleta de comandos"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <input
          ref={campo}
          className={styles.campo}
          value={texto}
          onChange={(e) => {
            setTexto(e.target.value);
            setResaltado(0);
          }}
          placeholder="Buscá una acción, una tabla o un objeto…"
          aria-label="Buscar"
          {...(entradas.length > 0 ? { "aria-controls": "kn-paleta-lista" } : {})}
          aria-activedescendant={entradas[activo] ? `kn-paleta-${activo}` : undefined}
        />

        {entradas.length === 0 ? (
          <p className={styles.vacio}>
            {/* Con el campo vacío no hay nada que «no coincida»: pasa al abrir
                la paleta mientras el esquema todavía se está leyendo, y el
                mensaje salía con las comillas vacías, que se lee como un bug. */}
            {texto.trim() === ""
              ? "Todavía no hay nada que buscar."
              : `Nada coincide con «${texto}».`}
          </p>
        ) : (
          <div className={styles.lista} id="kn-paleta-lista" role="listbox" ref={lista}>
            {entradas.map((e, i) => (
              <div
                key={claveDe(e)}
                id={`kn-paleta-${i}`}
                role="option"
                aria-selected={i === activo}
                data-activo={i === activo ? "1" : "0"}
                className={i === activo ? styles.filaActiva : styles.fila}
                // `mousedown` y no `click`: el mousedown de la capa de atrás
                // cierra la paleta, y para cuando llegaría el click el elemento
                // ya no está.
                onMouseDown={(ev) => {
                  ev.stopPropagation();
                  correr(e);
                }}
                // Solo si el puntero se MOVIO de verdad. Con el mouse quieto
                // sobre la lista, bajar con las flechas hace scrollear las
                // filas debajo del cursor y el navegador manda `mousemove`: sin
                // esta comparación, la selección saltaba de vuelta a la fila que
                // quedara bajo el puntero y las flechas no servían.
                onMouseMove={(ev) => {
                  if (ev.clientX === puntero.current.x && ev.clientY === puntero.current.y) return;
                  puntero.current = { x: ev.clientX, y: ev.clientY };
                  setResaltado(i);
                }}
              >
                <Glyph kind={glifoDeEntrada(e)} />
                <span className={styles.label}>
                  {resalta(etiquetaDe(e), texto.trim()).map(([parte, coincide], k) =>
                    coincide ? (
                      <mark key={k} className={styles.match}>
                        {parte}
                      </mark>
                    ) : (
                      <span key={k}>{parte}</span>
                    ),
                  )}
                </span>
                <span className={styles.meta}>{metaDeEntrada(e)}</span>
              </div>
            ))}
          </div>
        )}

        <footer className={styles.pie}>
          <span>↑↓ moverse</span>
          <span>↵ abrir</span>
          <span>Esc cerrar</span>
        </footer>
      </div>
    </div>
  );
}

/** Cuántas tablas y objetos entran. Sin tope, un esquema de doscientas tablas
 *  con el campo vacío dibuja doscientas filas que nadie va a leer. */
const TOPE = 40;

function buscar(texto: string, acciones: readonly Accion[], snapshot: Snapshot | null): Entrada[] {
  const q = texto.trim();

  const deAcciones: Entrada[] = acciones
    .map((a) => ({ tipo: "accion" as const, accion: a, puntos: puntaje(a.label, q) }))
    .filter((e) => e.puntos > 0)
    .sort((a, b) => b.puntos - a.puntos);

  const deDatos: Entrada[] = [];
  for (const sc of snapshot?.schemas ?? []) {
    for (const t of sc.tables ?? []) {
      const p = puntaje(t.name, q);
      if (p > 0) deDatos.push({ tipo: "tabla", schema: sc.name, name: t.name, puntos: p });
    }
    for (const o of sc.objects ?? []) {
      const p = puntaje(o.name, q);
      if (p > 0) deDatos.push({ tipo: "objeto", objeto: o, puntos: p });
    }
  }
  deDatos.sort((a, b) => b.puntos - a.puntos);

  // Las acciones SIEMPRE arriba, sin mezclar puntajes. Son cinco contra
  // doscientas tablas: ordenarlas juntas hace que una tabla llamada «consultas»
  // le gane a «Nueva consulta» y la paleta deje de servir para lo que más se usa.
  return [...deAcciones, ...deDatos.slice(0, TOPE)];
}

function etiquetaDe(e: Entrada): string {
  if (e.tipo === "accion") return e.accion.label;
  if (e.tipo === "tabla") return e.name;
  return e.objeto.name;
}

function metaDeEntrada(e: Entrada): string {
  if (e.tipo === "accion") return e.accion.meta ?? "";
  if (e.tipo === "tabla") return e.schema;
  const extra = metaDe(e.objeto);
  return extra ? `${e.objeto.schema} · ${extra}` : e.objeto.schema;
}

function glifoDeEntrada(e: Entrada): GlyphKind {
  if (e.tipo === "accion") return e.accion.kind;
  if (e.tipo === "tabla") return "table";
  return glifoDe(e.objeto.kind);
}

function claveDe(e: Entrada): string {
  if (e.tipo === "accion") return `a:${e.accion.id}`;
  if (e.tipo === "tabla") return `t:${e.schema}.${e.name}`;
  return `o:${idDe(e.objeto)}`;
}
