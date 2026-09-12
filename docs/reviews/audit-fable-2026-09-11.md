# Auditoría Kaname (backend) — 2026-09-11

Auditoría de solo lectura sobre `main`, commit `8f31033`. Se leyó el código Go completo de
`internal/` y `main.go`, la superficie de bindings de Wails, y del frontend únicamente
los puntos donde datos de la base o rutas cruzan hacia Go. No se corrió nada: ni tests,
ni builds, ni contenedores. Donde una afirmación depende del comportamiento de una
dependencia, se leyó la fuente en el módulo cacheado (`pgx v5.11.0` —la primera versión
de este texto decía `v5.9.2`; `go.mod` pinea `v5.11.0` y las líneas citadas de `pgxpool`
son las de esa versión—, `puddle v2.2.2`, `wails v3.0.0-beta.17`) y se cita.

El 2026-09-12 se hizo una **segunda pasada enfocada en correctitud**, sobre lo que la
primera había leído por encima. Sus hallazgos llevan prefijo `C-` y están en su propia
sección; el resumen de abajo cubre la primera pasada y el suyo está al principio de la
segunda.

## Resumen

| Severidad | Primera pasada (K-) | Segunda pasada (C-) | Total |
|---|---|---|---|
| CRÍTICO | 1 | 1 | 2 |
| ALTO | 3 | 8 | 11 |
| MEDIO | 6 | 11 | 17 |
| BAJO | 8 | 11 | 19 |

**Los tres riesgos principales**

1. **K-01 · Reconstrucción de tabla en SQLite sin transacción** (CRÍTICO). Con la
   casilla «Una sola transacción» apagada, el guion de rebuild —que lleva un `DROP
   TABLE`— sale por `Conn.Exec`, sin apagar `foreign_keys`. El propio código de SQLite
   documenta que ese camino «borraría las filas de las tablas hijas» sin dar error. El
   pragma correcto solo se pone en `Begin`, y el apply sin transacción nunca llama a
   `Begin` para un cambio de esquema.
2. **K-03 · Las transacciones manuales del editor SQL no existen contra Postgres**
   (ALTO). Cada sentencia toma su propia conexión del pool y `pgxpool` destruye la que
   se devuelve en medio de una transacción. `BEGIN; UPDATE …; ROLLBACK;` deja el UPDATE
   confirmado y el ROLLBACK responde bien.
3. **K-02 · «Bloquear DROP y TRUNCATE» es una casilla sin código detrás** (ALTO). El
   campo se guarda, se muestra encendido en el gestor y en la pestaña Safety, y ningún
   camino de Go lo lee. Lo mismo vale, en menor grado, para «pedir confirmación fuera
   de producción» y «desconectar por inactividad» (K-07).

De la segunda pasada, los tres que más pesan: **C-02** (el volcado de SQLite reemplaza
cada BLOB por `[N bytes]` y dice que no dejó nada afuera), **C-03** (en MySQL y MariaDB
todo DML del editor muestra «0 filas» y, si falla, se reenvía) y el grupo de `drift`
(**C-06** a **C-09**). C-01 cierra lo que en K-03 había quedado como sospechado para
MySQL y SQLite.

La base es sólida en lo que más pesa del modelo de amenazas: credenciales solo en el
keychain, DSN que se arma y se descarta, `Redact` sobre todo texto ajeno, ningún log,
verificación de host key sin atajos, TLS con semántica de libpq en los tres motores de
servidor, identificadores citados y valores parametrizados en los cuatro. Los hallazgos
de arriba son, casi todos, invariantes que se establecen en un camino y se saltean en
otro —exactamente lo que una revisión por diff no ve—.

## Hallazgos

### [K-01] [CRÍTICO] [VERIFICADO] El rebuild de SQLite sin transacción corre con las claves foráneas encendidas y borra filas hijas

> **Estado 2026-09-12:** CORREGIDO. Verificado empíricamente antes del fix (`hija` quedaba con 0 filas de 2). `aplicarPorTramos` hace transaccional todo tramo con `RebuildsTable` aunque la casilla esté apagada. Test: `service/apply_sqlite_test.go`.

- Ubicación: `internal/service/apply.go:861-875` y `:908-925`; `internal/sqlite/conn.go:36-43` y `:51-54`; `internal/sqlite/rebuild.go:113-117`; `frontend/src/screens/PendingChanges.tsx:84` y `:202-204`.
- Evidencia:

  ```go
  // apply.go:861-875 — con la casilla apagada, cada DDL va en un tramo NO transaccional
  if !unaSola {
      tramos = nil
      for i := range sts {
          tramos = append(tramos, engine.Tramo{
              Desde: i, Hasta: i + 1,
              Transaccional: cambios[i].Kind() == change.KindData,
          })
      }
  }
  // apply.go:908-925 — y ese tramo ejecuta con sesion.db (Exec), nunca con Begin
  if !t.Transaccional {
      ...
      for i := t.Desde; i < t.Hasta; i++ {
          r, err := s.correr(ctx, sesion, sesion.db, i, sts[i])
  ```

  ```go
  // sqlite/conn.go:36-43 — el propio motor lo advierte
  // **No sirve para una sentencia con Statement.RebuildsTable puesto.** Una
  // reconstrucción necesita las claves foráneas apagadas mientras corre, y eso
  // hay que pedirlo antes de abrir la transacción: Begin con
  // engine.TxOptions{RebuildsTables: true}. Mandada por acá funcionaría —sin dar
  // ningún error— y borraría las filas de las tablas hijas.
  func (c *Conn) Exec(ctx context.Context, sql string) error {
      _, err := c.db.ExecContext(ctx, sql)
  ```

  ```go
  // rebuild.go:113-117 — el guion que llega a Exec
  fmt.Fprintf(&b, "%s;\n", t.CreateComoTabla(temporal))
  fmt.Fprintf(&b, "INSERT INTO %s (%s)\n     SELECT %s FROM %s;\n", ...)
  fmt.Fprintf(&b, "DROP TABLE %s;\n", QuoteIdent(c.Table))
  fmt.Fprintf(&b, "ALTER TABLE %s RENAME TO %s;", QuoteIdent(temporal), QuoteIdent(c.Table))
  ```

  ```tsx
  // PendingChanges.tsx:84,202 — la casilla es libre para cualquier motor
  const [transaccion, setTransaccion] = useState(true);
  <Checkbox checked={transaccion} onChange={setTransaccion}>Una sola transacción</Checkbox>
  ```

- Impacto: contra SQLite, con «Una sola transacción» destildada, cualquier cambio que
  reconstruye (`SetColumnType`, `SetNotNull`, `DropNotNull`, `SetDefault`, `DropDefault`,
  `AddPrimaryKey`, `AddForeignKey`, `AddCheck`, `DropConstraint`) ejecuta `DROP TABLE`
  con `foreign_keys = 1` (lo pone el DSN en `connection.go:557-559`): las filas de toda
  tabla hija con `ON DELETE CASCADE` desaparecen sin error y sin aviso. Además, sin
  `legacy_alter_table = ON`, el `RENAME` final falla si una vista nombra a la tabla: en
  ese punto la original ya fue borrada y la copia queda como `kn_rebuild_<tabla>`, con
  el nombre real vacío y el próximo intento rechazado por `libre` (`rebuild.go:376-388`).
  El aviso de `avisos()` (`apply.go:499-520`) habla solo del caso CON transacción. Ningún
  test cubre este camino: `apply_test.go:222` prueba `SingleTransaction: false` contra
  Postgres, y `rebuild_test.go:37,347` siempre pasa por `Begin`.
- Fix recomendado: que el servicio decida el camino según lo que hay que ejecutar y no
  solo según la casilla. Mínimo, en `aplicarPorTramos`: cuando `!unaSola`, un tramo con
  `sts[i].RebuildsTable` tiene que ser `Transaccional: true` (sigue siendo una sentencia
  por tramo; lo que cambia es que pasa por `Begin` con `RebuildsTables`). Alternativa más
  defensiva: que `sqlite.Conn.Exec` se niegue a correr un texto que contenga el prefijo
  `kn_rebuild_`, o que `Statement` lleve un flag `RequiresTx` que `correrTramo`
  compruebe antes de la rama sin transacción.
- Cómo verificar el fix: test en `internal/service` contra SQLite —padre con una hija
  `ON DELETE CASCADE` con filas— que prepare un `SetNotNull` y aplique con
  `SingleTransaction: false`; después, contar las filas de la hija. Inyectá la
  violación (volvé a `Transaccional: false`) y confirmá que el conteo cae a cero.

### [K-02] [ALTO] [VERIFICADO] `BlockDropTruncate` se guarda y se muestra encendido, pero ningún código lo hace cumplir

> **Estado 2026-09-12:** CORREGIDO. `preparar` rechaza el changeset entero (`ErrBlockedByPolicy`) con un DROP o un `ReplaceObject{Recreate}`; `Queries.Run` rechaza el lote entero si alguna sentencia es `DROP`/`TRUNCATE`, antes de correr la primera. La vista previa avisa. `Explain` es lista blanca y no lo necesitaba. Tests: `service/safety_test.go` (con inyección de la violación).

- Ubicación: `internal/connection/connection.go:313-316`; `frontend/src/screens/ConnectionManager.tsx:726`; `frontend/src/screens/SafetyTab.tsx:55-56`. Sin ninguna lectura del campo en `internal/service` ni en los motores (búsqueda de `BlockDropTruncate|blockDropTruncate` sobre todo el repo, excluyendo tests y docs: solo el modelo y la UI).
- Evidencia:

  ```go
  // connection.go:313-316
  // BlockDropTruncate hace que aplicar se niegue a ejecutar DROP y TRUNCATE.
  // Las sentencias igual se generan y se muestran; lo que no se hace es
  // correrlas. Cero: no se bloquean, que es el default del diseño.
  BlockDropTruncate bool `toml:"block_drop_truncate" json:"blockDropTruncate"`
  ```

  ```tsx
  // ConnectionManager.tsx:726 — se muestra como protección activa
  <SafetyRow on={c.safety.blockDropTruncate} label="Bloquear DROP y TRUNCATE" />
  ```

  En `Session.Apply` (`apply.go:639-692`) las únicas puertas son `soloLectura` y la
  confirmación de producción; `Queries.Run` (`queries.go:98-153`) no mira `Safety`
  salvo `EffectiveRowLimit`.
- Impacto: una persona marca la casilla contra producción, ve «Bloquear DROP y
  TRUNCATE · activo» en el gestor, y un `DropTable`/`DropColumn` del changeset o un
  `TRUNCATE` tipeado en el editor se ejecutan igual. Es la definición de cartel que
  CLAUDE.md prohíbe. Se comparte la libreta, así que la casilla también viaja
  encendida a otra máquina donde tampoco hace nada.
- Fix recomendado: implementarla en Go, en los dos caminos. En `preparar`
  (`apply.go:699-718`): si `sesion.conn.Safety.BlockDropTruncate`, rechazar cualquier
  cambio con `Op() == OpDrop`, `Type == SetColumnType` con truncamiento no aplica—,
  y `ReplaceObject` con `Recreate` (lleva un DROP). En `Queries.Run` y `Explain`: si
  `query.Command(st.SQL, dialecto)` es `DROP` o `TRUNCATE`, devolver un `Failure` que
  nombre la casilla. Hasta que exista, sacar la fila del gestor y la casilla de la
  pestaña para no prometerla.
- Cómo verificar el fix: dos tests —uno en `apply_test.go` con un `DropTable` y la
  casilla puesta, otro en `queries_test.go` con `TRUNCATE`— que exijan el rechazo y que
  la tabla siga existiendo. Inyección: apagar la comprobación y ver que la tabla se va.

### [K-03] [ALTO] [VERIFICADO] Las transacciones manuales del editor SQL contra Postgres se pierden en silencio: cada sentencia va por su propia conexión y `pgxpool` destruye la que quedó en transacción

> **Estado 2026-09-12:** CORREGIDO, en dos partes. Comprobado contra Postgres: `BEGIN; DELETE; ROLLBACK;` devolvía OK y la tabla quedaba vacía. Con auto-commit (el default) `Queries.Run` rechaza el lote entero ante BEGIN/START TRANSACTION/COMMIT/ROLLBACK/SAVEPOINT/RELEASE/END y `SET autocommit`, con el motivo (`TestElEditorRechazaElControlManualDeTransacciones`). Y el **control manual por pestaña está implementado**: con auto-commit sacado la pestaña retiene una conexión (`engine.Session`) y BEGIN/COMMIT/ROLLBACK valen de verdad (`TestConAutocommitSacadoLaTransaccionEsDeVerdad`, cuatro motores). Detalles en la sección 6 del plan.

> Actualización 2026-09-12: la parte de MySQL y SQLite, que acá quedó como sospechada,
> está verificada y desarrollada en C-01.

- Ubicación: `internal/service/queries.go:117-149`; `internal/postgres/query.go:52-56`; `pgx/v5@v5.9.2/pgxpool/conn.go:33-34` (módulo cacheado).
- Evidencia:

  ```go
  // queries.go:117,128-129 — el texto se parte y cada sentencia entra sola
  sentencias := query.Split(sql, sesion.db.Dialect())
  for i, st := range sentencias {
      uno, f := sesion.db.Run(ctx, st.SQL, engine.RunOptions{RowLimit: limite})
  ```

  ```go
  // postgres/query.go:52-56 — y cada Run toma y suelta una conexión
  conn, err := pool.Acquire(ctx)
  ...
  defer conn.Release()
  ```

  ```go
  // pgxpool/conn.go:33-34 — soltar una conexión en transacción la destruye
  if conn.IsClosed() || conn.PgConn().IsBusy() || conn.PgConn().TxStatus() != 'I' {
      res.Destroy()
  ```

- Impacto: `BEGIN; UPDATE clientes SET …; ROLLBACK;` en el editor contra Postgres:
  `BEGIN` corre y devuelve su tag, la conexión se destruye al soltarla (rollback de una
  transacción vacía), el `UPDATE` corre en OTRA conexión en autocommit y queda
  confirmado, y el `ROLLBACK` corre en una tercera sin transacción: Postgres responde
  `ROLLBACK` con un WARNING que la pestaña Mensajes no distingue de un éxito. La
  persona cree que revirtió. El plan lo agenda como pendiente («control de transacción
  manual», `kaname-plan.md:860`) y el editor dice «envolver en transacción sigue sin
  estar» (`SqlEditorScreen.tsx:49`), pero nada impide escribirlo, y el resultado no
  es «no soportado» sino «al revés de lo pedido». Contra MySQL y SQLite el estado de
  transacción sí sobrevive en la conexión que vuelve al pool, así que funciona
  «cuando el pool devuelve la misma» —`database/sql` toma la última liberada— y deja
  de funcionar si otra llamada (el árbol, un conteo) se la lleva en el medio:
  SOSPECHADO para esos dos, sin confirmar empíricamente.
- Fix recomendado: mínimo, rechazar en `Queries.Run` —con `query.Command`— las
  sentencias `BEGIN`, `START TRANSACTION`, `COMMIT`, `ROLLBACK`, `SAVEPOINT` y
  `RELEASE` con un `Failure` que diga que el editor corre en autocommit. Cuando se
  implemente el control manual, hacerlo con UNA conexión tomada para toda la corrida
  de `Run` (un `pool.Acquire` alrededor del bucle, o un `engine.Tx`), no por sentencia.
- Cómo verificar el fix: test de integración contra Postgres: `BEGIN; INSERT …;
  ROLLBACK;` por `Queries.Run` y después contar filas. Hoy falla (la fila queda); con
  el rechazo, `RunResult.Failure` lo dice y no queda nada.

### [K-04] [ALTO] [VERIFICADO] `pg_dump` corre sin el modo TLS ni los certificados de la conexión: baja a `prefer` y manda `PGPASSWORD` por ese canal

> **Estado 2026-09-12:** CORREGIDO. La base va en `--dbname=` como cadena de conexión de libpq con `sslmode`, `sslrootcert`, `sslcert` y `sslkey` de `TLSOptions()`; el comando copiable los muestra; el nombre con `-` inicial deja de ser opción. Tests: `dump/pgdump_test.go` (`TestElComandoLlevaElTLSDeLaConexion`) y la aserción del modo en `service/pgdump_test.go`.

- Ubicación: `internal/service/pgdump.go:200-210` y `:248-260`; `internal/dump/pgdump.go:45-85`.
- Evidencia:

  ```go
  // service/pgdump.go:248-260 — lo único que se traduce
  return dump.PgDumpOptions{
      Host:      sesion.conn.Host,
      Port:      sesion.conn.Port,
      User:      sesion.conn.User,
      Database:  nombreDeLaBase(sesion),
      Schemas:   r.Schemas,
      ...
  }
  // service/pgdump.go:207-210 — la contraseña, por el entorno
  clave, err := d.queries.session.keyring.Get(sesion.conn.ID)
  if err == nil && clave != "" {
      cmd.Env = append(os.Environ(), "PGPASSWORD="+clave)
  }
  ```

  `dump.Comando` (`dump/pgdump.go:45-85`) no emite `sslmode`, `sslrootcert`,
  `sslcert` ni `sslkey`, y `cmd.Env` no lleva `PGSSLMODE` ni `PGSSLROOTCERT`. La
  conexión de Kaname sí los manda en el DSN (`connection.go:467-497`).
- Impacto: una conexión configurada `verify-full` con raíz propia —o `verify-ca`, o
  `require`— se vuelca con el default de libpq, `prefer`: cifra si el servidor ofrece
  y sin verificar el certificado; contra un servidor sin TLS sigue en claro. La
  credencial de producción (`PGPASSWORD`) y el volcado entero viajan por un canal
  que la persona configuró explícitamente para verificar. Con autenticación
  `password`/`md5` la contraseña es recuperable por un intermediario; con SCRAM no,
  pero los datos sí. Además, si la conexión exige certificado de cliente, `pg_dump`
  falla sin explicar por qué. El comando que se copia (`PgDumpStatus.Command`)
  tampoco lleva el modo, así que quien lo corre a mano hereda la misma baja.
  Detalle menor en el mismo camino: la base va como argumento posicional al final
  sin `--` (`dump/pgdump.go:81-83`); una base cuyo nombre empiece con `-` la parsea
  `getopt` como opción.
- Fix recomendado: pasar la conexión como URI en vez de flags sueltos —`pg_dump
  "postgresql://user@host:port/db?sslmode=verify-full&sslrootcert=…"`— que es lo que
  libpq entiende, o fijar `PGSSLMODE`, `PGSSLROOTCERT`, `PGSSLCERT`, `PGSSLKEY` en
  `cmd.Env` a partir de `c.TLSOptions()`; y en ambos casos reflejar el modo en el
  comando copiable. Agregar `--` antes del nombre de la base.
- Cómo verificar el fix: test de `dump.Comando`/`ComandoTexto` con una conexión
  `verify-full` y raíz cargada que exija ver el modo y la ruta; y uno de
  `RunPgDump` que inspeccione `cmd.Env` (o el comando) con una conexión `require`.

### [K-05] [MEDIO] [VERIFICADO] `Apply`, `DryRun` e `Imports.Run` no se excluyen entre sí: dos llamadas concurrentes ejecutan el mismo changeset dos veces

> **Estado 2026-09-12:** CORREGIDO. `openSession.escritura` con `TryLock` en Apply, DryRun e `Imports.correr`; el segundo falla con `ErrBusy` (importar: `FailureLock`) sin tocar la base ni consumir el changeset. Test: `TestDosEscriturasALaVezNoSePisan`.

- Ubicación: `internal/service/apply.go:639-692` y `:138-169`; `internal/service/importar.go:182-310`.
- Evidencia:

  ```go
  // apply.go:656-667 — no hay lock ni comprobación de ApplyRunning
  pendientes, sentencias, err := s.preparar(ctx, sesion)
  ...
  s.iniciarApply(sentencias)
  defer s.terminarApply()
  ...
  res = s.aplicarPorTramos(ctx, sesion, pendientes, sentencias, opts.SingleTransaction)
  ```

  `iniciarApply` (`:138-143`) solo pisa `s.progreso`; `ApplyRunning` no se consulta en
  ningún camino de escritura. Los bindings de Wails corren en goroutines
  independientes.
- Impacto: un doble clic, un reintento desde la interfaz mientras el primero sigue, o
  dos ventanas, hacen que `preparar` lea el mismo `Ordered()` dos veces y las dos
  corridas ejecuten todo. Con Postgres y transacción única, la segunda se bloquea y
  falla en «ya existe» —ruido—. Con tramos (MySQL/MariaDB) o sin transacción, un
  `INSERT` de la grilla sin clave única entra dos veces, y un `DELETE` ya hecho falla
  por conteo y corta el segundo apply a mitad de camino. `olvidarAplicados` de la
  primera y la segunda se pisan. El botón se deshabilita en la pantalla
  (`PendingChanges.tsx:290`), que es una comprobación del lado de la interfaz.
- Fix recomendado: un `sync.Mutex` de escritura en `openSession` (o `TryLock` que
  devuelva `Failure{Kind: FailureLock}` con «ya hay un apply en curso») tomado por
  `Apply`, `DryRun` e `Imports.correr`. Como son operaciones largas, `TryLock` y error
  es mejor que encolar.
- Cómo verificar el fix: test que lance dos `Apply` en paralelo sobre un changeset con
  un `InsertRow` en una tabla sin restricción única y exija exactamente una fila y un
  `Failure` de tipo lock en la segunda.

### [K-06] [MEDIO] [VERIFICADO] El modo «solo lectura» es un ajuste de sesión que el propio editor SQL puede revertir

> **Estado 2026-09-12:** CORREGIDO (red del lado del cliente). Comprobado contra Postgres: `SET default_transaction_read_only = off; DELETE` borraba en una conexión de solo lectura. `Queries.Run` rechaza lo que apaga el modo en los cuatro motores (`SET … read_only/tx_read_only`, `SET … TRANSACTION … READ WRITE`, `RESET`, `PRAGMA query_only`). Test: `TestSoloLecturaNoSeApagaDesdeElEditor`. Además, de paso: `postgres.Run` clasificaba los errores de sentencia con el clasificador de CONEXIÓN («El servidor rechazó la conexión con la consulta» para un error de sintaxis); ahora usa `ClassifyStatement`.

- Ubicación: `internal/postgres/connect.go:164-170`; `internal/mysql/sesion.go:93-97`; `internal/sqlite/connect.go:49-51` y `:67-71`; `internal/service/queries.go:128-129`; `puddle/v2@v2.2.2/internal/genstack/gen_stack.go:32` (Pop es LIFO).
- Evidencia:

  ```go
  // postgres/connect.go:164-166 — parámetro de arranque, modificable con SET
  if opts.ReadOnly {
      cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
  }
  // mysql/sesion.go:96 — SET SESSION, reversible con SET SESSION … READ WRITE
  out = append(out, sentencia{"SET SESSION TRANSACTION READ ONLY", true})
  // sqlite/connect.go:68 — PRAGMA, reversible con PRAGMA query_only = 0
  ex.ExecContext(ctx, "PRAGMA query_only = 1", nil)
  ```

  El comentario de `OpenOptions.ReadOnly` (`engine/conn.go:31-34`) promete: «el
  servidor rechaza toda escritura, incluidas las que no pasen por nuestro código».
- Impacto: `SET default_transaction_read_only = off; DELETE FROM …;` en el editor
  contra una conexión marcada solo lectura: la primera sentencia cambia la GUC de la
  conexión que tomó del pool, la devuelve, y la segunda —por el orden LIFO del pool—
  vuelve a tomar esa misma y escribe. En MySQL, `SET SESSION TRANSACTION READ WRITE`;
  en SQLite, `PRAGMA query_only = 0`. No es una protección contra un atacante —quien
  escribe eso es la persona— pero es la casilla que CLAUDE.md pide como «modo solo
  lectura por conexión» y la interfaz la presenta como bloqueo del lado del servidor.
  Un `SessionSQL` sí se protege contra esto (`connect.go:176-194`): la invariante se
  cuida en un camino y no en el otro.
- Fix recomendado: para Postgres, `pgxpool.Config.AfterRelease` que compruebe el
  estado y descarte la conexión si cambió (`SHOW default_transaction_read_only`, o
  más barato: `conn.PgConn().ParameterStatus` no lo reporta, así que hace falta la
  consulta), o volver a fijar las dos GUC al soltar. Para MySQL/SQLite, repetir el
  `SET`/`PRAGMA` en `Run` antes de cada sentencia cuando la conexión es solo lectura;
  son un viaje trivial. Como red adicional, `Queries.Run` puede rechazar `SET
  default_transaction_read_only`, `SET SESSION TRANSACTION` y `PRAGMA query_only`
  cuando `Safety.ReadOnly`.
- Cómo verificar el fix: test en la suite común (`enginetest`) con `ReadOnly: true`
  que corra por `Run` el par «desactivar + escribir» y exija que la escritura falle.

### [K-07] [MEDIO] [VERIFICADO] Dos protecciones más de `Safety` sin implementación: confirmación fuera de producción y desconexión por inactividad

> **Estado 2026-09-12:** CORREGIDO, las dos partes. Confirmación: `confirmarEscritura` usa `RequiresWriteConfirmation()` en Apply, DryRun e Import; `ChangesetView`/`ImportTarget` separan `Production` de `NeedsConfirmation`. Preparar un cambio destructivo sigue pidiendo la palabra solo en producción (no es una escritura). Inactividad: `service/inactividad.go`, temporizador por sesión con reloj inyectable, que no corta con apply o ejecución en curso; el Shell sondea `Current()` cada 30 s. Tests: `safety_test.go`, `inactividad_test.go`.

- Ubicación: `internal/connection/connection.go:308-311` y `:347-357` (`RequiresWriteConfirmation`, sin llamadores fuera del paquete); `:328-331` y `:383-393` (`IdleDisconnect`, sin llamadores); `internal/service/apply.go:649-654` y `:777-783`; `internal/service/importar.go:200-209`.
- Evidencia:

  ```go
  // connection.go:308-311 — «Cero: hay que tipear el nombre»
  // AllowWriteWithoutConfirmation saltea la confirmación por nombre de base.
  // Cero: hay que tipear el nombre. En producción se ignora
  AllowWriteWithoutConfirmation bool `toml:"allow_write_without_confirmation" ...`
  ```

  ```go
  // apply.go:649-650 — solo se pregunta por producción
  if sesion.conn.Environment.NeedsWriteConfirmation() &&
      strings.TrimSpace(opts.Confirm) != nombreDeLaBase(sesion) {
  ```

  `Warnings()` (`validate.go:368-373`) avisa «Las escrituras contra staging no van a
  pedir confirmación» cuando la casilla está puesta, dando por hecho que sin ella sí
  la piden. `IdleDisconnectMinutes` tiene default 15 y validación, y ninguna
  goroutine ni temporizador lo usa.
- Impacto: contra staging o dev, con la casilla apagada (el default, que el modelo
  documenta como «hay que tipear el nombre»), `Apply`, `DryRun` e importar no piden
  nada. Y una conexión a producción abierta a las 10 sigue abierta a las 18 con el
  pool y el túnel vivos, aunque la pestaña Safety diga «15 minutos». Son dos carteles
  más; el de inactividad además mantiene sesiones en el bastión y en el servidor que
  el administrador ve como activas.
- Fix recomendado: reemplazar `Environment.NeedsWriteConfirmation()` por
  `conn.RequiresWriteConfirmation()` en los tres lugares (`Apply`, `DryRun`,
  `Imports.correr`) y en `StageMany`; exponer el resultado en `ChangesetView` e
  `ImportTarget` para que la pantalla pida el nombre también fuera de producción.
  Para la inactividad: un `time.AfterFunc` en `openSession` que `Disconnect` reinicie
  cada binding que use la sesión (`abierta()` es el lugar natural) o, si no se va a
  implementar pronto, sacar el campo de la pestaña y del modelo.
- Cómo verificar el fix: tests de `Apply` e `Imports.Run` con `Environment: Staging`
  y la casilla en cero que exijan `ErrNeedsConfirmation`; para la inactividad, un
  test con reloj inyectable que confirme que `Current().Connected` pasa a false.

### [K-08] [MEDIO] [SOSPECHADO] Nada ata lo que se previsualizó a lo que se ejecuta: `Apply` vuelve a renderizar, y en SQLite lo hace contra el catálogo del momento

> **Estado 2026-09-12:** CORREGIDO. `ChangesetView.Fingerprint` (SHA-256 de las sentencias en orden) y `ApplyOptions.Fingerprint`: obligatoria salvo «Aplicar sin abrir la vista previa» —que hasta acá era un cartel, porque Go no podía distinguir—, y si viene y no coincide con lo que se va a ejecutar, no se ejecuta. Al escribir el test apareció un bug nuevo, no listado: en SQLite, un `AddColumn` + una reconstrucción de la misma tabla en el mismo changeset perdía la columna, y dos reconstrucciones se deshacían entre sí, con OK. Se rechaza la combinación en Stage y en Apply (`ErrRebuildNotAlone`); el rediseño va a pendientes. Tests: `TestApplyExigeLaHuellaDeLaVistaPrevia`, `TestUnaReconstruccionDeSQLiteVaSolaEnSuTabla`.

- Ubicación: `internal/service/apply.go:699-718` (`preparar`); `internal/sqlite/rebuild.go:54-75` y `:94-105`; `internal/service/compartir.go:181-191` (el precedente con huella).
- Evidencia:

  ```go
  // apply.go:706-716 — la SQL que corre se escribe acá, no es la que se mostró
  for _, c := range pendientes {
      st, err := sesion.db.RenderDDL(ctx, c)
      ...
      sentencias = append(sentencias, st)
  }
  ```

  `Apply` no recibe ningún identificador de la vista previa; `Changeset()` puede no
  haberse llamado nunca. En SQLite `renderDDL` lee `sqlite_schema` y
  `pragma_table_xinfo` en cada llamada (`rebuild.go:54,94`), así que el guion de
  apply se construye desde la tabla como está AHORA, no como estaba al mostrarla.
- Impacto: el requisito «nunca se aplica sin mostrar el SQL exacto» se cumple por la
  disciplina de la interfaz, no por el contrato. Si otro proceso agrega una columna a
  la tabla entre la vista previa y el clic, el rebuild que corre tiene otra
  definición que el que se leyó (es correcto para la base, pero no es «el SQL
  exacto» que se vio). Y cualquier camino nuevo del frontend que llame a `Apply`
  directo —o `AllowApplyWithoutPreview`, que el backend no puede distinguir— salta la
  vista previa sin que Go lo note. El proyecto ya resolvió esta misma clase de
  problema en `ImportConnections` con una huella del contenido mostrado.
- Fix recomendado: que `ChangesetView` lleve `Fingerprint` (SHA-256 de `Script`, que
  ya es la concatenación de las sentencias en orden) y que `ApplyOptions` exija
  `Fingerprint`; en `Apply`, renderizar, recalcular y rechazar si difiere («la SQL
  cambió desde la vista previa: volvé a abrirla»). `DryRun` puede usar el mismo.
- Cómo verificar el fix: test que obtenga el changeset, modifique la tabla por fuera
  (o agregue un cambio con `Stage`) y llame a `Apply` con la huella vieja: tiene que
  rechazar sin ejecutar nada.

### [K-09] [MEDIO] [VERIFICADO] Salida a internet: «Buscar actualizaciones» consulta `api.github.com`

> **Estado 2026-09-12:** DECIDIDO: se deja. La ayuda de Ajustes dice ahora que es la única salida a internet, que va a `api.github.com` solo al apretar, y qué ve GitHub (la IP).

- Ubicación: `internal/update/update.go:38` y `:120-186`; `internal/service/ajustes.go:112-119`; `frontend/src/screens/Settings.tsx:423`; `README.md:72-73`.
- Evidencia:

  ```go
  // update.go:38
  const endpoint = "https://api.github.com/repos/LucianoR23/kanamedb/releases/latest"
  ```

  ```go
  // ajustes.go:112-113 — único llamador de Check
  func (s *Settings) CheckForUpdates(ctx context.Context) UpdateCheck {
      res := s.updates.Check(ctx, s.version)
  ```

- Impacto: es la única salida a red del código propio y contradice el requisito «100%
  offline» del modelo de amenazas tal como está formulado. Verificado que no hay
  temporizador, no corre al arrancar ni al conectar, y que el único disparador es el
  botón de Ajustes (`Settings.tsx:423`). La petición no lleva versión ni identificador
  (`User-Agent: Kaname`), acota el cuerpo a 256 KiB, corta a los 10 s y rechaza
  redirecciones a otro host; la URL de la respuesta solo se acepta si es `https://`
  antes de pasarla a `Browser.OpenURL`. Lo que GitHub sí recibe es la IP y el hecho de
  que alguien usa Kaname. Documentado en el README.
- Fix recomendado: si el requisito es literal, compilar el paquete detrás de un build
  tag (`-tags updates`) y esconder el botón cuando no está; si el requisito admite el
  botón, dejarlo como está y mencionar en la pantalla, al lado del botón, que la
  consulta sale a `api.github.com` y qué envía.
- Cómo verificar el fix: `sockets_test.go` ya recorre imports; extenderlo para que
  `net/http` solo pueda importarse desde `internal/update`, y un test que compile
  sin el tag y confirme que `Settings` no expone `CheckForUpdates`.

### [K-10] [MEDIO] [VERIFICADO] La vista previa de «Importar conexiones» no muestra las protecciones ni los avisos de TLS que trae el archivo

> **Estado 2026-09-12:** CORREGIDO. `ImportCandidate` lleva `SSLMode` efectivo, `Safety` y `Warnings()`; la vista previa los pinta como badges y lista de avisos por entrada. Test: `TestLaVistaPreviaDeImportarMuestraLasProteccionesQueTraeElArchivo`.

- Ubicación: `internal/service/compartir.go:97-122` y `:148-171`; `internal/store/compartir.go:70-97`.
- Evidencia:

  ```go
  // compartir.go:98-122 — lo que se muestra antes de importar
  type ImportCandidate struct {
      Index int; Name string; Engine connection.Engine; Describe string
      Environment connection.Environment; Production bool; Folder string; SSH bool
      Existing string; SessionSQL string; Problems []connection.FieldError
  }
  ```

  No hay `Safety`, `SSLMode`/`TLS` ni `Warnings`. `Decode` acepta cualquier valor del
  archivo para esos campos (`store/compartir.go:88-90`), y `ImportConnections`
  (`:181-223`) los guarda tal cual, con ID nuevo.
- Impacto: un archivo de otra persona puede traer `environment = "local"` para un host
  de producción, `ssl_mode = "disable"`, `read_only = false`,
  `allow_apply_without_preview = true`, `statement_timeout_seconds = -1`. La vista
  previa muestra el entorno y el `Describe` —bien— pero ninguno de los otros, así que
  la libreta queda con una conexión menos protegida de lo que quien importó cree,
  hasta que abre el gestor y ve los `Warnings`. `SessionSQL` sí se muestra, que es el
  criterio correcto; falta aplicarlo al resto.
- Fix recomendado: agregar a `ImportCandidate` `SSLMode`, `Safety` y `Warnings`
  (`c.Warnings()` ya existe y ya cubre los casos), y que la pantalla los pinte con el
  mismo tratamiento que el gestor. Opción más conservadora: importar siempre con
  `Safety{}` (el cero es el lado seguro) y `Environment` sin degradar por debajo de
  `staging` si el host coincide con una conexión propia marcada producción.
- Cómo verificar el fix: test de `PreviewImport` con un archivo que traiga
  `ssl_mode = "disable"` y `allow_apply_without_preview = true`, exigiendo que el
  candidato lleve los avisos.

### [K-11] [BAJO] [SOSPECHADO] `Redact` corta el DSN de MySQL en el primer `@`: una contraseña con `@` queda parcialmente visible

> **Estado 2026-09-12:** CORREGIDO. Confirmado con el caso `kaname:p@ss@w0rd@tcp(…)`: el DSN quedaba entero. La expresión toma `\S*` hasta el `@` que precede a `tcp(`/`unix(`/`kaname-tunnel-N(`. Caso agregado a `TestRedactTapaLasContrasenasDeLosTresFormatos`.

- Ubicación: `internal/engine/failure.go:115` y `:123-126`; `internal/connection/connection.go:514-547`.
- Evidencia:

  ```go
  // failure.go:115
  var dsnMySQL = regexp.MustCompile(`([^\s:/@]+):([^@\s]*)@(tcp|unix|kaname-tunnel-\d+)\(`)
  ```

  El DSN de MySQL se arma sin escapar usuario ni contraseña (`connection.go:507-513`,
  a propósito: el driver parte por el ÚLTIMO `@`). Con contraseña `p@ss`, el texto es
  `kaname:p@ss@tcp(` y la expresión exige `@` seguido de `tcp|unix|kaname-tunnel-`: el
  primer `@` no lo cumple, pero `[^@\s]*` no puede pasar por encima de él, así que no
  hay coincidencia y el DSN entero queda sin enmascarar. El test
  (`failure_test.go:24-27`) no cubre `@` en la contraseña.
- Impacto: solo si algún error del driver o del servidor cita el DSN. Leyendo
  `go-sql-driver` no se encontró uno que lo haga, por eso SOSPECHADO. El Postgres no
  tiene el problema: `url.UserPassword` escapa el `@` como `%40`.
- Fix recomendado: enmascarar con un patrón que tome todo hasta el último `@` antes de
  `tcp(`/`unix(`/`kaname-tunnel-N(`: `([^\s:/@]+):(.*)@(tcp|unix|kaname-tunnel-\d+)\(`
  con `.*` codicioso, y agregar el caso al test.
- Cómo verificar el fix: caso `kaname:p@ss@tcp(127.0.0.1:3306)/db` en
  `TestRedactTapaLasContrasenasDeLosTresFormatos`.

### [K-12] [BAJO] [VERIFICADO] `LlevaSecreto` deja pasar formas reales de contraseña escrita al historial y a las consultas guardadas

> **Estado 2026-09-12:** CORREGIDO. Las siete formas listadas pasaban (cinco confirmadas por el test antes del fix). `formasConSecreto` ampliada; casos en `history_test.go`, más uno negativo (`WHERE password_hash = …` sigue guardándose).

- Ubicación: `internal/history/history.go:337-344` y `:357-364`.
- Evidencia:

  ```go
  var formasConSecreto = []*regexp.Regexp{
      regexp.MustCompile(`(?i)\bpassword\s+'`),
      regexp.MustCompile(`(?i)\bpassword\s*=\s*'`),
      regexp.MustCompile(`(?i)\bidentified\s+(by|with)\b`),
      regexp.MustCompile(`(?i)\bencrypted\s+password\b`),
      regexp.MustCompile(`(?i)\bcreate\s+subscription\b`),
      regexp.MustCompile(`(?i)\bconnection\s+'`),
  }
  ```

  No coinciden: `ALTER ROLE x PASSWORD E'…'` (literal con escape), `PASSWORD $$…$$`
  (dollar quoting), `SET PASSWORD FOR 'u'@'h' = '…'` (MySQL), `PASSWORD "…"` con
  `ANSI_QUOTES`, y `password=…` sin comillas adentro de una cadena de conexión
  (`dblink('host=… password=x …')`, `CREATE SERVER … OPTIONS (…)` con `password=`).
- Impacto: la sentencia con la contraseña va a `historial.json` (0600, esta máquina) y,
  si se guarda con nombre, a `consultas.json`, que se sincroniza. Requisito duro 1.
- Fix recomendado: sumar `\bpassword\s+(e|u&)?['"$]`, `\bset\s+password\b`,
  `\bpassword\s*=`, y `\bcreate\s+(server|user\s+mapping)\b`. Como el error tolerado
  es «no guardar de más», ampliar es barato.
- Cómo verificar el fix: casos en `history_test.go` con cada forma listada.

### [K-13] [BAJO] [SOSPECHADO] La verificación de host key no fija `HostKeyAlgorithms` según la clave guardada: un servidor con varias claves puede dar falsos «la clave cambió»

> **Estado 2026-09-12:** CORREGIDO. `algoritmosPreferidos` fija `HostKeyAlgorithms` con el tipo guardado (RSA: las tres firmas) en `Inspect` y en `Dial`. Test unitario `TestLaNegociacionPideElTipoDeClaveQueYaSeConoce`; el caso con dos tipos en el sshd de docker queda para la próxima corrida de la batería de túnel.

- Ubicación: `internal/tunnel/dial.go:228-236` y `:253-273`; `internal/tunnel/hostkey.go:110-148`.
- Evidencia:

  ```go
  // dial.go:228-236 — sin HostKeyAlgorithms
  conf := &ssh.ClientConfig{
      User:            cfg.User,
      Auth:            metodos,
      HostKeyCallback: verificador(cfg, kh, opts.AcceptOnce),
      Timeout:         DefaultConnectTimeout,
      ClientVersion:   "SSH-2.0-Kaname",
  }
  ```

  `Lookup` guarda UNA clave por dirección y compara solo la huella. La negociación
  elige el tipo de clave según la lista por defecto del cliente y lo que ofrece el
  servidor; si el administrador agrega un tipo (por ejemplo `ecdsa` a un host que
  solo tenía `ed25519`) o cambia el orden, el servidor presenta otra clave legítima y
  el veredicto es `changed`.
- Impacto: cada falso positivo entrena a apretar «Reemplazar la clave y conectar», que
  es exactamente el botón que un intermediario necesita que se apriete sin mirar. No
  debilita la verificación en sí (la huella se compara siempre).
- Fix recomendado: cuando `Lookup` devuelve una clave, poner
  `conf.HostKeyAlgorithms` con el algoritmo guardado primero (es lo que hace
  `knownhosts.HostKeyAlgorithms` de `x/crypto`), tanto en `Inspect` como en `Dial`.
- Cómo verificar el fix: test con el `sshd` de `docker/sshd` configurado con dos
  tipos de clave: confiar una, cambiar el orden en `HostKeyAlgorithms` del servidor, y
  exigir `VerdictTrusted`.

### [K-14] [BAJO] [VERIFICADO] Setters exportados que Wails expone como bindings: desde el webview se puede apagar el historial y los defaults de Safety

> **Estado 2026-09-12:** CORREGIDO. `UsarHistorial` y `UsarPreferencias` son funciones del paquete; `Running` y `TunnelDown` no se exportan. Bindings regenerados sin ellos. `TestElCableadoDelServicioNoEsUnBinding` lo fija por reflexión.

- Ubicación: `internal/service/queries.go:41`; `internal/service/connections.go:63`; `frontend/bindings/…/service/queries.ts:145` y `connections.ts:230` (generados).
- Evidencia:

  ```go
  func (q *Queries) UsarHistorial(h *history.Store) { q.historial = h }
  func (s *Connections) UsarPreferencias(p *config.Store) { s.prefs = p }
  ```

  ```ts
  export function UsarHistorial(h: history$0.Store | null): $CancellablePromise<void>
  export function UsarPreferencias(p: config$0.Store | null): $CancellablePromise<void>
  ```

- Impacto: `UsarHistorial(null)` deja de anotar consultas; `UsarPreferencias(null)`
  hace que las conexiones nuevas nazcan con `Safety{}` en vez de lo configurado. Es
  poco —quien controla el webview ya tiene `RevealPassword`— pero es superficie que
  existe solo por la forma en que se cablea el servicio. `Running()` y `TunnelDown()`
  también están expuestos, sin consecuencias.
- Fix recomendado: pasar `historial` y `prefs` por los constructores (`NewQueries`,
  `NewConnections`) o hacer los setters no exportados; `main_test.go` ya compara la
  lista de servicios y puede extenderse a comparar los métodos bindeados contra una
  lista blanca.
- Cómo verificar el fix: regenerar bindings y confirmar que `queries.ts` y
  `connections.ts` ya no exportan esas funciones.

### [K-15] [BAJO] [VERIFICADO] Sin CSP en el webview, con bindings que leen y escriben rutas arbitrarias

> **Estado 2026-09-12:** CORREGIDO. La CSP la inyecta un plugin de Vite solo en el build (`default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; base-uri 'none'; …`): en desarrollo Vite mete un script inline y usa websockets. Probado con el build de producción: la app arranca, el gestor lista las conexiones (binding `Connections.List`) con la política puesta. `TestElBuildLlevaUnaContentSecurityPolicy` fija el plugin. Las restricciones de ruta por extensión del lado de Go quedan anotadas.

- Ubicación: `frontend/index.html:1-13`; `internal/service/export.go:90-97`, `:213-217`, `:340-351`; `internal/service/migracion.go:70-86`; `internal/service/compartir.go:39-78`, `:125-173`; `internal/service/certificado.go:24-37`; `internal/service/importar.go:28-30`; `internal/service/pgdump.go:173-245`.
- Evidencia: `index.html` no declara `Content-Security-Policy`. Los bindings `Save`,
  `SaveTable`, `SaveTables`, `SaveMigration`, `ExportConnections`, `SaveCertificate`
  y `RunPgDump` escriben en la ruta que manda el frontend con contenido que también
  manda (o controla) el frontend; `Imports.Inspect`, `PreviewImport` y `DraftSQLite`
  leen cualquier ruta y devuelven su contenido o su nombre. Ninguno restringe el
  directorio: la elección de la ruta ocurre en el diálogo del sistema, del lado JS.
- Impacto: hoy no hay sumidero de HTML (búsqueda de `dangerouslySetInnerHTML`,
  `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `eval`, `new Function`,
  `document.write`, `srcdoc`, `javascript:` en `frontend/src`: cero resultados;
  `logs_test.go:136` lo hace estructural) ni recursos remotos (la fuente Inter es
  local; los dos `https://` son de `Browser.OpenURL`). Por eso es BAJO. Pero el día
  que aparezca una inyección de script desde un valor de celda, estos bindings son
  «escribí este contenido en Inicio\Programas» y «leé este archivo». Una CSP
  `default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'` es la
  segunda capa que hoy no existe.
- Fix recomendado: agregar la `<meta http-equiv="Content-Security-Policy">` en
  `index.html` y comprobar que el runtime de Wails v3 sigue funcionando (usa
  `fetch` contra el mismo origen y `postMessage`; en WebView2 no debería necesitar
  `unsafe-inline`). Complemento en Go: que las escrituras exijan extensión acorde al
  formato (`.csv`, `.sql`, `.toml`, `.pem`) y rechacen rutas dentro del propio
  directorio de la app salvo las que el store maneja.
- Cómo verificar el fix: `TestElFrontendNoInyectaHTMLNiEscribeEnLaConsola` puede
  exigir además la presencia de la meta CSP en `frontend/index.html`; probar a mano
  que el arranque, un binding y un diálogo de archivo funcionan con la CSP puesta.

### [K-16] [BAJO] [VERIFICADO] En MySQL, el «timeout de sentencia» solo corta `SELECT`

> **Estado 2026-09-12:** CORREGIDO en lo que se promete: `Caps.StatementTimeoutOnlyReads` (MySQL); el editor dice «corta un SELECT; una escritura no», la ayuda de Safety lo explica y el aviso del changeset deja de decir lo contrario. No se agregó un vigilante del lado del cliente.

- Ubicación: `internal/mysql/sesion.go:98-107`; `internal/service/session.go:94-99` y `:423-424`.
- Evidencia:

  ```go
  // sesion.go:103-106 — las dos variantes, opcionales
  sentencia{fmt.Sprintf("SET SESSION max_execution_time = %d", ms), false},
  sentencia{fmt.Sprintf("SET SESSION max_statement_time = %.3f", timeout.Seconds()), false},
  ```

  `max_execution_time` de MySQL aplica únicamente a sentencias `SELECT` (documentado
  por MySQL en la referencia de variables del sistema); `max_statement_time` de
  MariaDB aplica a todas. La interfaz muestra el valor como «el servidor corta a los
  N s» sin distinguir (`SessionView.StatementTimeoutSeconds`).
- Impacto: contra MySQL, un `UPDATE` sin `WHERE` escrito en el editor, o un `ALTER`
  que reescribe una tabla grande, no se corta nunca aunque Safety diga 30 s. El aviso
  de `avisos()` (`apply.go:529-538`) dice lo contrario para esos casos.
- Fix recomendado: en `SessionView` y en `avisos`, decir que en MySQL el límite vale
  solo para lecturas; o, si se quiere cumplirlo, un vigilante del lado del cliente
  con `context.WithTimeout` por sentencia en `Run`/`Apply` para MySQL (con
  `CheckConnLiveness` ya puesto, cancelar el contexto cierra la conexión y MySQL
  aborta la sentencia).
- Cómo verificar el fix: test de integración contra MySQL con timeout de 1 s y un
  `UPDATE … WHERE SLEEP(3)`, exigiendo el corte.

### [K-17] [BAJO] [VERIFICADO] Orden de secretos y libreta al guardar y al borrar: puede quedar un secreto huérfano o una conexión sin secreto

> **Estado 2026-09-12:** CORREGIDO. `SaveWithSSH` recuerda los dos secretos antes de tocarlos y los repone si la libreta falla (nueva: se borran; edición: vuelve el anterior). `Delete` borra los dos secretos aunque el primero falle y dice cuál mitad quedó. Tests `TestSiLaLibretaFalla…` (dos).

- Ubicación: `internal/service/connections.go:258-277` y `:302-312`.
- Evidencia:

  ```go
  // connections.go:260-276 — el keychain primero, el archivo después
  if err := s.aplicarSecreto(c.ID, action, password); err != nil { … }
  if err := s.aplicarSecreto(SSHSecretID(c.ID), sshAction, sshSecret); err != nil { … }
  err := s.store.Update(c)
  if errors.Is(err, store.ErrNotFound) { err = s.store.Add(c) }
  ```

  ```go
  // connections.go:303-311 — el archivo primero, el keychain después
  if err := s.store.Delete(id); err != nil { return err }
  if err := s.keyring.Delete(id); err != nil { return err }
  return s.keyring.Delete(SSHSecretID(id))
  ```

- Impacto: en `SaveWithSSH` de una conexión NUEVA, si `store.Add` falla (disco lleno,
  archivo corrupto) la contraseña ya quedó en el Credential Manager bajo un ID que
  ninguna libreta conoce: un secreto que solo se ve auditando el keychain. En
  `Delete`, si el keychain falla después de borrar del archivo, pasa lo mismo al
  revés. No es una fuga —el keychain es el lugar correcto— pero el comentario de
  `Delete` («dejarla huérfana sería peor») describe justo el resultado de ese orden.
- Fix recomendado: en `SaveWithSSH`, si la escritura de la libreta falla y la conexión
  era nueva, deshacer los `Set` con `Delete`; en `Delete`, borrar del keychain
  primero (borrar un secreto que ya no está es no-op) y del archivo después, o al
  menos informar cuál mitad quedó.
- Cómo verificar el fix: con `keyring_fake_test.go`, un store apuntando a un
  directorio de solo lectura, y exigir que después de un `Save` fallido el fake no
  tenga la clave.

### [K-18] [BAJO] [VERIFICADO] `Compare` y `Test` abren conexiones con la contraseña guardada hacia el host que diga el pedido, y corren `SessionSQL`

> **Estado 2026-09-12:** ANOTADO, sin cambio: coherente con el modelo de confianza del webview, como dice el propio hallazgo. Si se endurece el borde, `Test` debería resolver host/puerto/base por ID y `Compare` omitir `SessionSQL`; queda en `pendientes-auditoria-2026-09-12.md`.

- Ubicación: `internal/service/connections.go:400-448`; `internal/service/comparar.go:87-160`; `internal/service/session.go:172-247`.
- Evidencia: `Test(ctx, c, action, password)` toma `c` del frontend y, con
  `action != PasswordSet`, usa `keyring.Get(c.ID)` para conectar a `c.Host` (`:403-407`,
  `:421`). `Compare` abre dos conexiones cualesquiera por ID y ejecuta su
  `Advanced.SessionSQL` en cada una (`abrirConexion` → `connectOptions`).
- Impacto: es coherente con el modelo de confianza del proyecto —el webview ya puede
  pedir `RevealPassword`— así que no agrega capacidad a un atacante que lo controle.
  Se anota para que la enumeración de bindings quede completa: `Test` es «mandá la
  contraseña de la conexión X al host que yo diga», y `Compare` es «corré la SQL de
  sesión de X e Y sin vista previa». El aviso de `Warnings()` sobre `SessionSQL`
  (`validate.go:385-396`) y su muestra al importar (K-10) son las mitigaciones.
- Fix recomendado: opcional. Si alguna vez se endurece el borde con el webview, `Test`
  debería resolver host/puerto/base desde la libreta por ID y aceptar del frontend
  solo lo que se está editando, y `Compare` debería omitir `SessionSQL` (lee catálogos;
  no lo necesita).
- Cómo verificar el fix: test de `Test` con un `c.Host` distinto del guardado que exija
  que no se use la contraseña del keychain.

## Segunda pasada — correctitud (2026-09-12)

Misma metodología y mismas reglas que la primera: solo lectura, nada ejecutado, cada
afirmación sobre un driver citada contra la fuente cacheada (`go-sql-driver/mysql
v1.10.1`, `modernc.org/sqlite v1.58.0`, `pgx v5.11.0`, `database/sql` del toolchain).
El foco acá no es el modelo de amenazas sino los errores que un gestor de bases de datos
no puede tener: datos que se muestran o se exportan distintos de como están, contadores
que mienten, un diff que dice «alineado» cuando no lo está, un volcado que no se puede
volver a correr. Se leyó línea por línea lo que en la primera pasada quedó por encima:
`internal/drift`, `internal/export`, `internal/dump`, `internal/csvimport`,
`internal/query/split.go`, los `scan.go` y `query.go` de los tres motores, y
`service/{grid,export,volcado,comparar,migracion}.go`.

| Severidad | Cantidad |
|---|---|
| CRÍTICO | 1 |
| ALTO | 8 |
| MEDIO | 11 |
| BAJO | 11 |

Los tres que más pesan: **C-02** (un volcado de SQLite reemplaza cada BLOB por el texto
`[N bytes]` y dice que no dejó nada afuera), **C-03** (en MySQL y MariaDB todo
`INSERT`/`UPDATE`/`DELETE` del editor muestra «0 filas» y, si falla, se manda dos veces)
y el grupo de `drift` (**C-06** a **C-09**: la comparación no converge en SQLite y MySQL,
no compara nada entre bases con distinto nombre, y crea tablas sin identity ni clave
compuesta correcta).

### [C-01] [ALTO] [VERIFICADO] Cierre de K-03 para MySQL y SQLite: la transacción del editor queda pegada a una conexión del pool, y el siguiente `Apply` o la app entera pagan por ella

> **Estado 2026-09-12:** CORREGIDO por el mismo rechazo de K-03: ya no puede entrar un BEGIN ni un `SET autocommit = 0` al pool.

- **Ubicación**: `$GOROOT/src/database/sql/sql.go` (`conn()`: `conn := db.freeConn[last]`);
  `go-sql-driver/mysql@v1.10.1/connection.go:777-809` (`ResetSession`);
  `modernc.org/sqlite@v1.58.0` (sin `ResetSession`); `internal/sqlite/query.go:30-34`;
  `internal/mysql/query.go:32`; `internal/sqlite/conn.go:149,210`;
  `internal/mysql/conn.go:92-93`; `internal/mysql/connect.go:99`.
- **Evidencia**: `database/sql` entrega la última conexión devuelta (LIFO). `ResetSession`
  del driver de MySQL solo comprueba que el socket viva —no manda `ROLLBACK` ni resetea
  nada— y el driver de SQLite pineado no implementa la interfaz. Cada sentencia del
  editor pide su conexión y la devuelve (`db.Conn` + `defer cn.Close()` en SQLite,
  `db.QueryContext` en MySQL), así que `BEGIN; UPDATE …; ROLLBACK;` **funciona por
  accidente**: mientras nada más toque el pool entre sentencias, las tres caen en la
  misma conexión. Lo que no funciona es lo que queda después:
  - Un `BEGIN` sin cerrar —o una sentencia que falla en el medio, porque `Run` corta
    ahí— deja la conexión en transacción dentro del pool. En SQLite esa conexión tiene
    el lock `RESERVED` del archivo: cualquier escritura desde otra conexión (grilla,
    apply, import) recibe `database is locked` hasta que la app se cierre.
  - `sqlite.Conn.Begin` toma una conexión con `db.Conn` y hace `cn.BeginTx`
    (`conn.go:149,210`); si le toca la que quedó en transacción, SQLite responde
    «cannot start a transaction within a transaction» y el `Apply` falla sin explicar.
  - `mysql.Conn.Begin` hace `db.BeginTx` (`conn.go:92-93`): `START TRANSACTION` sobre
    una conexión con transacción abierta hace **commit implícito** de lo que el editor
    dejó pendiente. El `UPDATE` que el usuario creía sin confirmar se confirma cuando
    aplica un cambio de esquema que no tiene nada que ver.
  - `SetConnMaxLifetime(30m)` en MySQL cierra la conexión y con ella hace rollback
    silencioso de lo pendiente.
- **Impacto**: la parte SOSPECHADA de K-03 queda verificada, con matiz: no se pierde el
  `ROLLBACK`, se pierde el aislamiento entre lo que el editor dejó abierto y el resto de
  la aplicación.
- **Fix recomendado**: el mismo de K-03 —una sola conexión dedicada por `Run`, o rechazar
  `BEGIN`/`START TRANSACTION`/`COMMIT`/`ROLLBACK`/`SAVEPOINT` en el editor con un
  mensaje claro— y, en cualquier caso, que `Run` no devuelva al pool una conexión en
  transacción: con una conexión dedicada por `Run`, mandar `ROLLBACK` antes de soltarla
  (en SQLite se puede preguntar primero con `sqlite3_get_autocommit` a través de
  `driver.Conn`).
- **Cómo verificar el fix**: test contra SQLite: `Run("BEGIN")`, después `Apply` de un
  `AddColumn`; hoy falla con «cannot start a transaction». Test contra MySQL:
  `Run("BEGIN"); Run("UPDATE t SET n=1")`, después `Apply` de cualquier cambio, y leer `n`
  desde otra conexión: hoy vale 1.

### [C-02] [CRÍTICO] [VERIFICADO] SQLite: la exportación y el volcado escriben `[N bytes]` en lugar del contenido de cada BLOB

> **Estado 2026-09-12:** CORREGIDO. El recorrido de exportación entrega los BLOB en hexadecimal (mayúsculas, sin prefijo) y `engine.Quoting.Binary` los escribe `X'…'` en SQLite; Postgres cita el `\x…` que ya entrega. La grilla sigue mostrando `[N bytes]`. Como SQLite tipa el valor y no la columna, el recorrido expone la clase de cada celda (`engine.CellClasses`) y el escritor SQL decide por celda: un texto o un entero en una columna BLOB no van como `X'…'`. Tests: `sqlite/fidelidad_test.go`, `TestUnBlobDeSQLiteSobreviveALaExportacionSQL` (ida y vuelta con blob, texto y entero en la misma columna). MySQL (C-13) queda como Literal hasta que su recorrido entregue hex.

- **Ubicación**: `internal/sqlite/query.go:148-152`; `internal/sqlite/scan.go:94-101`;
  `internal/service/volcado.go:381-395`; `internal/export/sql.go:110-118`;
  `internal/dump/ddl.go:57-88` (cobertura).
- **Evidencia**:
  ```go
  // sqlite/query.go:148-152
  case []byte:
      // Es un BLOB. Se muestra su tamaño y no el contenido: volcar bytes
      // crudos en una celda llena la pantalla de basura ...
      return fmt.Sprintf("[%d bytes]", len(x)), true
  ```
  `aTexto` se escribió para la grilla, pero `scan.go:96` lo reutiliza para el flujo de
  exportación, y `volcado.go:381` manda ese flujo al escritor SQL, que cita el texto tal
  cual (`sql.go:117`). Un `BLOB` de 12 bytes sale a CSV, JSON y Markdown como
  `[12 bytes]` y al volcado como `'[12 bytes]'`, que al volver a correr inserta un
  `TEXT`. La cobertura del volcado (`dump/ddl.go`) solo sabe de objetos del catálogo y
  de columnas generadas o autonuméricas: la columna BLOB no se nombra y el archivo se
  presenta como completo. Ningún test pasa un BLOB por `Scan`.
- **Impacto**: pérdida de datos silenciosa en la única copia que la app sabe hacer de una
  base SQLite. Es la misma clase que K-01: no falla, no avisa, y el resultado se
  parece a uno correcto.
- **Fix recomendado**: en `Scan` no pasar por `aTexto`; escribir el BLOB como `X'…'`
  (hex) en SQL, base64 o hex en JSON/CSV (decidirlo y documentarlo), y que la grilla
  siga mostrando `[N bytes]` por su propio camino. Si se prefiere no exportar binarios,
  que la cobertura nombre la columna y el encabezado deje de decir que no faltó nada.
- **Cómo verificar el fix**: test en `enginetest`/`sqlite` que inserte `X'00FF10'`,
  exporte a SQL, vuelva a correr el archivo en una base vacía y compare `typeof(col)` y
  `hex(col)`: hoy da `text` y `5B3320627974...`.

### [C-03] [ALTO] [VERIFICADO] MySQL y MariaDB: todo `INSERT`/`UPDATE`/`DELETE` del editor muestra «0 filas devueltas», y una sentencia que falla se manda dos veces

> **Estado 2026-09-12:** CORREGIDO. Confirmado contra MySQL 9.7 (`UPDATE` de 2 filas → `ReturnsRows=true`, `AffectedRows=0`). `run` usa una conexión dedicada, no reintenta nunca, y con `Columns()` vacío pregunta `SELECT ROW_COUNT()` solo tras DML. Test `TestEnMySQLUnDMLInformaLasFilasQueAfecto` (MySQL y MariaDB).

- **Ubicación**: `internal/mysql/query.go:32-50,73`;
  `go-sql-driver/mysql@v1.10.1/connection.go:525-534`;
  `frontend/src/screens/SqlEditorScreen.tsx:510-523`.
- **Evidencia**:
  ```go
  // mysql/query.go:32-37
  rows, err := db.QueryContext(ctx, sql_)
  if err != nil {
      // Una sentencia que no devuelve filas —INSERT, ALTER— llega acá igual
      // con database/sql, así que se reintenta como Exec ...
      res, err2 := db.ExecContext(ctx, sql_)
  ```
  La premisa es falsa para el driver pineado: cuando el servidor responde con un paquete
  OK (`resLen == 0`), `mysqlConn.query` devuelve `rows, nil` (`connection.go:525-534`).
  Un `DELETE` no entra al `if`, sigue a `leerFilas`, y sale como
  `Result{ReturnsRows: true, AffectedRows: 0}` (`query.go:73` pone `ReturnsRows: true`
  sin mirar cuántas columnas hay). El editor muestra el conteo de filas devueltas (0) y
  el historial guarda 0. El `ExecContext` de la rama de error, en cambio, solo se
  alcanza cuando la primera ejecución falló de verdad, y reenvía la misma SQL sin
  condición.
- **Impacto**: (1) un `DELETE FROM pedidos WHERE …` que borró 10 000 filas y uno que no
  encontró ninguna se ven idénticos; en MySQL el conteo es la única confirmación que el
  usuario tiene de lo que hizo. (2) Si la conexión se corta después de que el servidor
  aplicó la escritura y antes de leer el OK —`cancel()` del driver cierra el socket,
  `connection.go:575-578`—, la sentencia se ejecuta dos veces: `UPDATE t SET n = n + 1`
  suma dos, un `CALL` con efectos no transaccionales los repite. Este segundo punto es
  VERIFICADO en el código y SOSPECHADO en cuanto a frecuencia (hace falta el corte en
  esa ventana).
- **Fix recomendado**: usar `rows.Columns()` como en SQLite (`sqlite/query.go:57-63`):
  cero columnas es DML/DDL, y el conteo sale de `ExecContext` **en lugar de**
  `QueryContext`, no después. Nunca reintentar una sentencia que falló. Con el driver
  pineado, la forma más simple es decidir por la primera palabra si va por `Exec` o
  por `Query`, o correr siempre `Exec` cuando `Columns()` está vacío en una conexión
  dedicada y leer `ROW_COUNT()`.
- **Cómo verificar el fix**: `editor_sql_test.go` contra MySQL con
  `UPDATE t SET n = 1` sobre tres filas: hoy `ReturnsRows == true` y
  `AffectedRows == 0`; tiene que dar `false` y `3`. Y un test con un `*sql.DB` de
  prueba (o un proxy) que cuente ejecuciones: hoy una sentencia inválida llega dos veces.

### [C-04] [ALTO] [VERIFICADO] Importación CSV con «saltear las que chocan» en MySQL: `INSERT IGNORE` convierte valores inválidos en ceros, recortes y defaults, y los cuenta como insertados

> **Estado 2026-09-12:** CORREGIDO. MySQL sigue con `INSERT IGNORE` —`ON DUPLICATE KEY UPDATE` contaba las salteadas porque la conexión pone `clientFoundRows` a propósito— y la transacción revisa `SHOW WARNINGS` tras cada lote (`engine.SkipVerifier`): cualquier código distinto de 1062 falla el lote, y si se alcanza `max_error_count` también. SQLite pasa de `OR IGNORE` a `ON CONFLICT DO NOTHING`. Test: `TestSaltearLasQueChocan` con una fila `abc` en un INT, cuatro motores.

- **Ubicación**: `internal/mysql/ddl.go:430-437`; `internal/dml/dml.go:189`;
  `internal/service/importar.go:246`.
- **Evidencia**:
  ```go
  // mysql/ddl.go:431-435
  if ignorar {
      // MySQL lo pone adelante y no al final. IGNORE degrada a aviso
      // MÁS cosas que un choque de clave —un valor fuera de rango, por
      // ejemplo— así que solo se usa cuando se pidió saltear.
      return "INSERT IGNORE INTO "
  ```
  El comentario conoce la semántica, pero el resultado no la refleja: con `IGNORE`,
  MySQL degrada **todos** los errores de datos a warning —`'abc'` en un `INT` entra
  como `0`, un valor fuera de rango se recorta al límite, una cadena larga se trunca,
  `NULL` en `NOT NULL` toma el default del tipo— y la fila se inserta y suma a
  `Inserted`. Nadie corre `SHOW WARNINGS`. En SQLite, `INSERT OR IGNORE`
  descarta en silencio también las filas que violan `NOT NULL` o `CHECK`, no solo las
  de clave (ahí al menos el conteo no las incluye).
- **Impacto**: un archivo con una celda mal escrita se importa «bien» con datos
  corruptos, en la opción que el usuario eligió justamente para no perder filas buenas.
- **Fix recomendado**: en MySQL implementar «saltear» con
  `INSERT … ON DUPLICATE KEY UPDATE pk = pk` (o `IGNORE` seguido de `SHOW WARNINGS` y
  fallo si aparece cualquier código que no sea 1062). En SQLite, `ON CONFLICT DO
  NOTHING` sobre la clave en vez de `OR IGNORE`. En los dos, mostrar cuántas se
  saltearon y por qué.
- **Cómo verificar el fix**: `importar_test.go` contra MySQL con una columna `INT`, una
  fila `'x'` y `ConflictSkip`: hoy `Inserted` la cuenta y la fila vale 0; tiene que
  fallar o quedar afuera con aviso.

### [C-05] [ALTO] [VERIFICADO] Volcado de Postgres: las columnas `GENERATED ALWAYS AS IDENTITY` hacen fallar los `INSERT`, y ninguna secuencia se reposiciona después de los datos

> **Estado 2026-09-12:** CORREGIDO. `engine.Conn.DumpHints`: Postgres devuelve `OVERRIDING SYSTEM VALUE` con identity ALWAYS y un `setval(pg_get_serial_sequence(…), COALESCE(max(col),1), max(col) IS NOT NULL)` por columna que se numera sola; MySQL y SQLite no necesitan nada. Test `TestLasSecuenciasVuelvenPosicionadasYLaIdentityAlwaysSeRestaura`: restaura e inserta sin id en las tres variantes (falla con 428C9 sin el fix).

- **Ubicación**: `internal/postgres/objetos.go:200-217`; `internal/dump/ddl.go:73-86`;
  `internal/service/volcado.go:181-199,368-405`; `internal/export/sql.go:74-81`.
- **Evidencia**: la estructura escribe `tipo GENERATED ALWAYS AS IDENTITY`
  (`objetos.go:202-203`) y `serial` para los `nextval(` (`:207-217`, secuencia nueva
  que arranca en 1). `insertables` (`volcado.go:181-199`) solo excluye `Generated != ""`,
  así que la columna identity va en `Columns` y `sql.go:74-81` escribe sus valores.
  Postgres rechaza el `INSERT` con `cannot insert a non-DEFAULT value into column`
  (SQLSTATE 428C9) salvo `OVERRIDING SYSTEM VALUE`, que no se emite. Y después de la
  sección de datos no hay `setval`, `ALTER SEQUENCE … RESTART` ni `AUTO_INCREMENT=` para
  MySQL: `grep` sobre `internal/` no encuentra ninguno fuera de los tests. Los tests de
  ida y vuelta usan solo `serial` y nunca insertan después de restaurar
  (`volcado_test.go`).
- **Impacto**: con identity `ALWAYS` el archivo no se puede correr. Con `serial` corre,
  pero el primer `INSERT` sin `id` sobre la base restaurada choca con una clave que ya
  existe (`duplicate key`), y va a seguir chocando una vez por cada fila vieja. En un
  restore de producción eso aparece semanas después. MySQL (InnoDB ajusta el contador
  con inserts explícitos) y SQLite (`max(rowid)+1`) no lo sufren.
- **Fix recomendado**: en la sección de datos, `INSERT … OVERRIDING SYSTEM VALUE` cuando
  hay identity `ALWAYS`; al final de cada tabla con secuencia,
  `SELECT setval(pg_get_serial_sequence('t','id'), COALESCE(max(id),1), max(id) IS NOT NULL)`.
  Alternativa: excluir la columna del `INSERT` y nombrarla en la cobertura, pero eso
  cambia los ids y rompe las claves foráneas, así que no.
- **Cómo verificar el fix**: extender `TestElVolcadoSePuedeVolverACorrer` con una tabla
  identity `ALWAYS` y, tras restaurar, un `INSERT` sin `id`: hoy falla en el restore.

### [C-06] [ALTO] [VERIFICADO] `drift` identifica las claves foráneas por nombre, y en SQLite el nombre se inventa a partir del `id` posicional del pragma: la comparación no converge y cada apply agrega una clave duplicada

> **Estado 2026-09-12:** CORREGIDO. Las claves se emparejan por firma (columnas, tabla destino sin distinguir mayúsculas, columnas destino) y se comparan por acciones; el nombre viaja a la sentencia solo si lo eligió alguien (`fk_<t>_<n>` y `<t>_ibfk_<n>` se tratan como vacío). Test `TestLasForaneasSeEmparejanPorLoQueHacenYNoPorSuNombre`.

- **Ubicación**: `internal/sqlite/introspect.go:223-238`; `internal/drift/drift.go:723-736,588-606,623-631`;
  `internal/sqlite/rebuild.go:260-263`.
- **Evidencia**:
  ```go
  // sqlite/introspect.go:231
  Name:      fmt.Sprintf("fk_%s_%d", tab, id),
  // drift.go:729-733
  clave := f.Name
  if clave == "" {
      clave = strings.Join(f.Columns, ",")
  }
  ```
  El respaldo por columnas nunca se usa en SQLite ni en MySQL porque `Name` siempre
  viene lleno (en MySQL, los nombres automáticos `<tabla>_ibfk_N` también son
  ordinales). Origen `pedidos` con claves →`clientes` y →`productos`, destino solo
  →`productos`: los `id` no coinciden, `fk_pedidos_0` sale como «apunta a otro lado» (sin
  sentencia) y `fk_pedidos_1` como «falta en el destino» con un rebuild que agrega una
  **segunda** clave →`productos`. Al volver a comparar los `id` se corren de nuevo. El
  comentario de `drift.go:592-597` explica que el nombre viaja para que la comparación
  converja; es exactamente la propiedad que esto rompe.
- **Impacto**: en SQLite y MySQL con claves sin nombre explícito, «Comparar → aplicar →
  comparar» no llega nunca a cero y cada vuelta duplica restricciones.
- **Fix recomendado**: emparejar primero por `(Columns, RefTable, RefColumns)` y recién
  después por nombre; tratar el nombre sintetizado de SQLite como vacío.
- **Cómo verificar el fix**: test de convergencia (`drift_test.go:482-507` ya existe
  para Postgres) contra SQLite y MySQL con dos claves creadas en distinto orden a cada
  lado: hoy la segunda comparación no da cero.

### [C-07] [ALTO] [VERIFICADO] `drift` empareja esquemas por nombre exacto: dos bases MySQL con distinto nombre —o cualquier comparación cruzada con MySQL o SQLite— no comparan ni una tabla

> **Estado 2026-09-12:** CORREGIDO para el caso de un solo esquema por lado: se emparejan por posición, las sentencias nombran el esquema del destino y `NoComparado` lo dice. Postgres con varios esquemas sigue por nombre. Test `TestDosBasesConDistintoNombreSeComparan`.

- **Ubicación**: `internal/drift/drift.go:183-225`; `internal/mysql/introspect.go:22`;
  `internal/sqlite/introspect.go:17,27`; `internal/service/comparar.go:122-124`.
- **Evidencia**: MySQL nombra su único esquema como la base (`esq := schema.Schema{Name:
  base}`), SQLite siempre `main`, Postgres el nombre real. `Comparar` recorre la unión
  de nombres y, si uno falta de un lado, emite una fila de esquema y `continue`
  (`drift.go:191-222`). `shop_dev` contra `shop_prod` —el caso normal de dev → prod en
  MySQL— produce dos filas («el esquema no existe en el destino · 40 tablas» y «existe
  solo en el destino»), cero diferencias de tabla y cero sentencias. Entre motores
  distintos, `NoComparado` afirma «Qué tablas y qué columnas existe de cada lado sí se
  comparó» (`:176-180`), que es falso en ese caso. `comparar.go` no pasa ningún mapeo de
  esquemas.
- **Impacto**: la pantalla de comparación no sirve para MySQL/MariaDB salvo que las dos
  bases se llamen igual, y lo dice de una forma que se lee como «no hay nada que
  comparar».
- **Fix recomendado**: cuando cada lado tiene un solo esquema (MySQL, SQLite), emparejar
  por posición y mostrar «`shop_dev` ↔ `shop_prod`»; en Postgres, permitir un mapeo
  explícito o al menos `public` ↔ `public` por defecto y avisar del resto.
- **Cómo verificar el fix**: test de `Comparar` con dos snapshots MySQL de distinto
  `Database` y una tabla que difiere: hoy `Diferencias` tiene dos entradas de clase
  esquema y ninguna de tabla.

### [C-08] [ALTO] [VERIFICADO] `drift` crea tablas y columnas sin identity, `AUTO_INCREMENT` ni columnas generadas, y la comparación siguiente las da por alineadas

> **Estado 2026-09-12:** PROVISORIO. `schema.Column.AutoIncrement` viaja desde las tres introspecciones (identity/serial, AUTO_INCREMENT, INTEGER PRIMARY KEY sola); `tablaSoloEnOrigen` sube el riesgo a Medio y nombra las columnas que se van a crear sin numerarse solas. Escribirlas en el CREATE TABLE exige que `change.Column` lo modele: en pendientes.

- **Ubicación**: `internal/drift/drift.go:294-329`; `internal/schema/snapshot.go:105-125`;
  `internal/postgres/introspect.go:184-186`; `internal/mysql/introspect.go:89`.
- **Evidencia**:
  ```go
  // drift.go:296-301
  for _, c := range t.Columns {
      cols = append(cols, change.Column{
          Name:     c.Name,
          DataType: c.DataType,
          Nullable: c.Nullable,
      })
  ```
  `schema.Column` no tiene `Identity` ni `Generated` (los tiene `DetailColumn`, que
  `drift` no usa). Una columna identity de Postgres no tiene fila en `pg_attrdef`, así
  que `atthasdef` es falso y el aviso «se crea sin defaults» de `drift.go:325-329` no
  se dispara; en MySQL `AUTO_INCREMENT` deja `column_default` nulo y `extra` sin
  `DEFAULT_GENERATED`, mismo resultado. Origen `orders(id bigint GENERATED ALWAYS AS
  IDENTITY PRIMARY KEY, …)` y destino sin la tabla → `CREATE TABLE orders (id bigint
  NOT NULL, …, PRIMARY KEY (id))`, riesgo Bajo, nota «crearla no puede romper nada».
- **Impacto**: el primer `INSERT` en el destino falla por `id` nulo, y la comparación
  siguiente reporta cero diferencias en esa columna: la deriva quedó escondida adentro
  de la corrección.
- **Fix recomendado**: llevar `Identity`, `Generated` y el default al snapshot (ya se
  leen en `Detail`) y usar `AutoIncrement` del motor al renderizar `CreateTable`, como
  hace `dump/ddl.go:73-86`. Mientras tanto, bajar el riesgo a Medio y avisar.
- **Cómo verificar el fix**: test de `Comparar` + `RenderDDL` con una tabla identity:
  hoy la SQL generada no contiene `IDENTITY` ni `serial`.

### [C-09] [ALTO] [VERIFICADO] `drift` compara la clave primaria columna por columna y emite un `ADD PRIMARY KEY` por cada una

> **Estado 2026-09-12:** CORREGIDO. La clave primaria se compara como conjunto ordenado a nivel tabla: un solo `AddPrimaryKey` con todas las columnas, ninguna sentencia si el destino ya tiene otra. Test `TestUnaClaveCompuestaSeCreaEnUnaSolaSentencia`.

- **Ubicación**: `internal/drift/drift.go:503-529`; `internal/service/migracion.go:230-237`.
- **Evidencia**:
  ```go
  // drift.go:519-522
  d.Cambio = &change.Change{
      Type: change.AddPrimaryKey, Schema: esquema, Table: tabla,
      Names: []string{origen.Name}, Source: "drift",
  ```
  Origen con clave `(order_id, line_no)` y destino sin clave → dos sentencias:
  `ALTER TABLE … ADD PRIMARY KEY ("line_no")` y después `… ("order_id")`. Origen `(a,b)`
  contra destino `(a)` → `ADD PRIMARY KEY (b)` sobre una tabla que ya tiene clave, que
  siempre falla; la operación correcta (soltar y volver a crear) no se ofrece.
- **Impacto**: en Postgres la migración va en transacción y se revierte entera. En MySQL
  y MariaDB no hay DDL transaccional (`migracion.go:230-237`): si `line_no` es único por
  casualidad, la primera sentencia confirma una clave primaria **equivocada** y la
  segunda falla.
- **Fix recomendado**: comparar la clave como conjunto ordenado a nivel tabla
  (`HasPrimaryKey` + lista de columnas) y emitir un solo `AddPrimaryKey` con todas, o
  ninguna sentencia si el destino ya tiene otra.
- **Cómo verificar el fix**: test de `Comparar` con clave compuesta de dos columnas: hoy
  salen dos diferencias con `Names` de un elemento.

### [C-10] [MEDIO] [VERIFICADO] Volcado con «DROP primero»: los `DROP TABLE` salen intercalados y en orden madre → hija, que es el único orden en que fallan

> **Estado 2026-09-12:** CORREGIDO. Todos los `DROP TABLE IF EXISTS` juntos al principio, en orden inverso al topológico. Test `TestDropFirstSirveSobreUnaBaseQueYaTieneLasTablas` corre el archivo sin borrar nada antes.

- **Ubicación**: `internal/service/volcado.go:319-325`; `internal/dump/orden.go:27-40`.
- **Evidencia**:
  ```go
  // volcado.go:319-322
  for i, ref := range p.tablas {
      detalle := p.detalles[i]
      if dropFirst {
          if _, err := fmt.Fprintf(w, "DROP TABLE IF EXISTS %s;\n", ...
  ```
  `p.tablas` es el orden topológico con las madres primero, pensado para los `INSERT`.
  El único motivo para pedir `DropFirst` es correr el archivo sobre una base donde las
  tablas ya existen, y ahí `DROP TABLE clientes` falla porque `pedidos` todavía la
  referencia (Postgres 2BP01, MySQL 3730). El test que vuelve a correr el volcado hace
  `DROP SCHEMA … CASCADE` antes y no pasa por esta rama.
- **Impacto**: la opción no sirve para lo que existe; falla ruidosamente, así que no
  corrompe.
- **Fix recomendado**: todos los `DROP` juntos al principio, en orden inverso al
  topológico (hijas primero), antes de cualquier `CREATE`.
- **Cómo verificar el fix**: test que vuelque con `DropFirst` sobre la misma base sin
  borrar el esquema antes: hoy falla en el primer `DROP`.

### [C-11] [MEDIO] [VERIFICADO] El divisor de sentencias parte cualquier rutina de MySQL/MariaDB que tenga `END IF`, `END LOOP`, `END WHILE` o `END REPEAT`, y manda `DELIMITER` al servidor

> **Estado 2026-09-12:** CORREGIDO. `END IF`/`END LOOP`/`END WHILE`/`END REPEAT` no restan (sus aperturas no suman); `END CASE` resta y consume la palabra. `DELIMITER x` al principio de una sentencia cambia el separador y la línea no viaja. `abreRutina` salta identificadores citados (`DEFINER = \`app\`@\`host\``). Test `TestUnProcedimientoConIfNoSeParte`.

- **Ubicación**: `internal/query/split.go:161-170,203-228`; `internal/service/queries.go:117-149`.
- **Evidencia**:
  ```go
  // split.go:162-168
  switch palabra {
  case "BEGIN", "CASE":
      profundidad++
  case "END":
      if profundidad > 0 {
          profundidad--
  ```
  Solo `BEGIN` y `CASE` abren nivel, pero todo `END` lo cierra. `CREATE PROCEDURE p()
  BEGIN IF x THEN SET @a = 1; END IF; SELECT 1; END` → el `END` de `END IF` baja a cero
  y el `;` siguiente parte: el servidor recibe `CREATE PROCEDURE … END IF` y responde
  con error de sintaxis. Casi cualquier procedimiento o trigger real de MySQL usa `IF`.
  Las líneas `DELIMITER //` pegadas de `mysqldump` o Workbench se envían como
  sentencias y fallan. `split_test.go` cubre un trigger de SQLite y `$$` de Postgres,
  no esto. Como `Run` corta en el primer fallo, no hay ejecución parcial.
- **Impacto**: no se puede crear una rutina con control de flujo desde el editor;
  el error que se ve es del servidor y no dice por qué.
- **Fix recomendado**: en modo compuesto, no decrementar cuando `END` va seguido de
  `IF`/`LOOP`/`WHILE`/`REPEAT`/`CASE`; contar `IF`… `END IF` como par propio o, más
  simple, reconocer `DELIMITER x` y partir por ese token hasta el próximo `DELIMITER`.
- **Cómo verificar el fix**: caso en `split_test.go` con el procedimiento de arriba:
  hoy devuelve tres sentencias.

### [C-12] [MEDIO] [VERIFICADO] SQLite: las columnas declaradas `DATE`, `DATETIME` o `TIMESTAMP` se muestran y exportan con un formato que no está en el archivo

> **Estado 2026-09-12:** CORREGIDO en grilla y exportación: `page` y `scan` leen las columnas declaradas DATE/DATETIME/TIMESTAMP con `CAST(… AS TEXT)` —el driver no parsea lo que no tiene tipo declarado— y reponen el tipo en el encabezado. En el editor no se puede reescribir la consulta: `textoDeFecha` escribe el formato más probable de SQLite en vez de RFC 3339 con `Z`. Test `TestLasFechasSeLeenComoEstanGuardadas` (falla 6 veces sin el CAST).

- **Ubicación**: `modernc.org/sqlite@v1.58.0/rows.go:196-201`; `internal/sqlite/query.go:164-165`;
  `internal/sqlite/scan.go:96`; `internal/connection/connection.go:516-524`.
- **Evidencia**: el driver, ante un `TEXT` en una columna cuyo `decltype` es
  `DATE`/`DATETIME`/`TIMESTAMP`, lo parsea a `time.Time` (`rows.go:196-197`); `aTexto`
  lo vuelve a texto con `RFC3339Nano` (`query.go:164-165`). Un `2021-01-02 16:39`
  guardado se ve como `2021-01-02T16:39:00Z`; un `DATE` `2021-01-02` se convierte en
  `2021-01-02T00:00:00Z`, con una zona `Z` que nadie escribió. Es precisamente lo que el
  DSN de MySQL evita al no poner `parseTime` (`connection.go:516-524`), y acá se hace.
- **Impacto**: la grilla y la exportación muestran algo distinto de lo que hay. Si una
  columna así es parte de la clave primaria, `Before` lleva el texto reescrito, el
  `UPDATE`/`DELETE` de la grilla encuentra 0 filas y se rechaza sin explicar. Un usuario
  que «vuelve a guardar» la celda escribe el formato nuevo.
- **Fix recomendado**: pedir el valor con `sqlite3_column_text` sin conversión —el driver
  expone `_time_format`/`_texttotime`; ver `rows.go:203-215`— o escanear a `*string`
  solo en esas columnas y tratar `time.Time` como error de invariante.
- **Cómo verificar el fix**: test con `CREATE TABLE t (d DATE)`, un `INSERT` de
  `'2021-01-02'` y `Run("SELECT d FROM t")`: hoy la celda vale `2021-01-02T00:00:00Z`.

### [C-13] [MEDIO] [VERIFICADO / SOSPECHADO en la capa JSON] MySQL: los valores binarios viajan como bytes crudos dentro de un `string`, se corrompen hacia la interfaz y en JSON, y hacen ineditable una clave `BINARY(16)`

> **Estado 2026-09-12:** CORREGIDO. Confirmado (`BINARY(16)` como bytes crudos, `BIT(8)=65` → «A»). `textoDe` entrega BINARY/VARBINARY/BLOB* en hexadecimal y BIT en decimal, en grilla y exportación; `Quoting.Binary` escribe `X'…'`. Ida y vuelta probada. Editar una clave binaria desde la grilla sigue sin soportarse (el WHERE compara texto): anotado.

- **Ubicación**: `internal/mysql/query.go:104-105`; `internal/mysql/scan.go:96-106`;
  `internal/mysql/mysql.go:66-72`; `internal/export/json.go:136-158`; `internal/mysql/query.go:119-121`.
- **Evidencia**: Postgres entrega `bytea` como `\x…` en texto; SQLite muestra `[N
  bytes]`; MySQL copia `RawBytes` a un `string` sin conversión. En la exportación SQL,
  `quoteString` escapa solo `\` y `'`: `NUL`, `0x1A`, `CR/LF` y bytes no UTF-8 van
  literales a un archivo que por lo demás es UTF-8 (`mysqldump` usa `--hex-blob` o escapa
  `\0 \n \r \Z`). En JSON, `codificar` reemplaza cada byte inválido por U+FFFD: la
  exportación de una columna binaria es con pérdida. `BIT(n)` está clasificado como
  número (`query.go:119-121`) pero su texto es el byte crudo: `BIT(8)=65` exporta como
  `"A"`. Hacia la interfaz, el `json` de Wails hace lo mismo con U+FFFD (SOSPECHADO: no
  se leyó el codificador de Wails), así que un `BINARY(16)` de clave primaria llega
  corrupto en `Before`, el `UPDATE` de la grilla no encuentra la fila y se rechaza. Es
  seguro, pero la tabla no se puede editar y nada dice por qué.
- **Impacto**: exportaciones JSON y CSV con pérdida, SQL con bytes crudos, y tablas con
  UUID binario —patrón común en MySQL— ineditables.
- **Fix recomendado**: en `leerFilas`/`Scan`, mirar `ColumnType.DatabaseTypeName()` y
  para `BINARY`/`VARBINARY`/`BLOB`/`BIT` entregar hex (`0x…`), que MySQL acepta como
  literal y que la grilla puede mostrar; en la exportación SQL, escribir `X'…'`.
- **Cómo verificar el fix**: test contra MySQL con `BINARY(16)` y un valor con bytes
  `0x00` y `0xFF`: hoy `Run` devuelve un `string` con esos bytes.

### [C-14] [MEDIO] [VERIFICADO] MySQL y MariaDB: «Cancelar» cierra el socket y nada más; la sentencia sigue en el servidor y confirma

> **Estado 2026-09-12:** CORREGIDO y confirmado: sin el fix, un `UPDATE … BENCHMARK(…)` cancelado a los 0,7 s terminaba y confirmaba las 2 filas; con `KILL QUERY <CONNECTION_ID()>` por otra conexión, no. Test `TestEnMySQLCancelarMataLaSentenciaEnElServidor`.

- **Ubicación**: `go-sql-driver/mysql@v1.10.1/connection.go:575-578`; `internal/mysql/*`
  (sin `KILL`); `internal/service/queries.go:339-346`.
- **Evidencia**: `cancel()` del driver llama a `cleanup()`, que cierra la conexión. No
  hay `KILL QUERY` en ningún archivo de `internal/` (`grep`). El servidor solo nota el
  socket cerrado cuando intenta escribir, así que un `UPDATE grande SET …` cancelado
  desde el editor se reporta como cancelado y termina y confirma en autocommit.
  Postgres manda `CancelRequest` y SQLite llama a `sqlite3_interrupt`; solo MySQL tiene
  el hueco.
- **Impacto**: el botón dice «cancelado» y la escritura ocurre igual.
- **Fix recomendado**: al abrir cada conexión guardar `CONNECTION_ID()`, y al cancelar
  abrir una conexión aparte y mandar `KILL QUERY <id>` antes de cerrar.
- **Cómo verificar el fix**: test contra MySQL con `UPDATE t SET n = SLEEP(5)` cancelado
  al segundo y `n` leído después: hoy cambia.

### [C-15] [MEDIO] [VERIFICADO] Postgres: `Run` pide una segunda conexión al pool mientras retiene la primera; con `PoolSize` 1, o dos pestañas sobre un pool de 2, se cuelga

> **Estado 2026-09-12:** CORREGIDO. Verificado: con `MaxConns: 1` y un enum, `Run` colgaba (el test falla a los 5 s con `pool`). Ahora resuelve con `conn`. Test: `postgres/pool_test.go`.

- **Ubicación**: `internal/postgres/query.go:52-56,130-134`; `internal/service/session.go:472,498-499`.
- **Evidencia**: `resolverTiposDesconocidos(ctx, pool, …)` corre antes del `defer
  conn.Release()`, con el `pool` y no con `conn`. Cualquier consulta que devuelva un
  enum, dominio o tipo de usuario necesita una segunda conexión. `Advanced.PoolSize`
  se respeta tal cual (`session.go:498-499`) y puede ser 1. `columnasDe` en `scan.go`
  documenta este mismo peligro y pasa `conn`.
- **Impacto**: cuelgue hasta cancelar; con dos pestañas sobre un pool de 2, las dos
  esperan a la otra.
- **Fix recomendado**: pasar `conn` (ya satisface `consultador`) o resolver después del
  `Release`.
- **Cómo verificar el fix**: test con `PoolSize: 1` y `SELECT 'a'::mi_enum`: hoy no
  termina.

### [C-16] [MEDIO] [VERIFICADO] La vista previa de exportación lee la tabla entera después del límite

> **Estado 2026-09-12:** CORREGIDO. `ScanOptions.Limit` con el LIMIT del motor en los tres; `volcar` lo pasa desde la vista previa. Test `TestLaVistaPreviaNoLeeLaTablaEntera` (cuatro motores).

- **Ubicación**: `internal/service/export.go:227-239,266-270`; `internal/postgres/scan.go:171-180`;
  `frontend/src/screens/ExportDialog.tsx:204-211`.
- **Evidencia**: `defer listo()` se registra antes que `defer flujo.Close()`, así que al
  volver `Close` corre primero, con el contexto todavía vivo. `volcarFlujo` hace `break`
  en `limit` sin cerrar; `Close` en Postgres llama a `rr.Close()`, que consume todo lo
  que falta del resultado; en MySQL `rows.Close()` descarta el resto por el cable. Una
  vista previa de 100 filas sobre 2 M de filas transfiere las 2 M, y el diálogo pide la
  vista previa de nuevo con cada cambio de opción.
- **Impacto**: minutos de espera y carga de red por una vista previa; sobre un túnel
  SSH, peor.
- **Fix recomendado**: agregar `Limit` a `ScanOptions` para el camino de vista previa (el
  `LIMIT` del motor es la solución real; cancelar el contexto a mitad de resultado en
  pgconn cierra la conexión, así que reordenar los `defer` no alcanza).
- **Cómo verificar el fix**: test de `PreviewTable` con un flujo instrumentado que cuente
  `Next()`: hoy recorre todas las filas.

### [C-17] [MEDIO] [VERIFICADO] Exportar una tabla a SQL desde la grilla incluye las columnas generadas, y el archivo falla al correr

> **Estado 2026-09-12:** CORREGIDO. `volcar` resuelve `insertables` y `DumpHints` para todo formato SQL; el volcado le pasa el detalle ya leído para no leerlo dos veces. Test `TestExportarASQLDesdeLaGrillaDejaAfueraLasGeneradas`.

- **Ubicación**: `frontend/src/screens/ExportDialog.tsx:165-177`; `internal/service/export.go:230-235`;
  `internal/postgres/scan.go:187-190`; `internal/service/volcado.go:386-389`.
- **Evidencia**: `pedidoDeTabla` no manda `columns`; `volcar` pasa `r.Columns` vacío y
  `listaDeColumnas` lo convierte en `*`. Solo el camino del volcado excluye las
  generadas (`volcado.go:389`). Con una columna `STORED` el archivo falla al correr
  (Postgres «cannot insert a non-DEFAULT value into column»; MySQL 3105).
- **Impacto**: el mismo problema que `insertables` ya resolvió, en el camino de al lado.
- **Fix recomendado**: resolver `insertables` dentro de `volcar` cuando el formato es
  SQL, en vez de esperar que cada llamador lo haga.
- **Cómo verificar el fix**: test de `SaveTable` en SQL sobre una tabla con columna
  generada, corriendo el archivo en una base vacía: hoy falla.

### [C-18] [MEDIO] [VERIFICADO] `drift` identifica los objetos por `clase:nombre`: los triggers de distintas tablas y las funciones sobrecargadas se pisan entre sí

> **Estado 2026-09-12:** CORREGIDO. Clave `Kind + Table + Name + Args`. Test `TestLosObjetosSeIdentificanPorTablaYArgumentos`.

- **Ubicación**: `internal/drift/drift.go:738-746,651-695`; `internal/schema/detail.go`
  (`Object.Table`, `Object.Args`); `internal/schema/snapshot.go:186-187`.
- **Evidencia**:
  ```go
  // drift.go:743
  m[string(o.Kind)+":"+o.Name] = o
  ```
  `schema.Object` tiene `Table` y `Args` justamente para esta identidad, y `Normalize`
  ya ordena por `Name+"\x00"+Args`. Origen con trigger `set_updated_at` en cinco tablas,
  destino en dos → «alineado». `calcular(int)` y `calcular(text)` de un lado, una sola
  del otro → «alineado». El último gana en el mapa, así que el resultado también
  depende del orden.
- **Impacto**: la deriva más común en triggers (una tabla nueva sin su trigger) no se
  detecta.
- **Fix recomendado**: clave `Kind + Table + Name + Args`.
- **Cómo verificar el fix**: test con dos triggers del mismo nombre en tablas distintas
  de un lado y uno del otro: hoy cero diferencias.

### [C-19] [MEDIO] [VERIFICADO] MySQL: `column_key = 'PRI'` también se reporta para un índice `UNIQUE NOT NULL` cuando la tabla no tiene clave primaria

> **Estado 2026-09-12:** CORREGIDO. Confirmado (`UNIQUE (id)` con `id NOT NULL` → `HasPrimaryKey=true`). La columna de clave sale de `information_schema.statistics` con `index_name = 'PRIMARY'`. Test `TestEnMySQLUnIndiceUnicoNoEsClavePrimaria`.

- **Ubicación**: `internal/mysql/introspect.go:90,111-113,532-537`.
- **Evidencia**: `information_schema.columns.column_key` devuelve `PRI` para un índice
  único sin nulos si la tabla no tiene `PRIMARY KEY` (documentado por MySQL). El
  snapshot usa eso para `PrimaryKey` y `HasPrimaryKey` (`:111-113`), mientras que
  `primaryKeyColumns` (`:532-537`) usa `index_name = 'PRIMARY'`: dos definiciones de
  «clave» que no coinciden.
- **Impacto**: origen con clave real y destino con solo `UNIQUE NOT NULL (id)` → sin
  diferencia de clave en el diff; la grilla cree que hay clave primaria donde hay un
  índice único.
- **Fix recomendado**: derivar `PrimaryKey` de `information_schema.statistics` con
  `index_name = 'PRIMARY'`, como ya hace `primaryKeyColumns`.
- **Cómo verificar el fix**: test contra MySQL con `CREATE TABLE t (id INT NOT NULL,
  UNIQUE (id))`: hoy `HasPrimaryKey` es verdadero.

### [C-20] [MEDIO] [VERIFICADO] `drift` en MySQL: los cambios de nulabilidad no se pueden escribir y los de tipo borran `NOT NULL`, `DEFAULT`, `AUTO_INCREMENT` y comentario

> **Estado 2026-09-12:** CORREGIDO. `drift` manda tipo y nulabilidad en `SetColumnType`/`SetNotNull`/`DropNotNull`, y el renderizador de MySQL escribe `NULL`/`NOT NULL` en el `MODIFY` de `SetColumnType` (la nota dice qué sigue perdiéndose: default, AUTO_INCREMENT, comentario). Test `TestLaNulabilidadYElTipoViajanCompletos`.

- **Ubicación**: `internal/drift/drift.go:455-462,480-492`; `internal/mysql/ddl.go:119-143`;
  `internal/service/comparar.go:182-187`.
- **Evidencia**: `SetNotNull`/`DropNotNull` salen con `Column{Name}` sin `DataType`
  (`drift.go:482,491`), y el renderizador de MySQL exige el tipo (`ddl.go:135-139`), así
  que **toda** diferencia de nulabilidad termina como «el motor del destino no supo
  escribir esta operación» aunque el diff tenía el tipo en `origen.DataType`.
  `SetColumnType` sí lleva el tipo, y se renderiza como `MODIFY COLUMN nombre tipo` a
  secas (`ddl.go:123-124`): la nota admite que se pierde lo demás, pero la sentencia es
  la que se ejecuta. Un `int` → `bigint` en la clave primaria le quita el
  `AUTO_INCREMENT`.
- **Impacto**: en la migración generada para MySQL, la mitad de las operaciones de
  columna no se escriben y la otra mitad es con pérdida.
- **Fix recomendado**: que `drift` complete `Column` con tipo, nulabilidad y (cuando se
  conozca) default al emitir `SetColumnType`/`SetNotNull`/`DropNotNull`, y que el
  renderizador de MySQL escriba la definición completa.
- **Cómo verificar el fix**: test de `Comparar` + `RenderDDL` en MySQL con una columna
  que difiere en nulabilidad: hoy no hay sentencia.

### [C-21] [BAJO] [VERIFICADO] `SaveTables` no se registra para cancelar: entre una tabla y la siguiente, «Cancelar» se pierde

> **Estado 2026-09-12:** CORREGIDO. `SaveTables` se registra una vez, después de validar la entrada. Sin test propio: la ventana entre tablas no se puede provocar a voluntad; el mecanismo es el mismo que `Dumps.Save` ya prueba.

- **Ubicación**: `internal/service/export.go:358-402,412-453`; `internal/service/queries.go:355-387`;
  `internal/service/volcado.go:125-131`.
- **Evidencia**: `aUnArchivo` y `aUnDirectorio` llaman a `volcar` por tabla, y cada
  `volcar` registra y borra su propia entrada. Un `Cancel` que llega cuando la tabla N
  terminó y la N+1 todavía no hizo `Scan` no encuentra entrada y se descarta. El
  comentario de `registrar` describe este bug exacto y cómo lo arregló el volcado
  (`volcado.go:130-131`); `SaveTables` no recibió el arreglo ni tiene test.
- **Fix recomendado**: `registrar` una vez en `SaveTables`, como en `Dumps.Save`.
- **Cómo verificar el fix**: test como `queries_test.go:274` para `SaveTables`.

### [C-22] [BAJO] [VERIFICADO] Un fallo parcial al listar objetos se compara como si la lista estuviera completa

> **Estado 2026-09-12:** CORREGIDO. `Opciones.SinObjetos` cuando algún lado tiene `ObjectsError`; `comparar.go` lo pasa. Test `TestSinLaListaDeObjetosNoSeComparanObjetos`.

- **Ubicación**: `internal/postgres/objetos.go:44-77`; `internal/service/session.go:517-536`;
  `internal/service/comparar.go:125-137`; `internal/drift/drift.go:651-695`.
- **Evidencia**: `Objects` devuelve lo leído junto con `errors.Join`; `session` guarda
  las dos mitades y `comparar` agrega la nota «no se pudo leer la lista», pero
  `compararObjetos` corre igual sobre la lista parcial. Si falla solo la consulta de
  políticas del origen, cada política del destino aparece como «existe solo en el
  destino · Alguien lo creó directamente», riesgo Medio. No genera DDL, pero la lista
  contradice la nota.
- **Fix recomendado**: con `ObjectsError` en cualquier lado, no comparar objetos (o
  comparar solo las clases que sí se leyeron, si `Objects` las distingue).

### [C-23] [BAJO] [VERIFICADO] La migración generada falla en dos casos comunes: tipos que se usan antes de existir y `ENUM` con guiones

> **Estado 2026-09-12:** CORREGIDO a medias: `tipoAceptable` valida solo fuera de las comillas (`enum('in-progress')` pasa; comillas sin cerrar, control y `\` adentro no). El riesgo de una tabla nueva que usa un tipo faltante queda como estaba: anotado en pendientes.

- **Ubicación**: `internal/service/migracion.go:273-291`; `internal/drift/drift.go:277,683-684`;
  `internal/mysql/ddl.go:25,59,93,120`.
- **Evidencia**: `rangoDeMigracion` pone `CreateTable` en 0 y un tipo sin sentencia en
  9 (comentario); una tabla nueva que usa un enum que el destino no tiene se etiqueta
  «crearla no puede romper nada» y falla en el `CREATE`. En MySQL,
  `tipoValido` (`^[A-Za-z0-9_ ,()'\[\]]+$`) rechaza `enum('in-progress','done')`, un
  tipo que el propio servidor produjo, y el `CREATE TABLE` entero pasa a «no supo
  escribir».
- **Fix recomendado**: cuando hay un tipo faltante, subir el riesgo de las tablas que lo
  usan y nombrarlo; ampliar `tipoValido` con `-`, `/`, `.`, `%`, letras acentuadas
  dentro de comillas (o validar solo fuera de las comillas).

### [C-24] [BAJO] [VERIFICADO] Casos que el diff no ve o ve mal, sin decirlo en `NoComparado`

> **Estado 2026-09-12:** PARCIAL. `RefTable` se compara sin distinguir mayúsculas; DEFERRABLE/MATCH, orden de columnas y particionado entran en `NoComparado`; la clave primaria se emite en el orden de las columnas de la tabla (el orden de declaración de la clave no está en el snapshot). El plegado de nombres de tabla en MySQL/Windows sigue byte a byte.

- **Ubicación**: `internal/drift/drift.go:166-174,296-310,707-713,624,824-834`;
  `internal/sqlite/introspect.go:216-235`; `internal/schema/snapshot.go:63,124`.
- **Evidencia**:
  - Tablas emparejadas por nombre byte a byte (`:707-713`). Con
    `lower_case_table_names=1` en Windows y un Linux que preserva mayúsculas, `orders`
    contra `Orders` es «no existe en el destino» y el `CREATE TABLE \`orders\`` deja dos
    tablas en Linux.
  - `RefTable` en SQLite guarda el texto tal cual se escribió en `REFERENCES`
    (`introspect.go:216-235`) y el diff lo compara literal (`:624`): `Padre` contra
    `padre` es «apunta a otro lado».
  - `Deferrable` y `MATCH` no entran en `resumenDeForanea` ni en `AddForeignKey`: una
    clave `DEFERRABLE INITIALLY DEFERRED` y una común se ven iguales, y la que se crea
    pierde la propiedad sin nota.
  - `CreateTable` escribe la clave primaria en orden de `attnum`, no de declaración de
    la clave (`:296-310`): `(tenant_id, id)` sale como `PRIMARY KEY ("id","tenant_id")`,
    y la comparación siguiente no puede verlo.
  - Posición de columnas y particionado están en el snapshot y no se comparan ni se
    listan como no comparados.
- **Fix recomendado**: agregar cada uno a `NoComparado` hasta que se compare; para las
  mayúsculas, comparar con `EqualFold` cuando el motor pliega.

### [C-25] [BAJO] [VERIFICADO] El volcado escribe claves foráneas hacia esquemas que no están en el archivo

> **Estado 2026-09-12:** CORREGIDO. `dump.ClavesForaneas` recibe los esquemas del volcado, deja afuera las que apuntan a otro y las devuelve para la cobertura (`schema.ObjForeignKey`, «clave foránea hacia otro esquema»). Test `TestUnaClaveHaciaOtroEsquemaNoVaAlArchivoYSeNombra`.

- **Ubicación**: `internal/service/volcado.go:347-353`; `internal/dump/orden.go:59-67`.
- **Evidencia**: `Orden` ignora a propósito las aristas hacia tablas fuera del volcado;
  la sección de claves foráneas no, y renderiza todas las de `d.ForeignKeys`. Sobre una
  base vacía, `ADD FOREIGN KEY … REFERENCES otro_esquema.t` falla y la cobertura no lo
  nombra.
- **Fix recomendado**: filtrar por `RefSchema ∈ esquemas` y nombrar las excluidas.

### [C-26] [BAJO] [VERIFICADO] Detalles de fidelidad en los formatos de exportación

> **Estado 2026-09-12:** CORREGIDO lo que era corregible: `\` se escapa en Markdown; el encabezado del CSV no se neutraliza; el REAL de SQLite conserva el `.0`. Lo de `standard_conforming_strings` y el float de MySQL por protocolo binario quedan como estaban (anotados, no objetados).

- **Ubicación**: `internal/export/markdown.go:90-108`; `internal/export/csv.go:42-46,76-82,105-117`;
  `internal/sqlite/query.go:155-158`; `internal/postgres/ddl.go:498`; `internal/export/json.go:110-113`.
- **Evidencia**:
  - Markdown: `\` pasa sin escapar y `|` sale como `\|`, así que `a\|b` se escribe
    `a\\|b`, que GFM lee como barra escapada + separador de celda: la fila se parte.
  - CSV con «neutralizar fórmulas»: el encabezado pasa por `Row` → `campo` → `esFormula`,
    y una columna llamada `-x` o `@id` se escribe `"'-x"`.
  - SQLite `REAL`: `%v` de `3.0` da `3`; al recargar en una columna sin afinidad `REAL`
    queda `INTEGER`.
  - Postgres: `QuoteString` dobla solo `'`; correcto con
    `standard_conforming_strings=on` (default desde 9.1), incorrecto si el servidor lo
    tiene apagado. MySQL sí lee `sql_mode` para el caso análogo.
  - JSON escribe sin comillas cualquier número válido, incluidos `int8` > 2^53 y
    `numeric` de 30 dígitos: válido según la gramática, redondeado por JS, `jq` y pandas.
    Es una decisión documentada (`row_to_json`); se anota, no se objeta.
  - SOSPECHADO: en MySQL, `Scan` con `Where` usa `QueryContext` con argumentos, que en el
    driver va por el protocolo binario y puede formatear los `float` con Go
    (`1e+06`) en vez del texto del servidor; sin filtro va por texto. No se verificó
    contra el driver.
- **Fix recomendado**: escapar `\` en Markdown; no neutralizar el encabezado; formatear
  `float64` con `strconv.FormatFloat(x, 'g', -1, 64)` y agregar `.0` si es entero, o
  pedir el texto con `CAST(x AS TEXT)`; leer `standard_conforming_strings` al conectar.

### [C-27] [BAJO] [VERIFICADO] Divisor de sentencias: casos de Postgres y MySQL que parten donde no deben

> **Estado 2026-09-12:** CORREGIDO. `E'…'` con escapes en Postgres; `BEGIN ATOMIC … END` se cuenta como bloque; los comentarios de bloque se anidan con `DollarQuotes` (también en `Command`/`Trim`); `DEFINER` ya no consume el presupuesto de palabras. Test `TestLosCasosDePostgresQuePartianDondeNoDebian`.

- **Ubicación**: `internal/query/split.go:203-228,232-235`; `internal/postgres/conn.go:154`;
  `internal/engine/sesion.go:19`.
- **Evidencia**: el dialecto de Postgres es `{DollarQuotes: true}` sin `Compound` ni
  `BackslashEscapes`. (a) `SELECT E'O\'Brien; x'`: el `\'` cierra la cadena y el `;`
  parte. (b) PG14+ `CREATE FUNCTION … BEGIN ATOMIC SELECT 1; SELECT 2; END;` se parte
  adentro del cuerpo. (c) Comentarios de bloque anidados (válidos en Postgres) terminan
  en el primer `*/`. (d) `abreRutina` mira como mucho 8 tokens: con
  `CREATE DEFINER = \`app\`@\`10.0.0.1\` PROCEDURE p() BEGIN …` nunca llega a
  `PROCEDURE` y el cuerpo se parte en cada `;`. Todos terminan en error del servidor
  sobre el primer fragmento, no en ejecución parcial.
- **Fix recomendado**: `E'…'` con escapes en Postgres; `BEGIN ATOMIC` como bloque;
  contar anidamiento de `/* */` en Postgres; en `abreRutina`, saltar la cláusula
  `DEFINER = x@y` completa antes de contar tokens.

### [C-28] [BAJO] [VERIFICADO] Importación CSV: los «números de línea» son de registro, y las filas con campos de más se aceptan en silencio

> **Estado 2026-09-12:** CORREGIDO. `Inspect` y `Rows` informan la línea FÍSICA (`FieldPos(0)`) y una fila con más campos que el mapeo se rechaza como despareja. Test `TestLaLineaEsLaFisicaAunqueUnCampoTengaSaltos`.

- **Ubicación**: `internal/csvimport/csvimport.go:157,348`; `internal/service/importar.go:367-379`.
- **Evidencia**: `linea++` por cada `r.Read()`: un campo citado con salto de línea
  desplaza todos los `Ragged.Line` e `ImportResult.Line` posteriores («el lote que falló
  empieza en la línea N» apunta mal). `csv.Reader.FieldPos`/`InputOffset` dan la línea
  física. `elegir` solo falla con menos campos; con más, los sobrantes se descartan sin
  aviso.
- **Fix recomendado**: usar `FieldPos(0)` para la línea; tratar «más campos» como
  desparejo igual que «menos».

### [C-29] [BAJO] [VERIFICADO] SQLite: `SELECT changes()` después de una sentencia sin columnas puede devolver el conteo de una sentencia anterior

> **Estado 2026-09-12:** CORREGIDO. `changes()` solo se consulta tras INSERT/UPDATE/DELETE/REPLACE/WITH. Test `TestElConteoDeAfectadasEsDeEstaSentencia` con pool de una conexión (falla sin el fix).

- **Ubicación**: `internal/sqlite/query.go:57-63`.
- **Evidencia**: `db.Conn(ctx)` entrega una conexión del pool; `changes()` reporta el
  último `INSERT`/`UPDATE`/`DELETE` **de esa conexión**. Un `CREATE TABLE` o un `PRAGMA`
  sobre una conexión que antes corrió un `UPDATE` de 3 filas muestra «3 afectadas».
- **Fix recomendado**: mirar `Command` y reportar afectadas solo para DML, o usar
  `sql.Result.RowsAffected()` de un `ExecContext` en la misma conexión dedicada.

### [C-30] [BAJO] [SOSPECHADO] Postgres: `format_type` califica los tipos de usuario según el `search_path` de cada conexión, y el diff los compara como texto

> **Estado 2026-09-12:** ANOTADO en `pendientes-auditoria-2026-09-12.md` (§4): forzar `search_path` vacío en la transacción de la introspección o normalizar el prefijo.

- **Ubicación**: `internal/postgres/introspect.go:184`; `internal/drift/drift.go:441,780-782`;
  `internal/connection/validate.go:94`.
- **Evidencia**: `pg_catalog.format_type` omite el esquema cuando el tipo está en el
  `search_path` y lo escribe cuando no. Cada conexión puede fijar el suyo
  (`Advanced.SearchPath`, `SessionSQL`). Origen con `search_path = demo, public` y
  destino con el default → `mood` contra `demo.mood` → «el tipo difiere», riesgo Medio,
  `ALTER TABLE … ALTER COLUMN estado TYPE mood`: un rewrite inútil, o un error si
  `demo` no está en el path del destino. Lo mismo aplica a
  `pg_get_function_identity_arguments` en `objetos.go`. SOSPECHADO en cuanto a
  frecuencia; el mecanismo es el documentado.
- **Fix recomendado**: en la introspección, forzar `set_config('search_path', '', true)`
  dentro de la misma transacción de lectura, o normalizar quitando el prefijo del
  esquema propio antes de comparar.

### [C-31] [BAJO] [VERIFICADO] El volcado planifica antes de registrarse: un esquema grande no se puede cancelar mientras se lee

> **Estado 2026-09-12:** CORREGIDO. `Dumps.Save` se registra antes de planear.

- **Ubicación**: `internal/service/volcado.go:125-131`.
- **Evidencia**: `planear` (un `Introspect`, un `Detail` por tabla, `Objects` y un
  `DDLDeTabla` por tabla) corre antes de `registrar`. Con 200 tablas es la parte larga,
  y «Cancelar» no encuentra nada que cortar.
- **Fix recomendado**: registrar primero.

## Revisado sin problemas

**Credenciales y keychain.** `Connection` no tiene campo de contraseña y se serializa
sin ella (`connection.go:243-285`); `DSN` la recibe por argumento y la descarta
(`:434-456`); `secrets.Keyring` valida el ID y acota el largo (`secrets.go:59-141`);
`Duplicate` e `ImportConnections` generan ID nuevo justamente para no heredar el
secreto (`connections.go:319-338`, `compartir.go:181-223`). `RevealPassword` y
`RevealSSHSecret` son las únicas funciones que devuelven un secreto y se llaman a
pedido. `TestResult`, `ConnectResult`, `Failure` y `ServerInfo` no llevan nada
sensible. `pg_dump` recibe la contraseña por entorno del hijo, nunca por argumentos
(`pgdump.go:207-210`) —el problema es el canal, K-04, no la forma—. `store`,
`config`, `history`, `layout` y `known_hosts` escriben con temporal + `Sync` +
`Chmod 0600` + `Rename` y directorios `0700`.

**Sin logs ni eventos.** No hay `log`, `slog`, `fmt.Print*` ni `os.Stderr/Stdout` en
`internal/` (`logs_test.go:38-77` lo prohíbe por AST). El logger de Wails: en
`application.go:59-64` del módulo, si no es modo debug el logger es `io.Discard`
aunque no exista el tag `production`; en debug el nivel por defecto es `Info` y
«Binding call complete» (`messageprocessor_call.go:131`) es `Debug`. `main.go` no
toca `Logger` ni `LogLevel` y `build/windows/Taskfile.yml:64` compila con `-tags
production`. `Redact` se aplica en `Classify` de los tres motores y en cada `Detail`
que arma el servicio.

**Túnel SSH.** No hay `InsecureIgnoreHostKey` (búsqueda sobre `*.go`: solo el test que
lo prohíbe). `Inspect` aborta el handshake en la devolución de llamada de la clave,
antes de la autenticación y sin métodos de auth (`dial.go:95-123`). `verificador`
exige clave conocida o huella aceptada para ESTE intento; `AcceptOnce` es un
parámetro por llamada y no estado (`session.go:139`). `Trust` guarda la clave que se
mostró, no una nueva del servidor (`hostkey.go:294-302`). Sin `net.Listen` en ningún
lado (`nolisten_test.go`); el túnel es un `DialFunc` en proceso. La clave privada se
lee de archivo y no aparece en errores (`dial.go:286-306`). El agente en Windows va
por `go-winio` al named pipe, sin exportar la clave.

**TLS.** Postgres: `sslmode`, `sslrootcert`, `sslcert`, `sslkey` van en el DSN con
`~` resuelto (`connection.go:467-497`) y pgx los verifica. MySQL: traducción a
`*tls.Config` fiel a libpq —`verify-ca` verifica cadena sin nombre a mano,
`verify-full` con nombre, `require` + raíz pasa a `verify-ca`, `prefer/allow`
permiten caer a claro— (`mysql/tls.go:56-134`). `VerifyConnection` se usa solo para
capturar el certificado. `Warnings()` avisa sobre `prefer`/`allow`/`disable` mirando
el modo EFECTIVO. `SaveCertificate` decodifica y recodifica: no escribe texto libre.

**SQL: identificadores, valores y filtros.** Los cuatro renderizadores citan todo
identificador (`"…"`, `` `…` ``) y validan largo y caracteres de control; los tipos
pasan por `tipoValido`; la firma de una función por `firmaValida`; las acciones
referenciales por tabla cerrada; el `Method` de un índice se cita como identificador.
Lo que va crudo es lo que es expresión por definición —`Default`, `Expression`,
`Where` del índice, `Definition` de un objeto— y siempre pasa por la vista previa.
`dml` escribe la forma legible con literales y la ejecutable con marcadores,
compartiendo el texto para que no puedan diferir (`dml.go:115-140`); `Where` de la
grilla usa operadores de lista cerrada, valores parametrizados y `LIKE … ESCAPE '!'`
con el patrón escapado (`filtro.go`); `InsertBatch` parametriza y respeta el tope de
65535 marcadores por sentencia (`importar.go:135-162`). `Page`, `Count`, `Scan` y
`CountWhere` parametrizan valores y citan columnas en los tres motores. Las
consultas al catálogo van con parámetros; los únicos nombres concatenados son los
de `SHOW CREATE …` en MySQL, citados, y `DROP … ON tabla` en Postgres, citados. La
introspección de los cuatro motores no interpola nombres del catálogo en SQL sin
citar.

**Vista previa y apply (camino principal).** `Stage`/`StageMany` renderizan antes de
guardar y exigen confirmación para destructivos contra producción; `Apply` y `DryRun`
verifican solo lectura y la confirmación en Go, no en la pantalla; `preparar` no
ejecuta nada si una sentencia no se puede escribir; `correrTramo` se niega a ensayar
sin transacción; `RolledBack` se afirma solo cuando el `ROLLBACK` devolvió bien;
`olvidarAplicados` mira `Applied` por sentencia. `TramosDe` parte correctamente en
MySQL/MariaDB y aísla `ALTER TYPE … ADD VALUE`. Los `Steps` evitan mandar dos
sentencias en una cadena a MySQL. `Explain` usa lista blanca de comandos y rechaza
`EXPLAIN` escrito a mano (`plan.go:86-108`).

**Importación y exportación.** `csvimport` lee de a una fila, acota la muestra y las
líneas crudas, y no interpreta valores. La importación va en UNA transacción con
`Rollback` bajo `context.WithoutCancel`. Las exportaciones escriben a temporal en el
mismo directorio y renombran al final; los nombres de archivo por tabla sanean
separadores y nombres reservados de Windows. `leerCompartido` acota el tamaño antes
de leer y exige la huella al importar. `Migration` y `Compare` no ejecutan nada.
`RunPgDump` acota `stderr` y borra el archivo parcial ante cualquier fallo.

**Segunda pasada — lo que se leyó y quedó bien.**
*Grilla y DML*: la clave que manda la grilla se contrasta con el catálogo
(`service/grid.go:64-97`), sin clave o sobre vistas se rechaza (`:118-121`), el `WHERE`
usa los valores leídos y no los editados (`:129-139`), `NULL` en la clave va como
`IS NULL` (`dml/dml.go:148-161`), `Bound.Rows = 1` y el conteo se exige exacto con
mensajes distintos para 0 y >1 (`apply.go:1055-1075`), siempre en transacción aunque
«una sola transacción» esté apagado (`apply.go:861-875`); `NULL`, `""` y el texto
`"NULL"` se distinguen por puntero de punta a punta; los valores nunca se concatenan
(`dml.go:132-140`, `filtro.go:32-35,100-119`); `ClientFoundRows` en MySQL para que
«afectadas» cuente coincidencias; el límite de filas es un corte de lectura con
`Truncated`, no un `LIMIT` inyectado. Sin guarda optimista: `Previous` viaja para
mostrar y no se compara; es una decisión de diseño, se anota. *Importación*: BOM,
comillas con saltos de línea, `EmptyAsNull` explícito, una transacción con rollback bajo
`context.WithoutCancel`, lote acotado por 65535 parámetros, confirmación de producción
en Go. *Divisor*: cadenas con comillas dobladas, comentarios `--`, `#` y `/* */` con
`;`, `$$`/`$tag$` sin confundir `$1`, literal sin cerrar que traga el resto en vez de
partir, `BEGIN;` de transacción distinguido del `BEGIN` de rutina, línea por sentencia y
corte en el primer fallo. *Exportación*: CSV con `NULL` sin citar y `""` citado,
JSON con solo números y booleanos inequívocos sin comillas y control chars como
`\u00XX`, SQL con lotes de 500 y último lote cerrado, escritura atómica a temporal con
`Sync` + `Rename` y borrado en todo camino de error, error del flujo mirado antes de
`End()`, `gzip` de un solo miembro alrededor del script; `Orden` (Kahn) determinista con
ciclos detectados y autorreferencias ignoradas; la cobertura muestra las clases que no
conoce en vez de callarlas; nombres de archivo con sufijo de colisión y reservados de
Windows. *Drift*: nunca emite `DROP`, la invariante «una operación o un motivo» se
sostiene en todas las ramas, orden determinista, `attnum` sin aliasing de punteros,
orden de columnas en claves compuestas preservado (`WITH ORDINALITY`, `ORDER BY
ordinal_position`, `ORDER BY id, seq`), acciones referenciales de ida y vuelta, un fallo
total de introspección aborta en vez de comparar contra vacío, `AddColumn NOT NULL` sin
default suprimido, operaciones entre motores distintos suprimidas, la migración ordena
tablas → columnas/clave → foráneas con `;` y transacción solo donde el DDL es
transaccional.

**Correctitud de introspección (revisión por lectura, no por ejecución).** Postgres
filtra por `has_schema_privilege`, distingue `Readable` con puntero para el `NULL`
de una tabla borrada en el medio, y el índice `pk` recorta `indkey` a
`indnkeyatts`. `Objects` en los tres motores devuelve lo leído junto con los errores
(`errors.Join`) y el árbol/volcado usan las dos mitades. `Dependents` distingue
«vacío» de «no se sabe» (`Unknown`). La suite común tiene «ciclo aplicar y releer»
y «las filas sobreviven al cambio de esquema» (`enginetest/suite.go:73-74`).

## Fuera de alcance / no se pudo evaluar

- **No se ejecutó nada.** Las afirmaciones sobre comportamiento en tiempo de ejecución
  se apoyan en lectura del código y de las dependencias cacheadas. K-01, K-03 y K-06
  merecen el test de verificación propuesto antes de darlos por cerrados.
- **WebView2.** Lo que el runtime manda a Microsoft por su cuenta está documentado en
  el README (`:78-90`) y no es código de este repositorio.
- **Runtime de Wails v3** más allá de lo leído: logger, modo debug, opciones de
  ventana. No se revisó el mecanismo de IPC ni si la CSP propuesta en K-15 lo afecta.
- **Frontend** fuera de los sumideros de HTML y de los puntos donde una ruta o un dato
  de la base cruzan a Go. No se auditó lógica de UI, estado ni accesibilidad.
- **`internal/drift`, `internal/export`, `internal/dump`, `internal/csvimport` y
  `query/split.go`**: en la primera pasada quedaron por encima; la segunda los leyó
  completos (ver «Segunda pasada»). Lo que sigue sin verificar ahí: el cuerpo de las
  vistas, funciones y triggers no se compara (el propio `NoComparado` lo dice) y no se
  evaluó qué haría falta para compararlo; y el codificador JSON de Wails para C-13.
- **Dependencias, CVE, secretos hardcodeados y licencias**: excluidos por el pedido.
- **Comportamiento del pool de `database/sql` con transacciones abiertas (MySQL y
  SQLite) en K-03**: resuelto en C-01 por lectura de `database/sql` y de los dos
  drivers; sigue valiendo que el test propuesto es lo que lo cierra.
- **Frecuencia real de tres casos** que se dieron por VERIFICADOS en el mecanismo y no
  en la práctica: el corte de conexión entre aplicar y leer el OK (C-03), el
  `search_path` distinto entre lados (C-30) y el protocolo binario para `float` con
  filtro (C-26).
