/**
 * El puntaje de una coincidencia, para ordenar lo que la paleta muestra.
 *
 * Es deliberadamente simple y explicable, no difuso. Una búsqueda difusa
 * —«nc» encuentra «Nueva consulta»— se siente mágica hasta que ordena mal y
 * nadie puede decir por qué: cuál de dos resultados sale primero pasa a ser una
 * propiedad emergente de los pesos. Acá hay cuatro casos, en este orden:
 *
 *  1. El texto ES lo buscado.
 *  2. Empieza con lo buscado.
 *  3. Alguna PALABRA empieza con lo buscado — es lo que hace que «cam»
 *     encuentre «Cambios pendientes» y también «pedidos_cam`biados`».
 *  4. Aparece en algún lado.
 *
 * Devuelve 0 cuando no coincide, y a mayor número mejor. La posición de la
 * coincidencia desempata: lo que empieza antes gana.
 */
export function puntaje(texto: string, buscado: string): number {
  if (buscado === "") return 1;
  const t = texto.toLowerCase();
  const b = buscado.toLowerCase();

  if (t === b) return 1000;
  if (t.startsWith(b)) return 800 - Math.min(t.length, 100);

  // Una palabra nueva empieza después de un espacio, un guion bajo, un punto o
  // un guion: son los cuatro separadores que aparecen en un nombre de tabla y
  // en una etiqueta de la interfaz.
  const inicioDePalabra = t.split(/[\s_.\-]+/).some((p) => p.startsWith(b));
  if (inicioDePalabra) return 600 - Math.min(t.length, 100);

  const donde = t.indexOf(b);
  if (donde >= 0) return 400 - Math.min(donde, 100);
  return 0;
}

/**
 * resalta parte el texto en los tramos que coinciden y los que no.
 *
 * Devuelve pares [texto, coincide]. Sirve para poner en negrita lo que se
 * escribió: sin eso, con una lista de veinte tablas parecidas hay que leerlas
 * enteras para ver por qué están ahí.
 */
export function resalta(texto: string, buscado: string): Array<[string, boolean]> {
  if (buscado === "") return [[texto, false]];
  const donde = texto.toLowerCase().indexOf(buscado.toLowerCase());
  if (donde < 0) return [[texto, false]];
  const out: Array<[string, boolean]> = [];
  if (donde > 0) out.push([texto.slice(0, donde), false]);
  out.push([texto.slice(donde, donde + buscado.length), true]);
  if (donde + buscado.length < texto.length) out.push([texto.slice(donde + buscado.length), false]);
  return out;
}
