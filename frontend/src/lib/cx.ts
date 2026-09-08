/** Une nombres de clase descartando lo falsy.
 *
 *  Los CSS Modules se tipan como índices, así que con `noUncheckedIndexedAccess`
 *  cada `styles.foo` es `string | undefined`. Mantenemos la flag —es la que va a
 *  cuidar los accesos por índice de la grilla de datos— y filtramos acá. */
export function cx(
  ...parts: readonly (string | false | null | undefined)[]
): string {
  return parts.filter(Boolean).join(" ");
}
