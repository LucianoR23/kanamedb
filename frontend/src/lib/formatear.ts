import { formatDialect, mariadb, mysql, postgresql, sqlite } from "sql-formatter";
import type { DialectOptions } from "sql-formatter";

/**
 * Ordena la SQL del editor con el dialecto del motor conectado.
 *
 * Con `sql-formatter` y nunca a mano: formatear mal el texto no es un botón
 * que no anda, es el texto de la persona alterado —una comilla mal cerrada,
 * un comentario movido adentro de una cadena— y en el editor no hay de dónde
 * recuperarlo. Un formateador correcto necesita un parser por dialecto, y eso
 * no se improvisa. Ver kaname-plan.md § 6.
 *
 * Se importan los cuatro dialectos y se usa `formatDialect`, no `format` con
 * el nombre del lenguaje: `format` arrastra los diecisiete dialectos al bundle
 * y el binario se distribuye copiando y pegando.
 *
 * Lo que NO se cambia: las mayúsculas. Ni las de las palabras clave ni las de
 * los identificadores, que en Postgres son parte del nombre si van entre
 * comillas y en MySQL distinguen tablas según el sistema de archivos. El
 * formateador acomoda espacios, saltos y sangría, y nada más.
 *
 * Lanza si el texto no se puede interpretar; quien llama lo dice y deja el
 * texto como estaba.
 */
export function formatearSQL(sql: string, engine: string): string {
  return formatDialect(sql, {
    dialect: dialectoDe(engine),
    // Postgres numera sus parámetros ($1) y el formateador ya lo sabe; lo que
    // no sabe es que en el editor también se escriben con nombre (:nombre), y
    // sin decírselo `x = :nombre` salía como `x =:nombre`. El `::` de un cast
    // no se confunde: probado con `x::int` en la misma consulta. Lo que sí
    // cambia es un corte de array con límite que empieza con letra:
    // `arr[lo:hi]` sale `arr[lo :hi]`, que Postgres acepta igual.
    ...(engine === "postgres" ? { paramTypes: { numbered: ["$"], named: [":"] } } : {}),
    tabWidth: 2,
    keywordCase: "preserve",
    identifierCase: "preserve",
    dataTypeCase: "preserve",
    functionCase: "preserve",
    logicalOperatorNewline: "before",
    expressionWidth: 60,
    linesBetweenQueries: 1,
  });
}

function dialectoDe(engine: string): DialectOptions {
  switch (engine) {
    case "mysql":
      return mysql;
    case "mariadb":
      return mariadb;
    case "sqlite":
      return sqlite;
    default:
      return postgresql;
  }
}
