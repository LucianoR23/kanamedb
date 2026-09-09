# Kaname — Plan de desarrollo

Gestor de bases de datos de escritorio con diagrama ERD editable.
Motores: PostgreSQL, MySQL, MariaDB, SQLite.

**Supuestos de este plan:** ~8–10 h/semana, trabajo solo, Postgres como motor
principal (ordena las iteraciones). Las pantallas referencian los IDs del set de
diseño (`design/Sxx-*`).

---

## 1. Plan

### Iteración 0 — Esqueleto

Wails v3 + React/Vite, assets embebidos, CI que compila win-x64 y win-arm64.

- ✅ **Scaffold** — Wails v3.0.0-beta.17 + React 19 + TypeScript 7 + Vite 8, assets
  embebidos, sin sockets. Ventana verificada. Commit `d63f04f`.
- ✅ **CI** — GitHub Actions, matriz win-x64 / win-arm64.
- ✅ **S00 Foundations** — tokens CSS (dark + light, acentos de entorno), tipografía,
  espaciado y componentes base: botones, inputs, tabs, badges, tree row, celda de
  grilla, dialog, toast, context menu.
- ✅ **S05 Main workspace shell** — shell vacío: sidebar, tab strip, status bar, panes
  redimensionables. Sin contenido real.
- ✅ **S25 About** — completa, con el servicio `appinfo` como contrato.

El diseño vive en Claude Design, proyecto
`025e0714-d352-4c71-a783-68c91cdc66e8`. Se baja con `DesignSync` a `design/`,
que está en `.gitignore`: una copia commiteada se desactualiza y miente.

### Iteración 1 — Conexiones

Manager de conexiones, keychain, conectar a Postgres, árbol de esquema.

- ✅ **Contrato de Go** — `connection`, `store`, `secrets`, `postgres`, `schema`
  y `service`, con tests. Verificado contra Postgres 18, 17, 16 y 14.
- ✅ **S01 Welcome** — sin la opción "Open SQLite file".
- ✅ **S02 Connection manager** — completa.
- ✅ **S03 Connection editor** — solo tab General. SSH, TLS, Safety y Advanced
  quedan como placeholders deshabilitados.
- ✅ **S24 Confirmation dialogs** — variante "connection error", que lleva al
  campo que hay que arreglar según la causa del fallo.
- ✅ **S05** — árbol de esquema con tablas de Postgres únicamente.

**Postergado a la Iteración 9**, aunque el diseño de S01, S02 y S05 lo muestre.
Todo lo de abajo necesita el mismo pedazo que todavía no existe: el estado local
en SQLite de `%APPDATA%`, que es lo que guarda qué pasó en esta máquina y no se
sincroniza con la libreta de conexiones.

| Qué | Dónde | Por qué espera |
|---|---|---|
| Lista de recientes | S01, S02 | Necesita registrar la última apertura por conexión. Va al estado local, no al archivo sincronizado: cuándo abriste algo es de esta máquina |
| Panel "última sesión" | S02 | Ídem: versión del servidor y sentencias aplicadas de la vez anterior |
| "Abrir una base" | S02 | Lista las bases del servidor de la última conexión. Sin estado local hay que conectarse antes, y entonces la lista deja de tener sentido ahí |
| Historial y consultas guardadas | S05 | Las dos pestañas del sidebar. Es literalmente la S21 |

**Fuera de alcance sin fecha**, porque no las pide el plan y no se pierde nada:
la detección de motores locales de S01 —escanear puertos, aunque sea en
localhost, es alcance que nadie pidió—, el import de conexiones y las carpetas
de S02.

### Iteración 2 — SQL básico

CodeMirror con autocompletado de esquema, ejecución con cancelación, grilla solo
lectura.

- ✅ **Contrato de Go** — `query` (resultado independiente del motor),
  `postgres.Run` con lote completo, `postgres.TableData`/`TableCount`/
  `PrimaryKeyColumns`, y el servicio `Queries` con cancelación por `runID`.
  Solo lectura y `statement_timeout` los hace cumplir el servidor.
- ✅ **S06 SQL editor** — editor, autocompletado contra el esquema real,
  ejecución y cancelación, estados corriendo/listo/error, pestaña Mensajes con
  el tag de cada sentencia. Sin Explain, Format, guardar consulta ni envolver en
  transacción: son de iteraciones posteriores y están deshabilitados.
- ✅ **S07 Results grid** — solo lectura, con NULL contra cadena vacía, etiquetas
  de tipo y de clave, orden contra el servidor. Sin modo edición.
- ⏳ **S09 Cell viewer** — modos por tipo: JSON formateado, hex para bytea, texto
  y el caso null. Falta el modo "Items" de arrays, que necesita un parser de
  literales de Postgres y va en Go cuando la Iteración 7 lo use para editar.
- ✅ **S10 Table data tab** — grilla, orden, "cargar más", conteo exacto y aviso
  cuando la tabla no tiene clave primaria.
- ✅ **Solo lectura** — el interruptor se adelantó desde la Iteración 5. Ver el
  registro de decisiones.

### Iteración 3 — SSH

Túnel integrado, `known_hosts` con TOFU, soporte ssh-agent.

- ✅ **Contrato de Go** — `internal/tunnel`: inspección de la clave del host sin
  autenticar, `known_hosts` propio con TOFU, los tres métodos de autenticación,
  y discado a través del túnel **sin abrir ningún puerto local**.
- ✅ **S03** — pestaña de túnel SSH. Sin el campo "Local port" del diseño: ver el
  registro de decisiones.
- ✅ **S04 SSH host key verification** — completa, incluida la variante de clave
  cambiada.
- ✅ **S24** — variante de túnel caído: el fallo dice que el túnel se cerró en
  vez de listar causas posibles.

### Iteración 4 — ERD lectura

> **Verificado el 2026-09-08 — Atlas NO las soporta.** Las tres se probaron
> contra una base 18 real: Atlas regenera la columna `VIRTUAL` como `STORED`
> (SQL válida, tabla distinta), emite `PRIMARY KEY USING gist (...)` para la
> clave temporal (error de sintaxis) y pierde el `NOT VALID` del `NOT NULL`.
> Detalle y consecuencias en § 6.

Introspección propia → modelo → canvas xyflow, auto-layout, posiciones
persistidas por conexión. **Atlas no entra en esta iteración**: el ERD de solo
lectura no genera DDL, y los datos que necesita ya salen del catálogo con la SQL
que tenemos. Atlas se evalúa en la Iteración 5, que es donde el differ se gana
el lugar.

- ✅ **S12 ERD canvas** — sin edición: nodos con columnas e indicadores, aristas
  con cardinalidad, zoom, auto-layout, búsqueda, inspector, toggles de densidad,
  esconder tablas y posiciones persistidas por conexión y esquema.
- ✅ **S11 Table structure** — solo lectura (Estructura, Índices, Claves
  foráneas, Restricciones, Triggers).

Tres diferencias con el enunciado original, decididas contra el diseño y
anotadas en § 6: **leyenda en vez de minimapa**, **un acomodado guardado en vez
de saved views con nombre**, y **sin «Copiar CREATE TABLE»**, que se pospone a la
Iteración 5 junto con el resto de la generación de DDL.

### Iteración 5 — ERD escritura

Edición → changeset pendiente → **renderizado propio** → preview SQL → aplicar →
re-inspeccionar. **Sin differ**: ver § 6.

- ✅ **S13 ERD edit interactions** — modo edición explícito con cinco
  herramientas, los cambios pendientes pintados sobre el diagrama, y el panel
  derecho mostrando el changeset.
- ✅ **S14 Pending changes panel** — completa.
- ✅ **S15 SQL preview & apply** — DDL numerado, marcado de destructivos,
  progreso por sentencia. Sin banners MySQL/SQLite y sin dry run todavía.
- ✅ **S11** — editable: columnas (agregar, renombrar, nulabilidad, sacar
  default, borrar), índices, claves foráneas y restricciones CHECK, todo
  alimentando el changeset.
- ✅ **S24** — variantes de confirmación: descartar el changeset, y preparar un
  cambio destructivo contra producción.

**Hito usable: Postgres completo de punta a punta.**

Queda fuera, con motivo anotado en § 6: **reordenar las sentencias a mano** (el
diseño lo ofrece arrastrando) y los **toasts** de S24, que son una preocupación
global y no de esta iteración.

Probada a mano contra `docker/demo.sql` el 2026-09-09; las siete correcciones
que salieron de ahí están en § 6.

### Iteración 6 — Otros motores

MySQL, MariaDB y SQLite por el mismo pipeline, **cada uno contra su última
versión estable**, igual que Postgres. Verificar cuál es en su momento en vez de
asumir: el default del `docker-compose.test.yml` se elige ahí, y la matriz de CI
cubre la última más las anteriores que se declaren soportadas.

- **S03** — variantes de engine.
- **S15** — banners de DDL no transaccional (MySQL) y de rebuild de tabla
  (SQLite); dry run en transacción para Postgres.
- **S01** — "Open SQLite file".

### Iteración 7 — Grilla editable

Edición de celdas con preview de `UPDATE`/`DELETE`, import/export CSV, y todo
lo que entra y sale de la grilla.

- **S07** — modo edición: celdas modificadas, filas nuevas, filas marcadas para
  borrar, tablas sin PK en solo lectura con explicación.
- **S08 Data change review** — completa, incluida la variante de producción.
- **S18 CSV import wizard** — completa. `COPY … FROM STDIN` de pgx hace el
  trabajo; lo caro es el asistente —mapear columnas, tipos, NULL contra cadena
  vacía, encoding, y qué hacer con las filas que no entran—, que es justamente
  por qué es una pantalla y no un botón.
- **S19 Export dialog** — completa, **una tabla y varias**. Varias no es otra
  función: es la misma en un bucle más un selector. Lo que hay que decidir es el
  formato del conjunto (un directorio de CSVs, un `.sql` con INSERTs, un zip), y
  eso se decide con el diseño delante, no acá.
- **Exportar el resultado del editor SQL** — CSV, JSON y Markdown. No estaba en
  el plan y es lo más barato de todo el grupo: el resultado ya está en memoria,
  así que es formateo puro, sin consulta ni streaming. Reusa el renderizador de
  S19 y se hace primero, porque es lo que valida el formato antes de meterlo en
  el camino difícil.
- **Ver la fila entera como JSON** — hoy S09 formatea JSON de UNA celda. La fila
  completa es un ítem del menú contextual y se resuelve del lado del servidor
  con `row_to_json`.
- **Filtros por columna en la grilla** — un constructor de `WHERE` sobre la
  tabla que se está mirando, que hoy obliga a irse al editor SQL. Va acá porque
  comparte pantalla y modelo con la edición de celdas.

**Sobre el volumen.** Exportar no puede juntar la tabla en memoria: una tabla de
dos millones de filas no pasa por un `[][]string`. Se escribe al archivo a
medida que llega, y para CSV conviene `COPY … TO STDOUT` (`PgConn().CopyTo`),
que es órdenes de magnitud más rápido que paginar con `LIMIT/OFFSET`.

### Iteración 8 — Objetos de texto

Vistas, funciones, procedures, triggers y enums como editor de definición.

- **S16 Object editor** — completa, con lista de dependientes y aviso de
  DROP + CREATE.
- **S17 Enum / type editor** — completa.
- **S05** — nodos de vistas, materialized views, funciones, procedures, triggers,
  enums y sequences en el árbol.

### Iteración 9 — Pulido (continuo)

Historial, atajos, drift check, builds Linux/macOS, firma de código.

- **S22 Command palette** — primero: es lo que más se usa.
- **S21 Query history / saved queries** — segundo.
- **S20 Drift check** — reutiliza S15 para la SQL de reconciliación.
- **S23 Settings** — completa, incluido el panel de seguridad y el check de
  updates manual.
- **S03** — tabs TLS y Advanced.
- **S24** — variante "unsaved changes on tab close".
- Tema claro de S05, S06, S12 y S15 — al final, no al principio.
- **Automatizar los bumps de dependencias.** Hoy CI avisa qué se puede subir
  (job `deps`) pero alguien tiene que leerlo y actuar. Dependabot y Renovate
  abren PRs solos; son funciones de la plataforma, no telemetría de la app, así
  que no chocan con la regla de no phone-home. **Buscar en el momento si hay una
  alternativa mejor**: para cuando lleguemos, el panorama puede haber cambiado y
  elegir hoy una herramienta para dentro de seis iteraciones es elegir a ciegas.

---

## 2. Stack

Versiones marcadas con ✅ están instaladas y verificadas en la máquina de
desarrollo (windows/arm64). El resto entra en la iteración que lo necesite.

### Instalado

| Pieza | Versión | Nota |
|---|---|---|
| Go | ✅ 1.26.8 | Un solo módulo. `CGO_ENABLED=0`, sin gcc |
| Wails | ✅ v3.0.0-beta.17 | Bindings Go↔JS, sin servidor HTTP |
| React | ✅ 19.2.8 | Con React Compiler 1.0.0, verificado activo |
| TypeScript | ✅ 7.0.2 | Puerto nativo en Go, es el `latest` de npm |
| Vite | ✅ 8.2.2 | `@vitejs/plugin-react` 6.1.1 |
| pnpm | ✅ 10.33.2 | No npm. Lockfile commiteado |
| pgx | ✅ v5.11.0 | Único driver por ahora. `pgConn.Exec` para lotes |
| x/crypto/ssh | ✅ v0.56.0 | Túnel sin abrir puertos locales |
| go-keyring | ✅ v0.2.8 | Secretos, MIT |
| CodeMirror 6 | ✅ | `@codemirror/lang-sql` 6.10.0 |
| TanStack Table + Virtual | ✅ 9.2.4 / 3.14.10 | Sobre CSS Grid propio |
| @xyflow/react | ✅ 12.11.6 | Canvas ERD. MIT, nodos DOM |
| @dagrejs/dagre | ✅ 3.1.1 | Auto-acomodado por capas. MIT |

### Pendiente por iteración

- **Diff / plan de migración (Iteración 5):** sin decidir. `ariga.io/atlas`
  v1.3.0 es el candidato pero **no soporta tres features de PostgreSQL 18 y una
  falla en silencio** — ver § 6. Alternativas a evaluar ahí: Atlas con un
  guardarraíl que se niegue a generar DDL para lo que no sabe leer, un planificador
  propio para el conjunto acotado de cambios que produce el editor de ERD, o
  [sqldef](https://github.com/sqldef/sqldef). El gate queda como programa
  reproducible y se vuelve a correr antes de decidir.
- **Introspección:** SQL propia contra el catálogo. No hace falta Atlas: las
  vistas materializadas, procedures, triggers, secuencias y RLS son de su plan Pro
  y había que escribirlas igual.
- **Drivers pendientes:** `go-sql-driver/mysql`, `modernc.org/sqlite` v1.58.0
  (verificado en arm64).
- **Estado local** (historial, config): en `%APPDATA%`, sin secretos. Las
  posiciones del ERD ya no van acá: viven en `layouts/`, al lado de la libreta de
  conexiones, para que se sincronicen con ella.
- **Estado UI:** Zustand, cuando haga falta. Ojo: xyflow trae `zustand@4`, así que
  agregar la 5 deja dos copias de ~1 KB. Es más barato que atarse a la mayor vieja.

### CI

GitHub Actions. Hoy: matriz win-x64 (`windows-latest`) y win-arm64
(`windows-11-arm`), un job de gofmt + vet + test + typecheck, y una matriz de
integración contra PostgreSQL 18, 17, 16 y 14 en `ubuntu-latest` con
`KANAME_REQUIRE_POSTGRES=1`, para que un job sin base se ponga rojo en vez de
verde. Linux y macOS se suman en la Iteración 9. El build usa el pipeline de `wails3 task`, no
`go build` a mano — ver el registro de decisiones.

---

## 3. Qué cambiaría de las decisiones iniciales

- **Wails v2 → v3.** v3 está en beta con la API estable desde agosto de 2026; v2
  queda en mantenimiento. No arranques un proyecto nuevo ahí.
- **El servidor HTTP local es un agujero en el propio requisito de seguridad.**
  Cualquier pestaña del navegador puede pegarle a `127.0.0.1:puerto` (y DNS
  rebinding salta CORS). Con credenciales de producción detrás, no cierra. Con
  bindings de Wails v3 no hay socket, y te ahorrás diseñar una API REST entera.
  Si igual querés modo navegador para desarrollar: token aleatorio por arranque +
  validación de `Origin`.
- **Atlas no puede ser el modelo canónico de todo.** Vistas, materialized views,
  procedures, funciones, triggers, sequences, composite types, domain types,
  extensions y RLS de Postgres son features del plan Pro. Vas a tener dos modelos
  sí o sí. Definí un `SchemaSnapshot` propio que envuelva al `schema.Realm`; la UI
  nunca toca tipos de Atlas. Si el open-core se mueve, cambiás un adaptador y no
  el frontend.
- **"Se reemplazan enteros con `CREATE OR REPLACE`" es cierto a medias.** En
  Postgres, `CREATE OR REPLACE VIEW` no puede quitar columnas ni cambiar tipos, y
  `... FUNCTION` no puede cambiar el tipo de retorno. MySQL no tiene
  `CREATE OR REPLACE` para triggers ni procedures (MariaDB sí). Hace falta una
  estrategia `DROP + CREATE` con detección de dependientes.
- **DDL no transaccional en MySQL/MariaDB.** Postgres hace rollback si falla a
  mitad del plan; MySQL deja el esquema a medio aplicar. El preview tiene que
  avisarlo y aplicar sentencia por sentencia mostrando hasta dónde llegó.
- **SQLite y `ALTER`.** Casi todo cambio de columna es rebuild de tabla. Atlas lo
  maneja, pero verificá que el plan que muestra sea el que aplica.
- **Ruido de normalización.** Después de aplicar, al re-inspeccionar Atlas puede
  ver `varchar(255)` vs `character varying(255)` y proponer diffs espurios.
  Normalizá el lado "deseado" antes de diffear y testeá que el ciclo
  inspect → apply → inspect dé vacío.
- **Sincronizar conexiones entre las dos máquinas.** El keychain no sincroniza;
  aceptalo. Config sin secretos en un archivo (JSON/TOML) sincronizado con
  Syncthing o un repo git privado; las contraseñas se cargan una vez por máquina;
  las claves SSH se referencian por path y viven en `~/.ssh` de cada máquina,
  idealmente con agente. Un export/import cifrado con `age` + passphrase queda
  para después si hace falta.
- **ELK.js** es EPL-2.0 (weak copyleft, no te ata, pero es una licencia más que
  auditar) y pesa. Para ERDs, dagre alcanza.
- **"Paginación por cursor"** para queries arbitrarias no existe (keyset exige
  orden único). Streaming con `LIMIT`+`OFFSET` o corte por cantidad de filas y
  "cargar más". Cancelar en MySQL requiere `KILL QUERY` desde una segunda conexión.
- **Descartar Rust fue correcto.** No lo vuelvas a mirar.

### Auditoría de dependencias

- ✅ **Atlas no arrastra `cloud/` ni `cmd/`.** Verificado con `go list -deps` sobre
  Atlas v1.3.0 importando los cuatro paquetes que necesitamos: linkea únicamente
  `sql/schema`, `sql/postgres`, `sql/mysql`, `sql/sqlite`, `schemahcl`,
  `sql/migrate`, `sql/sqlclient` y sus internos. Cero update-check.
- ✅ **`modernc.org/sqlite` v1.58.0 anda en windows/arm64.** No solo compila:
  corre (SQLite 3.53.4), sin cgo, y cross-compila a amd64. Riesgo descartado.
- ✅ **Sin sockets.** Se removió del template de Wails el modo servidor HTTP
  (`build:server`, `run:server`, `build:docker`). Ver registro de decisiones.
- WebView2 (Windows) se actualiza y reporta a Microsoft por su cuenta, y no lo
  controlás desde la app. Es el precio de no embeber Chromium.
- El resto (xyflow, CodeMirror, TanStack Virtual, Wails, pgx) no hace phone-home.
- ✅ **glide-data-grid revisada y descartada** (2026-09-08). No declara React 19
  en la versión estable y el repo lleva meses quieto. Ver el registro de
  decisiones. En su lugar, CSS Grid propio virtualizado con
  `@tanstack/react-virtual`: MIT, peer con React 19, un solo paquete.
- **`minimum-release-age` de 7 días** activo en `frontend/.npmrc`: pnpm rechaza
  paquetes publicados hace menos de una semana. Es defensa contra supply chain y
  no se desactiva. Cuando bloquee una versión, se baja a la anterior elegible; si
  de verdad hace falta la nueva, se agrega una excepción puntual y justificada en
  el mismo archivo.

---

## 4. Qué falta / casos borde

- Marcar conexiones como "producción" con confirmación extra y color distinto;
  modo solo lectura por conexión
- Clasificar cambios destructivos (`DROP COLUMN`, cambio de tipo con pérdida) en
  el preview, con conteo de filas afectadas
- Dry-run en transacción con rollback en Postgres antes de aplicar en serio
- TLS/sslmode para la conexión a la base, no solo SSH
- Múltiples schemas / `search_path` en Postgres; múltiples databases por conexión
  en MySQL
- Tablas sin PK → grilla no editable
- Control de transacción manual en el editor (autocommit on/off)
- NULL vs string vacío, timezones, bytea/JSON grandes en la grilla
- Filtrado del ERD: un esquema de 200 tablas no se dibuja entero; vistas guardadas
  del diagrama
- Undo/redo del changeset pendiente antes de aplicar
- Reconexión del túnel SSH y timeouts
- Firma de código en Windows: sin firmar, SmartScreen frena el "copiar y que ande"
- Wails en Linux/macOS necesita cgo: no vas a cross-compilar desde Windows, va por CI
- Tests de integración con Docker Compose de los cuatro motores desde el día uno;
  el differ se rompe en silencio
- Exportar ERD a SQL y a imagen

---

### Fuera de alcance, con motivo

Cosas que se evaluaron y **no** se van a hacer. Se anotan para no volver a
discutirlas desde cero dentro de seis meses.

**Backup y restauración nativos. No.** Un backup de verdad de PostgreSQL es
`pg_dump`/`pg_restore`, binarios externos que tienen que ser iguales o más
nuevos que el servidor. Las salidas posibles son tres y ninguna sirve:

1. *Llamarlos si están instalados.* Funciona, pero mete una dependencia que el
   usuario tiene que conseguir y una matriz de versiones por motor que es un
   pozo de soporte.
2. *Empaquetarlos.* Choca de frente con «el binario se distribuye copiando y
   pegando»: son cuatro motores por varias versiones cada uno.
3. *Reimplementar el dump en Go.* Eso **es** `pg_dump`: orden de dependencias,
   extensiones, secuencias, permisos, objetos grandes. No hay librería en la que
   confiaría para la mitad que importa, que es restaurar.

Y hay una cuarta que es la peligrosa: exportar esquema y datos con nuestro
propio renderizador y **llamarlo backup**. Parecería que funciona hasta el día
que hace falta. Un botón que dice «Backup» promete que se puede restaurar; si no
cumple, es peor que no estar.

Lo que sí se puede ofrecer con honestidad es **«Exportar el esquema como SQL»**
—ya sabemos generar DDL, es el mismo renderizador del changeset— dejando escrito
que no es un backup. Para backups de verdad, `pg_dump` desde la terminal, que
además puede ir por el túnel SSH que la aplicación ya sabe levantar.

**Constructor visual de consultas entre varias tablas. No por ahora.** Elegir
tres tablas, unirlas y filtrar sin escribir SQL es un producto adentro de este:
resolver joins, alias, ambigüedad de nombres y agregaciones en una interfaz. Lo
que la gente casi siempre quiere cuando lo pide —filtrar lo que está mirando—
está cubierto por los filtros por columna de la Iteración 7. Si después de
usarlo la necesidad sigue en pie, se reconsidera con un caso concreto delante.

---

## 5. Forma más simple

- **Postgres solo hasta el hito usable.** Atlas te da los otros tres casi gratis
  después; hacerlos antes multiplica bugs.
- **Sin grilla editable en v1.** El editor SQL ya cubre el `UPDATE` con preview
  implícito: lo escribís vos.
- **Sin API HTTP.** Bindings de Wails y listo.
- **Sin sync propio.** Archivo de config + Syncthing.
- **Objetos de texto al final.** Son un editor con un botón "aplicar"; no bloquean
  nada.

Con eso el proyecto son cuatro piezas: introspección, canvas, differ (prestado),
preview/apply. Todo lo demás es agregable cuando ya lo estés usando.

---

## 6. Registro de decisiones

Toda decisión técnica que no se deduzca del código va acá, con fecha y motivo.
Se anota **cuando se toma**, no al final de la iteración.

### Iteración 5 — correcciones de uso — 2026-09-09

Todo esto salió de probar la iteración a mano contra `docker/demo.sql`. Ninguno
lo habría encontrado un test de los que había: cuatro de los cinco son estados
que solo existen cuando alguien usa dos pantallas seguidas.

**Un error de sentencia no se explica con el vocabulario de una conexión.** El
apply clasificaba sus fallos con `postgres.Classify`, que contesta «¿por qué no
llegué al servidor?». Un `SET NOT NULL` sobre una columna con nulos —un 23502
perfectamente identificado, con el nombre de la tabla y de la columna adentro—
salía como **«No se pudo conectar con usuario@host:5432/base»**. Además de
inútil, es falso: el servidor contestó.

Había dos bugs encimados y el segundo tapaba al primero. `Apply` guardaba el
texto del error y después lo reconstruía con `errors.New` para clasificarlo, lo
que tira el `*pgconn.PgError`: el clasificador nunca veía un SQLSTATE, así que
*ningún* código llegaba a interpretarse y todo caía en el mismo cajón. Ahora
`correr` devuelve el error crudo junto al resultado, y lo interpreta
`postgres.ClassifyStatement`, que conoce treinta SQLSTATE de DDL y, cuando no
conoce uno, contesta por la clase del código en vez de rendirse.

Lo que cambia para quien lo lee: el mensaje nombra la columna, el `DETAIL` de
PostgreSQL da el valor que chocó —`(codigo)=(AAA)`— y el Hint trae la consulta
para encontrar las filas culpables. Es la diferencia entre saber que falló y
saber dónde mirar en una tabla de doce mil filas.

**Lo que quedó aplicado tiene que salir de la lista de pendientes.** `Apply`
vaciaba el changeset solo si el apply completo salía bien, así que el único caso
donde la lista y la base pueden discrepar —sin transacción, falla la tercera de
cinco— era justo el que quedaba mal. No es cosmético: al reintentar, las dos
primeras se vuelven a correr y un `ADD COLUMN` que ya corrió hace fallar el
conjunto entero por algo que ya estaba hecho. Ahora se mira sentencia por
sentencia, y `RolledBack` decide: con transacción única una sentencia puede
haber corrido bien y aun así no existir más.

**«Refrescar» tiene que refrescar lo que se está mirando.** Releía el árbol del
esquema y dejaba la pestaña de la tabla mostrando el catálogo de antes; la única
salida era cerrarla y volver a abrirla. Las pestañas quedan montadas y
escondidas a propósito —para no perder lo que tienen adentro—, y eso es
exactamente lo que las dejaba viejas. Ahora el Shell lleva un contador de
recarga que sube con el botón y **también después de un apply que dejó algo
aplicado**, y las pestañas lo miran.

Por lo mismo, la pantalla de cambios pendientes leía el changeset al montarse y
nunca más: volver a ella mostraba la lista de cuando se abrió, muchas veces
vacía. Ahora relee al volverse la pestaña activa.

**El editor de claves no ofrecía las columnas preparadas.** El orden natural es
crear la columna y después colgarla de otra tabla; la lista salía del catálogo,
que no la tiene ni la va a tener hasta aplicar. Kaname ya sabía ordenar las
sentencias para que ese orden funcione — lo que faltaba era poder pedirlo. Ahora
las columnas y las tablas preparadas aparecen marcadas como pendientes.

**El desplegable estiraba el diálogo.** La lista del `Combobox` estaba
posicionada dentro de un cuerpo con `overflow: auto`, así que contaba como
contenido: crecía el alto del diálogo y aparecía una segunda barra. Va en un
portal sobre `<body>`, con coordenadas de pantalla, alto máximo según el espacio
que de verdad hay y apertura hacia arriba cuando abajo no entra.

**El botón decía «Agregar» y agregaba a una lista.** En el alta de columna, el
único de los tres diálogos que no decía «Preparar». Prometía que la columna
quedaba puesta, y quien lo aprieta después no entiende por qué no está en la
tabla.

**`0A000` es un cajón, no un error.** Probando salió `ADD COLUMN cliente_id
bigint DEFAULT id`, que PostgreSQL rechaza con *«cannot use column reference in
DEFAULT expression»*. El código `0A000` cubre por lo menos tres cosas que se
arreglan de tres maneras distintas —default que nombra una columna, default que
es una subconsulta, y cambiar el tipo de una columna que usa una vista— así que
dejarlas con la misma frase no ayuda a ninguna. Se separan mirando el texto del
mensaje, que es frágil, y por eso se hace al final y degrada al mensaje general
si no coincide. Lo mismo con el `42601`, que PostgreSQL usa además para «esa
columna es generada»: darle el mensaje de error de sintaxis acusaba a Kaname de
un bug que no existe.

Y del lado de la interfaz: un valor por defecto que es una palabra suelta es casi
siempre el nombre de otra columna. Ahora se avisa al escribirlo, sin bloquear
—`current_date` es una palabra suelta y es válida—, para no descubrirlo recién al
aplicar con el changeset entero armado.

**Una columna generada no tiene «valor por defecto» que sacar.** PostgreSQL
guarda su expresión en `pg_attrdef`, el mismo lugar que un default, así que
`DetailColumn.Default` venía lleno y el menú ofrecía «Sacar el valor por
defecto» sobre `demo.pedidos.total`. La opción queda deshabilitada con el
motivo. Es la regla de siempre: una operación que el motor va a rechazar no se
ofrece, en vez de ofrecerla y explicar el error después.

**Cerrar pestañas con el botón del medio.** Lo que hace cualquier navegador o
editor. El `preventDefault` en `mousedown` no es opcional en Windows: sin él, el
sistema entra en modo autoscroll y deja el cursor de las flechitas dando vueltas.

### Iteración 5 — 2026-09-09

**No hay differ, y el motivo es más fuerte que el gate de Atlas.** El diseño de
S14 lo dejó a la vista: **cada cambio pendiente ya es una sentencia**, no un
modelo deseado. Un differ existe para averiguar la diferencia entre dos
esquemas; acá quien edita la hizo a mano, operación por operación. Pasarla por
un differ obliga a reconstruir el esquema entero, entregárselo y aceptar su
interpretación de vuelta — que es exactamente por donde se pierde lo que su
modelo no sabe representar.

El segundo argumento está en la misma pantalla: la lista mezcla cambios de datos
y de esquema, en una transacción y un solo apply. Atlas no tiene nada que decir
sobre un `UPDATE`, así que construir esto alrededor de un differ significaría dos
tuberías en paralelo y fusionarlas en la Iteración 7.

Lo que se gana es estructural y no una verificación más: **una operación que no
sabemos escribir es una operación que la interfaz no ofrece**. Son diecisiete, y
agregar una capacidad obliga a agregar su renderizado en cada motor a la vez.

Atlas sigue siendo candidato para los dos lugares donde el problema SÍ es un
diff: los rebuilds de tabla de **SQLite** en la Iteración 6 y el **drift check**
de S20 en la 9. El gate se volvió a correr el 2026-09-09 —sigue en v1.3.0, mismo
resultado— y se vuelve a correr antes de decidir.

**El test del renderizado no compara SQL contra una cadena esperada.** Renderiza,
EJECUTA contra Postgres y vuelve a leer el catálogo para verificar que quedó lo
que se pidió. Una sentencia puede verse perfecta, correr sin error y dejar otra
cosa: es literalmente lo que hace Atlas con las columnas VIRTUAL.

**PostgreSQL trunca los identificadores de más de 63 bytes sin avisar.** Lo
encontró el test de inyección, que fallaba porque la columna «no existía»: se
había truncado. Setenta caracteres entran, sesenta y tres salen, ningún error.
Es un renombrado silencioso, y dos nombres largos distintos pueden colisionar. Se
rechazan antes de generar la sentencia.

**La confirmación de producción pide el nombre de LA BASE, no el de la conexión.**
El de la conexión lo eligió quien la configuró y puede ser «prod» en las dos
máquinas; el de la base es lo que de verdad se va a modificar. Y se verifica del
lado de Go: una comprobación que vive solo en la interfaz no es una protección,
es un cartel.

**Nada se ejecuta si alguna sentencia del conjunto no se puede escribir.**
Aplicar la mitad de un changeset porque la otra mitad no compila es la peor
combinación posible.

**El `statement_timeout` de la conexión no se sube por dentro.** Un `ALTER` que
reescribe una tabla grande puede tardar más que el corte, y quedarse a medias en
un DDL es peor que no empezar — pero subirlo en silencio sacaría una protección
que alguien puso a propósito. El changeset avisa y quien aplica decide.

**El progreso se consulta, no se recibe.** No hay eventos de Wails en este
proyecto y un apply que puede tardar minutos no puede ser una espera sin
información. Preguntar cada doscientos milisegundos cuesta leer tres campos bajo
un lock.

**El tipo de una columna se elige de una lista LEÍDA DEL CATÁLOGO, no escrita
en el código.** La primera versión era texto libre, que se tipea mal. Pero una
lista fija habría sido peor: los tipos de PostgreSQL no son un conjunto cerrado
—cada extensión agrega los suyos, y cada enum o dominio definido en esa base es
uno más—, así que no habría forma de elegir un tipo propio. Se leen de
`pg_type`, con los de la base primero, que son los que nadie recuerda de memoria.

Tres detalles que hacen la diferencia:

- El **modificador** —el `(10,2)` de `numeric`— va en un campo aparte y solo
  para los tipos que lo admiten (`typmodin <> 0`). Es la parte que más se
  escribe mal, y separarla deja que el nombre del tipo venga siempre de la lista.
- Se **acepta lo que se escriba** aunque no esté listado. Una lista cerrada
  sería más prolija y a veces impediría usar un tipo instalado después de
  conectar.
- Se muestra **cómo va a quedar** el tipo completo antes de aceptar, armado de
  las tres partes. Ver `numeric(10.2)` en pantalla es más barato que descubrirlo
  en el error de Postgres.

Hizo falta un `Combobox` con búsqueda en el design system: el `<select>` nativo
ignora el tema en Windows y además no busca, y son cientos de tipos.

**Una verificación mía volvió a estar mal, y esta vez lo dijo un test verde.** Al
inyectar las cinco violaciones del apply, cuatro pusieron su test en rojo y la
del rollback quedó verde. La causa no era el test: inyecté en el lugar
equivocado. Saqué el corte temprano al fallar una sentencia, que NO es lo que da
la garantía — Postgres aborta la transacción por su cuenta y el `COMMIT` falla
igual. La inyección que sí prueba la invariante es sacar la transacción y correr
contra el pool; ahí el test se pone rojo con el mensaje exacto. **Inyectar en el
lugar equivocado da un falso «este test no sirve».**

**El modo edición del diagrama es explícito, no un estado en el que se cae.**
Un diagrama que se está mirando y uno que se está editando tienen que
distinguirse antes del primer clic: con las herramientas siempre activas, tocar
una tabla para leerla podría preparar un `DROP`. Entrar en edición es un botón, y
la tira de herramientas solo existe adentro.

Las cinco herramientas son las del diseño. La de relación usa conectores **por
columna**, que aparecen solo con esa herramienta elegida: arrastrar de una
columna a la que referencia es literalmente lo que la clave foránea dice, y con
conectores permanentes la tarjeta se llenaría de puntos que no hacen nada el 95%
del tiempo.

**Los cambios pendientes se pintan sobre el diagrama.** Una columna agregada, una
cambiada y una que se va llevan `+`, `~` y `−` además del color: verde y rojo no
alcanzan solos, y la que se borra va tachada. Las tablas nuevas se dibujan aunque
todavía no existan —si no, la clave nueva que las apunta no tendría de dónde
salir— y las relaciones nuevas y las que se borran tienen su propio color y su
etiqueta con palabras.

**No se puede reordenar las sentencias a mano**, aunque el diseño lo ofrezca
arrastrando. El orden lo calcula Kaname por dependencias, y un orden elegido a
dedo puede no poder ejecutarse: la escapatoria para dejar algo afuera es
destildarlo, que no puede producir un conjunto inválido.

**La confirmación de producción se pide DOS veces, y no es redundante.** Al
preparar un cambio destructivo y al aplicar el conjunto. Son preguntas distintas:
«¿de verdad querés borrar esta columna?» se contesta mirando la columna, y «¿de
verdad querés correr estas ocho sentencias?» mirando la lista. Contestar la
segunda no contesta la primera.

La regla vive en `Session.Stage`, no en las pantallas. Escrita en cinco lugares
es una regla que alguna pantalla nueva se va a olvidar de aplicar; escrita en Go,
una pantalla nueva queda protegida sin acordarse de nada. La interfaz solo
intenta, y si Go dice que falta confirmar, pregunta y reintenta.

**Descartar el changeset ahora pregunta.** Era un botón que borraba el trabajo de
un clic. La confirmación aclara lo que importa: no se pierde ningún dato —nada se
aplicó—, pero las ediciones hay que rehacerlas.

**Un `<datalist>` nativo se me coló en el editor de claves foráneas.** Elegir la
tabla referenciada abría el desplegable del sistema operativo, que ignora el tema
y no se parece a nada del resto. Es exactamente lo que la regla de no usar
elementos nativos existe para evitar, y se me pasó porque `<input list>` se
escribe como un input común. Ahora usa el `Combobox`, igual que el tipo de
columna y el tipo de la clave primaria: si un campo elige entre opciones
conocidas, elige con el mismo componente en toda la aplicación.

**Dos veces me llevé puesto un archivo con un script de parcheo.** `io.open(p,
"w")` trunca ANTES de validar sus argumentos, así que un `newline` inválido dejó
`ErdScreen.tsx` en cero bytes. Se recuperó de git las dos veces, pero la lección
quedó: los scripts que tocan archivos del repo escriben a un temporal y hacen
`os.replace`. Y para archivos grandes de JSX, edición directa en vez de scripts.

### Iteración 4 — 2026-09-08

**Atlas no ve las features de esquema de PostgreSQL 18.** El gate que pedía esta
iteración se corrió contra `ariga.io/atlas v1.3.0` y una base 18 real: se crean
las tres cosas, se introspeccionan con Atlas y se le pide el DDL que generaría
para recrearlas. Resultado, las tres fallan, y no todas del mismo modo:

| Feature | Qué hace Atlas | Gravedad |
|---|---|---|
| `GENERATED ALWAYS AS (…) VIRTUAL` | La regenera como `STORED` | **Silenciosa.** La SQL es válida y aplica sin error; la tabla resultante es otra. |
| `PRIMARY KEY (id, rango WITHOUT OVERLAPS)` | Emite `PRIMARY KEY USING gist (…)` | Ruidosa: es error de sintaxis en Postgres. |
| `ADD CONSTRAINT … NOT NULL col NOT VALID` | Reporta la columna como `NOT NULL` a secas | Silenciosa: Atlas cree que el dato ya cumple cuando puede no cumplir. |

No es un bug de borde: `VIRTUAL` no aparece en todo el paquete `sql/postgres`,
`WITHOUT OVERLAPS` no aparece en todo el módulo, y `sqlspec.go` tiene literal
`func generatedType(string) string { return "STORED" }` — una función que ignora
su argumento. La información *sí* sobrevive a la introspección en un caso: Atlas
guarda el operador `&&` de la parte `WITHOUT OVERLAPS` del índice. Lo que falta
es el generador de DDL.

Consecuencias:

- **Iteración 4 no usa Atlas.** El ERD de solo lectura no genera DDL, así que
  nada de esto lo afecta; y los datos que necesita —claves foráneas con sus
  acciones, índices, constraints, triggers— salen del catálogo con la SQL que ya
  tenemos. Meter una dependencia grande para después mapearla igual a
  `schema.Snapshot` no compra nada.
- **Iteración 5 arranca con un guardarraíl, no con confianza.** Antes de aplicar
  cualquier DDL generado, la introspección tiene que marcar los objetos que usan
  estas features y la ruta de apply tiene que **negarse y explicar**, no
  intentar. Un preview que muestra SQL válida y equivocada es peor que un error.
- **Re-verificar en la Iteración 5**, no dar por sentado este resultado: Atlas
  llegó a v1.0 y sigue activo. El gate queda como programa reproducible, no como
  una nota.

**El esquema se lee en dos niveles, no en uno.** Las claves foráneas viajan en
el `Snapshot` entero porque son las aristas del ERD y el diagrama las dibuja
todas juntas: pedirlas tabla por tabla serían tantos viajes como tablas antes de
mostrar la primera línea. Todo lo demás —índices, constraints, triggers,
defaults, comentarios, tamaños— va en un `TableDetail` que se pide por tabla y a
demanda. De doscientas tablas eso es un orden de magnitud más de datos que el
árbol, y la pantalla de estructura muestra una por vez.

**El detalle de una tabla son siete consultas en un solo lote.** No es
microoptimización: con un túnel SSH de por medio cada viaje cuesta la latencia
completa hasta el bastión, y siete viajes secuenciales contra un servidor a 50 ms
son 350 ms de nada antes de la primera fila. El precio es que los resultados hay
que consumirlos en el orden en que se encolaron.

**Tres cosas del catálogo que no eran lo que parecían**, las tres encontradas por
un test rojo y no leyendo documentación:

- `pg_get_indexdef(oid, columna, true)` devuelve **solo la expresión** de la
  columna: nunca el `DESC` ni el `NULLS`. Eso está en `pg_index.indoption`, un
  vector de bits que —a diferencia del resto de los arrays de Postgres— se
  **indexa desde cero**. Sin esto, un índice descendente se ve igual que uno
  ascendente y parece servir para un `ORDER BY` que no cubre.
- `pg_total_relation_size` **no recorre el árbol de particiones**. Sobre el padre
  de una tabla particionada de 400 GB devuelve el tamaño del padre, que está
  vacío. Hay que sumar `pg_partition_tree`.
- Las columnas de `INCLUDE` van en un campo aparte de las claves. Mezclarlas hace
  ver una columna incluida como si fuera clave, que es lo que lleva a creer que
  un índice sirve para un `WHERE` que no cubre.

**La introspección marca las features que Atlas no ve** —`Generated: "virtual"` y
`NotNullNotValid`— aunque la Iteración 4 no las use para nada. Es lo que le va a
permitir a la Iteración 5 negarse a generar DDL en vez de romper en silencio: el
dato hay que tenerlo antes de necesitarlo.

**xyflow y dagre siguen siendo la elección correcta, y esta vez se comprobó.**
`@xyflow/react` 12.11.6 y `@dagrejs/dagre` 3.1.1 están vivos —el primero publicó
hace una semana, el segundo hace un mes—, los dos MIT. No es el caso de
glide-data-grid. De las alternativas, JointJS quedó abandonada en npm con ese
nombre (último release 2023), GoJS, yFiles y Syncfusion son comerciales, y X6
está viva pero dibuja los nodos en SVG.

Lo que decide es lo mismo que decidió la grilla: **en xyflow cada nodo es un
componente de React que se dibuja como DOM**, así que la tarjeta de tabla usa
nuestros tokens y el tema claro de la Iteración 9 la va a alcanzar sin escribir
una línea. En un canvas habría que releer los colores desde JavaScript y
repintar a mano.

ELK acomoda mejor que dagre —puertos de verdad, ruteo ortogonal— y pesa 7,7 MB
de Java transpilado. Contra la regla de cuidar el tamaño y el arranque en frío,
no cierra. Si dagre falla en un esquema real se revisa con evidencia, no antes.

Aviso para el futuro: el paquete `reactflow` a secas es el nombre viejo y está
muerto desde 2024. El vivo es `@xyflow/react`. Y xyflow arrastra `zustand@4`; el
día que agreguemos Zustand para nuestro estado van a convivir dos copias de
~1 KB, que es más barato que atarnos a la mayor vieja.

**Las líneas del diagrama calculan su propia geometría.** Con conectores fijos,
arrastrar una tabla a la izquierda de otra deja la línea saliendo por el lado
equivocado y cruzando la tarjeta entera. Leyendo las posiciones desde la propia
arista, elige el lado que mira al otro nodo y se reacomoda mientras se arrastra.
Y sale de la FILA de la columna, no del medio de la tarjeta: en una tabla con
tres claves foráneas, tres líneas naciendo del mismo punto no dicen cuál es cuál.

**Tres diferencias con el diseño y el enunciado, decididas a propósito:**

- **Leyenda en vez de minimapa.** El plan pedía minimapa; el diseño pone una
  leyenda en ese mismo rincón. En un ERD la leyenda informa más —qué significa
  punteada, qué significa naranja— y el minimapa sobra cuando el panel derecho
  ya lista todas las tablas y lleva a cualquiera de un clic.
- **Un acomodado guardado, no vistas con nombre.** El diseño dice «layout: saved
  locally», en singular. Las vistas con nombre se agregan cuando alguien las
  pida; guardar el acomodado es lo que evita rehacer trabajo.
- **Sin «Copiar CREATE TABLE».** Postgres no tiene `pg_get_tabledef`, así que hay
  que componerlo, y una clave primaria temporal con `WITHOUT OVERLAPS` saldría
  como una clave común: SQL válida y una tabla distinta. Es exactamente el fallo
  que este mismo registro documenta como inaceptable en Atlas. Hacerlo bien es un
  generador de DDL con test de ida y vuelta y un camino de negarse para lo que no
  sabe representar: maquinaria de la Iteración 5, que conviene construir una sola
  vez y ahí.

**La pata de gallo estaba dibujada al revés, y la línea la atravesaba.** Dos
cosas distintas que se veían como una sola fealdad, las dos encontradas mirando
el diagrama y no el código:

- El marcador que venía del diseño es una flecha cuyo vértice toca la tarjeta y
  cuyos brazos se abren hacia afuera: apunta *hacia adentro* de la tabla. La pata
  de gallo de verdad es al revés — los **tres dedos tocan la caja** y el vértice
  queda sobre la línea. Es la notación estándar y además es la que se lee: los
  tres dedos «en» la tabla son literalmente el «muchos».
- Los marcadores de SVG se dibujan ENCIMA del trazo, pero el trazo sigue llegando
  hasta donde termina. Así que la línea corría por debajo de los dedos y tocaba
  el recuadro igual. La línea arranca ahora `RETIRO_PATA` píxeles afuera del
  borde y el marcador hace el último tramo; el anclaje del marcador va en el
  vértice y no en los dedos para que encaje. Los dos números tienen que
  coincidir, y por eso están comentados uno en función del otro.

**Las líneas eligen lado por la separación entre CAJAS, no entre centros.** Dos
tablas apiladas una arriba de la otra tienen los centros casi alineados en
horizontal, así que unirlas por los costados obliga a la línea a salir, bajar por
afuera y volver a entrar. Comparando el hueco entre las cajas en cada eje —hueco
negativo significa que se solapan— la línea corta derecho por arriba o por abajo
cuando corresponde. Por los costados sigue apuntando a la fila de la columna; por
arriba y abajo sale del centro, porque ahí la coordenada que manda es la
horizontal y la fila no tiene dónde expresarse.

**Dos cosas que se veían iguales y no lo son.** Dos claves entre las mismas dos
tablas casi siempre apuntan a la misma columna del otro lado —`autor` y `revisor`
van los dos a `id`—, así que salían de filas distintas pero convergían en un
punto y el último tramo quedaba superpuesto: ahora se reparten alrededor del
centro, corriendo LAS DOS puntas (correr una sola las cruza en vez de
separarlas). Y una clave compuesta es *una* relación, así que se dibuja con *una*
línea, indistinguible de una de una columna: lleva una etiqueta con las columnas,
solo cuando son más de una, para no llenar de ruido las simples.

**`hidden` no funcionaba en toda la aplicación.** El `display: none` que trae el
atributo es la regla de menor prioridad que existe, así que cualquier clase con
`display` lo anula sin que nadie se entere: el elemento se sigue viendo *y* queda
escondido para los lectores de pantalla. Se refuerza una vez en los tokens. Salió
al plegar paneles, pero estaba latente para cualquiera que usara el atributo.

**Los paneles se reabren desde el borde por el que se fueron**, con una franja de
18 px que ocupa el lugar que dejó el panel. Los enlaces en la barra de estado se
sacaron: reabrir un panel desde el otro extremo de la ventana no se le ocurre a
nadie.

**«Diagrama» abre el primer esquema CON TABLAS**, no el primero a secas. El
snapshot pone `public` primero porque es donde está casi todo, pero en una base
donde no se usa queda vacío y el diagrama abría en blanco.

**«sin analizar» no significa «vacía», y se leía así.** Es que a esa tabla nunca
se le corrió `ANALYZE`, así que el planificador no tiene estimación: puede tener
millones de filas. La que sí está vacía dice «vacía». La etiqueta es correcta y
demasiado corta para explicarse, así que `TreeRow` ganó un `metaTitle`.

**Lo primero que encontró CI en Linux fue una funcion a medio hacer, no un test
caprichoso.** `expandirRuta` acepta el prefijo `~` con barra invertida en TODAS
las plataformas a proposito: el archivo de conexiones se sincroniza, asi que una
ruta configurada en Windows se abre en Linux. Pero traducia solo el prefijo, no
el resto — y en Linux la barra invertida no separa nada, es un caracter valido de
un nombre de archivo. La ruta terminaba apuntando a un archivo llamado
`.ssh` mas barra invertida mas `id_ed25519`, que no existe. Aceptar el prefijo
sin traducir el resto es media funcion.

La traduccion se hace SOLO cuando la ruta empieza con la tilde de Windows, que es
sintaxis inequivoca. Una ruta con tilde y barra normal conserva sus barras
invertidas: en Linux son parte del nombre y cambiarlas lo romperia. Hay un caso
de test para cada una de las dos formas de equivocarse, y las dos se verificaron
corriendo la suite dentro de un contenedor de Linux — en Windows ninguno de los
dos casos puede distinguir nada, porque las dos barras separan igual.

Vale la pena anotar de donde salio: **de la pata de Linux de CI, que es la unica
que puede verlo.** La maquina de desarrollo es Windows y ahi el test pasa con la
funcion rota. Es exactamente para esto que la matriz corre en los dos sistemas.

**Las posiciones del diagrama van al lado de la libreta de conexiones**, no en el
directorio de estado. Es el mismo razonamiento que puso ahí la libreta: acomodar
cuarenta tablas es trabajo, y quien sincroniza sus conexiones entre máquinas con
Syncthing no quiere volver a hacerlo del otro lado. Un archivo JSON por conexión
—JSON y no TOML porque lo escribe la máquina y un mapa de coordenadas en TOML es
incómodo de leer—, con escritura atómica igual que las conexiones.

El identificador de conexión **se valida aunque hoy lo genere la propia
aplicación**: termina siendo un nombre de archivo y llega desde el proceso de la
interfaz, así que un `../..` ahí escribiría donde quisiera. Hay un test que
prueba diez formas de salirse del directorio.

**La revisión de código encontró ocho cosas y las ocho eran ciertas.** Cuatro
serias, y vale anotar el patrón: **tres de las cuatro son el mismo error repetido
en otro lugar.**

- `ReferencedBy` no decía qué tabla declara la clave. En las claves entrantes el
  destino es siempre la tabla que se está mirando, así que sin el dueño dos
  tablas apuntando a la misma con restricciones del mismo nombre —legal— daban
  dos filas idénticas.
- `fkEntrantesQuery` no excluía particiones. El filtro **existía** en la consulta
  del snapshot y faltaba en esta: una tabla referenciada desde otra particionada
  en cincuenta meses aparecía con cincuenta y una claves idénticas.
- `a.attnum = ANY(i.indkey)` marcaba como clave primaria a las columnas de
  `INCLUDE`. Estaba **en dos consultas**: la del detalle, nueva, y la del
  snapshot, desde la Iteración 1, alimentando las etiquetas PK de la grilla. Al
  arreglarlo casi meto la pata de nuevo: `(indkey::int2[])[1:n]` parece lo
  correcto y devuelve **la columna equivocada**, porque `int2vector` empieza en
  cero y el casteo conserva ese límite inferior. Se agarró probándolo contra la
  base.
- `pg_relation_size` daba 0 para los índices de una tabla particionada. Es el
  mismo problema que ya se había resuelto para el tamaño de la tabla, un campo
  más arriba, sin ver que se repetía.

Las otras cuatro, menores y también ciertas: un índice inválido —el que deja un
`CREATE INDEX CONCURRENTLY` que falló— se veía igual que uno bueno; dos lecturas
superpuestas de la estructura podían dejar datos viejos con hora nueva; la
pastilla decía «Estructura 0» mientras el snapshot no había llegado; y un
comentario decía seis consultas donde hay siete.

**Los cinco invariantes nuevos se verificaron rompiéndolos.** Orden de columnas
de una clave compuesta, cálculo de `Optional`, suma del árbol de particiones,
detección del `NOT NULL NOT VALID` y separación de `INCLUDE`: se inyectó la
violación de cada uno y se confirmó que su test se pone rojo. Los cinco arreglos de la revisión tienen su propio test y se
verificaron igual, reinyectando cada bug. Lo que importa es que ninguno es un
test que no puede fallar.

### Iteración 3 — 2026-09-08

**Las pantallas de carga las hizo necesarias el túnel.**
Conectar directo tarda milisegundos y nadie extraña una pantalla de espera. Por
un bastión son varios segundos —TCP, handshake SSH, autenticación, y recién ahí
Postgres— y sin nada en pantalla la aplicación parece colgada. El contador de
segundos no es decoración: es lo que distingue "está tardando" de "se colgó".

El botón de cancelar corta de verdad, no esconde la pantalla: las llamadas que
genera Wails se pueden cancelar y eso corta el contexto del lado de Go, que
tanto `tunnel.Dial` como `postgres.Connect` respetan.

Vale anotar de dónde salió: no estaba en el plan de ninguna iteración. Apareció
porque una función nueva volvió lenta una operación que antes era instantánea, y
recién ahí se notó que nunca había habido una espera que mostrar.

**El túnel no abre ningún puerto local.**
Un túnel SSH se implementa habitualmente escuchando en `127.0.0.1` y
reenviando. Este proyecto no puede: un puerto en loopback es alcanzable desde
cualquier pestaña del navegador, que es la misma razón por la que no hay
servidor HTTP. Con credenciales de producción del otro lado, no cierra. pgx
acepta una función de discado propia, así que la conexión a la base viaja por
dentro del canal SSH y existe solo dentro del proceso.

Un test funcional no distingue las dos implementaciones —las dos conectan— así
que la garantía es estructural: un test lee los archivos del paquete y falla si
aparece un `net.Listen`.

Por lo mismo, la pestaña de S03 **no tiene el campo "Local port"** que muestra
el diseño. Ese campo existía porque el diseño asumía la implementación clásica.
En vez de dejarlo sin hacer nada, la pantalla explica la diferencia.

**Se verifica la clave del host antes de mandar ninguna credencial.**
El protocolo SSH intercambia y verifica la clave del host ANTES de la
autenticación, así que abortar en la devolución de llamada de la clave garantiza
que no viajó nada. Es lo que permite que S04 diga "todavía no se envió ninguna
credencial" y sea cierto, y no una promesa de la interfaz. El test lo prueba con
credenciales deliberadamente inválidas: si la inspección autenticara, fallaría.

**La clave viaja en la inspección y vuelve para guardarse.**
La primera versión reconectaba para confiar. Dos motivos para no hacerlo. El de
corrección: garantiza que se guarda exactamente la clave cuya huella se mostró;
reconectar abría una ventana en la que el servidor podía presentar otra. Y el
que enseñó el servidor de pruebas: OpenSSH 9.8 penaliza a quien se conecta sin
intentar autenticarse (`PerSourcePenalties`, activado por default), así que dos
conexiones sin autenticar seguidas hacían que el bastión empezara a rechazar.

**`known_hosts` es propio, no el de OpenSSH.**
Escribir en `~/.ssh/known_hosts` es meterse con configuración que otras
herramientas también usan. Con archivo propio, las decisiones de confianza de
Kaname se auditan en un solo lugar. El formato sí es el de OpenSSH, así que se
lee con `ssh-keygen -F`. Lleva la fecha en el comentario de cada línea, porque
el diálogo de clave cambiada la muestra y el formato no tiene campo de fecha.

**pgx resuelve los nombres del lado equivocado, y hay que decírselo.**
Resuelve el host ANTES de llamar a la función de discado, así que un nombre que
solo existe desde el bastión —el caso normal de un túnel— fallaba con "no such
host". `LookupFunc` se instala junto con `DialFunc` y no como opción aparte:
quien ponga una sin la otra se come un error que no se parece a la causa.

**El fallo dice si fue el túnel, en vez de listar sospechosos.**
Un túnel muerto se manifiesta como un error de red de la base. El diseño de S24
dice "el servidor puede estar caído, o el túnel puede haberse cerrado". El
cliente SSH avisa cuando la conexión termina, así que se puede saber cuál de las
dos y decirlo. Se consulta después de un fallo y no antes de cada operación:
sondear en cada consulta agregaría trabajo a todas para atajar un caso raro.

**El servidor SSH de pruebas se construye acá.**
La imagen de terceros que se probó primero trae `AllowTcpForwarding no` con el
`Include` comentado, así que no hay forma de sobrescribirlo desde afuera. Con
Dockerfile propio el `sshd_config` está a la vista y se revisa en el diff.

**La ruta del pipe del agente en Windows tenía una barra de menos.**
`\.\pipe\...` en vez de `\\.\pipe\...`. El pipe no existe nunca con esa ruta,
así que el método por defecto no funcionaba y el error decía "¿está corriendo el
servicio ssh-agent?" con el servicio arrancado. Lo encontró el test del agente en
su primera corrida contra un agente real. Vale anotar la limitación: sin agente
ese test se saltea, no falla, así que en CI no se habría detectado nunca.

---

### Iteración 3 — 2026-09-08

**Wails se queda en beta.17 por ahora.** El informe de versiones detectó
beta.18 el mismo día en que salió. Se revisó y no se sube, por tres motivos que
se suman: es una release nocturna automática, no curada; su único cambio es una
pérdida de memoria de `Calloc` **en Linux y Darwin**, y hoy solo distribuimos
Windows; y subir Wails obliga a mover `@wailsio/runtime` en el mismo paso,
porque si las versiones no coinciden exacto los bindings generados no matchean
el runtime.

Además, cambiar el framework de la ventana justo antes de arrancar el túnel SSH
mezcla dos fuentes de problemas: si algo se rompe, no se sabe cuál fue.

Se revisa de nuevo cuando lleguen los builds de Linux y macOS —ahí el arreglo sí
nos toca— o si aparece uno que afecte a Windows. Que exista una versión nueva no
es motivo para subir; el informe es para decidir.

**Leer el esquema tolera que borren tablas mientras lee.**
`has_table_privilege` devuelve NULL —no `false`— cuando el OID ya no existe, y
entre que `pg_class` lista una tabla y se evalúa su permiso, otra sesión puede
haberla borrado. Escanear eso a `bool` hacía fallar la introspección entera con
`cannot scan NULL into *bool`, un mensaje que no se parece en nada a "alguien
borró una tabla". Ahora se escanea a puntero y la fila se omite: un fantasma en
el árbol es peor que una tabla de menos, porque al hacerle clic da un error que
no explica nada.

Lo encontraron los tests de integración corriendo en paralelo contra la misma
base, no un test escrito para eso. Vale anotarlo: la paralelización de `go test`
entre paquetes no es solo velocidad — es el único lugar del proyecto donde algo
concurrente golpea la base al mismo tiempo que otra cosa.

**CI tiene dos señales de dependencias, y responden preguntas distintas.**
`govulncheck` rompe el build: dice "esto hay que arreglarlo", y solo cuenta las
vulnerabilidades alcanzables desde nuestro código, así que un CVE en una función
que no llamamos no rompe nada por gusto. El job `deps` no bloquea: dice "esto se
puede mejorar" —bugs, rendimiento, versiones atrasadas— y escribe un informe en
el resumen de la corrida.

Hacían falta las dos. Pinear todo, que es lo que este proyecto hace, deja de ser
prudente y pasa a ser negligente si nadie mira nunca si lo pineado envejeció. El
caret del frontend no resolvía eso: con lockfile commiteado y `--frozen-lockfile`
en CI, un caret no actualiza nada hasta que alguien corre `pnpm update` a mano.
Daba la ilusión de actualización automática sin darla.

**El toolchain de Go sube a 1.26.8.** En su primera corrida, `govulncheck`
encontró 19 vulnerabilidades alcanzables y las 19 eran de la biblioteca estándar
por estar en 1.26.0. Se sube dentro de la línea 1.26 —los parches son solo
correcciones— y no a 1.27, que es un cambio de lenguaje que merece su propia
decisión. Después del bump: cero.

El informe de versiones se filtra a dependencias **directas**. Con `all`, el
grafo entero trae las dependencias del CLI de Wails, que ni siquiera se linkean
en el binario: doce líneas de ruido antes de la primera accionable, y un informe
que no se lee no sirve de nada.

---

### Iteración 2 — 2026-09-08

**El interruptor de solo lectura se adelanta de la Iteración 5 a la 2.**
El plan ponía toda la tab Safety en la 5, junto con el ERD que escribe. Pero lo
primero que puede escribir en la base es el editor SQL, que es de esta
iteración: eran tres iteraciones con la protección existiendo y sin forma de
activarla salvo editando `connections.toml` a mano. Se adelantó solo ese
interruptor, no la tab entera.

**La etiqueta de clave sale del esquema, no del resultado.**
El encabezado del diseño distingue PK y FK. Un resultado de consulta no puede
saberlo —`select 1 as id` devuelve un entero que no es clave de nada—, así que
la grilla recibe las claves de afuera y solo cuando se está mirando una tabla
concreta. En el editor SQL las columnas se etiquetan por tipo, que es todo lo
que ahí se puede afirmar con certeza.

**El visor de celda no descompone arrays todavía.**
Partir `{a,"b,c",NULL}` en elementos tiene casos borde con comillas y escapes.
Escribirlo en TypeScript sería lógica sin pruebas, que es exactamente el error
que este proyecto ya cometió una vez hoy. El array se muestra crudo hasta que la
Iteración 7 necesite editarlo elemento por elemento, y ahí el parseo va en Go
con sus tests. Mostrar el literal es correcto; mostrarlo mal partido, no.

**Solo lectura y statement_timeout los hace cumplir el servidor, no nosotros.**
Van como parámetros del paquete de arranque de cada conexión del pool
—`default_transaction_read_only` y `statement_timeout`—, no como un guard por
consulta. La diferencia es la que importa: un guard cubre los caminos que nos
acordamos de cubrir, y `default_transaction_read_only` cubre también los que
todavía no escribimos. El corte por tiempo, además, sigue vigente aunque la app
se cuelgue o se cierre; cancelar desde el cliente depende de que el cliente siga
vivo. Verificado con el motor: un INSERT sobre una conexión de solo lectura
vuelve con 25006, y `pg_sleep(10)` con timeout de 300 ms vuelve con 57014.

**Cancelar es por identificador de ejecución, no global.**
El editor puede tener varias pestañas corriendo. Con un solo `cancel`
compartido, apretar cancelar en una pestaña corta la consulta de otra — y de
forma intermitente, que es la peor manera de tener un error. La interfaz elige
un `runID` y `Cancel(runID)` corta esa y nada más.

**Citar identificadores siempre, no "cuando hace falta".**
Decidir caso por caso se equivoca: `order` es reservada, `Mi Tabla` tiene
espacio, `año` no es ASCII y `a"b` es un nombre válido. El test lo prueba contra
el motor con una tabla cuyo nombre es `raro"; drop table "<esq>"."victima" --`,
armado para que la SQL resultante sea válida y destructiva si el citado falla —
un nombre que rompe el parser haría fallar el test por el motivo equivocado. Con
el citado roto, la víctima desaparece; con el citado bien, no.

**El paginado dice cuándo no es confiable.**
Sin ORDER BY, LIMIT/OFFSET no define qué filas devuelve: el motor puede entregar
la misma fila en dos páginas y saltearse otra. `TableData` no inventa un orden;
el servicio resuelve la clave primaria y devuelve `OrderedBy`. Vacío significa
que la tabla no tiene clave y que "cargar más" es aproximado — y la interfaz
tiene que decirlo, no taparlo.

**La grilla no es una librería de grilla: es CSS Grid propio más virtualización.**
Descartada `glide-data-grid`, que era lo que decía el plan: su estable declara
`react: ^16 || 17 || 18`, React 19 solo está en una alpha sin promover desde
junio de 2025, el repo no recibe un push desde enero de 2026, y arrastra
`lodash`, `marked` y `react-responsive-carousel` como peers. Sobre todo, dibuja
en canvas: los colores se pintarían desde JS y acá salen solo de tokens CSS, de
los que dependen el tema claro y los acentos por entorno.

El reemplazo elegido primero fue `react-data-grid`, y se revirtió el mismo día al
leer los diseños. Vale anotarlo así: se eligió la librería antes de mirar la
pantalla, y la pantalla cambió la respuesta.

S07 y S10 son literalmente un `display:grid`. Anchos por columna en
`grid-template-columns`, filas de 25px, el canal `#` de 38px con su propio fondo,
y cada celda pintada con tokens: `--cell-null` en itálica, `--cell-modified` con
outline `--warning`, `--cell-deleted` tachada, `--bg-stripe` en las impares.
Encima, popovers anclados por coordenada de fila y columna. Una librería de
grilla no ahorra nada de eso — hay que pelearle su DOM y sus clases para llegar
al mismo lugar.

TanStack Table v9, que ya es estable (9.2.4), tampoco entra, por una razón que
solo se ve mirando el diseño: el contador dice `1,000 of 12,481 rows · filtered`.
El orden y el filtro son SQL, no estado del cliente. Los modelos de sorting,
filtering y pagination de TanStack son justo lo que no vamos a usar.

Lo único genuinamente difícil es virtualizar: mil filas por ocho columnas son
ocho mil nodos, y con "cargar más" crece. Eso lo resuelve
`@tanstack/react-virtual` —MIT, peer con React 19, un solo paquete— sin opinar
sobre el marcado. Al instalarlo, `minimum-release-age` bajó de 3.14.11 a 3.14.10
por sí solo: la defensa de supply chain está viva y no es decorativa.

Desvío deliberado del diseño: el encabezado va dentro del contenedor con scroll y
`position:sticky`, no como hermano de arriba. En el mock las columnas entran
justas y no hay scroll horizontal; con cuarenta columnas sí lo hay, y con el
encabezado afuera se desincroniza.

---

### Iteración 1 — 2026-09-08

**El contrato de Go va antes que las pantallas, y en paquetes puros primero.**
`internal/connection` no toca disco, red ni keychain: modelo, validación y DSN.
Encima van `internal/store` (TOML), `internal/secrets` (keychain) e
`internal/postgres` (conexión). Cada capa se prueba sola.

**TOML con `github.com/BurntSushi/toml`, cero dependencias transitivas.**
El archivo de conexiones se sincroniza entre máquinas y se edita a mano, así que
tiene que diffear limpio y admitir comentarios —JSON no admite—. TOML 1.0 es una
especificación congelada, de modo que la cadencia baja de releases de esa
librería no es un riesgo, y a cambio no suma nada que auditar.

**La contraseña se indexa por ID de conexión, no por nombre.**
El nombre cambia y el ID no: renombrar una conexión no puede dejar una
credencial huérfana en el keychain. De ahí que el ID sea obligatorio y que el
store rechace el archivo entero si encuentra uno vacío o repetido.

**Los errores de conexión se clasifican, no se muestran crudos.**
"No se pudo conectar" obliga al usuario a adivinar entre un host mal escrito,
una contraseña vieja y un firewall, y cada uno se arregla en un lugar distinto.
`postgres.Classify` traduce SQLSTATE y errores de red a una causa y una
sugerencia. Los errores tipados ganan siempre sobre la heurística de texto.

**Todo texto de origen ajeno pasa por `postgres.Redact`.**
Un test demostró que el mensaje del servidor puede citar una cadena de conexión
completa. No se puede asumir que quien escribió un mensaje de error tuvo
cuidado, así que se enmascara la contraseña y se conserva el usuario, que sirve
para diagnosticar y no es secreto.

**Postgres 18 es el objetivo, no 17.** 18.6 es la estable a septiembre de 2026 y
la 19 está en beta. El mínimo soportado es 14, la más vieja con soporte oficial.
`docker-compose.test.yml` toma la versión de `PG_VERSION` para que CI corra la
matriz.

**Los tests de integración se saltean, no se excluyen con un build tag.**
Con tag, el código de test no compila en el día a día y un error ahí se
descubre tarde. Sin tag, siempre pasa por `vet` y se saltea con un mensaje que
dice cómo levantar la base.

**Pegar una URI avisa qué parámetros se descartan.**
El DSN se rearma desde los campos de la conexión, así que de la cadena pegada
solo sobrevive `sslmode`. `channel_binding`, `connect_timeout`, `options` y
compañía se perdían en silencio, y la cadena que entregan los proveedores
alojados los trae de fábrica. `channel_binding=require` tiene aviso propio
porque es el único descarte que cambia la seguridad: pgx lo sigue negociando
—su default es `prefer`—, lo que se pierde es *fallar* cuando el servidor no lo
ofrece, que es justamente la defensa contra un intermediario que lo saque de la
lista anunciada. Guardarlos como campos propios queda pendiente; el aviso es lo
que impide que la conexión guardada sea distinta de la que el usuario pegó sin
que nada se lo diga.

**Un control que no controla se dibuja como estado, no como interruptor.**
El panel de protecciones de S02 mostraba cuatro switches de 28x16 con perilla.
Los valores existen y el backend los respeta, pero todavía no se editan desde
ahí —la tab Safety es de la Iteración 5—, así que el switch prometía un clic que
no hacía nada. El usuario lo apretó y concluyó, con razón, que la app estaba
rota. Ahora es un punto: informa sin ofrecer. La forma de un control es una
promesa, y una promesa que no se cumple cuesta más que la función que falta.

De paso quedó a la vista un agujero de orden en el plan: lo primero que puede
escribir en la base es el editor SQL de la Iteración 2, y el interruptor de solo
lectura está agendado para la 5. Mientras tanto se edita en `connections.toml`,
que para eso se eligió un formato que se edita a mano.

**El paquete `main` solo se compila donde tiene sentido compilarlo.**
El primer push puso todo en rojo, y por dos causas distintas que se destaparon
una después de la otra. La primera: `pattern all:frontend/dist: no matching
files found`. `main` embebe el frontend construido y `frontend/dist/` es un
artefacto ignorado, así que en un clon limpio `go vet ./...` y `go test ./...`
ni siquiera compilan `main`. El checklist local no lo detecta nunca, porque en
la máquina de desarrollo el directorio quedó de un `wails3 task build` anterior
— una verificación que solo pasa por tener basura previa no verifica nada. El
job `check` crea un `.gitkeep` vacío antes de los comandos de Go; el prefijo
`all:` hace que un archivo con punto cuente como coincidencia. Commitear ese
marcador habría sido más corto y peor: un `dist` vacío en el repo deja que
`go build` produzca un binario que abre una ventana en blanco, sin avisar.

La segunda apareció recién con la primera arreglada: en `ubuntu-latest`,
compilar `main` arrastra el backend GTK4/WebKitGTK de Wails y `pkg-config` no
los encuentra. Instalarlos en la matriz de Postgres no probaría nada — el
binario de Linux recién se arma en la Iteración 9 — así que el job de
integración corre `./internal/...` y no `./...`. Se puede porque `internal/` no
importa Wails por ningún lado (`go list -deps ./internal/... | grep wails` da
cero): la regla de mantener los bindings en su propio paquete se paga acá, en
poder probar toda la lógica en cualquier sistema operativo. Que `main` compile
lo cubre `check`, en Windows, que es donde el binario existe.

---

### Iteración 0 — 2026-09-07

**No se puede verificar la UI capturando la ventana desde un script.**
La superficie del WebView2 se compone por GPU: `CopyFromScreen` devuelve negro, y
`PrintWindow` con `PW_RENDERFULLCONTENT` sí devuelve píxeles pero **sin escalar**.
Peor: un script de PowerShell no es consciente de DPI, así que `GetWindowRect`
devuelve coordenadas virtualizadas —1440×900 en vez de los 1800×1125 reales a
125 %— y el bitmap sale con el tamaño equivocado. El contenido real se vuelca 1:1
en un lienzo un 20 % más chico y parece que la app recorta la interfaz. No
recorta nada.

Si hace falta medir la ventana desde un script, primero
`SetThreadDpiAwarenessContext(-4)`. Y para verificar la UI, dos caminos que sí
sirven: medir el DOM en Chrome contra el `dist` de producción
(`pnpm preview` + `getBoundingClientRect`), y preguntarle al humano qué ve. Los
números cierran: cliente real 1782 px físicos ÷ 1.25 = 1426, exactamente el
viewport CSS que reporta el webview.

**Fuentes autohospedadas, no Google Fonts.**
Los artboards cargan Inter y JetBrains Mono desde `fonts.googleapis.com`, pero el
propio S00 aclara que viajan con la app. Un `<link>` a un CDN es una petición
saliente en cada arranque —IP, User-Agent, horario— y rompe sin internet. Se
usan los `.woff2` de `@fontsource-variable/*`, con `@font-face` propios para
incluir solo los subconjuntos latin y latin-ext: 189 KB en vez de los ~700 KB
que entrarían con cirílico, griego y vietnamita.

**CSS Modules, sin librería de estilos.**
Alcanzan y no agregan dependencias. Los tokens quedan como variables CSS, que es
como los define el diseño. `noUncheckedIndexedAccess` los tipa como
`string | undefined`, así que hay un helper `cx()` en `src/lib`; la flag se
mantiene porque es la que va a cuidar los accesos por índice de la grilla.

**El contrato de Go va antes que la pantalla, también para About.**
`internal/appinfo` expone versión, plataforma y rutas, con tests que además
verifican que ninguna ruta huela a secreto. S25 no tiene ni una constante propia
sobre el binario: no puede mentir sobre lo que está corriendo.

**Se omite de S25 lo que todavía no existe.**
El artboard muestra `v0.4.2`, "28 MB installed", rutas `~/.config/kaname/` y 40
atajos de teclado. Nada de eso es cierto hoy. La pantalla usa los datos reales
del binding, marca motores y plataformas con su estado verdadero, y deja la
sección de atajos vacía con una explicación. Se documenta cada atajo cuando la
función que dispara exista. La grilla "All screens" del artboard es navegación
del canvas de diseño, no de la app.


**Nombre del repo: `kanamedb`; producto: Kaname.**
`kaname` solo es inbuscable (colisiona con la palabra japonesa y con personajes
de anime). El guión de `kaname-db` lo hace sonar a componente de otra cosa. La
categoría entera usa un solo token: chartdb, dbeaver, duckdb, surrealdb. El
título de ventana, el binario y `build/config.yml` siguen diciendo "Kaname".

Módulo Go: `github.com/LucianoR23/kanamedb`. Repo privado, acceso por SSH.

**Fuera el modo servidor HTTP del template de Wails.**
Wails v3 trae de fábrica `build:server`, `run:server` y `build:docker`, que
levantan la app como servidor HTTP sin GUI. Es exactamente el agujero descrito
en la sección 3. Se borraron las tareas y `build/docker/`. También se removió el
scaffolding de Android e iOS: no es plataforma objetivo y es superficie muerta.

**El build de producción va por `wails3 task build`, no por `go build`.**
`go build` a mano omite dos cosas que importan: el tag `production` —sin él el
webview queda en modo desarrollo— y el `.syso` con ícono, manifest de DPI y
metadata de versión. El comando real es
`wails3 task build ARCH=amd64|arm64`, que encadena tidy, frontend, iconos, syso
y `go build -tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui"`.
Salida siempre en `bin/kaname.exe`; CI la renombra por arquitectura.

**TypeScript 7.0.2, no 5.x.**
El puerto nativo en Go es el `latest` de npm desde julio de 2026 y trae binario
para `win32-arm64`. Mismas semánticas de lenguaje, chequeo mucho más rápido.
`tsc --noEmit` para typecheck; el transpilado lo hace esbuild vía Vite.

**pnpm con `minimum-release-age=10080`, y una sola excepción.**
El filtro rechaza paquetes publicados hace menos de 7 días. Obligó a usar
`@types/react-dom` 19.2.5 en vez de 19.2.7, que es el precio correcto a pagar.
La única exclusión es `@wailsio/runtime`: su versión tiene que coincidir exacto
con la del módulo Go `github.com/wailsapp/wails/v3` o los bindings generados no
matchean el runtime. Está anotada y justificada en `frontend/.npmrc`.

**React Compiler verificado, no asumido.**
`babel-plugin-react-compiler` 1.0.0 activo en `vite.config.ts`. Se comprobó
compilando un componente memoizable y confirmando que aparece `_c()` en el
bundle. Consecuencia práctica: no se usan `useMemo`, `useCallback` ni
`React.memo` por defecto.

**Commits sin atribución de herramienta.**
Nada de `Co-Authored-By` ni enlaces de sesión en los mensajes de commit ni en
las descripciones de PR. Anotado también en `CLAUDE.md`.

**Sin licencia por ahora. El repo no se publica hasta estar probado y seguro.**
Sin archivo `LICENSE`, el default legal es "todos los derechos reservados", que
es lo correcto para código privado. Las dependencias no condicionan la elección:
Atlas es Apache 2.0, Wails y pgx MIT, `modernc.org/sqlite` BSD-3 — todas
permisivas, ninguna copyleft. Cuando se decida publicar, la recomendación es
Apache 2.0: concesión explícita de patentes (pesa en una herramienta que planifica
migraciones) y cláusula de marcas que preserva el nombre "Kaname" ante un fork.

Consecuencia que aplica desde hoy: **si el repo se publica, se publica el
historial completo.** Un secreto commiteado ahora sigue en el historial aunque se
borre en el commit siguiente. La regla de no commitear credenciales, rutas de
claves ni datos de conexiones reales no es higiene: es irreversible.

### Checklist de pre-publicación

No es una lista para "algún día": es la condición para que el repo pase a
público. Nada se marca por confianza, todo con evidencia.

- [ ] Auditoría del historial completo en busca de secretos, no solo del árbol
      actual (`gitleaks detect --log-opts=--all`, como job de CI).
- [ ] Verificar que ningún log, mensaje de error ni evento hacia el frontend
      contenga credenciales, connection strings ni valores de filas.
- [ ] Confirmar que los secretos viven solo en el keychain y que el SQLite de
      estado local y el archivo de config no tienen ninguno.
- [ ] Confirmar que la app no abre ningún socket en ninguna configuración.
- [ ] Revisar que `known_hosts` haga TOFU real y que no exista ninguna ruta con
      `InsecureIgnoreHostKey`.
- [ ] Tests de integración de los cuatro motores en verde.
- [ ] Elegir y agregar la licencia.

---

## Cómo ejecutar cada iteración

1. Pasarle a Claude Code solo los `design/Sxx-*` de esa iteración, diciendo
   explícitamente qué parte de la pantalla entra y qué no.
2. Antes de la UI, definir el contrato: qué datos muestra la pantalla y qué
   acciones dispara. Eso es el binding Go de la iteración; se prueba desde un
   test, no desde React.
3. Recién después, implementar la pantalla contra ese binding, con datos reales.
4. La iteración cierra cuando la usás con una base tuya, no cuando "se ve igual al
   diseño".

Dos reglas que aplican a todas:

- **El plan se actualiza en el momento.** Cada decisión técnica va al registro de
  la sección 6 apenas se toma, y el estado de las pantallas se marca en la
  sección 1. Un plan desactualizado miente peor que no tener plan.
- **El README acompaña.** Lo que cambie en cómo se instala, se corre o se
  construye el proyecto se refleja en `README.md` en el mismo commit.
