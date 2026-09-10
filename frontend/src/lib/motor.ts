import { MariaSQL, MySQL, PostgreSQL, SQLite, StandardSQL } from "@codemirror/lang-sql";
import type { SQLDialect } from "@codemirror/lang-sql";

/**
 * Lo que el frontend necesita saber de un motor, en un solo lugar.
 *
 * `nombreDeMotor` estaba copiado en dos pantallas y estaba por copiarse en una
 * tercera. Tres copias de un switch de cuatro casos es donde alguien agrega un
 * motor y arregla dos.
 */

/** El nombre como lo escribe cada proyecto: «MySQL» y «MariaDB» llevan
 *  mayúsculas y «sqlite» no. */
export function nombreDeMotor(k: string): string {
  switch (k) {
    case "postgres":
      return "PostgreSQL";
    case "mysql":
      return "MySQL";
    case "mariadb":
      return "MariaDB";
    case "sqlite":
      return "SQLite";
  }
  return "El motor";
}

/**
 * El dialecto de CodeMirror para resaltar y autocompletar.
 *
 * Estaba fijo en PostgreSQL, así que conectado a MySQL el editor pintaba con
 * reglas de Postgres y la barra de estado decía «dialecto PostgreSQL». No es
 * cosmético: cambia qué es palabra reservada, cómo se citan los identificadores
 * —acento invertido en MySQL, comilla doble en los otros— y qué ofrece el
 * autocompletado.
 *
 * MariaDB tiene su propio dialecto en CodeMirror y no es el de MySQL: divergen
 * en palabras reservadas, igual que divergen los motores.
 */
export function dialectoDe(k: string): SQLDialect {
  switch (k) {
    case "postgres":
      return PostgreSQL;
    case "mysql":
      return MySQL;
    case "mariadb":
      return MariaSQL;
    case "sqlite":
      return SQLite;
  }
  // Sin conexión abierta todavía no hay motor. El estándar es lo único honesto:
  // resaltar con las reglas de un motor que quizá no sea el que se va a abrir
  // sería adivinar.
  return StandardSQL;
}

/**
 * Si el motor guarda una descripción de las tablas y las columnas.
 *
 * SQLite no: no existe en ningún lado, y `internal/sqlite` devuelve
 * `ErrUnsupported` con ese motivo. Preguntarlo ANTES es lo que evita ofrecer
 * una opción que al aplicar va a hacer fallar el changeset entero.
 */
export function soportaComentarios(k: string): boolean {
  return k !== "sqlite";
}

/**
 * Plural en castellano para los contadores que se muestran.
 *
 * Existe porque «1 filas» y «1 sentencias» aparecieron en cuatro lugares
 * distintos, y cada uno se arregló por separado hasta que fueron demasiados. Un
 * contador que no concuerda hace dudar del resto del mensaje.
 */
export function plural(n: number, uno: string, varios: string): string {
  return n === 1 ? uno : varios;
}
