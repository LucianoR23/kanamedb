# Pendientes grandes de la auditoría — 2026-09-12

Lo que la auditoría del 2026-09-11 (`audit-fable-2026-09-11.md`) dejó a la
vista y **no entra como fix**: son piezas de producto o rediseños que necesitan
su propia unidad de trabajo, con contrato, pantalla y tests contra los cuatro
motores. Cada una lleva el estado provisorio con el que quedó el código hoy,
para que se sepa qué promesa se está haciendo mientras tanto.

Se actualiza en el mismo commit que la cierre o la cambie.

## 1. Control manual de transacciones en el editor SQL

**Origen:** K-03 y C-01. **Estado provisorio:** `Queries.Run` rechaza el lote
entero si alguna sentencia es `BEGIN`, `START TRANSACTION`, `COMMIT`,
`ROLLBACK`, `SAVEPOINT`, `RELEASE`, `END` o `SET autocommit`, con un mensaje
que dice que el editor corre en autocommit. Falla antes de tocar nada; antes
fallaba al revés de lo pedido.

**Por qué no es un fix.** Cada sentencia del editor toma su propia conexión
del pool. Contra Postgres, `pgxpool` destruye la que vuelve en transacción, así
que `BEGIN; UPDATE …; ROLLBACK;` corría el `UPDATE` en otra conexión, en
autocommit, y el `ROLLBACK` respondía bien sobre una tercera (comprobado:
`OK=true`, tabla vacía). Contra MySQL y SQLite «funcionaba» porque
`database/sql` devuelve la última conexión liberada, y un `BEGIN` sin cerrar
dejaba la conexión en transacción adentro del pool: lock del archivo en SQLite,
commit implícito en el próximo `Apply` de MySQL.

**Qué es lo correcto** (lo que hacen DBeaver, DataGrip y pgAdmin):

- Un toggle **Auto-commit** en la barra del editor, encendido por defecto, por
  pestaña.
- Con auto-commit apagado, la pestaña toma **una conexión dedicada** del pool
  (`pool.Acquire` en Postgres, `db.Conn` en `database/sql`) y la retiene entre
  ejecuciones. `BEGIN`, `COMMIT` y `ROLLBACK` corren ahí y significan lo que
  dicen. El rechazo actual se levanta solo en ese modo.
- Un indicador visible **«Transacción abierta · N sentencias»** con botones
  Commit / Rollback, para que nunca quede una transacción huérfana sin verse.
  Sin eventos en este proyecto, la pestaña lo sabe porque cada `Run` devuelve
  el estado de la transacción (`pgconn.TxStatus()`; en SQLite
  `sqlite3_get_autocommit` a través de `driver.Conn`; en MySQL, seguir el
  estado por los comandos y por `@@autocommit`/`@@in_transaction` en MariaDB).
- Al cerrar la pestaña, desconectar, o vencer la inactividad: **`ROLLBACK`
  explícito antes de soltar la conexión**. Nunca devolver al pool una
  conexión en transacción; en Postgres además `Release` la destruiría.
- Contra producción, el `COMMIT` pide el nombre de la base como cualquier
  escritura (`confirmarEscritura`), y «Bloquear DROP y TRUNCATE» y el rechazo
  de lo que apaga el solo lectura siguen valiendo sentencia por sentencia.
- La conexión dedicada cuenta como «ocupada» para la desconexión por
  inactividad mientras tenga una transacción abierta, o se cierra con
  `ROLLBACK` al vencer: decidirlo, y decirlo en el indicador.
- Tests: `BEGIN; INSERT; ROLLBACK;` en tres `Run` separados deja cero filas en
  los cuatro motores; cerrar la pestaña con una transacción abierta hace
  rollback y el pool no queda envenenado (un `Apply` posterior en SQLite no
  da «cannot start a transaction within a transaction»); `PoolSize` 2 con una
  pestaña en transacción sigue dejando correr el árbol.

**Tamaño:** estado por pestaña en Go (`Queries` tiene el `runID`; hace falta
un identificador de pestaña), dos bindings nuevos (`SetAutocommit`,
`TransactionState` o el estado dentro de `RunResult`), la barra del editor, y
los tests. Media iteración.

## 2. Una sola reconstrucción por tabla en SQLite

**Origen:** encontrado al atender K-08; no estaba en la auditoría. **Estado
provisorio:** `Stage` y `preparar` rechazan (`ErrRebuildNotAlone`) que un
cambio que reconstruye una tabla comparta el changeset con cualquier otro
cambio de estructura sobre la misma tabla. Hay que aplicar de a uno.

**Por qué.** El guion de la reconstrucción —`CREATE` de la copia, `INSERT …
SELECT`, `DROP`, `RENAME`— se escribe leyendo el catálogo al renderizar, y
todos los cambios se renderizan antes de ejecutar el primero. `AddColumn a` +
`SetNotNull n` dejaba la tabla **sin `a`**; `SetNotNull n` + `SetNotNull m`
dejaba `n` nullable. Con `OK`.

**Qué es lo correcto.** Que el renderizador de SQLite reciba TODOS los cambios
de estructura de una tabla y escriba **una** reconstrucción que los acumule:
columnas agregadas, tipos, nulabilidad, defaults, claves, checks. Es lo que
hace la propia documentación de SQLite («make the desired changes to the table
definition, then …»), y lo que permitiría además que la vista previa muestre
la definición final. Implica que `RenderDDL` deje de ser por cambio en SQLite
—o que `preparar` agrupe y delegue a un `RenderRebuild(cambios)`— y que la
huella de la vista previa (K-08) se calcule sobre ese guion. Tests: los dos
casos de arriba con el conteo de columnas y la nulabilidad al final, y el
ciclo `inspect → apply → inspect` con diff vacío.

## 3. Volcado y exportación con fidelidad binaria

**Origen:** C-02 (SQLite escribe `[N bytes]` por cada BLOB), C-13 (MySQL
entrega los binarios como bytes crudos en un `string`), C-26 (formatos).
**Estado:** ver el estado de cada hallazgo en el documento de la auditoría a
medida que se cierran; lo que quede acá es lo que necesite un contrato nuevo
(por ejemplo, un tipo de celda binaria en `RowStream` en vez de `[]*string`).

## 4. `drift`: identidad, claves compuestas y esquemas de distinto nombre

**Origen:** C-06 a C-09, C-18, C-20, C-24. **Estado:** ídem. Lo que necesite
llevar `Identity`, `Generated` y el default al snapshot (C-08) toca las cuatro
introspecciones y el renderizador de `CreateTable` de cada motor; se decide al
llegar si entra como fix o se especifica acá.
