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
