import { ObjectKind } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { $Object as DBObject } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { ObjectKind as GlyphKind } from "../components/ui";

/**
 * El orden y el nombre de cada grupo de objetos del árbol.
 *
 * Es el MISMO orden que usa la cobertura del volcado (`ordenDeTipos`, en
 * `internal/dump/cobertura.go`), y hay que mantenerlo así: las dos listas salen
 * de la misma llamada al catálogo, y ver los mismos objetos en dos órdenes
 * distintos —uno en el árbol, otro en el panel de cobertura— hace dudar de si
 * son la misma información. Ya habían divergido una vez.
 *
 * Las clases están puestas de lo más usado a lo menos: nadie abre Kaname para
 * mirar una extensión.
 */
const GRUPOS: ReadonlyArray<{ kind: ObjectKind; singular: string; plural: string; articulo: "un" | "una" }> = [
  { kind: ObjectKind.ObjView, singular: "vista", plural: "Vistas", articulo: "una" },
  { kind: ObjectKind.ObjMatView, singular: "vista materializada", plural: "Vistas materializadas", articulo: "una" },
  { kind: ObjectKind.ObjFunction, singular: "función", plural: "Funciones", articulo: "una" },
  { kind: ObjectKind.ObjProcedure, singular: "procedimiento", plural: "Procedimientos", articulo: "un" },
  { kind: ObjectKind.ObjTrigger, singular: "trigger", plural: "Triggers", articulo: "un" },
  { kind: ObjectKind.ObjEnum, singular: "enum", plural: "Enums", articulo: "un" },
  { kind: ObjectKind.ObjDomain, singular: "dominio", plural: "Dominios", articulo: "un" },
  { kind: ObjectKind.ObjComposite, singular: "tipo compuesto", plural: "Tipos compuestos", articulo: "un" },
  { kind: ObjectKind.ObjSequence, singular: "secuencia", plural: "Secuencias", articulo: "una" },
  { kind: ObjectKind.ObjPolicy, singular: "política de RLS", plural: "Políticas de RLS", articulo: "una" },
  { kind: ObjectKind.ObjEvent, singular: "evento", plural: "Eventos", articulo: "un" },
  { kind: ObjectKind.ObjExtension, singular: "extensión", plural: "Extensiones", articulo: "una" },
];

export interface GrupoDeObjetos {
  kind: ObjectKind;
  titulo: string;
  objetos: DBObject[];
}

/**
 * agrupar arma los grupos que el árbol dibuja, salteando los vacíos.
 *
 * Un grupo vacío no se muestra. Un esquema de Postgres sin políticas de RLS ni
 * secuencias no gana nada con dos renglones que dicen «(ninguna)»: gana con
 * que no estén, porque lo que queda en pantalla es lo que hay.
 *
 * Lo que NO se saltea es una clase que el backend mande y esta lista no
 * conozca: va a un grupo con su propio nombre crudo al final. Esconderla sería
 * exactamente el silencio que la cobertura del volcado existe para evitar,
 * repetido en el árbol.
 */
export function agrupar(objetos: readonly DBObject[]): GrupoDeObjetos[] {
  const porClase = new Map<ObjectKind, DBObject[]>();
  for (const o of objetos) {
    const ya = porClase.get(o.kind);
    if (ya) ya.push(o);
    else porClase.set(o.kind, [o]);
  }

  const out: GrupoDeObjetos[] = [];
  for (const g of GRUPOS) {
    const enGrupo = porClase.get(g.kind);
    if (enGrupo && enGrupo.length > 0) {
      out.push({ kind: g.kind, titulo: g.plural, objetos: enGrupo });
    }
    porClase.delete(g.kind);
  }
  // Lo que quedó es una clase que este archivo no conoce.
  for (const [kind, enGrupo] of porClase) {
    out.push({ kind, titulo: kind, objetos: enGrupo });
  }
  return out;
}

/** Nombre en singular de una clase, para un título o un aviso. */
export function nombreDeClase(kind: ObjectKind): string {
  return GRUPOS.find((g) => g.kind === kind)?.singular ?? kind;
}

/**
 * El nombre de la clase con su artículo: «una vista», «un procedimiento».
 *
 * El género va en la tabla y no se deduce de la terminación. Probando a mano
 * salió «Editar un secuencia», que es lo que pasa cuando el artículo se elige
 * con un caso especial para la única palabra que se probó.
 */
export function conArticulo(kind: ObjectKind): string {
  const g = GRUPOS.find((x) => x.kind === kind);
  return g ? `${g.articulo} ${g.singular}` : kind;
}

/**
 * glifoDe traduce la clase del catálogo a la ficha de dos letras.
 *
 * Es la identidad para todo lo que el design system conoce —los nombres se
 * alinearon a propósito— y cae en `table` para lo que no. Sin el respaldo, una
 * clase nueva del backend rompería el render entero de la fila.
 */
const CONOCIDAS = new Set<string>([
  "view", "materializedView", "function", "procedure", "trigger",
  "enum", "domain", "composite", "sequence", "policy", "event",
]);

export function glifoDe(kind: ObjectKind): GlyphKind {
  return CONOCIDAS.has(kind) ? (kind as GlyphKind) : "table";
}

/**
 * idDe identifica un objeto de forma única dentro del árbol.
 *
 * Lleva la CLASE y los ARGUMENTOS, no solo el nombre. Sin la clase, una vista y
 * una secuencia que se llamen igual —perfectamente legal, son catálogos
 * distintos— serían la misma fila; sin los argumentos, dos sobrecargas de una
 * función se seleccionarían juntas.
 */
export function idDe(o: DBObject): string {
  return `${o.kind}:${o.schema}.${o.name}${o.args ? `(${o.args})` : ""}${o.table ? `@${o.table}` : ""}`;
}

/** Lo que se muestra a la derecha del nombre: de qué tabla cuelga, o su firma. */
export function metaDe(o: DBObject): string {
  if (o.table) return o.table;
  if (o.args) return `(${o.args})`;
  return "";
}
