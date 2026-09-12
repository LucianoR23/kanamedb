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
- ✅ **S01 Welcome** — sin la opción "Open SQLite file", que llega en la
  Iteración 6.
- ✅ **S02 Connection manager** — completa.
- ✅ **S03 Connection editor** — en la Iteración 1 solo la tab General; SSH
  llegó en la 3 y TLS, Safety y Advanced en la 9. Hoy están las cinco.
- ✅ **S24 Confirmation dialogs** — variante "connection error", que lleva al
  campo que hay que arreglar según la causa del fallo.
- ✅ **S05** — árbol de esquema con tablas de Postgres únicamente.

**Postergado a la Iteración 9**, aunque el diseño de S01, S02 y S05 lo muestre.
Todo lo de abajo necesita el mismo pedazo que todavía no existe: el estado local
de `%APPDATA%`, que es lo que guarda qué pasó en esta máquina y no se sincroniza
con la libreta de conexiones. *(Terminó siendo JSON y TOML, no SQLite; ver
`appinfo.Paths`.)*

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
- ✅ **S09 Cell viewer** — modos por tipo: JSON formateado, hex para bytea, texto
  y el caso null. El modo "Items" de los arrays, la fila entera como JSON y los
  botones de escritura del pie se completaron en la **Iteración 7**, en la
  unidad del visor.
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

> **El apply va en tramos, no en una transacción.** Contra MySQL y MariaDB un
> DDL en el medio de una transacción **commitea todo lo anterior** y deja la
> conexión afuera, así que lo posterior también se commitea solo y el ROLLBACK
> no revierte nada. Ver § 6. Los cambios de **datos** sí son transaccionales de
> verdad en los cuatro motores, así que la grilla de la Iteración 7 tiene la
> misma garantía en todos.

MySQL, MariaDB y SQLite por el mismo pipeline, **cada uno contra su última
versión estable**, igual que Postgres. Verificar cuál es en su momento en vez de
asumir: el default del `docker-compose.test.yml` se elige ahí, y la matriz de CI
cubre la última más las anteriores que se declaren soportadas.

Backend, por motor:

- ✅ `internal/engine` — la costura: `Conn`, `Caps`, `TramosDe`, vocabulario de
  fallos, `ServerInfo`, `TxOptions`.
- ✅ `internal/engine/enginetest` — la batería que todo motor tiene que pasar.
  Es la definición ejecutable de «está implementado».
- ✅ `internal/postgres` implementa `Conn`. Pasa la batería.
- ✅ `internal/mysql` — MySQL **y** MariaDB. Pasan la batería las dos.
- ✅ `internal/sqlite` — incluida la reconstrucción de tabla. Pasa la batería.
- ✅ `internal/service` detrás de la costura, y el apply por tramos. El
  servicio ya no nombra ningún motor: el despacho vive en `motores.go` y el
  resto habla con `engine.Conn`.

Frontend y CI:

- ✅ **S03** — los cuatro motores habilitados, el puerto por defecto se arrastra
  al cambiar de motor, y con SQLite el formulario cambia de forma: desaparecen
  host, puerto, usuario, contraseña y SSL, y aparece el selector de archivo del
  sistema.
- ✅ **S15** — la casilla «Una sola transacción» dice lo que el MOTOR va a
  hacer, no lo que la casilla sugiere, y el resultado explica por qué quedó a
  medias sin culpar al usuario de no haberla marcado. Aviso de reconstrucción
  de tabla para SQLite.
- ✅ **S15** — ensayo: corre el changeset entero adentro de una transacción y
  la revierte. Responde lo que la vista previa no puede —la SQL puede estar
  impecable y fallar contra los datos que hay— y solo existe donde el motor
  tiene DDL transaccional: en MySQL y MariaDB «correr y revertir» dejaría todo
  aplicado, así que el botón no está.
- ✅ **S01 y S02** — "Abrir archivo SQLite…" en las dos. `DraftSQLite` arma el
  borrador del lado de Go —motor, ruta y nombre sacado del archivo— y termina en
  el editor, no en la base: la conexión se guarda en la libreta, y escribir ahí
  algo que la persona no vio es cómo la lista se llena de entradas que nadie
  creó a sabiendas.
- ✅ **S06** — el editor parte el texto en sentencias y las corre de a una, así
  que los cuatro motores hacen lo mismo: antes Postgres corría todas, MySQL
  ninguna, y SQLite todas mostrando una sola. Cada resultado trae su línea y el
  fallo dice cuál sentencia fue.
- ✅ **CI** — `KANAME_REQUIRE_ENGINES` puesto, y en el compose las **dos LTS de
  cada motor**: MySQL 9.7 y 8.4, MariaDB 12.3 y 10.11. Las cuatro corren la
  batería entera, y además los dos tests que dependen de la versión —modo solo
  lectura y límite de tiempo por sentencia—, que son justo donde los motores se
  separan.

#### Pruebas manuales — hechas el 2026-09-09

Se corrieron contra la aplicación de verdad, manejando la ventana por CDP:
WebView2 acepta `AdditionalBrowserArgs: --remote-debugging-port`, así que la
ventana de Wails es un target de depuración como cualquier página. **Eso no va
al binario**: se compiló aparte, sin commitear, porque un socket que permite
evaluar JS en la página puede llamar cualquier binding —`RevealPassword`
incluido—.

| | Qué | Resultado |
|---|---|---|
| 1 | MariaDB 10.11 desde S03 | ✅ conecta, «MariaDB 10.11.19» en la barra, árbol y detalle sin errores |
| 2 | Solo lectura contra MySQL | ✅ el DELETE falla con 25006 y «esta conexión está abierta en modo solo lectura» |
| 3 | Límite de tiempo | ✅ `SELECT SLEEP(10)` cortado a los **2012 ms** |
| 4 | Comentario con barra invertida | ✅ hecha el 2026-09-09, después de construirle la puerta de entrada |
| 5 | SQLite con `#` y `%` en la ruta | ✅ abre el archivo real y muestra sus 3 filas, no una base vacía |
| 6 | Atajo «Abrir archivo SQLite…» | ✅ el selector nativo no se puede manejar por CDP; **probado a mano por el usuario el 2026-09-10**, abre el archivo y llega al editor |
| 7 | Ensayo de S15 | ✅ el cambio inválido se caza antes de aplicar, con SQLSTATE 23502 y la consulta para encontrar las filas; el válido dice «Nada aplicado» en violeta, no en verde |

Lo que salieron de ahí, todo corregido salvo lo último:

- **La limpieza de dos tests no limpiaba.** `defer c.Close()` corre ANTES que los
  `t.Cleanup`, así que los DROP se ejecutaban sobre un pool cerrado y fallaban
  en silencio —el error iba a `_`—. Dejaban `kn_ver_padre`/`kn_ver_hija` en
  Postgres y `kn_solo_lectura` en los cuatro servidores de MySQL. Los cleanup
  son LIFO: registrar el cierre primero lo deja último.
- **El editor SQL hablaba PostgreSQL contra los cuatro motores.** `dialect`
  estaba fijo y la barra decía «dialecto PostgreSQL» conectado a MySQL 9.7. No
  es cosmético: cambia qué es palabra reservada, cómo se citan los
  identificadores y qué ofrece el autocompletado. Ahora sale del motor abierto
  —y MariaDB usa su propio dialecto, que no es el de MySQL—. Comprobado con `#`,
  que es comentario en MySQL y no en Postgres.
- **Tres plurales rotos**: «1 sentencias bloquean su tabla», «1 cambios pierden
  datos» y «1 tablas visibles».
- **Un texto vencido**: el panel de conexiones decía que la tab Safety «llega en
  la Iteración 5».

Y dos que **no son bugs sino agujeros de planificación**, los dos encontrados
por mirar la aplicación y no el código:

- ✅ **La tab Safety de S03 quedó agendada en la Iteración 9** (decidido el
  2026-09-10). No la agendaba ninguna: estaba deshabilitada con el cartel
  «Llega en la Iteración 5», que ya había pasado, y la 9 solo listaba «S03 —
  tabs TLS y Advanced». El cartel ahora dice 9. Mientras tanto el límite de
  tiempo por sentencia y las otras dos protecciones solo se editan en
  `connections.toml` —que es lo que hubo que hacer para correr la prueba 3—.
- ✅ **`setColumnComment` ya tiene puerta de entrada**: «Comentar…» en el menú
  contextual de la columna, deshabilitado con SQLite y explicando por qué. Y el
  campo «Comentario» del alta de columna, que existía y no hacía nada, ahora
  arma su propio cambio. Ver el registro de la iteración 6.

### Iteración 7 — Grilla editable

Edición de celdas con preview de `UPDATE`/`DELETE`, import/export CSV, y todo
lo que entra y sale de la grilla.

> **Con qué arranca.** La iteración 6 dejó tres cosas puestas que esta usa
> directamente, y conviene saberlo antes de empezar:
>
> 1. **El apply va por tramos** y el resultado dice cuántos hubo y cuál falló.
>    Un changeset de puros cambios de DATOS —que es exactamente lo que produce
>    la grilla— es **un solo tramo transaccional en los cuatro motores**: el DML
>    de InnoDB es transaccional de verdad. Así que editar celdas tiene la misma
>    garantía contra MySQL que contra Postgres, y la casilla de S15 no miente.
>    La limitación de MySQL es solo del esquema.
> 2. **El ensayo de S15** ya existe y sirve igual para datos: un `UPDATE` que
>    viola una clave se ve antes de aplicarlo.
> 3. **`StatementResult.Applied` significa «quedó aplicada en la base»**, no
>    «corrió sin error». Importa acá: `olvidarAplicados` saca del changeset lo
>    que figure aplicado, y con la definición vieja una edición de un tramo
>    revertido se perdía sin existir en la base.
>
> Y una que **faltaba** y que la grilla necesita: el changeset no tenía
> operaciones de datos. Fue lo primero de la iteración; ver abajo y § 6.

- ✅ **Contrato de Go para los cambios de datos** — `insertRow`, `updateRow` y
  `deleteRow` en el changeset, uno por fila; renderizado único en
  `internal/dml` para los cuatro motores, en dos formas —la legible con los
  valores escritos y la que corre con los valores como parámetros—;
  `Modify` en la costura con conteo de filas, y el apply exige que cada cambio
  toque exactamente una fila o revierte el tramo; `StageMany` para preparar
  una tanda con una sola confirmación. Todo con tests contra los cuatro
  motores, incluida la fila que otro borró y la clave que alcanza dos filas.
- ✅ **S07 / S10** — modo edición: doble clic o Enter abre el editor en la
  celda, clic derecho para NULL, volver al valor leído y borrar la fila;
  «Agregar fila» con las celdas sin cargar marcadas como por defecto; filas
  marcadas para borrar; la tira de ediciones con una pastilla por fila y su
  cruz para deshacer; «Preparar» manda la tanda a Go (`StageGrid`) con una sola
  confirmación de producción. Tablas sin PK y conexiones de solo lectura no se
  editan, y el motivo se ve en el botón. Sin el selector de valores de enum
  —los enums no se introspectan todavía; llega con S17 en la iteración 8— y sin
  Ctrl+Z: deshacer es la cruz de la pastilla. Probado a mano contra los cuatro
  motores el 2026-09-10, incluida la fila que otro borró; ver § 6 por lo que
  salió de MySQL.
- ✅ **S08 Data change review** — una fila por vez: la lista de filas tocadas,
  la fila entera unificada o lado a lado con lo que había y lo que queda, la
  sentencia, y las comprobaciones que Go hace mirando la base —la clave
  identifica una sola fila, el padre de cada clave foránea existe, cuántas
  hijas arrastra un borrado y qué les pasa, columnas NOT NULL sin valor—.
  Se abre desde la grilla («Revisar las filas») y desde Cambios pendientes.
  La variante de producción es la de siempre: la confirmación la pide Go al
  preparar y al aplicar. Probada a mano en los cuatro motores el 2026-09-10.
- ✅ **S18 CSV import wizard** — «Importar…» en la barra de la tabla abre el
  asistente de cuatro pasos, y son cuatro porque cada uno responde una
  pregunta que el siguiente da por contestada: **origen** (el archivo, el
  delimitador, si la primera línea es el encabezado, si el vacío es NULL, con
  las líneas crudas y los avisos de UTF-8 y de líneas desparejas), **mapeo**
  (emparejado por nombre sin distinguir mayúsculas; lo que no coincide queda
  sin elegir a propósito), **validación** (el ensayo, que arranca solo) e
  **importar**. La ruta se puede **escribir**, igual que en el formulario de
  SQLite de S03: el selector del sistema es la comodidad, no el único camino.
  Importar no pide clave primaria —un INSERT no identifica ninguna fila—, así
  que lo único que lo impide es que la conexión no escriba. Probado a mano en
  los cuatro motores el 2026-09-10, incluidos el archivo con la fecha
  inválida, el delimitador equivocado y las tres formas de «saltear las que
  chocan».
- ✅ **Exportar el resultado del editor SQL** — CSV, JSON, JSON Lines y
  Markdown, desde «Exportar…» en la barra de resultados, con el diálogo de
  S19 —formato, delimitador, opciones, vista previa, guardar y copiar— y un
  «Copiar» al lado que pega en una planilla. No estaba en el plan y fue lo
  primero del grupo porque valida el formato antes del camino difícil: los
  escritores de `internal/export` ya van de a una fila, que es lo que S19
  necesita. Probado por CDP en los cuatro motores el 2026-09-10; **el selector
  nativo de «guardar como» lo probó a mano el usuario el 2026-09-10**: deja
  `kn_cli.csv`, y `kn_cli.csv.gz` con el gzip tildado.
- ✅ **S19 Export dialog — una tabla, en streaming.** «Exportar…» en la barra
  de la tabla abre el mismo diálogo con el alcance «la tabla entera»: la leen
  el motor y Go a medida que escriben el archivo, así que el tamaño de la
  tabla no entra en la cuenta. `engine.Conn.Scan` es la costura, con su caso
  en la batería que todo motor pasa. Probado a mano en los cuatro motores el
  2026-09-10.
- ✅ **S19 — varias tablas y el formato «SQL inserts».** El alcance «todas las
  del esquema» y un quinto formato que escribe INSERTs que se pueden volver a
  correr. **El formato del conjunto lo decide el formato de cada tabla**: con
  SQL todo va a UN archivo, porque un volcado que se pueda correr es un solo
  script; con los demás va un archivo POR TABLA en una carpeta, porque un CSV
  con tres tablas adentro no lo lee nadie. El zip se descartó: no se puede
  mirar sin abrirlo y no ahorra nada que el disco no ahorre solo. «Schema only»
  del diseño va con el volcado, que es donde vive la cobertura declarada.
  El selector de carpeta lo **probó a mano el usuario el 2026-09-10**: queda un
  archivo por tabla, con el nombre de cada una.
- ✅ **El visor: la fila entera como JSON, el modo Items y los botones del
  pie.** Hoy S09 formatea JSON de UNA celda; la fila completa es un ítem del
  menú contextual y se resuelve del lado del servidor con `row_to_json`. En la
  misma unidad entra lo que S09 dejó pendiente desde la Iteración 2: el modo
  «Items» de los arrays —el parser del literal de Postgres va en Go, con sus
  casos de comillas y escapes probados— y los botones «Revertir» y «Preparar
  cambio» del pie, que siguen deshabilitados con «Llega en la Iteración 7».
  Ahora que la grilla tiene estado de edición, el visor es la forma de editar
  un valor largo —un JSON, un texto— sin hacerlo en una celda de una línea.
- ✅ **Filtros por columna en la grilla** — un constructor de `WHERE` en la
  misma pestaña donde se mira la tabla: columna, operador y valor, y varias
  condiciones combinadas con Y. Catorce operadores que se escriben igual en los
  cuatro motores. Los valores viajan como parámetros. El filtro alcanza a la
  lectura, al conteo y a la exportación, así que «exportar lo que estoy
  mirando» exporta lo que se está mirando. Probado a mano en los cuatro motores
  el 2026-09-10.
- ✅ **Volcado del esquema, de los datos, o los dos** — «Volcar…» en la barra de
  arriba, con las tres piezas del diseño: solo datos en orden de dependencias,
  solo estructura con la cobertura declarada, y `pg_dump` manejado sin
  empaquetarlo, en su propia pestaña. El archivo se puede volver a correr, y
  eso está probado de la única forma que sirve: se vuelca un esquema, se borra,
  se corre el archivo y se comprueba que las tablas, las filas, las claves y
  los índices volvieron. Probado a mano en los cuatro motores el 2026-09-10.

**Sobre el volumen.** Exportar no puede juntar la tabla en memoria: una tabla de
dos millones de filas no pasa por un `[][]string`. Se escribe al archivo a
medida que llega, y para CSV conviene `COPY … TO STDOUT` (`PgConn().CopyTo`),
que es órdenes de magnitud más rápido que paginar con `LIMIT/OFFSET`.

#### El volcado, en tres piezas separadas

Se separan porque son tres problemas distintos y juntarlos fue el error de la
primera evaluación: se argumentó contra el más difícil y se descartaron los
otros dos de arrastre.

**Solo datos.** `COPY … TO STDOUT` por tabla da lo mismo que
`pg_dump --data-only`. Lo difícil no es sacar los datos sino poder volver a
meterlos: emitir las tablas en orden de dependencias, guardar el valor actual de
las secuencias y decidir qué hacer con los triggers al restaurar. El orden ya lo
sabemos calcular — es el mismo ordenamiento topológico del changeset.

**Solo estructura, con la cobertura declarada.** El problema no es la dificultad
—ya introspectamos el catálogo y ya renderizamos DDL— sino el **silencio**: un
export que se olvida de una política de RLS se ve idéntico a uno correcto. Es el
mismo defecto por el que rechazamos Atlas, y no vale hacérnoslo a nosotros.

La condición para que exista es que **diga qué deja afuera, con nombre y
apellido**. No un aviso genérico: Kaname puede consultar el catálogo y contar lo
que no sabe renderizar, así que el aviso dice «3 funciones (`demo.tocar`,
`demo.calcular`, `demo.auditar`), 2 vistas y 1 política de RLS quedan fuera».
Va en el encabezado del archivo generado **y** en la pantalla, detrás de un
botón que abre el detalle completo — la lista larga no puede vivir en un
tooltip ni en una nota al pie, porque es la información que decide si el
archivo sirve para lo que uno lo quiere usar.

El hueco se achica solo después de la Iteración 8: cuando vistas, funciones y
triggers existan como objetos, pasan a estar cubiertos.

**Manejar `pg_dump`, sin empaquetarlo.** Es la opción completa de verdad y es la
más barata de las tres:

1. **Armar el comando y mostrarlo**, con host, puerto, base y las opciones que
   correspondan a lo tildado, resuelto a través del túnel SSH que la aplicación
   ya levanta. Riesgo cero y valor real: la mitad de los errores con `pg_dump`
   son al escribir la línea.
2. **Ejecutarlo si está en el `PATH`**, comprobando primero que su versión sea
   igual o más nueva que la del servidor. Esa comprobación es lo que separa una
   herramienta de una trampa: un `pg_dump` viejo contra un servidor nuevo falla,
   o peor, no falla.
3. Si no está, decirlo y dejar el comando para copiar.

La palabra «backup» sigue sin ser nuestra para lo que generamos nosotros. Para
lo que genera `pg_dump`, lo es.

**Cómo salió el volcado, y las cuatro cosas que aparecieron al escribirlo.**

- **El orden de dependencias NO era «el mismo del changeset».** El changeset
  ordena por FASE —primero las tablas, después las claves— y funciona porque
  las claves van aparte. Un volcado de datos no tiene esa salida, así que hay un
  orden topológico de verdad en `internal/dump`. Un ciclo no se rompe: no existe
  ningún orden que funcione, se nombra y se avisa en el archivo.
- **El DDL sale del renderizador del changeset**, armando `change.Change` desde
  la introspección. Un segundo generador serían dos verdades sobre cómo se
  escribe una columna en cada motor. La consecuencia es la razón de ser de la
  cobertura: lo que el changeset no expresa, el volcado no lo escribe.
- **`pg_dump` no puede usar el túnel de Kaname.** El túnel es un dialer de Go y
  `pg_dump` es otro proceso; darle un puerto en `127.0.0.1` lo dejaría
  alcanzable por cualquier cosa de la máquina, y este proyecto no abre sockets
  locales. Se dice, con el `ssh -L` que lo resuelve.
- **La clave primaria va adentro del CREATE TABLE.** SQLite no tiene `ALTER
  TABLE … ADD PRIMARY KEY`, así que la forma separada dejaba al volcado sin
  clave en uno de los cuatro motores.

**El review `high` del volcado: catorce hallazgos, y los dos peores eran del
mismo tipo — un archivo roto Y silencioso.**

- **Un `serial` se escribía como `integer DEFAULT nextval('t_id_seq')`.** El
  archivo apuntaba a una secuencia que nunca creaba: no se podía correr. Y la
  cobertura decía que no faltaba nada, porque la consulta del catálogo excluye
  las secuencias de columna justamente porque «vienen con la columna». Venían
  con la columna en la introspección y no en el archivo.
- **Una columna GENERADA se salteaba del CREATE TABLE y entraba igual en el
  INSERT**, porque los datos se leían con `SELECT *`. Fallaba con «column
  "total" does not exist», y la cobertura tampoco la nombraba: el comentario
  decía «la cobertura la nombra» y nadie la ponía ahí.

Los dos se arreglan con la misma idea: **`Conn.AutoIncrement` y las columnas
saltadas son parte de la cobertura**. La forma de una columna autoincremental
cambia por motor y la introspección lo refleja distinto —Postgres la manda como
`nextval` o como `Identity`, MySQL y SQLite como `Identity`— así que cada motor
dice cómo se escribe, o que no puede. SQLite no puede: `AUTOINCREMENT` exige la
clave pegada a la columna. Ahí se nombra en la cobertura, que ahora tiene DOS
mitades: lo que el catálogo tiene y Kaname no renderiza, y lo que Kaname
renderizó distinto. Sin la segunda, un archivo que pierde el autoincremento de
la clave primaria se veía idéntico a uno completo.

Del resto, tres que valen como regla:

- **Una ejecución anidada no se registra encima de la de afuera.** El volcado se
  registra una vez y llama a `volcar` por cada tabla, que se registraba otra vez
  con el mismo identificador y borraba la clave al terminar: «Cancelar» quedaba
  inútil desde la primera tabla, justo en el volcado largo que es el único donde
  alguien lo aprieta. No hace falta registrarla: el context de adentro deriva
  del de afuera.
- **Los datos del volcado se ordenan por la clave primaria.** El encabezado se
  limpió de espacios colgando porque «un volcado se guarda para compararlo con
  el de mañana», y sin `ORDER BY` dos corridas de una tabla sin cambios ya
  podían diferir: el argumento se contradecía a sí mismo.
- **El aviso de codificación mira el archivo entero.** Se decidía sobre los
  primeros 64 KiB aunque `Inspect` ya lo recorre todo para contar las filas, así
  que un archivo cuya basura empieza después se importaba como caracteres rotos.
  En el corte exacto del prefijo la pregunta no tiene respuesta —un `0xF1` ahí
  es a la vez una eñe de latin-1 y el arranque de un carácter que sigue afuera—
  y se elige no avisar: el falso aviso manda a rehacer un archivo que está bien.

### Copiar desde la grilla

No estaba en el plan y sale del uso: copiar es lo que más se hace con una celda
y era lo único que no se podía sin abrir un diálogo.

- ✅ **Copiar una celda** — `Ctrl+C` sobre la grilla, y «Copiar la celda» primero
  en el menú contextual. Vale en la grilla de la tabla y en la del editor SQL.
  Copia el valor **tal cual**, sin comillas ni encabezado: copiar una celda es
  sacar su contenido, no exportarla. Una celda NULL copia la cadena VACÍA, que
  es lo que significa; copiar la palabra `NULL` metería cuatro letras donde no
  había nada. Lo que se pierde —distinguir NULL de la cadena vacía— lo dice el
  visor con todas las letras.
- ✅ **Copiar filas en un formato** — «Copiar…» en la barra de la tabla y en la
  del editor abre un menú con TSV, CSV, JSON y Markdown; el mismo menú, sobre
  la fila del clic derecho, copia UNA fila. **El alcance lo decide dónde se
  aprieta**, como en Beekeeper: no hay un selector aparte. El título del menú
  dice CUÁNTAS filas —copiar quinientas sin saberlo es una sorpresa fea— y son
  las CARGADAS, no la tabla entera: para eso está Exportar, y dos millones de
  filas en el portapapeles no son una función sino un cuelgue.

  El texto lo arma Go, con los mismos escritores que la exportación: copiar y
  exportar tienen que dar el mismo texto de las mismas filas, o el archivo y el
  portapapeles dirían cosas distintas y nadie sabría cuál creer.

**El Markdown que se copia va ALINEADO, con un tope de 40 caracteres.** Sin
relleno, una tabla pegada en un ticket no se lee hasta que algo la renderiza. Y
sin tope tampoco: una columna con un JWT de trescientos caracteres deja a todas
las demás con doscientos noventa espacios y la tabla queda peor que sin alinear.
Las columnas más anchas que el tope se escriben sin rellenar: desalinean su
renglón y dejan el resto legible.

Alinear **solo se puede al armar texto**, y el contrato lo hace cumplir: para
saber el ancho de una columna hay que tener todas las filas antes de escribir la
primera, así que `NewInto` —el escritor de a una fila, el que va al archivo—
RECHAZA la opción en vez de aceptarla y quedarse sin memoria en una tabla de dos
millones. El ancho se mide en RUNAS: con bytes, un acento cuenta dos y la
columna queda corrida justo en las tablas en castellano.

**El TSV no encomilla todo**, que es lo que hacen otras herramientas: Excel no
siempre saca las comillas al pegar y quedaría `"81758"` literal en la celda. El
escritor encomilla solo donde hace falta, así que un valor con una tabulación
adentro sigue estando bien. Lo que se pierde es distinguir el NULL de la cadena
vacía, y es a propósito: una planilla no tiene forma de mostrar esa diferencia.
El CSV sí la distingue, y es el que sirve para volver a importar.

**Copiar no exige poder editar**, y el orden de las guardas lo refleja: una
tabla sin clave primaria o una conexión de solo lectura se leen igual, y copiar
es leer. Es justamente donde más sirve.

**`Ctrl+C` no avisa cuando sale bien.** Es un contrato del sistema y nadie
espera un cartel al copiar; el fallo sí se dice, donde cada pantalla ya muestra
los suyos. El `Toast` de S00 sigue sin montarse en ninguna parte: montarlo para
esto habría sido inventar una pieza de infraestructura para una acción que no
la necesita.

### Iteración 8 — Objetos de texto

Vistas, funciones, procedures, triggers y enums como editor de definición.

- ✅ **El contrato de objetos** — `Conn.Objects` y `Conn.ObjectDefinition` en los
  cuatro motores, probados con el ciclo borrar → volver a correr. Es la base de
  las tres pantallas de abajo.
- ✅ **S16 Object editor** — completa: la definición se edita, el cambio pasa
  por el changeset con su vista previa, y la lista de dependientes y el aviso de
  DROP + CREATE están al lado del botón que los necesita.
- ✅ **S17 Enum / type editor** — agregar un valor eligiendo su posición y
  renombrar uno. Sacar un valor NO se ofrece, y se explica por qué: PostgreSQL
  no tiene `DROP VALUE`. Los dominios y los tipos compuestos siguen mostrándose
  en el árbol y abriéndose con el aviso de que Kaname todavía no los escribe.
- ✅ **S05** — nodos de vistas, materialized views, funciones, procedures,
  triggers, enums, dominios, tipos compuestos, secuencias, políticas y eventos
  en el árbol, agrupados por clase, y la definición de cada uno para leer.

> **Con qué arranca.** La cobertura del volcado de la iteración 7 ya recorre el
> catálogo de los cuatro motores buscando vistas, funciones, triggers,
> políticas, tipos y secuencias — para poder decir qué queda afuera del archivo.
> Es exactamente la lista que el árbol necesita, así que esa consulta es **una
> sola** y ahora se llama `Conn.Objects`. Lo que el volcado sabe escribir se
> decide en `dump`, del lado que lo sabe: cuando S16 haga que una vista se pueda
> volcar, la cobertura se achica cambiando un filtro y no ocho consultas.

### Iteración 9 — Pulido (continuo)

Historial, atajos, drift check, builds Linux/macOS, firma de código.

- ✅ **S22 Command palette** — acciones, tablas y objetos, con Ctrl K. El chip
  llevaba desde la Iteración 1 prometiendo un atajo que no existía; en la
  pantalla de bienvenida se sacó en vez de cumplirlo, porque ahí no hay nada que
  buscar. Ver el registro de la § 6.
- ✅ **Botón de desborde en la barra de título.** Se esperó a que existiera S23:
  antes solo habría duplicado «desconectar» y «about», que ya están en la barra
  de estado. Ahora lleva Ajustes, Acerca de, dónde se guarda todo, desconectar y
  salir. Se decidió
  expresamente **no hacer una barra de menús** —Archivo / Edición / Vista—: es
  la respuesta de 1984 a descubrir acciones, obliga a inventar categorías para
  cosas que no las tienen («Aplicar changeset» no es Archivo ni Edición), cuesta
  alto vertical permanente en una pantalla que muestra filas y un diagrama, y
  como no se usan elementos nativos habría que construirla entera a mano.

  Una barra de menús resuelve tres problemas distintos, y acá cada uno va por
  su lado: **encontrar cualquier acción** es la paleta; **actuar sobre un
  objeto** es el menú contextual sobre el objeto, que ya existe en
  `components/ui` y usa S02; y **lo de la app que nadie hace a diario** —Acerca
  de, ajustes, dónde está el archivo de conexiones, salir— es este botón de
  desborde, uno solo.

  **La excepción es macOS**, que exige un menú de app de verdad: Cmd+Q, Cmd+, y
  Edición→Copiar/Pegar son contratos del sistema. Cuando esta iteración haga
  ese build va un menú nativo **solo con lo que el sistema obliga**, no un
  espejo de las funciones de la aplicación.
- ✅ **S21 Query history / saved queries** — las dos pestañas del sidebar. El
  historial va al directorio de estado —es de esta máquina— y las guardadas al
  lado de la libreta de conexiones, que es la que se sincroniza. Ninguno de los
  dos guarda una sentencia con una contraseña escrita. El servicio quedó sin
  registrar en `main.go` y no se anotaba nada; ahora hay un test que compara la
  lista de servicios contra lo que el frontend importa. Ver el registro § 6.
- ✅ **S20 Schema drift** — el artboard lo dice mejor que el nombre del ítem:
  no es «revisar una base», es **comparar dos conexiones** —dev contra
  producción— y generar la SQL que alinearía a la segunda. `internal/drift`
  (la comparación, pura y probada), `Session.Compare` (abre las dos, lee los
  catálogos, le pide al motor del DESTINO la sentencia de cada diferencia y
  las cierra sin tocar la que está abierta), `Session.Migration` /
  `SaveMigration` (el archivo `.sql`, desde la comparación en memoria y en un
  orden que se puede correr) y la pantalla, que entra desde el gestor de
  conexiones y desde la paleta con la conexión abierta como origen. Ver el
  registro de la § 6 (2026-09-11). **Queda afuera y se dice en pantalla**:
  índices y restricciones que no sean la clave primaria, que viven en el
  detalle por tabla —una consulta por tabla— y el cuerpo de las vistas y
  funciones, que se pide de a uno.

  **Atlas no hace falta acá tampoco**, y por el mismo motivo que en la
  iteración 5: la comparación emite `change.Change`, o sea las mismas diecisiete
  operaciones que la interfaz ya sabe escribir y renderizar en los cuatro
  motores. Lo que no entra en ese vocabulario se reporta como diferencia **sin**
  sentencia, que es justo lo que el diseño pide para la columna que solo está en
  producción. Un differ externo agregaría un modelo intermedio para producir
  algo que ya se produce.
- ✅ **S23 Settings** — completa, con el panel de seguridad y el check de
  updates manual. `config.toml` era hasta ahora una ruta que About mostraba y
  que nadie escribía nunca. Se guarda sola, y no tiene ni un control que no haga
  algo. De paso, About dejó de mentir: decía que la aplicación todavía no se
  conectaba a ninguna base.
- ✅ **S03** — **Safety**, **TLS** y **Advanced**: las cinco pestañas del
  editor están. Los tres límites de Safety se eligen entre «por defecto», «un
  valor» y «sin límite» en vez de editar el número crudo, porque en el archivo
  el cero significa «usá el default» y no «ninguno». TLS: el modo se mudó de
  General a su pestaña, con la raíz y el par de cliente **por ruta** —como la
  clave del túnel— y una tarjeta con el certificado que el servidor presentó
  al probar: sujeto, emisor, para qué nombres vale, vigencia, huella SHA-256 y
  canal. Un botón guarda ese certificado como raíz de confianza, que es el
  camino para un servidor propio. MySQL y MariaDB ganan `verify-ca` de verdad
  y los certificados, que su driver no sabía leer de la cadena. Advanced:
  search_path (Postgres), nombre de aplicación, tamaño del pool y **SQL de
  sesión**, que corre en cada conexión del pool antes que nada y **no puede
  apagar las protecciones**, porque estas se aplican después. Sin «Default
  schema», que el artboard tenía: ver la § 6.
- ✅ **Poner un comentario a una tabla o a una columna.** El de columna ya
  estaba desde la iteración 6 —este ítem quedó desactualizado—; faltaba el de
  la tabla, que ahora está en el pie de la pantalla de estructura. Con eso se
  puede comprobar el citado desde la aplicación: un comentario con una comilla
  simple adentro sale como `O''Brien` y vuelve del catálogo como `O'Brien`.
- ✅ **S24** — variante «unsaved changes on tab close». Solo pregunta cuando hay
  algo que perder: un diálogo en cada cierre entrena a apretar «sí» sin leer, y
  entonces no protege del único caso en que hacía falta.
- ✅ **Tema claro de S05, S06, S12 y S15** — al final y no al principio, como
  decía el plan: recién con S23 hay forma de prenderlo. Las cuatro pantallas
  estaban bien —todo lo suyo sale de tokens— y lo que fallaba era transversal:
  dieciocho colores escritos a mano en otros componentes, con el valor del tema
  OSCURO. Ver el registro de la § 6.
- ✅ **Marca en Linux y macOS, y los builds.** Los 9 PNG de hicolor más el
  SVG escalable, el `.desktop` y los `.deb`/`.rpm`/AppImage con nfpm y
  linuxdeploy; en macOS el documento de Icon Composer con la marca por capas
  —fondo claro y oscuro, primer plano recoloreado por apariencia— que `actool`
  compila en el runner de macOS 26, y el `.icns` del tile como respaldo. CI
  construye los tres sistemas y un tag `v*` deja un release en borrador con
  los seis archivos y `SHA256SUMS`. **Sin firma**, por decisión: ver la § 6
  (2026-09-11, «Marca en Linux y macOS, builds en CI y la licencia»). Lo que
  falta es que alguien abra el `.app` y el AppImage en una máquina de verdad.
- ✅ **Renombrar los tokens de color del ERD y del preview.** El kit define
  `--erd-rel-cascade`, `--erd-pk`, `--schema-drop`… y el código usa genéricos:
  hoy la línea de cascada es `--env-stage` y la clave foránea es `--accent`. Los
  valores coinciden exactamente, así que no se ve nada mal — pero cambiar el
  color de una cascada exige saber que se llama como un entorno de staging. Es
  un renombre, no un rediseño.
- ✅ **Plan de ejecución** — el primero de los dos botones grises del editor
  SQL. Pestaña «Plan» al lado de Resultados y Mensajes; explica la sentencia
  bajo el cursor con `EXPLAIN` —`EXPLAIN QUERY PLAN` en SQLite—, que **no la
  ejecuta**, y el test lo prueba en los cinco motores pidiendo el plan de un
  DELETE. Postgres y MySQL 9 devuelven un árbol de texto y se muestra como en
  psql; MariaDB y SQLite una tabla, en la grilla con las columnas medidas por
  su contenido. Ver la § 6.
- ✅ **Formatear** — el segundo, con `sql-formatter` 15.8.2 en su propio
  commit, versión exacta y changelog leído; +94 kB minificados al bundle.
  Ordena espacios, saltos y sangría con el dialecto del motor conectado y no
  cambia mayúsculas de nada; si no puede interpretar el texto, lo dice y no lo
  toca. Ver la § 6.
- ✅ **Pase de movimiento.** Las tres duraciones de los tokens y nada fuera
  de ellas: los diálogos entran con `@starting-style` y **salen** —velo que
  funde, panel que baja seis píxeles— gracias a `display` y `overlay` con
  `allow-discrete`, y a que el diálogo dibuja la salida antes de avisarle al
  padre que cierre; los toasts entran desde el borde derecho y se van por el
  mismo lado; menús, desplegables, globos de ayuda y el panel de filtros en
  `--dur-fast`; la paleta y el editor de conexión en `--dur`. Las
  confirmaciones destructivas y todo diálogo de producción **aparecen, no
  llegan**. Ver la § 6 (2026-09-11).
- ✅ **S02 Carpetas.** Pedido del usuario el 2026-09-11, y estaba en el artboard
  desde el principio —«New folder», «Move to folder»— aunque la implementación
  agrupaba por entorno. Una carpeta es un proyecto: adentro conviven local, dev
  y producción del mismo sistema, ordenadas así y cada una con su color y su
  etiqueta de entorno en la fila. Campo `folder` en la conexión, que viaja en
  la libreta; **planas, un solo nivel**; una carpeta existe mientras una
  conexión la tenga. Se asigna en la pestaña General del editor y con «Mover a
  carpeta ▸» en el menú contextual y en el ⋯, que ofrece las existentes,
  «Sacar de…» y «Nueva carpeta…». Las cabeceras pliegan, y el plegado queda en
  esta máquina. Ver la § 6.
- ✅ **S02 Exportar e importar conexiones.** Mismo pedido. «Exportar para
  compartir…» en el ⋯ y en el menú contextual —un `.toml` con el formato de
  la libreta, **sin ningún secreto**—, «Exportar carpeta…» en el menú de la
  cabecera de una carpeta (⋯ o clic derecho), e «Importar…» en la cabecera de
  la pantalla, con vista previa: qué trae, qué está roto, qué ya se tiene, y
  qué claves del archivo se ignoraron. Review `high`; el test que protege
  guarda la contraseña y la frase de paso y exige que el archivo no las
  contenga. Ver la § 6.
- ✅ **Automatizar los bumps de dependencias.** Dependabot, activado el
  2026-09-11 antes de hacer público el repo: un PR por dependencia, siete días
  de espera, sin mergear solo, con Wails, los drivers, `x/crypto` y el
  toolchain excluidos. `staticcheck` entró a CI en el mismo commit, porque un
  bump que compila y pasa los tests puede estar usando algo deprecado. El job
  `deps` se borró. Por qué Dependabot y no Renovate, verificado contra lo que
  tiene este repo, en `bumps-de-dependencias.md`. Ver la § 6.

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

GitHub Actions. Hoy: builds win-x64 (`windows-latest`), win-arm64
(`windows-11-arm`), linux-x64 (`ubuntu-24.04`, GTK4 + WebKitGTK 6.0:
AppImage, .deb y .rpm) y mac-universal (`macos-latest`, `.app` arm64 +
x86_64 en un .zip); un job de gofmt + vet + staticcheck + govulncheck + test
+ typecheck; un job `secretos` que corre gitleaks sobre el historial entero con `.gitleaks.toml`,
después de comprobar que esas reglas detectan una muestra; una matriz de
integración contra PostgreSQL 18, 17, 16 y 14 en `ubuntu-latest` con
`KANAME_REQUIRE_POSTGRES=1`, para que un job sin base se ponga rojo en vez de
verde; y con un tag `v*`, un job `release` que junta los seis archivos, calcula
`SHA256SUMS` y crea el release **en borrador**. El build usa el pipeline de
`wails3 task`, no `go build` a mano — ver el registro de decisiones.

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
  (`build:server`, `run:server`, `build:docker`). *Con corrección: el
  `build/docker/` sí se había borrado, las tareas no; salieron el 2026-09-11,
  con un test que impide que vuelvan.* Ver registro de decisiones.
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
- Firma de código: sin firmar, SmartScreen frena el «copiar y que ande» en
  Windows y Gatekeeper el `.app` en macOS. La vía gratuita para Windows es
  SignPath Foundation, con sus condiciones y el orden para hacerlo en
  `firma-de-codigo.md`; se hace cuando el repo lleve unos días público y haya
  un release. macOS no tiene vía gratuita. (2026-09-11)
- Tests de integración con Docker Compose de los cuatro motores desde el día uno;
  el differ se rompe en silencio
- Exportar ERD a SQL y a imagen
- WebView2 abre dos HTTPS salientes a Microsoft al arrancar, antes de que la
  app conecte a nada. No es de Kaname y no es un puerto escuchando; los flags
  de Chromium compilados en el binario no lo cambian, y el host no está
  identificado (`pktmon` con admin lo mostraría por el SNI). Ver § 6
  (2026-09-11)
- Android: un APK propio, acotado a leer y consultar, con las contraseñas
  cifradas por una clave del Keystore atada a biometría. Qué viaja del núcleo
  (casi todo), qué no (el keychain, la interfaz, cgo), el alcance por pantalla
  y el orden si se hace, en `kaname-android.md`. No ahora: es un segundo
  producto, semanas, y después del 1.0.0 de escritorio. (2026-09-11)
- Si el keychain guarda la contraseña de la base y falla al guardar la del
  bastión, la conexión no se guarda —correcto— pero la primera credencial
  queda en el keychain bajo un ID que ninguna conexión referencia. No es un
  secreto en disco; es una entrada huérfana en el gestor del sistema.

---

### Fuera de alcance, con motivo

Cosas que se evaluaron y **no** se van a hacer. Se anotan para no volver a
discutirlas desde cero dentro de seis meses.

**Reimplementar `pg_dump` en Go. No.** Eso *es* `pg_dump`: orden de
dependencias, extensiones, secuencias, permisos, objetos grandes. No hay
librería en la que confiaría para la mitad que importa, que es restaurar. Y
empaquetar el binario oficial choca de frente con «se distribuye copiando y
pegando»: son cuatro motores por varias versiones cada uno.

Lo que sí se hace es volcar datos, volcar estructura con la cobertura declarada,
y **manejar** el `pg_dump` que ya esté instalado. Está en la Iteración 7.

**Llamar «backup» a lo que generamos nosotros. No.** Es la trampa de verdad, y
es distinta de las anteriores porque no es una limitación técnica sino una
promesa. Un botón que dice «Backup» promete que se puede restaurar; si el
archivo se olvidó de una política de RLS, parece que funcionó hasta el día que
hace falta. Nuestros archivos se llaman por lo que son: «exportar el esquema»,
«exportar los datos».

**Backup físico** —`pg_basebackup`, PITR, snapshots del volumen— queda afuera
entero: es operación del servidor, no de un cliente.

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

### Iteración 9 — 2026-09-12

**Auditoría del 2026-09-11, primera tanda: las casillas de Safety tienen
código detrás.** Cada hallazgo se verificó contra el código y con un test que
falla antes del fix; el estado por hallazgo queda anotado en el propio
`docs/reviews/audit-fable-2026-09-11.md`. Lo que fijó esta tanda:

- **K-01 (CRÍTICO), reconstrucción de SQLite sin transacción.** Verificado
  empíricamente: con «Una sola transacción» apagada, un `SetNotNull` sobre
  `padre` dejaba a `hija` (`ON DELETE CASCADE`) con 0 filas de 2, sin error.
  El fix es en `aplicarPorTramos`: un tramo con `RebuildsTable` es
  transaccional aunque la casilla esté apagada, porque el único lugar donde
  se apagan las claves foráneas es `Begin`. **La casilla dice cómo agrupar,
  no qué invariantes saltear.** Test: `TestElRebuildDeSQLiteSinTransaccionNoDisparaElCascade`.
- **K-02 (ALTO), «Bloquear DROP y TRUNCATE».** Se guardaba y se mostraba
  activa; nadie la leía. Ahora `preparar` (Apply y DryRun) rechaza el
  changeset entero si tiene un `Op() == DROP` o un `ReplaceObject` con
  `Recreate`, y `Queries.Run` rechaza el lote entero si alguna sentencia tiene
  `Command` `DROP` o `TRUNCATE` —antes de correr la primera, igual que
  `preparar` no ejecuta nada si una no se sabe escribir—. Una reconstrucción
  de SQLite lleva `DROP TABLE` en el guion y **no** se bloquea: la tabla vuelve
  con sus filas tres sentencias después, y bloquearla sería bloquear todo
  cambio de columna. `ErrBlockedByPolicy` es el error; la vista previa avisa
  antes de apretar. `Explain` no necesita nada: es lista blanca.
- **K-07 (MEDIO), confirmación por nombre fuera de producción.** Decidido
  hacer cumplir lo que el gestor mostraba: `confirmarEscritura` usa
  `Connection.RequiresWriteConfirmation()` —producción siempre; los demás
  entornos salvo «Escribir sin confirmar el nombre de la base»— en los tres
  caminos que escriben: `Apply`, `DryRun`, `Imports.Run`. **Preparar un
  cambio destructivo sigue pidiendo la palabra solo en producción**: no es
  una escritura, y es la segunda pregunta que el diálogo de la interfaz
  describe como de producción. `ChangesetView` e `ImportTarget` llevan ahora
  `Production` además de `NeedsConfirmation`, porque dejaron de coincidir:
  el rojo es para producción; el campo, para quien pide la palabra.
  Consecuencia para quien ya tiene conexiones: **las locales van a pedir el
  nombre hasta que marquen la casilla**, que es lo que la pestaña Safety
  siempre dijo que pasaba. Los helpers de test marcan la casilla; el test de
  la confirmación la saca.
- **K-07 (MEDIO), desconexión por inactividad.** Implementada en
  `service/inactividad.go`: un `time.AfterFunc` por sesión que al vencer mira
  el último uso y se vuelve a armar por lo que falta. **Actividad es un
  binding que usa la sesión** (`abierta()` y `Schema`); `Current()` y
  `ApplyStatus()` no cuentan, porque la interfaz los consulta sola. No corta
  con un apply en curso ni con una ejecución registrada en `Queries`
  (`vigilarActividad`): vuelve a mirar en un minuto. Al cerrar deja
  `SessionView.ClosedReason` y la próxima llamada lo dice en el error.
  **Terminar una operación también es actividad** (`tocarActual` desde
  `terminarApply` y desde la limpieza de `registrar`): una consulta de catorce
  minutos que termina a los 14:40 no cierra la sesión a los 15:00 con la
  persona leyendo el resultado. Como no hay eventos, el Shell consulta
  `Current()` cada 30 s; al descubrir el cierre **se queda** —las pestañas y
  el texto de los editores viven ahí, y volver al gestor los tiraría sin
  aviso— y muestra el motivo con un botón «Reconectar» en el lugar. El test
  dispara el vencimiento a mano con un reloj inyectado (`usarReloj`, no
  exportado a propósito: un método exportado del servicio es un binding).
- **K-14 (BAJO), setters expuestos como bindings.** `UsarHistorial` y
  `UsarPreferencias` pasaron a ser funciones del paquete —Wails bindea
  métodos, no funciones— y `Running`/`TunnelDown` dejaron de exportarse.
  `TestElCableadoDelServicioNoEsUnBinding` lo fija por reflexión sobre los
  tipos, porque los bindings generados no están en el repo.
- **K-11 (BAJO)** `Redact` toma la contraseña de MySQL codiciosa hasta el `@`
  que precede a `tcp(`: el DSN se arma sin escaparla y el driver parte por el
  último `@`. **K-12 (BAJO)** `LlevaSecreto` suma `PASSWORD E'…'`, `$$…$$`,
  `"…"`, `SET PASSWORD`, `password=` sin comillas y `CREATE SERVER`/`USER
  MAPPING`. **C-15 (MEDIO)** `postgres.Run` resuelve los tipos desconocidos
  con la conexión que ya tiene y no con el pool: con `PoolSize` 1 se colgaba
  (verificado: el test cuelga 5 s con `pool` y pasa con `conn`).
- **Review del cambio (`high`)** encontró que el bloqueo del editor miraba
  solo el comando: `ALTER TABLE … DROP COLUMN` pasaba mientras el changeset
  lo rechazaba. Ahora un `ALTER` con `DROP COLUMN|CONSTRAINT|INDEX|KEY|…`
  también se bloquea; lo que sigue sin verse es un DROP adentro de un `DO
  $$…$$` o del cuerpo de una rutina, y la ayuda de la casilla lo dice.

**Auditoría, segunda tanda: lo que se ve es lo que corre, y el editor no
promete lo que no cumple.**

- **K-03/C-01 (ALTO), transacciones manuales en el editor.** Comprobado
  contra Postgres: `BEGIN; DELETE; ROLLBACK;` devolvía OK y la tabla quedaba
  vacía —cada sentencia toma su conexión del pool, y pgxpool destruye la que
  vuelve en transacción—. Decidido con el usuario: **hoy se rechaza**
  (BEGIN/START TRANSACTION/COMMIT/ROLLBACK/SAVEPOINT/RELEASE/END y `SET
  autocommit`, antes de correr nada, con el motivo) y **el control manual por
  pestaña se implementa después**, con una conexión dedicada que sobreviva
  entre ejecuciones, toggle de auto-commit, indicador de transacción abierta
  y ROLLBACK al cerrar. Queda especificado en
  `docs/reviews/pendientes-auditoria-2026-09-12.md`.
- **K-04 (ALTO), `pg_dump` sin TLS.** La base va ahora en `--dbname=` como
  cadena de conexión de libpq —`dbname=… sslmode=… sslrootcert=…`—, que es el
  único lugar donde caben el modo y los certificados; el comando copiable los
  muestra, así quien lo corre a mano hereda lo mismo. Sigue sin contraseña.
- **K-05 (MEDIO), escrituras concurrentes.** `openSession.escritura` con
  `TryLock` en Apply, DryRun e importar: el segundo falla enseguida con
  `ErrBusy` en vez de encolarse; son operaciones largas y el segundo pedido
  casi nunca es a propósito.
- **K-06 (MEDIO), solo lectura reversible desde el editor.** Comprobado: `SET
  default_transaction_read_only = off; DELETE` borraba. Se rechaza en `Run`
  lo que apaga el modo en los cuatro motores. Es una red del lado del
  cliente, y se dice así: el modo sigue siendo un parámetro de sesión.
- **Fuera de la auditoría, encontrado al pasar:** `postgres.Run` clasificaba
  los errores de sentencia con `Classify` —el de conexión—, así que un error
  de sintaxis, una tabla inexistente o una división por cero se mostraban en
  el editor como «El servidor rechazó la conexión con la consulta». Ahora usa
  `ClassifyStatement`, que ya existía para esto.
- **K-08 (MEDIO), la vista previa no ataba lo que se ejecuta.**
  `ChangesetView.Fingerprint` (SHA-256 de las sentencias en orden) viaja de
  vuelta en `ApplyOptions`: obligatoria salvo «Aplicar sin abrir la vista
  previa» —casilla que hasta acá tampoco tenía código, porque la interfaz
  siempre pasaba por la vista previa y Go no podía saberlo— y si no coincide
  con lo que se va a ejecutar, no se ejecuta. `DryRun` no la exige: no deja
  nada. **Al escribir el test apareció un bug de la misma clase que K-01, no
  listado en la auditoría:** en SQLite, `AddColumn a` + `SetNotNull n` sobre
  la misma tabla dejaba la tabla SIN `a`, y `SetNotNull n` + `SetNotNull m`
  dejaba `n` nullable, las dos con OK. El guion de la reconstrucción se
  escribe contra el catálogo de ANTES de que corra lo anterior. Hasta que
  haya **una sola reconstrucción por tabla que acumule sus cambios**
  (pendientes), un cambio que reconstruye tiene que ser el único cambio de
  estructura de su tabla en el changeset: se rechaza en `Stage` —para
  enterarse al preparar— y otra vez en `preparar` (`ErrRebuildNotAlone`).
  Los cambios de datos sobre la misma tabla no molestan: corren después.
- **K-09 (MEDIO), «Buscar actualizaciones».** Decidido dejarlo: es manual,
  acotado y documentado. La ayuda de Ajustes dice ahora que es la única
  salida a internet, que va a `api.github.com` solo al apretar, y que GitHub
  ve la IP.
- **K-10 (MEDIO), vista previa de importar conexiones.** `ImportCandidate`
  lleva el modo TLS efectivo, `Safety` y `Warnings()`, y la pantalla los pinta
  con badges y una lista de avisos por entrada: el mismo criterio que ya se
  aplicaba a `SessionSQL`, extendido al resto.
- **Review de la tanda (`high`)** encontró que `sinComentariosAdelante`
  volvía a buscar la primera palabra en el texto entero y la encontraba
  adentro del comentario inicial: `-- settings\nSET default_transaction_read_only
  = off` pasaba las dos guardas nuevas. Ahora `query.Trim` expone el mismo
  escaneo que `Command`. También: `ALTER TABLE t DROP c` sin la palabra
  COLUMN pasaba (ahora cualquier `DROP x` dentro de un ALTER se bloquea salvo
  `DROP NOT NULL|DEFAULT|IDENTITY|EXPRESSION`, que quitan una propiedad y no
  borran nada); `READ WRITE` en otra línea pasaba (`(?s)`); `START REPLICA`
  se tomaba por `START TRANSACTION`; `StageMany` renderizaba también los
  cambios de datos (cuadrático en una sesión de grilla); el aviso de sesión
  cerrada volvía cada 30 s tras descartarlo; y el badge de TLS de la vista
  previa de importar decidía el tono por el modo en vez de por el aviso de Go
  (`require` con raíz verifica). Todo corregido con casos en los tests.

**Auditoría, tercera tanda: un volcado que se puede volver a correr.**

- **C-05 (ALTO), identity y secuencias.** `engine.Conn.DumpHints(detalle)`:
  lo que los INSERT de una tabla necesitan alrededor en este motor. Postgres
  devuelve `OVERRIDING SYSTEM VALUE` cuando hay identity ALWAYS —sin eso el
  archivo no corre, 428C9— y un `setval(pg_get_serial_sequence(…))` por
  columna que se numera sola —sin eso el primer INSERT sin id sobre la base
  restaurada choca, una vez por fila vieja—. MySQL y SQLite devuelven vacío:
  InnoDB ajusta el contador con inserts explícitos y SQLite usa
  `max(rowid)+1`. Se probó restaurando e insertando sin id en `serial`,
  `BY DEFAULT` y `ALWAYS`.
- **C-17 (MEDIO) y el lugar de la regla.** `volcar` es el único camino por el
  que pasa toda exportación a SQL, así que ahí se resuelven las columnas
  insertables y los hints; la grilla daba un archivo que fallaba con una
  columna STORED. El volcado le pasa el detalle que ya leyó (campo no
  exportado de `TableExport`) para no leerlo dos veces.
- **C-16 (MEDIO), la vista previa leía la tabla entera.** `ScanOptions.Limit`
  con el LIMIT del motor. Cortar del lado del cliente no servía: cerrar el
  recorrido consume el resto por el cable.
- **C-10 (MEDIO)** los DROP de «DROP primero» van todos juntos al principio y
  en orden hijas → madres. **C-25 (BAJO)** las claves hacia esquemas fuera
  del archivo no se escriben y se nombran en la cobertura
  (`schema.ObjForeignKey`). **C-21 y C-31 (BAJO)** `SaveTables` y `Dumps.Save`
  se registran para cancelar antes de la parte larga.

**Auditoría, cuarta tanda: lo que se muestra y se exporta es lo que hay.**

- **C-12 (MEDIO), fechas de SQLite.** El driver parsea a `time.Time` todo
  TEXT de una columna declarada DATE/DATETIME/TIMESTAMP y no se le puede
  pedir que no lo haga. La grilla y la exportación **leen esas columnas con
  `CAST(… AS TEXT)`** —una expresión no tiene tipo declarado y llega tal
  cual— y reponen el tipo en el encabezado desde `pragma_table_xinfo`, que
  ahora se lee una vez por página o recorrido. El editor no puede reescribir
  la consulta: ahí `textoDeFecha` escribe el formato más probable de SQLite
  en vez de RFC 3339 con una `Z` que nadie escribió. Es una heurística y se
  dice; la lectura fiel es la de la grilla.
- **C-02 (CRÍTICO), BLOB de SQLite.** El recorrido de exportación entrega
  hexadecimal y `engine.Quoting.Binary` lo escribe `X'…'`; la grilla sigue
  mostrando `[N bytes]`, que es lo que se quiere ver en una celda. Postgres
  cita el `\x…` que ya entrega. MySQL queda como literal hasta C-13.
- **C-29, C-26, C-28 (BAJO)** `changes()` solo tras DML; `\` escapada en
  Markdown, encabezado CSV sin neutralizar, REAL con `.0`; líneas físicas en
  el CSV y filas con campos de más rechazadas.
- **K-16 (BAJO)** `Caps.StatementTimeoutOnlyReads` para MySQL: el editor, la
  ayuda y el aviso del changeset dicen que el límite corta solo lecturas.
- **K-17 (BAJO)** guardar recuerda los secretos y los repone si la libreta
  falla; borrar intenta los dos secretos y dice cuál mitad quedó.
- **K-13 (BAJO)** `HostKeyAlgorithms` con el tipo de clave guardado, en
  `Inspect` y `Dial`.

**Auditoría, quinta tanda: `drift` converge.**

- **C-06 (ALTO), identidad de las claves foráneas.** Se emparejan por lo que
  hacen —columnas, tabla destino (sin distinguir mayúsculas), columnas
  destino— y se comparan por acciones. El nombre no sirve para emparejar:
  SQLite lo sintetiza con el `id` posicional del pragma y MySQL nombra las
  automáticas `<t>_ibfk_N`; con dos claves en distinto orden a cada lado, la
  comparación no convergía y cada apply duplicaba una. El nombre viaja a la
  sentencia solo cuando lo eligió alguien: `nombreSintetizado` reconoce los
  dos patrones y los deja vacíos.
- **C-07 (ALTO), esquemas de distinto nombre.** Un solo esquema de cada lado
  se empareja por posición —es el caso MySQL/MariaDB, donde el esquema se
  llama como la base— y las sentencias nombran el del destino. Se dice en
  `NoComparado`. Postgres con varios esquemas sigue por nombre exacto.
- **C-09 (ALTO), clave primaria como conjunto.** Se compara a nivel tabla:
  un `AddPrimaryKey` con todas las columnas, o ninguna sentencia si el
  destino ya tiene otra clave. Antes salía uno por columna, y en MySQL el
  primero quedaba confirmado con una clave equivocada.
- **C-08 (ALTO), provisorio.** `schema.Column.AutoIncrement` viaja desde las
  tres introspecciones (Postgres: `attidentity` o default `nextval(`; MySQL:
  `extra like '%auto_increment%'`; SQLite: `INTEGER PRIMARY KEY` sola). El
  CREATE TABLE de `drift` sube el riesgo a Medio y nombra las columnas que se
  crean sin numerarse solas. Escribirlas exige que `change.Column` modele el
  autoincremento —«el modelo no promete lo que no se puede escribir»— y eso
  va a pendientes.
- **C-18, C-20, C-22, C-23, C-24.** Objetos por `Kind + Table + Name + Args`;
  tipo y nulabilidad completos en los cambios de columna y `MODIFY … NULL |
  NOT NULL` en MySQL; sin comparar objetos cuando una lista vino incompleta;
  `tipoAceptable` valida fuera de las comillas; DEFERRABLE/MATCH, orden de
  columnas y particionado en `NoComparado`.
- **Review de las tandas cuatro y cinco (`high`)**, diez hallazgos, dos
  altos: (1) al emparejar esquemas de distinto nombre, las claves foráneas
  del origen seguían apuntando al esquema de origen y la migración emitía
  `REFERENCES shop_dev.…` contra prod —`renombrado` reescribe `RefSchema`—;
  (2) `X'…'` se aplicaba a TODO valor de una columna declarada BLOB, y SQLite
  tipa el valor: un texto `'hello'` salía `X'hello'` (archivo roto) y un `42`
  se volvía un blob de un byte. **El recorrido de SQLite expone la clase de
  cada celda** (`engine.CellClasses`) y el escritor SQL decide por celda
  (`export.ClassAware`); los motores tipados siguen por columna. Los medios:
  `HostKeyAlgorithms` con el tipo conocido PRIMERO y los demás después —solo
  el conocido dejaba sin entrada a quien cambió de tipo de clave y escondía el
  diálogo de «cambió»—; `recordarSecreto` no lee el keychain con «dejar como
  está» ni toma un error transitorio por «no había»; **el changeset sobrevive
  al cierre por inactividad** y vuelve con «Reconectar» a la misma base
  (`rescate`), porque tirar treinta ediciones por quince minutos afuera era
  peor que la sesión abierta. Los bajos: `setval` con `GREATEST(…, 1)` para
  ids en 0 o negativos; el encabezado del CSV se neutraliza solo si parece
  una llamada (`=HYPERLINK(…)`), no por empezar con `-`; `textoDeFecha`
  colapsa a fecha sola únicamente en columnas DATE; la línea del error de
  lectura del CSV sale del `ParseError`.

### Iteración 9 — 2026-09-11

**S03 TLS, hecha: los certificados van por ruta, MySQL habla libpq, y el
certificado del servidor se ve antes de confiar en él.** Lo que fijó la
implementación:

- **`SSLMode` se mudó a `engine`**, y `connection.SSLMode` es un alias, como
  `Engine` desde la iteración 6. Lo necesitaba el motor de MySQL: su driver no
  tiene «verify-ca» —`tls=true` verifica también el nombre del host— ni lee
  certificados de una cadena de conexión, así que el modo y los archivos le
  llegan por `engine.OpenOptions.TLS` y en `internal/mysql/tls.go` se traducen
  a un `*tls.Config`. La traducción sigue a libpq, que es lo que los nombres
  prometen: `verify-ca` verifica la cadena sin mirar el nombre (con
  `InsecureSkipVerify` y la verificación a mano, igual que pgx), `verify-full`
  deja que la biblioteca estándar haga las dos cosas, `prefer` y `allow` pueden
  seguir en claro. **Y `require` con una raíz cargada verifica la cadena**,
  como en libpq; `connection.VerifiesCertificate` es esa regla, y el aviso de
  «no verifica el certificado» la mira a ella y no al modo: sin eso, el aviso
  seguía ahí justo después de cargar la raíz para que verificara. El DSN de
  MySQL ya no lleva `tls=`: sería un segundo lugar que decide lo mismo y que
  el driver ignoraría.
- **Postgres lo lee del DSN**, con los nombres de libpq —`sslrootcert`,
  `sslcert`, `sslkey`— porque es donde pgx los espera, y pgx es quien abre los
  archivos. Las rutas llegan con el `~` resuelto: pgx abre la ruta tal cual y
  un `~` sin resolver es «no existe». Se guardan sin resolver, como la clave
  del túnel, para que la misma libreta sincronizada apunte al directorio de
  cada persona. `tunnel.CleanPath` y `tunnel.ExpandHome` se exportaron para
  eso: son las mismas rutas copiadas del mismo Explorador.
- **Rutas, no contenidos, y la clave de cliente sin cifrar.** Los tres
  archivos viven en esta máquina y la libreta —que se sincroniza y se
  comparte— solo los nombra; el archivo compartido no lleva ningún secreto
  igual que antes. La clave del cliente es un archivo que entrega el DBA, no
  algo que se escriba, y va sin cifrar porque es como la lee pgx; una cifrada
  necesitaría su frase de paso en el keychain, y eso espera a que alguien la
  tenga. Certificado y clave **van juntos o no van**: pgx rechaza las mitades
  sueltas con un error que habla de archivos y no de que falta la otra mitad.
- **El certificado del servidor se ve después de probar.** `ServerInfo.TLS`
  describe el canal: sujeto, emisor, para qué nombres e IP vale, vigencia,
  autofirmado o no, huella SHA-256 como la escribe openssl, versión y cifrado.
  Postgres lo lee del `*tls.Conn` que pgx expone; MySQL, de una devolución de
  llamada `VerifyConnection` en el `*tls.Config`, que corre también con
  `InsecureSkipVerify` —es lo que permite mostrar lo que NO se verificó, que
  es justamente cuando hace falta mirarlo—. Nil es «en claro», y el panel de
  la conexión lo dice con esas palabras en vez de omitir la fila.
- **«Guardar como raíz de confianza…»** es el camino para un servidor propio,
  que casi siempre es autofirmado: probar con `require`, comparar la huella
  con la que pasó quien lo administra, guardar el PEM, y el modo pasa a
  `verify-ca` si no verificaba. El binding `SaveCertificate` decodifica y
  vuelve a codificar el certificado antes de escribirlo: no sirve para
  escribir cualquier cosa en cualquier ruta desde la interfaz, y el test lo
  prueba con un texto suelto, otro tipo de bloque y basura después del
  bloque. El diálogo de guardar es del sistema, así que esa parte es prueba
  manual.
- **Los fallos de TLS traen el detalle del driver.** «Revisá el modo SSL» no
  alcanza para arreglar nada; «certificate signed by unknown authority» o
  «valid for db.interna, not 127.0.0.1» sí. Va en `Failure.Detail`, redactado,
  y la barra de la prueba lo muestra entre paréntesis.
- **Pegar una URI conserva `sslrootcert`, `sslcert` y `sslkey`**, que ahora
  tienen campo propio, y ya no los lista entre los descartados.
- **El Postgres de pruebas pasó a ser una imagen propia con TLS.** La oficial
  arranca con `ssl = off` y sin certificado, así que hasta ahora ninguna
  prueba de Postgres cifraba nada y la pestaña no se podía probar contra
  nada. `docker/postgres/Dockerfile` genera un autofirmado válido para
  `localhost` y `127.0.0.1` **en el build** —no en el repo: una clave privada
  commiteada es una filtrada aunque sea de juguete— y el test prueba las tres
  cosas que importan: `require` muestra el certificado, `verify-full` lo
  rechaza con las raíces del sistema y lo acepta con él mismo cargado como
  raíz, `verify-ca` con otra raíz lo rechaza. En MySQL y MariaDB el test no
  supone si el contenedor ofrece TLS —9.7, 8.4 y 12.3 sí, 10.11 no—: compara
  lo que capturó el canal con `Ssl_cipher`, que es lo que el servidor dice de
  la sesión, dos fuentes independientes. Y los seis modos se prueban sin
  motor, contra un servidor TLS de la biblioteca estándar con una PKI de
  juguete generada por el test: es la misma negociación que hace el driver,
  sin el protocolo de MySQL arriba.
- **Inyecciones en rojo:** un `verify-ca` que no verifica nada (dos tests lo
  ven), `require` con raíz que no verifica, el Postgres que nunca describe el
  canal, la regla de la raíz sacada de `VerifiesCertificate`, el par de
  cliente sin exigir. Las cinco fallaron y se revirtieron.
- **Lo que encontró el review `high`, los tres arreglados.** (1) pgx abre los
  certificados DURANTE `ParseConfig`, así que una raíz con la ruta mal
  tipeada, o que no está en esta máquina porque la libreta se sincronizó,
  llegaba como «la cadena de conexión no es válida» sin detalle: el error
  interior del `ParseConfigError` no lleva la cadena y nombra el archivo, y
  ahora es un fallo de TLS con ese detalle; una cadena inválida de verdad
  sigue sin citarse. (2) MySQL mandaba el nombre del host como SNI solo con
  `verify-full`; pgx lo manda en todos los modos, y un frente que elige el
  certificado por SNI presentaba el equivocado con `require` o `verify-ca`.
  Va siempre que sea un nombre; una IP no se manda, SNI no las admite.
  (3) «Guardar como raíz» cambia la conexión y eso limpia la prueba, así que
  la confirmación —que vivía adentro de la tarjeta del certificado— no se
  veía nunca; ahora ocupa el lugar de la tarjeta que se vació y pide probar
  de nuevo, que es lo que va a decir «verifica».
- **Lo que no se hizo, y por qué.** `allow` no se ofrece como chip —es
  `prefer` al revés y nadie lo elige a propósito—, pero si viene del archivo
  se muestra para que se vea. `channel_binding=require` sigue sin campo: pgx lo
  negocia solo cuando el servidor lo ofrece, y con un modo que verifica el
  certificado da lo mismo, que es lo que la pestaña ahora deja configurar
  bien. Una clave de cliente cifrada, ídem: espera a que alguien la tenga.

**Pase de movimiento, hecho: entra lo que aparece, sale lo que se cierra sin
desmontarse, y producción aparece de golpe.** Lo que fijó la implementación,
sobre las tres duraciones y la regla escritas el 2026-09-08:

- **Los diálogos salen, y cómo.** Un `<dialog>` cerrado se va de la capa
  superior y deja de dibujarse en el mismo instante: no hay nada que animar.
  `display` y `overlay` con `transition-behavior: allow-discrete` posponen ese
  cambio al final de la transición, y `@starting-style` da el estado del
  primer frame para la entrada. Pero casi todos los diálogos de la aplicación
  se **desmontan** cuando el padre recibe `onClose`, y un elemento desmontado
  tampoco anima: por eso `Dialog` dibuja la salida ANTES de avisar —clase
  `saliendo` mientras sigue abierto, y `onClose` cuando la transición del
  panel terminó, con un tope de 220 ms por si `transitionend` no llega—. Vale
  para lo que pide la persona: Esc, ✕, el clic en el velo. Lo que cierra el
  padre por su cuenta —guardó, aplicó— desaparece en el acto, que es lo que
  corresponde a algo que se acaba de accionar. Comprobado por CDP frame a
  frame: el diálogo sigue con `open` mientras la opacidad baja, y se cierra
  cuando llega el `transitionend` del panel.
- **Los toasts, igual y por lo mismo**: `ToastStack` saca el toast de la lista
  al recibir `onDismiss`, así que `Toast` se va primero y avisa después. Entran
  y salen por el borde derecho, que es donde viven. Es el único movimiento de
  la aplicación que hace trabajo de verdad: aparecen sin que nadie los pida.
- **Lo destructivo aparece, no llega.** `ConfirmDialog` con severidad `aviso`
  o `produccion` pasa `abrupto`; el diálogo de borrar una conexión y la vista
  previa de SQL contra producción también; y todo `Dialog` con `production`
  lo es sin que la pantalla lo pida —una clave de host que cambió entra por
  ahí, y es una alarma—. Sin transición de entrada ni de salida: `¿Borrar
  demo?` se cierra en el mismo frame.
- **Todo lo demás usa los tokens y nada más**: menús, submenús, la lista del
  combobox, los globos de ayuda y el panel de filtros en `--dur-fast`; la
  paleta, el editor de conexión y el velo de «Conectando» en `--dur`; el
  interruptor y las flechas de plegar, que cambian de posición, en
  `--dur-fast`. Quedaban cinco duraciones escritas a mano —`.1s`, `.12s`—
  con `ease` y `ease-out` sueltos; ya no hay ninguna. La barra de progreso
  del apply conserva su `linear`: no es una entrada, es una medida.
- **Lo que no se movió**, tal como estaba escrito: grillas, el lienzo del ERD,
  pestañas, paneles laterales. Y `prefers-reduced-motion` sigue apagando todo
  desde los tokens: ahí `transitionend` llega en el acto y la salida diferida
  de diálogos y toasts se vuelve inmediata sola.
- **Lo que encontró el review, y el más viejo de los bugs de la interfaz.**
  Leyendo el CSS compilado: CSS Modules localiza TODO nombre de animación que
  aparece en una declaración de un `.module.css` —lo reescribe como
  `_kn-slidein_hash_1`— aunque los keyframes vivan globales en `tokens.css`,
  y el navegador busca una animación que no existe. Así estuvieron **desde la
  iteración 0** el spinner de los botones, el pulso del punto de producción,
  el destello del contador de cambios —el que se «adelantó» el 2026-09-08
  porque hacía trabajo de verdad— y la entrada de menús, diálogos y toasts:
  con la duración puesta y sin moverse un píxel. El arreglo es
  `animation: global(kn-slidein)`, que es la forma que CSS Modules da para
  decir «este nombre es global»; los keyframes que cada módulo define para sí
  —el cursor de la firma, el spinner del preview— siguen locales. Comprobado
  en el CSS compilado antes y después.
- **Los otros tres del review.** «Cancelar» y «Cerrar» del pie son la persona
  pidiendo cerrar y salían en el acto mientras Esc salía suave: `DialogClose`
  es el botón del pie que pasa por la misma salida —un componente y no un
  hook, porque el pie lo escribe la pantalla que abre el diálogo, que está
  FUERA de él, y un hook ahí no vería el contexto—; va en `ConfirmDialog` y en
  los seis diálogos cuyo Cancelar solo cierra. Los que cancelan un trabajo en
  curso —volcado, exportación, importación— cierran en el acto: es accionar.
  `SqlPreview` pasaba, mientras corría, un `onClose` que no cerraba, y con la
  salida diferida eso dejaba el diálogo abierto e invisible tapando la
  aplicación: ahora no pasa `onClose` mientras corre, que es lo que los otros
  hacían, y además `Dialog` vuelve a mostrarse si a los 50 ms de avisar nadie
  cerró. Y `saliendo` se limpia cuando el padre cierra, con un efecto de
  layout para que no haya un frame en que el panel empiece a volver.
- **Con la ventana minimizada se cierra en el acto.** Salió de probar con el
  diálogo abierto y la ventana minimizada: el navegador congela las
  transiciones y estira los temporizadores hasta un minuto, y el `Cancelar`
  tardó sesenta segundos en cerrar. No hay nada que dibujar ahí: `Dialog` y
  `Toast` miran `document.hidden` y se van sin salida.

**S03 Advanced, hecha: search_path, nombre de aplicación, pool y SQL de
sesión — y la SQL de sesión no le gana a Safety.** Lo que fijó la
implementación:

- **Sin «Default schema».** El artboard lo tenía al lado de «Search path». No
  está, y no es un recorte: en Postgres el esquema por defecto ES el primer
  nombre del search_path, en MySQL es la base de la pestaña General y en
  SQLite es `main`. Un campo aparte sería una segunda forma de decir lo mismo,
  y dos formas de decir lo mismo se separan.
- **El search_path viaja en el paquete de arranque**, no como un `SET` después
  de abrir. pgx manda como parámetro de arranque cualquier clave de la cadena
  que libpq no conozca —es lo que ya hacía `application_name`—, así que va en
  el DSN y llega a CADA conexión del pool. Un `SET` en el editor vale para una
  sola conexión, y la consulta siguiente puede salir por otra: es la misma
  trampa que ya había costado el modo solo lectura. Solo Postgres; Normalize
  lo borra en los otros motores, que no tienen la noción.
- **El nombre de aplicación va donde cada motor lo mira**: `application_name`
  en Postgres —`pg_stat_activity`— y `program_name` en los atributos de sesión
  de MySQL —`performance_schema.session_connect_attrs`, la clave que usan el
  cliente de línea de comandos y Workbench—. Vacío es `kaname`, que es lo que
  se mandaba escrito a mano desde la iteración 1. Sin dos puntos ni comas,
  que son los separadores del formato de MySQL, y de 63 caracteres como
  máximo, que es donde Postgres trunca en silencio.
- **Los espacios van como `%20` en el DSN de Postgres, no como `+`.** pgx 5.11
  decodifica la cadena como libpq, que solo entiende `%XX`: con el `+` de
  `url.Values` un nombre de aplicación «lemy en dev» llegaba al servidor como
  «lemy+en+dev», comprobado con `SHOW application_name` desde la interfaz.
  Aplica también a las rutas de certificados con espacios.
- **El tamaño del pool tiene piso en dos**: cancelar una consulta necesita otra
  conexión. Cero es el default de siempre —cuatro, dos en solo lectura—; el
  tope de 32 es para que un error de tipeo no abra trescientas sesiones.
- **La SQL de sesión corre en cada conexión del pool, antes que nada, y las
  protecciones se aplican DESPUÉS.** Es la decisión importante de la pestaña.
  Sin ella, `SET default_transaction_read_only = off` en la SQL de sesión de
  una conexión de solo lectura apagaba la protección en silencio —y lo mismo
  `SET SESSION TRANSACTION READ WRITE` en MySQL y `PRAGMA query_only = 0` en
  SQLite—. Así que Postgres vuelve a pedir en `AfterConnect` lo que ya mandó
  en el arranque, MySQL corre primero la SQL de la persona y después las
  suyas, y SQLite vuelve a poner `query_only` después. Lo último que se dice
  es lo que queda, y los tres tests lo prueban con una SQL de sesión que
  intenta apagarlas: el INSERT sigue fallando y el `pg_sleep(10)` sigue
  cortándose. Para eso los dos motores de database/sql pasaron a compartir un
  conector con inicialización —`engine.WithInit`—, que antes solo tenía MySQL.
  **Y en MySQL las protecciones van también ANTES**, lo encontró el review:
  en Postgres y SQLite el modo solo lectura viaja en el arranque, así que la
  SQL de sesión ya corre protegida; en MySQL es un SET, y con las protecciones
  solo después, un `DELETE` en la SQL de sesión de una entrada importada
  corría con escritura en cada conexión del pool de una conexión marcada
  solo lectura. Ahora los tres motores fallan al abrir si la SQL de sesión
  escribe bajo solo lectura, y los tres tests lo prueban.
- **Una sentencia que falla dice que es la SQL de sesión y en qué línea**, con
  el código del motor, y la conexión no se abre. Antes de esta pestaña un
  error así habría sido «no se pudo conectar». Las sentencias se corren de a
  una —el driver de MySQL no acepta varias por viaje, a propósito— y por eso
  la línea se sabe.
- **Es SQL escrita por la persona y corre sin vista previa ni confirmación.**
  Por eso Warnings avisa si tiene algo que no sea configurar la sesión —todo
  lo que no sea SET, RESET, PRAGMA, USE o SHOW, nombrado sin repetir— y contra
  producción lo dice más fuerte. SELECT no está en la lista blanca a
  propósito: `SELECT set_config(…)` es configurar y `SELECT
  pg_terminate_backend(…)` no, y no hay forma de distinguirlos sin interpretar
  la función. Y **la vista previa de importación la muestra entera**: es SQL de
  otra persona que va a correr con las credenciales de esta, y lo mínimo es
  verla antes de tildar. Se importa —es configuración, no un secreto— pero se
  ve.
- **Encontrado de paso:** el panel de protecciones de S02 seguía diciendo que
  las otras tres «se editan por ahora en connections.toml y van a tener su tab
  en el editor», con la pestaña Safety hecha hace dos días. Y el comentario de
  `main.tsx` decía que el tema claro «llega en la Iteración 9».
- **Lo demás que encontró el review `high`, arreglado.** El Ping de MySQL
  corre la configuración de sesión —la SQL de la persona incluida— y no
  tenía tope: `cfg.Timeout` acota solo el discado, y un `DO SLEEP(100000)`
  dejaba «Conectando…» para siempre; ahora tiene el mismo tope que Postgres.
  Las pestañas TLS y Advanced guardan su bloque entero con `set("tls", …)`,
  así que lo tocado era el bloque y los problemas vienen por campo: escribir
  un 1 en «Conexiones» no mostraba nada hasta apretar Guardar; un campo
  cuenta como tocado también por su pestaña. Y el aviso de producción decía
  «escritura automática» aunque la conexión fuera de solo lectura, donde la
  escritura no corre: la conexión no abre, y ahora lo dice así.
- **Inyecciones en rojo:** Postgres sin reponer las protecciones después de la
  SQL de sesión, MySQL con el orden al revés, MySQL sin protegerla antes. Las
  tres fallaron y se revirtieron.

**Builds de Linux y macOS, y firma: qué hace falta y qué no.** La pregunta era
si había que conseguir una Mac y una Linux. No para construir ni para firmar;
sí, o alguien que la tenga, para probar lo que salió.

- **Construir.** Wails en Linux necesita GTK/WebKitGTK y en macOS Cocoa/WebKit,
  los dos por cgo, así que no se cross-compila desde Windows. Se agregan a la
  matriz de `build.yml` un `ubuntu-latest` y un `macos-latest`, que hoy es
  Apple Silicon. En beta.17 el backend de Linux por defecto es **GTK4 +
  WebKitGTK 6.0** (`libgtk-4-dev`, `libwebkitgtk-6.0-dev`; visto en el
  `pkg-config` de `linux_cgo.go`); GTK3 queda detrás del tag `gtk3`. Elegir uno
  es elegir qué distros lo corren: 6.0 pide Ubuntu 24.04 o más nuevo. Los Taskfiles por plataforma ya están en `build/`; lo que
  hay que corregir es la marca: `build/darwin/Info.plist` dice
  `CFBundleExecutable = kaname.exe` y `build/linux/desktop` apunta a
  `/usr/local/bin/kaname.exe`, porque los generó el template con el nombre del
  binario de Windows.
- **Linux no se firma.** No existe una cadena de confianza como SmartScreen o
  Gatekeeper; lo que se distribuye es un AppImage o un `.deb`/`.rpm` (nfpm ya
  está en `build/linux/`) más el `SHA256SUMS` del release. Nada que comprar.
- **macOS: Developer ID + notarización, sin Mac.** Apple Developer Program,
  USD 99 por año, disponible en Argentina. Con eso se emite un certificado
  «Developer ID Application» —el CSR se genera con `openssl` en cualquier
  sistema— y se guarda como `.p12` en un secret de GitHub. El runner de macOS
  hace `codesign`, `notarytool submit --wait` y `stapler`. Sin esto, desde
  macOS 15 no alcanza con clic derecho → Abrir: el usuario tiene que ir a
  Ajustes → Privacidad y seguridad → «Abrir de todos modos», y la mayoría no
  llega.
- **Windows: la opción barata no está disponible acá.** Azure Artifact Signing
  (ex Trusted Signing) es lo que Microsoft recomienda y lo que cuesta menos,
  pero **los desarrolladores individuales tienen que estar en Estados Unidos o
  Canadá**, y las organizaciones en una lista que no incluye a la Argentina.
  Queda el certificado OV de una CA (Sectigo, DigiCert, GlobalSign, SSL.com,
  Certum), del orden de USD 200–400 por año, con la clave en un token de
  hardware o en el HSM en la nube de la CA —desde 2023 el CA/B Forum no permite
  clave en archivo—. Para CI sirve solo la variante en la nube. Si el proyecto
  se publica como código abierto con licencia, hay dos caminos más baratos que
  vale la pena mirar en el momento: el certificado «Open Source» de Certum y
  SignPath Foundation, que firma gratis proyectos abiertos desde su propio
  pipeline. La firma OV **no da reputación instantánea** en SmartScreen: se
  gana con descargas de releases firmados con la misma identidad.
- **Probar.** Un runner de CI construye y firma pero no muestra una ventana.
  Antes de publicar un build de macOS o Linux alguien lo tiene que abrir: una
  Mac prestada, una Mac mini usada, o una VM de Linux —esa sí se puede tener
  en esta máquina—.

**Marca en Linux y macOS, builds en CI y la licencia: el repo se hace público
sin pagar nada.** La decisión del usuario, con las opciones de arriba sobre
la mesa: builds para los tres sistemas **sin firma**, licencia Apache 2.0, y
el repo público cuando el checklist de pre-publicación esté en verde. Lo que
fijó la implementación:

- **Linux: nueve PNG, un SVG y un `.desktop` escritos, no generados.** Los
  íconos van al árbol hicolor de freedesktop (`16 22 24 32 48 64 128 256
  512` más `scalable/apps/kaname.svg`), tal cual los entrega el kit —los de
  16, 24 y 32 son píxel por píxel los mismos cortes del `.ico` de Windows;
  el de 22 es el corte de 16 hecho para ese tamaño—, y sin los ~6 KB de
  metadatos C2PA que traía cada uno. El `.desktop` está commiteado porque
  `wails3 generate .desktop` escribía `Keywords=wails` y `Name=kaname` en
  minúscula, y `Exec=/usr/local/bin/kaname.exe` era lo que el template había
  dejado. El `nfpm.yaml` también decía `kaname.exe`, `license: MIT` y
  `homepage: wails.io`: ahora instala en `/usr/bin/kaname`, lleva los diez
  íconos, el `.desktop`, el LICENSE en `/usr/share/doc/kaname/copyright`, y
  el postinstall actualiza el caché de íconos además del de menús. Se
  comprobó armando el `.deb`, el `.rpm` y el paquete de Arch acá, en
  Windows, con un binario de mentira —nfpm es Go puro— y listando lo que
  quedó adentro. El AppImage no se puede armar acá: linuxdeploy es Linux.
- **macOS: el ícono por capas es un documento de texto.** `build/appicon.icon`
  es una carpeta con `icon.json` y las capas; el template traía el logo de
  Wails. El de Kaname declara el fondo del tile claro (`#f7f7f5`) con
  especialización `dark` (`#1c1e22`), y una capa —la marca en `#4a5bd6`—
  recoloreada a `#7c8cff` en `dark` y a un gris claro en `tinted`, a escala
  0,6 del lienzo, que es la proporción del tile del kit. `actool`, que viene
  con Xcode 26, lo compila a `Assets.car` **y** a un `.icns` renderizado;
  `macos-latest` es macOS 26 con Xcode 26 desde julio de 2026, así que
  pasa en CI. El formato se verificó contra el esquema publicado
  (`fill-specializations` a nivel raíz para el fondo, `appearance` en
  `base|light|dark|tinted`), no contra actool: eso recién lo dice el primer
  build de CI. **`Assets.car` salió del repo**: era 1,6 MB del ícono de
  Wails, y ahora se genera; lo mismo los 3,7 MB de fondo e ícono de volumen
  del DMG del template, junto con la tarea `package:dmg`. El `.app` va en un
  `.zip` hecho con `ditto`, que conserva los atributos del bundle; un DMG con
  arte propio queda para cuando haya un Mac donde mirarlo. El `.icns`
  commiteado se regeneró desde el tile claro del kit, que es lo que el kit
  indica como respaldo para macOS anteriores a 26.
- **`generate:icons` ya no toca `windows/icon.ico`.** El flag
  `-windowsfilename` tiene `build/windows/icon.ico` por defecto, así que
  omitirlo no alcanzaba —lo decía la nota del 2026-09-09—; se le pasa `""`
  y la tarea produce solo lo de macOS. Con Xcode 26 compila el `.icon`; sin
  él, o en otro sistema, cae al `.icns` del tile, que es idéntico al
  commiteado. Comprobado acá: correrla deja el `.ico` intacto y el `.icns`
  byte a byte igual.
- **`Info.plist` decía `CFBundleExecutable = kaname.exe`.** El template lo
  generó con el nombre del binario de Windows. Ahora `kaname`, con
  `LSApplicationCategoryType = developer-tools` y el copyright con nombre.
- **La versión vive en seis lugares y un test los compara.** `appinfo.Version`
  es lo que muestra About; `config.yml`, `info.json`, los dos `Info.plist` y
  `nfpm.yaml` son lo que lee cada empaquetador. `wails3 task
  common:update:build-assets` los regeneraría desde `config.yml`, pero pisa
  también lo editado a propósito —`Info.plist`, `nfpm.yaml`— así que no se
  usa: la versión se sube a mano y `TestLaVersionEsLaMismaEnTodosLados`
  avisa si una quedó atrás. Inyección: `nfpm.yaml` en 0.1.1 lo puso en rojo.
- **CI: tres jobs de build y un release en borrador.** Linux instala
  `libgtk-4-dev libwebkitgtk-6.0-dev` más lo que el plugin de GTK de
  linuxdeploy pide (gdk-pixbuf, glib, dpkg-dev), y corre AppImage, `.deb` y
  `.rpm` como pasos separados para que un fallo diga cuál. macOS corre
  `darwin:package:universal`, que compila las dos arquitecturas nativas y
  las une con `lipo`. El job `release` solo existe con un tag `v*`, es el
  único con `contents: write`, y crea el release **en borrador**: se publica
  a mano después de mirar las notas. En un repo público los runners son
  gratis e ilimitados; mientras sea privado, el de macOS cuenta ×10 contra
  los 2.000 minutos mensuales, y por eso los builds de tag son la única vez
  que vale la pena.
- **Sin firma, con los avisos escritos en el README.** Windows: SmartScreen y
  «Ejecutar de todos modos». macOS 15+: Gatekeeper y «Abrir de todos modos»,
  o `xattr -d com.apple.quarantine`. Linux: nada. Quien clona y compila no ve
  ninguno: la cuarentena es para lo descargado. Lo que cambiaría la decisión
  —usuarios de Mac que lo pidan, o descargas que justifiquen SignPath, que
  firma gratis proyectos abiertos en Windows— no existe hoy.
- **Apache 2.0, texto canónico, copyright en `NOTICE`.** El `LICENSE` es el
  texto oficial byte a byte (SHA-256 `cfc7749b…523d30`), que es lo que
  GitHub detecta; el titular y el año van en `NOTICE`, que la propia licencia
  obliga a conservar en cada redistribución. El motivo de Apache sobre MIT
  está en la iteración 0: concesión de patentes y la cláusula de marcas que
  preserva el nombre ante un fork. `nfpm.yaml` dice `Apache-2.0` y el
  `.deb` lleva el texto en `/usr/share/doc/kaname/copyright`.
- **Lo que encontró el review.** `Info.plist` declaraba `CFBundleIconName =
  appicon` siempre, y con `Assets.car` fuera del repo eso es un `.app` que
  apunta a un catálogo que no está: macOS resuelve el ícono por ese nombre
  ANTES que por `CFBundleIconFile`, así que en vez de caer al `.icns` se
  queda sin ícono. La clave salió de los dos plist y la agrega
  `create:app:bundle` con PlistBuddy solo cuando el catálogo existe —que es
  solo en macOS, donde PlistBuddy siempre está—. El test de versiones miraba
  una sola de las dos claves de `Info.dev.plist`; ahora las dos, y con la
  inyección en rojo. Y el job `release` compara el tag con
  `appinfo.Version` antes de bajar nada: un `v0.2.0` sobre binarios 0.1.0
  salía con el título bien y todo lo de adentro mal.
- **Lo que este commit no hace público.** La visibilidad del repo la cambia
  el usuario, y no antes de que el checklist de pre-publicación esté en
  verde: `gitleaks` sobre el historial completo, la revisión de logs, y los
  tests de integración de los cuatro motores. La licencia era el último
  ítem que se podía marcar desde el código.

**Dos tests que no probaban lo que decían, encontrados por la matriz de CI.**
El primer push después de los builds puso en rojo las cuatro patas de
integración, y ninguna por el código:

- `TestExportarEncimaDeLaLibretaSeNiega` exigía negarse a exportar sobre la
  ruta de la libreta EN MAYÚSCULAS. Eso es la libreta solo donde el sistema
  de archivos no distingue mayúsculas —Windows, y macOS por defecto—; en
  Linux es otro archivo, que además no existe, y negarse ahí sería un bug.
  Ahora le pregunta al sistema: la variante entra solo si `os.Stat` de la
  ruta en mayúsculas resuelve. No es `runtime.GOOS == "windows"` a propósito:
  la pregunta es del sistema de archivos, no del sistema operativo.
- `TestLaVistaNoPierdeSusOpcionesAlVolverAEscribirla` creaba la vista con
  `security_invoker`, que existe desde Postgres 15, y la 14 sigue en la
  matriz porque es el mínimo soportado. En 14 prueba `check_option`, que es
  lo que la 14 sabe; en 15+ las dos, como antes. Verificado acá contra un
  Postgres 14 levantado para eso, con el paquete entero en verde.

De paso el README dice, en una tabla, desde qué versión de cada motor se puede
usar Kaname: la pregunta la hizo el usuario y la respuesta estaba repartida
entre `engine.minimas` y tres comentarios.

**gitleaks sobre el historial entero, como job de CI: el primer ítem del
checklist de pre-publicación.** Primero se corrió acá, sobre los 146 commits:
con las reglas de fábrica, **limpio**. Pero limpio con reglas que no buscan lo
que este proyecto podría filtrar, y eso se comprobó antes de creerlo: una
muestra con una connection string de Postgres con la contraseña `hola1234`
adentro, y la misma contraseña en un `password = "…"`, pasó sin un solo
hallazgo. Las reglas de fábrica
conocen tokens de proveedores y claves privadas, y la genérica exige entropía
alta; una DSN con la contraseña adentro, o una contraseña de persona entre
comillas, no llegan. Lo que quedó:

- **Dos reglas propias en `.gitleaks.toml`.** `kaname-dsn-con-contrasena`:
  `motor://usuario:contraseña@` para postgres, mysql, mariadb, mongodb, redis,
  amqp, ssh, http… `kaname-password-literal`: `password = "…"` —o `:`— con
  la clave `password`, `passwd` o `pwd`, con prefijo en MAYÚSCULAS
  (`DBPASSWORD`, `POSTGRES_PASSWORD`) o con guion bajo (`db_password`), entre
  comillas o no (JSON), y el valor entre comillas, sin entropía mínima. Los
  dos límites son a propósito. Una variante que aceptaba `password = valor`
  sin comillas disparaba sobre `password = guardada` y `sec.Password =
  secreto`, que son asignaciones de Go; y el prefijo no puede ser camelCase
  porque `sshPassword = "kaname"` y `sqlStateInvalidPassword = "28P01"`
  también lo son, y una lista blanca de identificadores es la que hace que la
  gente apague el escáner. Con las dos reglas, el historial tiene 50
  disparos: todos fixtures de tests (`s3cr3t`, `hunter2`, `kaname` del
  compose, `a%40b` de los tests de escape), un comentario de `uri.go` y el
  ternario `? "password" : "text"` de `PasswordField`, que no es una
  asignación y tiene su lista blanca por forma de línea.
- **Lista blanca por valor, no por archivo.** Una lista por archivo
  —«todo `_test.go`»— dejaría pasar una DSN real pegada «un momento» en un
  test, que es exactamente el descuido plausible. Cada contraseña de mentira
  está enumerada por su valor; agregar una nueva a un test es agregarla al
  `.gitleaks.toml` a conciencia.
- **El job se prueba a sí mismo antes de escanear.** Arma esa misma muestra
  —en partes, con `printf`, para que ni el YAML ni este plan contengan la
  forma que la regla busca— y exige que las dos reglas
  disparen. Se comprobó que sirve: con la lista blanca cambiada a `^.*$`, el
  paso falla. Y el paso del escaneo cuenta los commits del clon y falla si hay
  menos de dos, porque sin `fetch-depth: 0` checkout baja uno y «todo el
  historial» es un commit.
- **`go install` pineado, no la GitHub Action.** Misma integridad que
  govulncheck (versión exacta, checksum database de Go) y sin la validación
  de licencia contra un servidor externo que la action hace en repos de
  organización. `--redact` obligatorio: el log de un repo público es público.
- **Si un día encuentra algo, la respuesta no es borrarlo**: es rotar el
  secreto —ya está comprometido— y reescribir el historial con
  `git filter-repo`, que cambia todos los hashes y obliga a un force push.
  Por eso este ítem fue el primero del checklist.

Review medium, un hallazgo: la primera versión de la regla exigía un guion
bajo antes de `password`, y se le pasaban la clave de JSON entre comillas y
`DBPASSWORD` sin guion. De ahí el prefijo en mayúsculas o con guion bajo, y
la comprobación de que camelCase sigue afuera.

El job `release` pasa a llamarse «Release» a secas: con `${{ github.ref_name
}}` en el nombre, GitHub lo mostraba sin evaluar en cada push que no es un
tag, y parecía un error.

**Keychain, sockets y TOFU: de «así está diseñado» a «así se comprueba».**
Los tres ítems siguientes del checklist. Ninguno cambió el diseño; cada uno
ganó el test que faltaba, y uno encontró que el plan mentía.

- **Los secretos solo van al keychain.** `TestLosSecretosVanAlKeychainYANingunArchivo`
  arma los mismos stores que `main.servicios` sobre el reparto real de rutas
  —`appinfo.PathsIn`, exportada para eso— en dos raíces temporales, hace todo
  lo que la app hace con el disco (guardar, editar y duplicar una conexión con
  contraseña y bastión, preferencias, diagrama, historial, consultas guardadas,
  known_hosts) y después lee cada archivo **byte a byte** buscando tres
  centinelas. Byte a byte y no por formato: `json.Unmarshal` no ve un campo que
  el tipo no declara. Exige además que el keychain sí los tenga —un Save que
  tirara la contraseña pasaría un test que solo mira el disco— y que haya
  mirado por lo menos seis archivos. `TestSiElKeychainFallaNoSeGuardaNadaEnNingunLado`
  es Linux sin Secret Service: el `Set` falla con el error de D-Bus, la
  conexión no se guarda y nada toca el disco. Ese caso estaba afirmado en un
  comentario y `failSet` existía en el fake desde la iteración 1 **sin que
  ningún test lo usara**. Las dos inyecciones que los ponen en rojo: escribir
  el secreto a un archivo al lado de la libreta, y guardar la conexión antes
  que el secreto. Dato que respalda la afirmación sobre Linux:
  `go-keyring` v0.2.8 no tiene ni un `os.WriteFile`; sin Secret Service
  devuelve error, nunca un archivo.
- **Ningún socket, en ninguna configuración.** Tres capas. La estática es
  `sockets_test.go`, en la raíz: recorre `main.go` e `internal/` por el árbol
  sintáctico —no `strings.Contains`, para que un comentario no lo rompa— y
  prohíbe `net.Listen*`, `http.ListenAndServe*`, `http.Serve*`,
  `httptest.New*Server`, `.ListenAndServe()` con cualquier receptor y
  `ssh.InsecureIgnoreHostKey`. Es la generalización a todo el módulo del que
  `internal/tunnel` ya tenía para su paquete. La dinámica es
  `scripts/sockets.ps1`: lanza el binario limpio —rechaza el de debug, que
  abre el 9222—, espera, y le pregunta al sistema por cada socket del árbol de
  procesos, WebView2 incluido; falla con un TCP en Listen, un endpoint UDP o
  cualquier socket de `kaname.exe`. Corrido el 2026-09-11 contra el build
  limpio: siete procesos, cero Listen, cero UDP, cero sockets de
  `kaname.exe`. Comprobado que falla: contra un binario de cinco líneas que
  escucha en loopback, sale con 1. La tercera capa es lo que encontró el
  test de Taskfiles: **el plan decía desde la iteración 0 que `build:server`,
  `run:server` y `build:docker` se habían borrado, y no era cierto** —`git
  log -S` muestra que entraron con el esqueleto y nunca salieron—. Son la app
  compilada como servidor HTTP sin ventana, con los bindings y las
  credenciales detrás de un puerto. Ahora sí están borradas, y
  `TestNingunTaskfileCompilaElModoServidor` falla si vuelven por nombre o si
  algún Taskfile compila con `-tags server` o `mcp`, que son los dos
  listeners que Wails v3 trae detrás de build tags. El `build:docker` de cada
  sistema —cross-compilar dentro de una imagen— no se busca por nombre porque
  no levanta nada.
- **Lo que el script muestra y no es de la app.** `msedgewebview2.exe` abre
  dos HTTPS salientes a Microsoft (52.97.x.x, tres la primera vez que se crea
  el perfil) al arrancar, con la app sin haber conectado a nada. Es el runtime
  de WebView2 reportando y buscando configuración, que la sección 3 ya tenía
  anotado como el precio de no embeber Chromium. Se intentó apagarlo con
  flags de Chromium, y la primera medición engañó: vía
  `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` parecía bajar de tres a dos, pero
  **esa variable no la lee esta combinación de Wails y runtime** —un
  `--remote-debugging-port` por ahí no abre ningún puerto—, así que las
  corridas medían el mismo binario sin flags, y el «tres» era el primer
  arranque. La medición válida fue con los flags compilados en
  `WindowsOptions.AdditionalBrowserArgs`: `--disable-background-networking`,
  `--disable-component-update`, `--disable-domain-reliability`,
  `--metrics-recording-only` y `--no-pings`, tres corridas, **dos conexiones
  igual**. No hacen nada contra esto, y lo que no se mide no se optimiza a
  ciegas: no van al binario. No se identificó el host —Chromium resuelve por
  su cuenta y no pasa por el caché de DNS de Windows; verlo es `pktmon`, con
  admin—. Lo que sí lo gobierna es el ajuste de diagnóstico opcional de
  Windows, y eso está escrito en el README, en «Principios», donde antes decía
  «sin phone-home» a secas. Queda en la sección 4.
- **TOFU sin servidor.** `verificador` es la única puerta entre «el servidor
  presentó una clave» y «se manda una credencial», y es una función pura:
  `TestElVerificadorSoloAceptaLaClaveQueSeAcepto` la prueba con las seis
  combinaciones de known_hosts × aceptación de una vez × clave presentada,
  con claves ed25519 generadas en el test. Los de integración cubrían dos de
  las seis; las otras cuatro no se pueden provocar contra un servidor que no
  cambia de clave, y la que importa es esa: la persona aceptó una huella en
  el diálogo y al conectar el servidor presenta OTRA. «Conectar una vez»
  acepta **esa** huella, no la que venga. Dos inyecciones, dos rojos: la rama
  «cambió» devolviendo nil, y `AcceptOnce` aceptando cualquier huella.

Review high, cuatro hallazgos, los cuatro huecos en los guardias nuevos y no
en la app: el test del código miraba el NOMBRE del identificador y no la ruta
del import, así que `import n "net"` lo esquivaba —ahora resuelve
`f.Imports`, y `.Serve(l)` sobre un `*http.Server` entró a la lista—; los
tags se cortaban solo por coma, y Go acepta `-tags "server production"` con
espacio; el recorrido de Taskfiles miraba todo el repo, con lo que un worktree
viejo en `.claude/` lo ponía en rojo —ahora solo el de la raíz y `build/`—; y
el script daba por bueno un binario que muere al arrancar, porque un proceso
muerto no tiene sockets —ahora exige que siga vivo y que el árbol tenga los
procesos de WebView2—. Cada uno verificado con su inyección.

**Logs, errores y eventos sin credenciales: el inventario, con guardia.** Los
dos ítems que quedaban del checklist. La revisión empezó por listar cada canal
por el que algo sale de la aplicación y qué podría llevar; lo que sigue es esa
tabla, con lo que la prueba.

| Canal | Qué podría llevar | Cómo se sabe que no |
|---|---|---|
| Logs propios | Cualquier cosa | No hay: dos `log.Fatal` en `main.go` sobre el directorio de datos, y nada en `internal/`. `TestLaAplicacionNoLogueaNiEmiteEventos` prohíbe `log`, `log/slog`, `fmt.Print*` y `os.Stderr/Stdout` por ruta de import. |
| Logger de Wails | **Los argumentos y el resultado de cada binding**: Wails loguea «Binding call complete: args=… result=…» en Debug —la contraseña de `SaveWithSSH`, las filas de cada consulta—. | En producción el logger es `io.Discard` (`logger_prod.go`). `TestMainNoConfiguraElLoggerDeWails` impide que `main.go` le dé un `Logger` o un `LogLevel`. |
| Eventos al frontend | Lo que se empuje | No hay ninguno; el mismo test prohíbe `.Emit`/`.EmitEvent`. Lo que cruza el puente es un valor de retorno, revisado abajo. |
| Fallo de conexión (`engine.Failure`, cruza como JSON) | La contraseña, si el driver o el servidor la citan | `engine.Redact` enmascara toda DSN con credenciales antes de armar el `Detail`. Y ahora la suite común tiene «el fallo de autenticación no lleva la contraseña»: manda una incorrecta **por la red**, exige `Kind == auth`, y revisa `Message`, `Hint`, `Detail`, `%v`, `%+v`, `%#v` y el JSON. Corrido contra Postgres, MySQL 9.7 y 8.4, MariaDB 12.3 y 10.11; SQLite lo saltea porque no tiene contraseña. |
| Error del túnel | La contraseña del bastión o la frase de paso | `TestLosErroresDeAutenticacionNoLlevanElSecreto`: contraseña incorrecta contra el servidor y frase de paso incorrecta sobre una clave cifrada, las cuatro formas de convertir el error en texto. El contenido de la clave ya lo cubría `TestUnErrorDeClaveNoFiltraSuContenido`. |
| Error de sentencia (UI) | Valores de fila: Postgres pone `Key (email)=(…)` en el DETAIL, MySQL `Duplicate entry '3'` | Llega a la pestaña Mensajes, que es donde corresponde: es lo que la persona acaba de escribir. Lo que importa es que no se **persista**: el historial guarda `Failed`, no el mensaje (comentario en `history.Entry`, con el motivo). |
| Historial y consultas guardadas | Una contraseña escrita en la SQL | `LlevaSecreto` rechaza la forma; `TestLosSecretosVanAlKeychainYANingunArchivo` lo verifica byte a byte. |
| `pg_dump` | La contraseña en la línea de comandos, que se ve en la lista de procesos | Va por `PGPASSWORD` en el entorno del proceso hijo, con `--no-password`; el comando que se copia no la lleva. Ya estaba así; verificado leyendo. |
| Frontend | HTML inyectado con datos de la base; `console.*` con filas | `TestElFrontendNoInyectaHTMLNiEscribeEnLaConsola`: sin `dangerouslySetInnerHTML`, `innerHTML` ni `console.*` en `frontend/src`. |

Tres cosas que salieron de hacer la tabla y no de suponerla. Una: lo del logger
de Wails no estaba escrito en ningún lado, y es la fuga más fácil de meter
—`LogLevel: slog.LevelDebug` «para ver qué pasa»— y la más difícil de ver
después. Dos: `%v` y `%+v` de un `*engine.Failure` no filtran ni con la
inyección, porque `Failure` implementa `error` y `fmt` imprime `Message`;
`%#v` y el JSON sí, y son los que un ticket copia. Tres: el Failure de un
`Kind` que no sea `auth` no prueba nada —un fallo de red tampoco lleva la
contraseña—, por eso el caso exige el tipo. Cada guardia y cada caso se puso
en rojo con su inyección: un `slog.Info` y un `.Emit` en un paquete,
`LogLevel` en `main.go`, un `console.log` en el frontend, el DSN entero
pegado al `Detail` de Postgres, la contraseña y la frase de paso en los dos
errores del túnel.

Review high, tres hallazgos, los tres huecos en los guardias: el valor de
`-tags` se cortaba en la primera comilla, así que un
`{{if eq .X "true"}}server{{end}}` escondía el tag —ahora se sacan los
`{{…}}` y se mira hasta el fin de la línea—; los dos tests de código miraban
`main.go` e `internal/` y un `debug.go` en la raíz quedaba afuera —ahora
todos los `.go` de la raíz, con un helper compartido—; y el del logger de
Wails solo veía la clave del literal, no `opts.LogLevel = …` después ni un
literal armado en otro archivo de la raíz. Los tres verificados con su
inyección.

**La batería de los cuatro motores, con la evidencia que pedía el checklist.**
Corrida acá el 2026-09-11 con los seis contenedores y
`KANAME_REQUIRE_ENGINES=1 KANAME_REQUIRE_POSTGRES=1 KANAME_REQUIRE_SSH=1`:
23 paquetes en verde, **1248 casos PASS, 0 FAIL, 4 SKIP**, los cuatro con su
motivo escrito —columnas VIRTUAL de Postgres 18 contra un 14, un nombre de
archivo con `?` que Windows no acepta, el agente SSH que no se puede levantar
desde un test, y SQLite sin contraseña—. Es lo mismo que CI corre en cada push
contra Postgres 18, 17, 16 y 14, y que estuvo verde en las corridas de hoy.

**Automatizar los bumps de dependencias: postergado.** Decisión del usuario:
con `govulncheck` y el job `deps` alcanza por un tiempo. El análisis quedó en
`bumps-de-dependencias.md`, breve y con las siete reglas que la herramienta
tiene que cumplir, para no rehacerlo cuando llegue el momento.

**Dependabot, activado; `staticcheck` en CI; la firma, anotada para después.**
El mismo día, al mirar qué cambia con el repo público: los PRs del bot
corren CI con minutos gratis, y un CVE conocido sin subir es visible para
cualquiera. Lo que decidió entre Dependabot y Renovate no fue la lista de
funciones —las dos cumplen las siete reglas— sino **quién ejecuta**: Dependabot
corre dentro de GitHub; Renovate Cloud es Mend corriendo contra el repo desde
su infraestructura con permiso de escritura. Para un proyecto cuyo argumento es
«ninguna dependencia llama a casa», sumar un tercero con acceso de escritura
para ahorrarse tres `go install` en un YAML no cierra. El resto de la
comparación, verificada contra lo que tiene el repo y con fuentes, está en
`bumps-de-dependencias.md`.

- **`.github/dependabot.yml`**: `gomod`, `npm` (en `/frontend`) y
  `github-actions`, semanal, `cooldown.default-days: 7` en los tres —el mismo
  reloj que `minimum-release-age` de pnpm; el default de GitHub bajó a tres
  días en julio—, `versioning-strategy: increase` en npm para que nunca
  vuelva un `^`, sin `groups`, y `ignore` para `wails/v3`, `@wailsio/runtime`,
  los tres drivers y `x/crypto`. El job `deps` se borró: lo duplicaba. Review
  medium, un hallazgo: un `ignore` sin `update-types` calla también los PRs
  de seguridad, y un CVE es justo el único motivo por el que una de esas se
  sube sin esperar; cada entrada ignora solo las actualizaciones de versión.
- **Lo que un bump no revisa solo.** El PR trae el changelog y CI en verde,
  y eso agarra una API que desapareció y un cambio de comportamiento que un
  test cubra. Una **deprecación** no: compila y pasa. Por eso entró
  `staticcheck` v0.8.1 al job `check`, con SA1019 que marca cada uso de algo
  deprecado; verificado inyectando un `ioutil.ReadAll`. La primera corrida dio
  nueve avisos: siete `ST1005` —«los errores no van con mayúscula ni punto»—
  sobre mensajes que la interfaz muestra tal cual («Kaname todavía no sabe
  leer esta clase de objeto»), que se apagan en `staticcheck.conf` con el
  motivo escrito; un helper de test sin usar, borrado; y un helper de test que
  devolvía `(error, *TLSInfo)`, dado vuelta.
- **La firma, para otro día.** El usuario la hace después de unos días de uso
  público. `firma-de-codigo.md` tiene lo verificado contra los términos de
  SignPath Foundation: licencia OSI, release publicado, build automatizado,
  MFA, política de firma con tres roles, y que el certificado se emite a
  SignPath Foundation —el publicador que muestra Windows es ese—. Y el orden:
  público, `v0.1.0`, política, aplicar, paso en el job `release`.

**S20, la pantalla y el archivo: cinco decisiones y un bug que solo la prueba a
mano encontró.**

- **Las sentencias las escribe el motor del destino, al comparar.** La
  comparación emite `change.Change`, que no es SQL; y `RenderDDL` es un método
  de la conexión abierta porque SQLite necesita leer la tabla para
  reconstruirla. Así que `Compare` deja el destino abierto hasta el final y
  devuelve `Statements`, un mapa por ID de diferencia. Si el motor no supo
  escribir una —no debería: la comparación solo emite lo que `Validate`
  acepta—, la diferencia vuelve SIN operación y con el motivo, que es el
  contrato del paquete. La pantalla no arma SQL ni la manda de vuelta.
- **La migración sale de la comparación en memoria, no de la interfaz.**
  `Compare` devuelve un ID; `Migration` y `SaveMigration` lo exigen y se
  niegan si ya hubo otra comparación. Es lo que garantiza que el archivo dice
  exactamente lo que la pantalla mostró, y de paso que ningún texto de la
  interfaz termina en un archivo `.sql` con nombre de migración.
- **El archivo va en orden ejecutable, no en el de la pantalla.** La lista se
  ordena por esquema y nombre, que es cómo se lee; un archivo se corre, y ahí
  una clave foránea hacia una tabla que se crea diez líneas más abajo falla.
  Van por clase: tablas nuevas, columnas, claves foráneas, el resto, y al
  final —como comentarios, con el motivo— las diferencias sin sentencia que se
  eligió incluir. Un test manda los IDs al revés y exige el CREATE TABLE antes
  del FOREIGN KEY.
- **Nada corre desde esta pantalla, y por eso comparar contra producción no
  pide confirmación.** Es texto: se guarda con el diálogo nativo del sistema
  —prueba manual, como todo diálogo nativo— o se copia. El «Copiar» pide el
  texto a Go y lo copia en el mismo clic, para que nunca se copie una
  migración de antes de tocar una casilla.
- **Entradas: el gestor de conexiones y la paleta, no el botón de desborde.**
  El botón del gestor aparece con dos conexiones o más; el menú contextual
  ofrece «Comparar contra otra…» con la fila como origen; desde el workspace
  la paleta la abre con la conexión abierta como origen, encima del Shell y
  no en su lugar, como Ajustes. El desborde es de la aplicación, no de la
  base, y ahí no va.

**El bug: las vistas y funciones no se comparaban, y la pantalla decía que sí.**
`Introspect` no trae los objetos que no son tablas —los pega la sesión después,
con `conObjetos`— y la primera versión de `Compare` no lo llamaba. La lista de
«no comparado» afirmaba que se comparaban «por nombre» mientras una vista que
faltaba en el destino no aparecía. Lo encontró la prueba a mano con
`docker/demo-drift.sql`, que tiene una vista justamente para eso; los tests de
Go con dos SQLite lo cubren ahora, y si un lado no puede listar sus objetos, la
comparación lo dice en vez de comparar contra una lista vacía. Del mismo pase:
la nota de una tabla nueva avisa que sus claves foráneas no van en el CREATE
TABLE —aparecen en la comparación siguiente— y «1 columnas» dejó de existir.

**El review encontró que el archivo no se podía correr entero, dos veces, y las
dos en SQLite.** Las sentencias son correctas de a una; lo que fallaba era
juntarlas en un archivo.

- **Una reconstrucción de tabla se escribe desde la definición que hay AHORA.**
  Si en el archivo la precede otra sentencia sobre la misma tabla, la
  reconstrucción no la conoce: un `ADD COLUMN email` seguido del rebuild de un
  `SET NOT NULL` crea la copia sin `email`, la llena sin `email` y tira la
  original. La columna desaparece sin un solo error. Regla: se recorre en el
  orden del archivo y toda reconstrucción cuya tabla ya fue tocada se
  convierte en una diferencia sin sentencia, con el motivo y el camino
  —aplicar y volver a comparar—. La primera se queda: una sentencia común
  DESPUÉS de una reconstrucción sí opera sobre la tabla ya reconstruida. El
  fixture de los tests tiene el caso a propósito.
- **Las sentencias peladas no son las que corre la aplicación.** Al aplicar,
  `Begin` apaga las claves foráneas ANTES del BEGIN y prende
  `legacy_alter_table`; sin eso el DROP TABLE del rebuild dispara los
  `ON DELETE CASCADE` de las tablas hijas y una vista hace fallar el RENAME.
  Un archivo con solo las sentencias, corrido desde cualquier cliente con las
  claves prendidas, borraba las filas hijas en silencio. El archivo lleva
  ahora la misma envoltura que el apply, por motor: pragmas + `BEGIN`/`COMMIT`
  con `foreign_key_check` antes del `COMMIT` en SQLite; `BEGIN`/`COMMIT` en
  Postgres; y en MySQL/MariaDB **nada**, con el aviso de que cada DDL confirma
  solo —escribir una transacción ahí prometería un «todo o nada» que el motor
  no cumple, que es la lección de la iteración 6—. Y el archivo pide correrse
  con una herramienta que pare en el primer error: la transacción solo
  revierte si el error corta la corrida.
- Y una de orden: la clave primaria va con las columnas, antes que las
  foráneas. Una foránea hacia una tabla que recién recibe su primaria en la
  misma migración falla en Postgres y MySQL.

**Carpetas de conexiones: planas, sin atajo para el trío, y sin secretos al
exportar.** El usuario pidió agrupar conexiones por proyecto —local, dev y
producción de un mismo sistema juntas— y poder compartir una conexión sin
abrir el editor. Las dos cosas están en el artboard de S02 desde la
iteración 1 («New folder», «Move to folder», «Import…»); la implementación
las había dejado afuera y agrupa por entorno, que corta cada proyecto en
tres. Tres decisiones, tomadas con el usuario:

- **Planas.** Una carpeta es un nombre en la conexión, no una entidad: existe
  mientras alguna conexión la tenga, como una etiqueta, y las que no tienen
  carpeta van al final. El campo «Carpeta» del editor es un desplegable con
  las existentes o lo que se escriba; «Mover a carpeta» es un submenú de
  nombres; exportar una carpeta es un archivo con sus conexiones. Anidar
  costaría una ruta en vez de un nombre, un árbol para elegir destino, y
  persistir carpetas aparte —una intermedia vacía no tendría conexión que la
  sostenga—. Se justifica con muchas más conexiones de las que una persona
  sostiene sola, y el cambio de plano a anidado es barato después —el nombre
  pasa a ser una ruta—, mientras que el inverso obliga a aplanar lo de
  alguien. El plegado de cada carpeta se guarda en esta máquina, no en la
  libreta: es cómo se mira, no qué hay.
- **Sin «Duplicar como ▸ producción».** Se evaluó un atajo que abriera el
  duplicado ya marcado como producción con las protecciones puestas, para
  que no se pueda olvidar el entorno. El usuario decidió que no: con las tres
  conexiones diferenciadas dentro de la carpeta ya tiene lo que necesita, y
  duplicar y cambiar el entorno son pantallas que existen. Queda anotado por
  si el olvido aparece en la práctica: son quince líneas, y la mitad es lo que
  lo evita.
- **Exportar nunca lleva un secreto.** El archivo es el formato de la libreta
  —así el que lo recibe puede hasta pegarlo a mano— con la contraseña y la
  passphrase afuera, porque nunca estuvieron ahí: viven en el keychain. La
  ruta de la clave privada SSH sí viaja, porque es una ruta y no la clave.
  Importar da un ID nuevo, para no chocar con una entrada del keychain que
  no es suya, muestra qué trae antes de agregar —nombre, motor, host,
  entorno, si es producción— y la contraseña se pide al conectar. El test
  que lo protege inyecta una contraseña en la conexión y exige que el archivo
  no la contenga.

**Carpetas, hechas: lo que se decidió al implementarlas.** Cinco detalles que
la decisión de arriba no fijaba y que salieron al escribirlas y al probarlas a
mano:

- **La carpeta se normaliza a espacios simples**, no solo se recorta:
  `Ahorra  app` y `Ahorra app` son la misma carpeta, porque agrupar es
  comparar por igualdad exacta y dos carpetas con el mismo aspecto serían un
  bug imposible de ver. El límite es el del nombre (120 caracteres), y vacío
  es válido: es el estado de toda conexión anterior a las carpetas. En la
  libreta la clave `folder` **solo se escribe cuando hay carpeta**
  (`omitempty`): un archivo que se sincroniza no gana una línea nueva en cada
  conexión por una función que no se usa.
- **«Mover a carpeta» es un binding propio, `MoveToFolder(id, folder)`**, y no
  un `Save` con el formulario entero: el menú manda un ID y un nombre, y una
  llamada que solo mueve no tiene por qué saber que existe una acción sobre la
  contraseña —el keychain no se toca, y el test lo exige—. Pasa por la
  validación del store como todo lo demás, así que sobre una conexión rota la
  entrada está deshabilitada con «mal configurada»: primero se arregla.
  Duplicar hereda la carpeta, con test.
- **El submenú es una entrada nueva del `ContextMenu`** (`kind: "submenu"`),
  con un nivel y nada más: acciones, separadores y rótulos adentro, no otro
  submenú. Se abre al pasar el mouse o con → —que además enfoca el primer
  ítem; ← vuelve al disparador—, sale a la derecha y se da vuelta si no
  entra, y solo puede haber uno abierto. Adentro van las otras carpetas,
  «Sacar de «X»» si tiene, y **«Nueva carpeta…»**, que pide un nombre y mueve.
  No es el «+ Nueva carpeta» de cabecera que se descartó —ese crearía una
  carpeta vacía, que no puede existir—: acá la carpeta nace con su primera
  conexión adentro, que es la única forma en que puede nacer. Sin eso, la
  primera carpeta obligaba a abrir el editor.
- **La lista muestra «Sin carpeta» solo cuando hay alguna carpeta.** Con
  ninguna, un rótulo sobre todas las conexiones no separa nada, y la lista
  queda como una sola, ordenada local → dev → staging → producción y después
  por nombre. Buscando o con el filtro de producción puesto, el plegado se
  ignora: una carpeta plegada que esconde lo que se busca se lee como «no
  está» —lo del filtro lo encontró el review—. El filtro también busca en el
  nombre de la carpeta.
- **Encontrado de paso:** el panel de detalle decía «PostgreSQL» para todas
  las conexiones —estaba escrito a mano desde S02— y mostraba host, usuario,
  contraseña y TLS para un archivo SQLite. Ahora usa `nombreDeMotor` y para
  SQLite muestra el archivo. En la lista, SQLite muestra la ruta en vez de
  `:0` y no dice «sin clave», porque no tiene.

**Exportar e importar, hechos: el codec vive en `store`, la importación es
todo o nada, y lo que se agrega es lo que se vio.** Lo que fijó la
implementación, además de la decisión de arriba:

- **El formato es la libreta, y el codec está en `store`** (`Encode`,
  `Decode`), no en el servicio: el paquete que escribe `connections.toml` es
  el único que sabe cómo es un archivo de conexiones, y un export es ese mismo
  cuerpo con otro encabezado. `Decode` no es `read`: no exige IDs ni que sean
  únicos —al importar se reemplazan todos—, y no falla por una entrada rota,
  porque la vista previa tiene que poder mostrarla con sus problemas. Un
  archivo de más de un mega se rechaza **antes de leerlo** (`Stat`): lo eligió
  una persona en un selector y puede ser cualquier cosa.
- **Lo que el archivo trae y Kaname no lee, se dice.** `toml.MetaData.
  Undecoded()` devuelve las claves ignoradas con su ruta
  (`connection.password`, `connection.ssh.passphrase`), y la vista previa las
  muestra; las que parecen un secreto llevan su frase propia: quien escribió
  `password = "…"` a mano está esperando que se importe, y hay que decirle
  que no y por qué antes de que se entere al conectar. El aviso no repite el
  valor.
- **Importar es todo o nada.** `store.AddAll` valida todas, revisa IDs contra
  la libreta y dentro del lote, y escribe una sola vez: importar tres y fallar
  en la cuarta no puede dejar tres. Cada conexión entra con un ID nuevo aunque
  el archivo traiga uno —el ID es la clave del keychain, y heredarlo haría que
  una importada tome la contraseña de otra que casualmente lo comparta—, la
  carpeta se conserva, y la contraseña se pide al conectar.
- **Lo que se agrega es lo que se vio.** La vista previa devuelve la huella
  SHA-256 del contenido, e `ImportConnections` la exige de vuelta: si el
  archivo cambió entre la vista previa y el clic, se pide volver a abrirlo.
  Los IDs de la vista previa son índices del archivo, y la huella es lo que
  los hace estables.
- **La vista previa avisa qué ya se tiene.** Una candidata que apunta adonde
  ya apunta una conexión propia —motor, host, puerto, base y usuario; para
  SQLite, el archivo— sale destildada con el nombre de la que ya está, pero
  elegible: importar el mismo archivo dos veces no duplica la libreta sin
  avisar, y tampoco lo impide. Las rotas salen con sus problemas y no se
  pueden tildar; el problema «sin id» no se muestra, porque al importar no
  cuenta.
- **Exportar no valida**: exporta lo que hay, rota o no. Quien importa ve el
  problema en la vista previa, que es donde corresponde decidir. Los IDs
  viajan en el archivo —son aleatorios y no dicen nada— para que pegado a
  mano en `connections.toml` siga siendo una libreta válida.
- **Exportar encima de la propia libreta se niega.** Lo encontró el review
  `high`: el archivo exportado es una libreta válida, así que elegir
  `connections.toml` en el selector la reemplazaría por el subconjunto y
  dejaría las contraseñas de las demás huérfanas en el keychain, escribiendo
  además por fuera del cerrojo del store. `ExportConnections` compara la ruta
  limpia y, si los dos archivos existen, `os.SameFile` —en Windows la misma
  ruta se escribe con mayúsculas o sin ellas—, con test. Del mismo review:
  la importación relee la libreta fuera del `try` que importa —un fallo al
  releer no puede devolver el diálogo con el botón habilitado, que
  importaría lo mismo otra vez—, el diálogo no se cierra mientras importa, y
  «Exportar carpeta…» exporta lo que la carpeta muestra, con el mismo número
  que su cabecera.
- **Dos cosas que el usuario vio al probar**, arregladas aparte (`fix(s00)`):
  el toast de «Se agregó 1 conexión» no se iba nunca —los de éxito y de
  información ahora se van solos a los ocho segundos; los de error y de
  advertencia siguen quedándose, porque un error que desaparece antes de
  leerlo es peor. La regla vive en `Toast`, no en quien lo muestra, para que
  el próximo no nazca sin ella; el temporizador depende del id del toast y
  no de `onDismiss`, que suele ser una función nueva por render y lo
  reiniciaría con cada cambio de estado (medido por CDP: 8 s exactos con la
  lista re-renderizando)— y el velo de los diálogos
  era un rectángulo oscuro alrededor del diálogo en vez de tapar la
  pantalla. Lo segundo venía desde S00: el `<dialog>` nativo se dibuja del
  tamaño de su contenido (`fit-content`, `margin: auto`, máximo `100% -
  2em`) y `inset: 0` solo no lo cambia; el elemento, que es el que lleva el
  color del velo, nunca había llenado la ventana. `width`/`height: 100%`,
  sin máximos ni márgenes, y `box-sizing: border-box` por el padding.
- **Los selectores nativos son la prueba manual del usuario.** El resto se
  probó por CDP con un gancho temporal en `App.tsx`, quitado antes del
  commit, que llamaba a los bindings con una ruta fija: vista previa con
  cuatro casos (buena con carpeta, rota, producción con túnel, y `password` y
  `passphrase` escritos a mano que salieron como avisos), importación de tres
  con toast y carpeta, segunda vista previa del mismo archivo con las tres
  marcadas «ya apunta ahí», y export por el gancho sin ningún secreto en el
  archivo y con `key_path` adentro.

### Iteración 9 — 2026-09-10

**Los dos botones grises del editor: qué hacer con cada uno.** Se agendan, y no
son la misma clase de trabajo — uno no necesita nada y el otro necesita una
dependencia, así que van en dos commits.

**Plan de ejecución: se hace, y sin dependencia.** Los cuatro motores lo dan
gratis, y lo importante es que la variante que se usa **NO ejecuta la consulta**:

| Motor | Sentencia | ¿Ejecuta? |
|---|---|---|
| PostgreSQL | `EXPLAIN <consulta>` | No |
| MySQL · MariaDB | `EXPLAIN <consulta>` | No |
| SQLite | `EXPLAIN QUERY PLAN <consulta>` | No |

La distinción es la única decisión de seguridad del ítem: **`EXPLAIN ANALYZE` sí
ejecuta**, incluido un `DELETE`, así que no entra. Ni como opción escondida:
un botón que a veces corre la consulta y a veces no es exactamente la clase de
cosa que esta aplicación no hace. Si alguna vez se quiere, va por el camino
largo —confirmación de producción, respeto del solo lectura— y no por este botón.

Los cuatro devuelven FILAS, así que el plan no necesita un tipo nuevo: es un
`query.Result` más, y se dibuja en una pestaña «Plan» al lado de Resultados y
Mensajes. El prefijo por motor va en `engine.Caps`, que es donde ya viven las
diferencias entre motores; el resto del camino es el de `Run`, con su `runID`
para poder cancelar.

Una sola sentencia por vez: `EXPLAIN` toma una. El editor ya sabe partir el texto
desde la iteración 6, así que se explica **la que está bajo el cursor** y, si no
se puede saber cuál es, se dice en vez de adivinar.

**Plan de ejecución, hecho: lo que fijó la implementación.**

- **El prefijo vive en `engine.Caps.ExplainPrefix`**, y un motor desconocido
  lo tiene vacío: cero es «no da plan», que es el lado seguro. `EXPLAIN` en
  Postgres, MySQL y MariaDB; `EXPLAIN QUERY PLAN` en SQLite, porque un
  `EXPLAIN` a secas ahí devuelve el bytecode de la máquina virtual.
- **`Queries.Explain(runID, sql, line)`** recibe el texto entero y la línea
  del cursor, parte con `query.Split` y elige **la última sentencia que
  empieza en esa línea o antes**: el cursor en la línea en blanco después de
  una sentencia sigue apuntando a ella. Con una sola no hace falta cursor;
  con varias y el cursor antes de la primera, se dice en vez de adivinar.
  Comparte el `runID` con Run —cancelar corta cualquiera de los dos—, así que
  no corren a la vez: la barra deshabilita el otro botón.
- **Lista blanca, no lista negra.** La primera versión rechazaba solo lo que
  empezaba con EXPLAIN, y el review `high` la rompió en un minuto: `ANALYZE
  DELETE FROM t` bajo el cursor, pegado detrás del prefijo, es `EXPLAIN
  ANALYZE DELETE FROM t` —Postgres acepta las opciones después de la palabra,
  con o sin paréntesis; MySQL también tiene EXPLAIN ANALYZE— y **borra**.
  Comprobado contra el Postgres de prueba: el test lo ve (con la lista negra
  vuelta a poner, «quedan 0 filas»). Ahora solo llega al motor lo que
  EXPLAIN sabe planificar —SELECT, INSERT, UPDATE, DELETE, WITH, VALUES,
  TABLE, MERGE, REPLACE— y el resto se dice: un EXPLAIN a mano, una opción
  suelta, un DDL. Quien quiera un EXPLAIN ANALYZE lo ejecuta como cualquier
  otra sentencia, por el camino que confirma.
- **Sin límite de filas para el plan**: es chico y llega entero, y cortado a
  la mitad del árbol sin que la vista lo diga es peor que no darlo. Del mismo
  review: Ctrl+↵ con el plan en curso corría la consulta sin registrarla
  —comparten el `runID`— y «Cancelar» no la encontraba; y el bloque
  «Ejecutando…» ahora se muestra en cualquier pestaña, que es el único lugar
  con «Cancelar».
- **No va al historial.** El historial es lo que se ejecutó, y esto no lo fue.
- **Cuatro tests, cuatro inyecciones en rojo**: `EXPLAIN ANALYZE` puesto en
  el prefijo de Postgres borra las filas y el test lo ve; elegir siempre la
  primera sentencia; aceptar el EXPLAIN a mano; anotar en el historial. El
  primero corre contra los cinco motores y por eso el armado de sesión por
  motor de `session_test.go` pasó a un helper (`abrirMotorDePrueba`).
- **La forma del plan no es una sola.** Postgres da UNA columna de texto donde
  la sangría es el árbol; en una grilla se pierde la sangría y cada línea se
  corta en la primera palabra. Así que un plan de una columna se muestra como
  texto tal cual, como en psql. **MySQL 9.7 también**: desde la 8.3 el formato
  por defecto de `EXPLAIN` es TREE, no la tabla, y así llegó en la prueba a
  mano. MariaDB y SQLite dan una tabla, y esa va en la grilla con un prop
  nuevo, `anchoDeColumna`: la grilla saca los anchos del tipo y no del
  contenido porque una página cambia al cargar más filas, pero un plan son
  pocas filas y llegan enteras, así que ahí sí se mide —ni `select_type`
  ocupa media pantalla ni `Extra` se corta—.
- **Ctrl+C y el visor de celda trabajan sobre lo que se está mirando**: con la
  pestaña Plan abierta, sobre el plan y no sobre el resultado de al lado.

**Formatear: se hace, pero con una dependencia y nunca a mano.** Es lo que
respondía a la pregunta de si alguno traería problema: escribir un formateador
de SQL a mano lo trae. Formatear mal el texto del editor no es un botón que no
anda, es **el texto de la persona alterado** —una comilla mal cerrada, un
comentario movido adentro de una cadena— y en el editor no hay de dónde
recuperarlo; es lo mismo que S24 protege al cerrar una pestaña. Un formateador
correcto necesita un parser por dialecto, y eso no se improvisa.

Así que va con `sql-formatter`, que cubre los cuatro dialectos, **en su propio
commit, con versión exacta y el changelog leído**, y midiendo lo que agrega al
bundle: el binario se distribuye copiando y pegando. Si al mirarlo de cerca no
cierra —tamaño, dialectos, mantenimiento— el botón se saca en vez de quedarse
gris, que es la regla que el chip «Ctrl K» dejó escrita.

**Formatear, hecho: `sql-formatter` 15.8.2 y lo que se decidió al usarlo.**

- **La dependencia.** `sql-formatter` 15.8.2 (MIT, publicada el 2026-06-21,
  más de los siete días que exige `.npmrc`), con versión exacta. Arrastra
  ocho transitivas: `nearley` y su parser, y `argparse` y `commander` que son
  de la línea de comandos y no entran al bundle. Sin red ni telemetría. El
  changelog reciente es de dialectos —funciones de Postgres 18, la sintaxis
  `$param` de SQLite— sin cambios que rompan.
- **`formatDialect` con los cuatro dialectos importados uno por uno**, no
  `format` con el nombre del lenguaje: `format` arrastra los diecisiete
  dialectos al bundle, y el binario se distribuye copiando y pegando. Medido:
  **+94 kB minificados** (1.237 → 1.331 kB), que es el costo de cuatro parsers
  y no de diecisiete.
- **No cambia mayúsculas de nada.** Palabras clave, identificadores, tipos y
  funciones quedan como estaban (`preserve` en los cuatro): en Postgres las
  mayúsculas de un identificador entre comillas son parte del nombre, y en
  MySQL distinguen tablas según el sistema de archivos. El formateador
  acomoda espacios, saltos y sangría, y nada más. Es la misma regla que
  justificó no escribirlo a mano: el texto de la persona no se altera.
- **Reemplaza el documento en una sola transacción de CodeMirror**, así
  Ctrl+Z lo deshace de un golpe. Si el texto no se puede interpretar —una
  comilla sin cerrar—, no se toca nada y se dice al lado del botón, seis
  segundos, con la primera línea del error del parser: dónde se trabó.
- **Parámetros con nombre en Postgres.** El dialecto de Postgres conoce `$1`
  y no `:nombre`, y sin decírselo `x = :nombre` salía como `x =:nombre`.
  Se le declaran los dos; el `::` de un cast no se confunde, probado con
  `x::int` en la misma consulta. MySQL (`?`, `@v`) y SQLite (`:n`, `?1`,
  `@z`) ya venían bien. El precio, que encontró el review: en un corte de
  array con límite que empieza con letra, `arr[lo:hi]` sale como
  `arr[lo :hi]` —Postgres lo acepta igual—; `arr[1:3]` no cambia. Se
  prefiere eso a `x =:nombre`, que es lo que se ve todos los días.

**«Explain» estaba en inglés, y al lado había un botón muerto.** Lo vio el
usuario. La etiqueta era el nombre de la sentencia de Postgres puesto como texto
de botón, entre «Guardar…» y «Formatear»; lo que uno quiere no es escribir
EXPLAIN, es ver el plan, así que ahora dice eso.

Lo de al lado era peor: **«Guardar consulta», deshabilitado, prometiendo «llega
en la Iteración 9»** — en la iteración 9, y con el botón «Guardar…» que HACE eso
dos lugares a la izquierda, funcionando desde S21. Quedó de cuando se hizo S21 y
nadie lo sacó: la barra tenía dos botones para guardar, uno bueno y uno que
prometía lo que el otro ya hacía.

Y el título de los dos que quedan dejó de prometer una fecha. El plan los nombra
UNA vez —«sin Explain, Format… son de iteraciones posteriores»— y nunca los
agendó, así que «llega en una iteración posterior» era el mismo cartel vacío que
el chip «Ctrl K» tuvo durante ocho iteraciones. Ahora dicen «Todavía no está»,
que es verdad.

**S20 no era lo que decía el ítem del plan.** «Drift check — reutiliza S15 para
la SQL de reconciliación» sonaba a revisar una base contra algo. El artboard es
otra cosa y bastante más grande: **comparar dos conexiones**, dev contra
producción, y generar la SQL que alinearía a la segunda. Se bajó el diseño antes
de escribir nada, que es para lo que está.

**La comparación tiene dirección, y las cuatro reglas del paquete son de
seguridad.** Origen → destino, no simétrica. Lo que está solo en el origen se
crea; **lo que está solo en el destino NO se borra, nunca** —puede tener datos y
desde ahí no hay forma de saber cuántos—, se reporta con el motivo escrito. Una
diferencia que no se sabe escribir sigue siendo una diferencia, con su motivo. Y
lo que no se comparó se dice: el catálogo trae las vistas y las funciones por
NOMBRE, así que dos homónimas con cuerpos distintos se ven iguales desde acá, y
un «no hay diferencias» que incluya eso es una mentira. Es la misma lección del
panel de dependientes: una lista vacía y un «no sé» son la misma lista y
significan lo contrario.

El test central recorre una comparación con las cuatro formas de «esto sobra del
otro lado» —esquema, tabla, columna y objeto— y exige que ninguna traiga
operación y que ninguna operación de la comparación entera sea destructiva.
Inyectado el `dropColumn` que uno escribiría sin pensar, falla nombrando la
columna.

**El review encontró que la comparación generaba operaciones que el resto del
sistema rechaza, y cuatro cosas más.** Las cinco tienen la misma forma: la
comparación parecía funcionar y producía algo que no servía.

- **Un `addColumn` de una columna NOT NULL nunca podría entrar al changeset.**
  `change.Validate` lo rechaza —una columna NOT NULL nueva necesita un valor por
  defecto, y el catálogo dice que el origen tiene uno pero no cuál—, así que la
  pantalla ofrecía una operación y el changeset la devolvía con un error de
  validación, sin camino hacia adelante. El comentario decía «se genera igual con
  el aviso»: era una promesa falsa.
- **Un `createTable` salía sin la CLAVE PRIMARIA.** La tabla recreada quedaba sin
  clave, y `HasPrimaryKey` es lo que habilita editar la grilla: la tabla nueva
  nacía de solo lectura en Kaname. Y no se notaba, porque la clave tampoco se
  comparaba — dos errores que se tapaban entre ellos.
- **Un `addForeignKey` salía sin el NOMBRE de la restricción**, así que el motor
  le ponía uno generado. Como las claves se comparan por nombre, la comparación
  siguiente reportaba la misma clave como «falta en el destino» y «sobra en el
  destino» a la vez, para siempre — y aplicar otra vez dejaba una duplicada. Una
  comparación que no converge no es una comparación.
- **La clave primaria no se comparaba, y la lista de «no comparado» lo excusaba
  mal**: decía que las restricciones viven en el detalle por tabla, y la clave
  primaria SÍ está en el catálogo.
- **`MismoMotor` suprimía la comparación de tipos pero no el COPIADO de tipos.**
  El `createTable` y el `addColumn` seguían llevando el tipo escrito por el motor
  de origen: un `jsonb` o un `timestamp with time zone` adentro de un CREATE
  TABLE de MySQL. Media corrección es peor que ninguna, porque parece que anda.

Lo que faltaba no era un caso más sino **un test de otra clase**: correr TODA
operación generada por `change.Validate()`. El que había comprobaba que el campo
no fuera nil, no que sirviera. Y la primera versión de ese test nuevo tampoco
falló al inyectarle el error — el fixture no tenía ninguna columna NOT NULL
faltante, así que no cubría lo que decía cubrir. Lo descubrió la inyección, no la
lectura.

**Entre motores distintos los tipos NO se comparan.** `character varying(255)`
contra `varchar(255)` es el mismo tipo escrito por dos motores, y hay uno así por
columna: comparar Postgres contra MySQL daría cientos de «el tipo difiere» que no
son diferencias, cada uno con una sentencia que además estaría mal. Ruido con esa
forma no molesta, **esconde las diferencias de verdad**. Lo que sí se compara
igual es qué tablas y qué columnas hay de cada lado, que es lo que uno mira
cuando está migrando de un motor al otro — y que los tipos quedaron afuera se
dice, como todo lo demás que no se miró.

**Atlas no hace falta, y es la tercera vez que se evalúa.** La comparación emite
`change.Change`: las mismas diecisiete operaciones que la interfaz ya sabe
escribir y que cada motor ya sabe renderizar. Lo que no entra en ese vocabulario
se reporta sin sentencia, que es exactamente lo que el diseño pide para la
columna que solo existe en producción. Un differ externo agregaría un modelo
intermedio para producir algo que ya se produce, y volvería a traer el problema
de la iteración 5: aceptar su interpretación del esquema de vuelta.

**Comparar necesitaba una segunda conexión, y ahí estaba el trabajo real.** Todo
el camino sensible —contraseña del keychain, DSN, túnel antes que la base, cerrar
el túnel si la base falla— vivía adentro de `ConnectAccepting`, que además
INSTALA la conexión como la sesión en curso. Se extrajo `abrirConexion`, que abre
y no instala: quien llama es el dueño de lo que recibe. La comparación abre las
dos, les lee el catálogo y las cierra en un `defer` —una conexión que queda
abierta tras una comparación fallida es una sesión colgada en el servidor y en el
bastión—, y el workspace no se entera.

El test que lo protege no mira los campos de la vista: **usa** la conexión
abierta después de comparar. Inyectada la versión ingenua —que instala la
conexión de la comparación como la actual— el fallo es `closed pool`: la
comparación le cerró el pool al workspace. Mirando solo la vista habría pasado,
porque es un struct en memoria que sigue diciendo «conectado».

**El botón de desborde esperó a tener algo adentro.** El plan lo agendaba junto
con S22 y se difirió a propósito: en ese momento solo habría repetido
«desconectar» y «about», que están a la vista en la barra de estado, y un lugar
de más donde buscar lo mismo no es descubrimiento, es ruido. Con S23 hecho tiene
contenido propio: ajustes, dónde se guarda todo, y salir.

**«Dónde se guarda todo» abre la carpeta, no muestra la ruta.** Quien pregunta
eso quiere respaldar el archivo o copiarlo a otra máquina, y para eso hay que
llegar a la carpeta. El método de Go **no recibe la ruta**, y es la única
decisión de seguridad del archivo: si la recibiera del frontend, esto sería
«ejecutá el explorador sobre lo que yo te diga». Sale del store, y va como
argumento y no como línea de comandos, así que no hay shell que interprete nada.

Y un error que ningún test habría visto: estaba escrito con
`exec.CommandContext`, que **mata al hijo cuando el contexto termina** — y el
contexto de un binding de Wails es el de LA LLAMADA, que se cancela apenas el
método devuelve. El explorador se lanzaba y moría en el mismo instante: el
método contestaba sin error y la persona apretaba y no pasaba nada. `Start()`
devuelve `nil` igual, así que solo se ve probando. La ventana que se abre es del
usuario y no tiene por qué vivir atada a una llamada que ya terminó.

**Y «Salir» pregunta si hay trabajo sin guardar.** La entrada nueva era una
segunda forma de perder el texto de un editor —y más rápida que la primera:
cerrar una pestaña avisa, y salir se llevaba todas—. Es la misma regla de S24 y
por la misma razón: ese texto no está en la base, ni en el changeset, ni en el
historial.

**El tema claro pintaba con los colores del oscuro, y las cuatro pantallas que
el plan señalaba no tenían nada que ver.** S05, S06, S12 y S15 estaban bien: todo
lo suyo sale de tokens, y los alias del ERD —`--erd-pk`, `--erd-fk`— siguen al
tema solos porque son `var()` de otro token. Lo que fallaba era transversal:
**dieciocho colores escritos a mano** en botones, insignias, diálogos y la
paleta, todos con el valor del tema oscuro y alfa encima. Un color con alfa
sigue siendo un color.

Lo peor no se veía, se medía. El botón de peligro llevaba texto casi negro con
un comentario que explicaba que en tema claro el blanco «no llega a 4.5:1» sobre
el rojo. Calculado: sobre el `#c72c22` del tema claro, el **blanco da 5.5:1 y
ese casi negro da 3.5:1** — el literal fallaba justo en el tema para el que se
lo había elegido. En oscuro los números se dan vuelta (5.6 contra 3.4), que es
por lo que nadie lo notó: el valor era correcto para el único tema que existía.
`--text-on-accent` ya hacía exactamente eso —casi negro en oscuro, blanco en
claro— y alcanzaba con usarlo.

La paleta de comandos tenía su propio velo negro al 45 % y su propia sombra,
teniendo `--scrim` y `--shadow-dialog` al lado, los dos redefinidos para el tema
claro. En claro oscurecía la pantalla como si fuera de noche.

Los tokens nuevos son tres niveles y nada más: `-quiet` para un fondo apenas
teñido, `-border` para un borde que se tiene que ver, y `--prod-wash` para el
lavado de las cabeceras de producción — que estaba escrito con **tres alfas
distintas (.05, .08 y .09)** que nadie puede distinguir. En el tema claro los
alfas son un poco más bajos: sobre un fondo claro, el mismo porcentaje de un
color oscuro pesa bastante más.

**Y ahora hay un test que exige que cada `var(--algo)` tenga de dónde salir.**
Es el que faltaba cuando el panel de dependientes se pintó con cuatro tokens
inventados: un `var()` que no resuelve NO es un error, la propiedad simplemente
no se aplica, así que los tres estados se dibujaban idénticos mientras el
comentario decía que estaban distinguidos por color — y ni el build, ni `tsc`,
ni la pantalla tenían nada que objetar. Busca los nombres definidos también en
el TSX, porque `--env-color` sale de un `style` en línea, en vez de mantener una
lista de excepciones que se desactualiza.

De paso cayó un selector muerto que sí molestaba: `About.module.css` definía
`.shortcut` **dos veces** —quedaba la lista de atajos de un diseño anterior que
ninguna clase usaba— y la segunda ganaba por estar más abajo, así que la grilla
de dos columnas nueva se dibujaba como una fila con borde.

**S23: el archivo de preferencias existía como promesa.** `config.toml` era una
ruta que About mostraba, y nada la escribía nunca. Ahora es `internal/config`,
con las dos reglas que ordenan el paquete entero:

- **Ahí no entra ningún secreto**, y hay un test que recorre la estructura por
  reflexión para que agregar un campo llamado `token` falle en CI, más otro que
  mira el archivo escrito de verdad — un campo puede llamarse bien y llevar un
  secreto igual.
- **El valor cero es lo que la aplicación ya hacía.** Por eso los campos se
  llaman `HideLineNumbers` y `NoWrap` y no `LineNumbers` y `Wrap`: con el cero,
  esos dos apagarían cosas que hoy están prendidas. Es la misma regla que
  `connection.Safety`, y por el mismo motivo: el archivo se edita a mano y una
  build nueva lee archivos viejos.

**La pantalla se guarda sola y no tiene un solo control que no haga algo.** Sin
botón de «Guardar»: cada control es una decisión discreta y la escritura es
atómica, así que no hay estado intermedio que confirmar; un botón ahí solo
agrega una forma de perder lo que uno ya creía cambiado. Las escrituras se
agrupan 350 ms porque el número de un límite se teclea dígito a dígito.

Y un ajuste a medio conectar no entra. Una pantalla de preferencias llena de
interruptores que no hacen nada enseña a desconfiar también de los que sí.

**Los defaults de una conexión nueva son la MISMA pantalla que la pestaña
Safety**, con el mismo tipo de Go detrás. Dos formularios para los mismos seis
campos se separan, y un default que se separa del valor real empieza a mentir.
Solo tocan el borrador: ninguna conexión existente cambia. Y si el archivo de
preferencias no se puede leer, el borrador nace con el cero —el lado protegido—:
un error de lectura no puede terminar creando conexiones menos seguras que las
de fábrica.

**El check de versiones es lo único que sale a internet por su cuenta**, así que
las reglas son estrechas: solo al apretar el botón, un GET sin query string, sin
cookies, y con `User-Agent: Kaname` a secas —sin la versión, que es el único dato
de esta máquina que la petición podría llevar; la comparación se hace acá—. La
respuesta viene de afuera, así que tiene tope de tamaño, timeout y redirecciones
que no pueden salir del host. Nada se descarga ni se instala.

Tres cosas que el resultado distingue y que es fácil confundir: «hay una más
nueva», «tenés la última» y **«no pude comparar»**. Si `Comparable` no existiera,
una versión con un tag raro se mostraría como «estás al día» — que es exactamente
la respuesta que esconde una actualización para siempre.

**El tema y el cuerpo de letra se aplican por la raíz del documento, no por
estado de React.** El tema lo consume CSS (`[data-theme]`) y el cuerpo lo consume
CodeMirror, que arma su hoja de estilos UNA vez al montarse: con estado de React
habría que redibujar la aplicación entera para cambiar un color y recrear el
editor para cambiarle el tamaño, perdiendo cursor, deshacer y foco. Los números
de línea y el ajuste sí son extensiones, así que van en un compartimento —los
dos juntos, porque `reconfigure` reemplaza todo lo que el compartimento
contiene—.

**Abrir About o Ajustes borraba el workspace.** Las dos reemplazaban al Shell, y
desmontarlo se lleva las pestañas, el árbol y —lo peor— el texto sin guardar de
cualquier editor, sin preguntar nada. Es exactamente lo que S24 existe para
evitar al cerrar una pestaña, y pasaba por apretar «about» desde la iteración 0.
Ahora se dibujan ENCIMA, con el Shell montado debajo e `inert`.

Cubrir y no esconder: con `display:none` CodeMirror mide cero y al volver aparece
con el alto viejo y el texto cortado, que es el mismo problema que ya tenían las
pestañas escondidas. Y Ctrl+K tuvo que aprender a callarse con el panel encima
—el handler vive en `window` y el Shell sigue escuchando—, que es el mismo error
que el `<dialog>` modal, por otra vía.

Tres cosas que salieron de probar y del review, todas de la misma familia —una
pantalla que se guarda sola tiene que guardar de verdad—:

- **Dos interruptores cambiados en el mismo lote** partían los dos de la
  configuración de la renderización anterior, y el segundo pisaba al primero.
  Se parte de lo último pedido, no de lo dibujado.
- **Salir antes de que venciera el agrupado perdía el cambio**, y en silencio:
  la pantalla ya estaba pintada con el tema nuevo y el archivo tenía el viejo,
  así que volvía al anterior al reabrir la aplicación sin ninguna explicación.
  Al desmontar se GUARDA lo que quedaba, no se cancela. Reproducido: elegir
  «Claro» y apretar «Volver» dejaba `theme = "system"`.
- **Consultar versiones borraba la constancia de haber consultado.** `Check`
  escribe —anota cuándo fue, releyendo el archivo para no pisar nada— y la
  pantalla se quedaba con una copia que acababa de envejecer; el próximo cambio
  de cualquier ajuste la guardaba y se llevaba puesto el `last_check`. Ahora la
  consulta devuelve el resultado Y las preferencias releídas: una sola fuente.

Y del review, dos del lado de Go:

- **`Save` pisaba un archivo de una versión que esta build no entiende.** La
  guarda estaba solo en `Load`, y eso cubre la mitad tranquila: la pantalla
  muestra el cartel de que no se pudo leer y deja todos los controles vivos, así
  que el primer clic escribía un archivo viejo encima del nuevo. El test que
  existía probaba únicamente la lectura, así que daba confianza de más.
- **`rc10` se ordenaba antes que `rc9`**, porque los sufijos se comparaban como
  texto. Publicar una rc10 con una rc9 corriendo reportaba «tenés la última»:
  exactamente el error que el paquete dice existir para evitar. De paso, los
  metadatos de build (`1.0.0+20260911`) se tomaban por prelanzamiento y quedaban
  por DEBAJO de la misma versión sin fecha; semver dice que no cuentan para la
  precedencia.

**El historial guardaba valores de fila, y nadie los había puesto ahí a
propósito.** Lo encontró el `/code-review high`. `anotar` escribía
`e.Error = f.Message`, y el mensaje de un fallo de DATOS no es genérico: el
traductor de errores de Postgres arma «Ya hay filas con (email) =
(ana@example.com) repetido» a partir del `Detail` del `unique_violation`. O sea
que cualquier INSERT que chocara contra un índice único dejaba el valor de la
fila en un archivo de texto del directorio de estado — sin el control de acceso
de la base, sin su cifrado, y contra lo que CLAUDE.md dice y contra lo que el
propio encabezado del paquete promete.

Se anota QUE falló y no QUÉ dijo el motor. El detalle está en la pestaña
Mensajes del editor, que es donde hace falta: en la corrida, no en el registro.
El test no comprueba que el campo esté vacío —comprueba que el valor no esté en
el ARCHIVO—, y por eso agarra también la inyección que lo mete por otro campo.

Del mismo review, seis cosas más de esta iteración:

- **El «+» de la tira de pestañas reventaba la consulta nueva.** `onNew={openQuery}`
  compila —`(sql?: string) => void` es asignable a `() => void`, y está bien que
  lo sea— y en ejecución React le pasa el evento del mouse como `sql`. El objeto
  terminaba en `sqlInicial`, el editor hacía `.trim()` sobre él y la pestaña se
  caía al dibujarse. El botón de la barra ya estaba envuelto en una flecha; éste
  no. TypeScript no puede cazarlo: la regla que lo permite es la correcta.
- **`SchemaTree.tsx` era un archivo binario para git.** Tenía un byte NUL
  escrito literal como separador de clave. Con eso, git no lo muestra en
  `git diff` ni lo pasa por `blame` —la pantalla del árbol era invisible para
  cualquier revisión— y además se saltea la normalización de `.gitattributes`,
  así que era el único archivo del proyecto guardado con CRLF. Ahora va como
  escape.
- **La escritura del historial prometía una atomicidad que no daba.** El
  comentario decía «un corte a mitad de escritura no puede dejar medio archivo»
  y usaba `os.WriteFile` + `os.Rename` sin `Sync`, así que el rename podía
  publicar contenido que seguía en el caché. Y el temporal era un `ruta + ".tmp"`
  fijo: el mutex ordena las escrituras de ESTE proceso, y dos Kaname contra el
  mismo `%APPDATA%` compartían ese nombre. Ahora hace lo mismo que la libreta de
  conexiones y los diagramas.
- **Sin conexión abierta, el historial se pedía entero.** `conexion()` devuelve
  `""` y `history.Store.List("")` significa «sin filtro» —lo usa el borrado total
  de los ajustes—, así que los dos vacíos querían decir cosas opuestas. La
  pantalla no lo dibujaba, pero el historial de todas las conexiones cruzaba el
  puente igual, que es justo lo que el comentario de `List` dice que no puede
  pasar.
- **Un archivo corrupto se perdía en la consulta siguiente.** El comentario
  decía «queda en disco para que alguien lo mire» y la primera escritura lo
  pisaba. Ahora se aparta con otro nombre, que es la única forma de que siga
  estando cuando alguien lo busque.
- **Dos IDs seguidos podían ser el mismo.** Eran `time.Now().UnixNano()` y el
  reloj de Windows avanza de a ~15 ms. Para las guardadas es destructivo: `Save`
  con un ID que ya existe REEMPLAZA la otra. El test que lo prueba genera IDs en
  un bucle cerrado y no guarda dos consultas «seguidas»: entre una y otra hay una
  escritura a disco, y para cuando vuelve el reloj ya avanzó — así que el test
  por la puerta de adelante pasaría en esta máquina y fallaría en otra.

Y **dos controles nativos** que no tendrían que haber entrado: el radio de los
límites de Safety y la casilla de «Borrar y volver a crear» del editor de
objetos. La regla de CLAUDE.md no es estética: el nativo se pinta con los colores
del sistema operativo, así que ignora el tema y los acentos de entorno — y el
tema claro de esta misma iteración los habría mostrado con el azul de Windows
adentro de otra paleta. La casilla ya tenía su reemplazo (`Checkbox`); el radio
no existía y ahora está en `components/ui`, que es donde la regla dice que se
agrega lo que falta.

**El historial estaba entero y no estaba enchufado.** `internal/history` con sus
tests, el servicio, la pantalla, el filtro de secretos — y `main.go` nunca
registró el servicio ni construyó el store. Consecuencia: `UsarHistorial` jamás
se llamaba, así que `anotar` salía por el `if q.historial == nil` y no se
guardaba una sola consulta; y la pestaña, al pedir la lista, llamaba a un
servicio que no existía.

Lo grave no es el olvido sino **por qué nada lo detectó**. El generador de
bindings de Wails no lee la lista de servicios: recorre el código. Así que
`service.History` tuvo su `history.ts` generado igual, con sus tipos, y el
frontend lo importó y lo llamó con `tsc` en verde. `go vet` tampoco tiene nada
que decir sobre una lista a la que le falta un elemento. Compila, typechequea,
pasa los tests — y falla al apretar el botón.

La causa más probable del olvido es el procedimiento de la build de depuración:
se edita `main.go` para agregar el puerto de CDP, se compila, y se **restaura
`main.go`**. Si el cableado nuevo estaba ahí sin commitear, el restore se lo
lleva. Pasó otra vez mientras se arreglaba esto —un `git checkout -- main.go`
para revertir una inyección de fallo borró el arreglo entero—, así que la
lección es doble: ese archivo se commitea antes de tocarlo para depurar.

El test que faltaba compara `servicios()` contra **lo que el frontend importa de
verdad**: recorre `frontend/src` buscando imports de la forma
`bindings/…/internal/<paquete>/<servicio>` y exige que cada uno esté registrado.
Lee el código commiteado y no `frontend/bindings/`, que está en `.gitignore` y
existiría o no según quién corrió la tarea de Wails. Inyectada la falla —sacar
la línea del historial— el test nombra el archivo y el servicio.

De paso, `Paths` ganó `History` y `SavedQueries`: las ubicaciones son de
`appinfo`, que es el contrato de dónde vive cada cosa, y no de `main.go`.
Y el test que prohíbe rutas de secretos pasó a recorrer la estructura **por
reflexión** en vez de una lista escrita a mano — con la lista, agregar un campo
lo dejaba sin mirar y el test seguía afirmando que había revisado todo.

El reparto —historial al estado local, guardadas al lado de la libreta— se
prueba contra una función pura con dos raíces distintas. Con las rutas reales no
se puede: **en Windows el directorio de estado ES el de configuración**, así que
los dos lados de la afirmación coinciden y el caso pasa diga lo que diga el
código. Y Windows es donde se desarrolla.

**La pestaña Safety no edita el número crudo, y esa es toda la pantalla.** Los
tres límites —tiempo por sentencia, filas por consulta, desconexión por
inactividad— comparten una convención en el archivo que NO es la que parece:
cero significa «usá el default» y para sacar el límite hay que pedir −1. La
razón está en el modelo y es buena —un archivo al que le falta la clave tiene
que quedar PROTEGIDO, y el valor ausente de un entero es cero— pero convierte el
campo en una trampa: un 0 en una casilla de segundos se lee como «ninguno» y
significa treinta. Es la clase de detalle que nadie adivina y que se descubre
cuando una consulta se corta sola. Por eso se elige entre tres opciones que
dicen lo que son, y el número aparece solo cuando se eligió poner uno.

Las dos protecciones que se APAGAN van en su propio grupo —«Qué se saltea»— y se
describen por lo que se pierde. Puestas entre las otras con el mismo aspecto, un
tilde de más se lee como «más seguro».

Y un detalle que salió de mirar la pantalla: el `Toggle` del design system pone
el rótulo solo en `aria-label`, así que la primera versión mostraba las
explicaciones sin ninguna etiqueta arriba. El texto visible va al lado, adentro
del mismo `<label>`, así que clickear la palabra también conmuta.

**Cerrar una pestaña con trabajo sin guardar es de lo único que esta aplicación
hace sin deshacer.** El texto de un editor no está en la base, ni en el
changeset, ni en el historial —que guarda lo que CORRIÓ, no lo que se está
escribiendo—: no hay de dónde recuperarlo. Por eso S24 pregunta.

Y pregunta **solo cuando hay algo que perder**. Un diálogo en cada cierre
entrena a apretar «sí» sin leer, y entonces no protege del único caso en que
hacía falta —que es exactamente el modo en que un cartel de confirmación deja de
ser una protección y pasa a ser un trámite—.

Quién sabe si hay algo que perder es la pestaña, no el Shell: deducirlo desde
afuera sería adivinar. Cada editor lo reporta, y correr la consulta NO lo limpia
—el historial guarda lo que se corrió, pero la versión que quedó escrita
después, la que se estaba afinando, no está en ningún lado—.

**El comentario de una TABLA no existía, y el del ítem del plan tampoco era
cierto.** El plan decía que faltaban los dos —tabla y columna— y el de columna
estaba desde la iteración 6: el ítem se escribió antes y nadie lo tachó. El de
tabla sí faltaba, y de una forma particular: `setTableComment` existía en el
changeset, se renderizaba bien, tenía etiqueta en la pantalla de pendientes y
badge en el panel del ERD —y **nada lo producía nunca**. Media operación
completa en el modelo, sin ninguna forma de llegar a ella.

Con eso se puede comprobar desde la aplicación lo que el plan quería: un
comentario con una comilla simple adentro sale como `… IS 'O''Brien'` en la
vista previa y vuelve del catálogo como `O'Brien`. El citado se prueba con el
caso que lo rompe, no con uno que no lo ejercita.

**Los tokens de color del ERD ahora se llaman como lo que significan.** La clave
primaria se pintaba con `--env-stage` —el color de un entorno de STAGING— y la
foránea con `--accent`. Los valores coinciden, así que no se veía nada mal; lo
que estaba mal es que cambiar el color de una clave exigía saber esa
coincidencia, y hacerlo habría cambiado de paso la insignia de las conexiones de
staging y el resaltado de la selección. Son ALIAS —`--erd-pk: var(--env-stage)`—
así que nada cambia de aspecto y siguen al tema solos, sin redefinirse en el
bloque claro.

**S21: el historial y las consultas guardadas son DOS cosas con dos dueños, y
por eso viven en dos archivos.** El historial es de esta máquina —qué corriste
el martes a la tarde no es algo que quieras ver replicado en la notebook del
trabajo— y va al directorio de estado. Las consultas guardadas son trabajo:
escribir una de veinte líneas cuesta, y quien sincroniza su libreta de
conexiones no quiere volver a escribirla del otro lado, así que van al lado de
`connections.toml` como los diagramas del ERD y por la misma razón. Es el mismo
criterio que el plan ya había fijado para la lista de recientes.

**El historial NO guarda una sentencia que lleve una contraseña escrita.** Es la
regla dura de CLAUDE.md —los secretos van al keychain del sistema y a ningún
otro lado— aplicada al único lugar donde este paquete la podía violar: alguien
escribe `ALTER USER … PASSWORD 'x'` en el editor, lo corre, y el historial lo
pone en un archivo de texto sin cifrar. Las consultas guardadas menos todavía,
porque ese archivo además se sincroniza.

El filtro reconoce FORMAS, no intención: `PASSWORD '…'`, `IDENTIFIED BY`,
`IDENTIFIED WITH`, `ENCRYPTED PASSWORD`, y el `CREATE SUBSCRIPTION` de Postgres,
que lleva un connection string entero. Se equivoca hacia el lado seguro —un
`SELECT * FROM passwords` no lleva ninguna contraseña y aun así no se guarda— y
esa asimetría es deliberada: el costo de ese error es una consulta que no queda
en la lista, y el del error contrario es una contraseña en un archivo.

El test no comprueba que la función diga «no»: comprueba que **el archivo no
tenga la contraseña adentro**. Es la diferencia entre probar la decisión y
probar el efecto, y una versión que dijera «no guardado» y escribiera igual
pasaría el primero.

**Tampoco se guarda ningún valor de fila.** El historial es lo que escribiste
vos, no lo que contestó la base: los resultados en un archivo local serían una
copia de los datos del servidor sin su control de acceso y sin su cifrado.

**Dos topes y no uno.** El archivo se reescribe entero en cada consulta que
corre, así que el de cantidad (500) evita una lista ingobernable y el de bytes
(512 KB) evita que unas pocas sentencias enormes hagan cara cada escritura.
Cualquiera de los dos solo cubre la mitad: quinientas entradas de ocho
kilobytes serían cuatro megabytes por cada Enter del editor.

**Correr lo mismo dos veces no agrega un renglón.** Repetir un SELECT afinando
nada es lo que uno hace todo el tiempo, y seis renglones idénticos vuelven
inútil la lista justo cuando más se la mira. Se compara contra la MÁS NUEVA y no
contra todas: repetir algo de hace una hora sí es una corrida nueva y merece
subir.

**Una inyección que no fallaba, y lo que le faltaba al test.** Podar el
historial por el lado equivocado —quedarse con las 500 más VIEJAS y tirar todo
lo nuevo— pasaba en verde, porque el caso contaba entradas y no miraba cuáles
sobrevivían. Es el mismo error que un test de «se borraron N filas» sin
preguntar cuáles. Ahora comprueba que la primera sea la última que se corrió y
que la última sea la que corresponde al corte.

**El chip «Ctrl K» estaba desde la Iteración 1 y no hacía nada.** No había
ningún handler de teclado en el frontend: la barra de título de S01 y de S05
prometía un atajo que no existía. Eso es peor que no tenerlo, porque quien lo
prueba concluye que la aplicación está rota, no que la función falta.

La paleta lo arregla en el workspace. **En la pantalla de bienvenida el chip se
sacó y no se reemplazó por nada**: las tres cosas que esa pantalla hace
—conexión nueva, abrir un archivo, acerca de— están las tres a la vista, y una
paleta para buscar entre tres botones visibles no agrega nada. Arreglar una
promesa vacía puede ser cumplirla o retirarla, y de qué lado cae depende de si
hay algo que buscar.

**Las acciones salen de las mismas funciones que los botones.** Una lista
paralela de comandos se separa de la barra en cuanto alguien agrega uno de los
dos, y la que queda vieja es siempre la que menos se mira. Y solo entran las que
se pueden hacer AHORA: sin conexión no hay ninguna, sin tablas no aparecen
«Diagrama» ni «Volcar», y «Cambios pendientes» aparece cuando hay alguno. Una
paleta llena de entradas muertas es la misma promesa vacía del chip, repetida
veinte veces.

**Las acciones van siempre arriba, sin mezclar puntajes con los objetos.** Son
cinco contra doscientas tablas: ordenarlas juntas hace que una tabla llamada
`consultas_guardadas` —que EMPIEZA con lo buscado— le gane a «Nueva consulta»,
y la paleta deje de servir para lo que más se usa. Comprobado escribiendo
`consulta` con las dos cosas en la base.

**El puntaje es explicable a propósito, no difuso.** Cuatro casos en orden: es
exactamente lo buscado, empieza con lo buscado, alguna PALABRA empieza con lo
buscado, aparece en algún lado. Una búsqueda difusa —donde `nc` encuentra «Nueva
consulta»— se siente mágica hasta que ordena mal y nadie puede decir por qué:
cuál de dos resultados sale primero pasa a ser una propiedad emergente de los
pesos. Los separadores de palabra son espacio, guion bajo, punto y guion, que
son los cuatro que aparecen en un nombre de tabla.

**El atajo se escucha en `window` y en fase de CAPTURA.** El foco casi siempre
está adentro de algo que ya escucha teclas —la grilla, CodeMirror— y un handler
en burbuja llegaría después de que el editor SQL se quedara con el evento.

**Del review, el hallazgo que no se veía venir: el atajo y los diálogos.**
`<dialog>.showModal()` pone al diálogo en la TOP LAYER del navegador y deja
inerte al resto del documento. El handler de `window` seguía disparando, así que
con el diálogo de volcado abierto Ctrl+K prendía la paleta: se dibujaba DETRÁS
—ningún z-index le gana a la top layer—, el foco al campo fallaba en silencio
por la inercia, y al cerrar el diálogo la paleta aparecía abierta y muerta, sin
nada enfocado y sin responder a ninguna tecla. El arreglo es no abrirla: si hay
un `dialog[open]`, el atajo no hace nada.

Y el teclado estaba colgado del `<input>`. Un solo clic adentro de la caja —en el
pie, en el relleno de la lista, en el borde— lo desenfocaba, y desde ahí las
flechas, Enter y Escape quedaban muertos: la única salida era clickear afuera.
Ahora se escucha en `window` mientras está abierta.

De los otros diez, tres valen como recordatorio de que la interfaz tiene sus
propias trampas. El `onMouseMove` de cada fila peleaba con las flechas: con el
mouse quieto sobre la lista, bajar hace scrollear las filas debajo del cursor y
el navegador manda `mousemove`, así que la selección saltaba de vuelta a donde
estaba el puntero —ahora solo se hace caso si las coordenadas cambiaron—. Cerrar
la paleta dejaba el foco en `body`, y la grilla y el editor SQL manejan sus
teclas mirando el foco: quedaban sordos hasta que alguien les clickeara encima.
Y en Windows AltGr ES Ctrl+Alt, así que escribir un carácter con AltGr+K en el
editor abría la paleta y se comía la tecla.

**No se agregó un runner de tests al frontend para probar el puntaje**, y eso es
una decisión y no un olvido. El proyecto no tiene ninguno después de ocho
iteraciones, y meter el primero como efecto colateral de esta unidad va contra
la regla de subir una dependencia por vez y en su propio commit. El
ordenamiento, además, es de los que molestan cuando fallan y no de los que
pierden datos: la energía de los tests va a lo segundo. Queda anotado que este
es el primer pedazo de lógica del frontend que justificaría uno.

### Iteración 8 — 2026-09-10

**Reemplazar un objeto es UNA operación del changeset, no dos.** Un `dropObject`
y un `createObject` sueltos dejarían un changeset en el que alguien puede
excluir la segunda mitad y aplicar la primera: el objeto borrado y nada en su
lugar. `replaceObject` lleva la definición entera y una bandera, y el
renderizador decide cuántas sentencias hacen falta —que van juntas en la misma
`Statement`, como ya hace la reconstrucción de tablas de SQLite—.

**La definición se ejecuta TAL CUAL, sin reescribirla.** Es la promesa del
editor —lo que se ve es lo que se ejecuta— y es además lo único defendible:
para agregarle un `OR REPLACE` al texto habría que parsearlo, y un cuerpo de
PL/pgSQL con `$$` adentro no se parsea con una expresión regular. Donde el
motor sabe reemplazar en el lugar, la definición que Kaname muestra ya viene
en esa forma; donde no sabe, va precedida de un DROP que se ve en la vista
previa.

**Reemplazar en el lugar contra borrar y crear no es una preferencia: es la
diferencia entre no poder romper nada y poder perder el objeto.** Si un `CREATE
OR REPLACE` falla —SQL mal escrita, o en Postgres una vista que cambió su lista
de columnas— el objeto queda exactamente como estaba y el error se ve. Borrar y
crear rompe lo que dependa del objeto, y **en MySQL y MariaDB cada sentencia de
DDL hace commit sola**: un CREATE que falla después de un DROP que funcionó deja
el objeto PERDIDO, sin transacción que lo devuelva. Por eso reemplazar es el
valor por defecto, borrar y crear es una escalada que se pide con la lista de
dependientes a la vista, y la nota de la vista previa dice cosas distintas según
el motor. En Postgres y en SQLite el DDL es transaccional y el par se revierte
entero; la nota lo dice y no miente por comodidad.

Qué sabe reemplazar cada motor: Postgres, vistas, funciones, procedimientos y
triggers —`CREATE OR REPLACE TRIGGER` existe desde la 14, que es el mínimo que
soportamos—; MySQL y MariaDB, solo vistas; SQLite, nada. Las vistas
materializadas y las secuencias de Postgres tampoco: recrear una matview pierde
sus filas hasta el próximo REFRESH, y recrear una secuencia le devuelve el valor
inicial, así que la próxima fila puede recibir un id que ya se usó. Lo que el
motor no sabe reemplazar se EXIGE recrear; sin ese candado el CREATE chocaba con
un «already exists» que no dice qué hacer.

MariaDB sí tiene `CREATE OR REPLACE` para rutinas y triggers y **no se
aprovecha**: distinguirla de MySQL ahí haría que la misma pantalla se comportara
distinto contra dos motores que se presentan como compatibles, y el caso que
importa —perder el objeto si el CREATE falla— se cubre igual avisando.

**Lo único que se valida de la definición es que empiece con CREATE**, y eso no
es pereza sino el límite deliberado. Es el error que de verdad pasa: pegar un
SELECT, o el cuerpo suelto de una función, donde iba el CREATE entero. Con
«borrar y volver a crear» puesto eso es un DROP seguido de una sentencia que no
crea nada, y **ningún motor se queja**: un SELECT es SQL perfectamente válida,
corre, devuelve filas y no deja nada. El objeto desaparece y el apply termina en
verde. Más allá de eso no se valida: decidir si un CREATE entero es correcto es
trabajo del servidor, y adivinarlo acá terminaría rechazando SQL válida que
Kaname no entendió.

**El caso de los cuatro motores no mira que la sentencia corra sino que la vista
devuelva OTRA COSA después.** Se parte de una vista que deja pasar una fila de
tres y se reemplaza por una que deja pasar dos: un reemplazo que corre sin error
y deja la definición vieja se ve idéntico a uno que funcionó, y es un fallo
posible —un `CREATE OR REPLACE VIEW` sobre un nombre distinto crea una vista
nueva y deja la vieja intacta—. Después se relee la definición, porque un motor
que devolviera el texto viejo dejaría el editor mostrando lo que ya no es.

**El review `high` de esta mitad encontró dos fallos que hacían el editor
inservible en caminos enteros, y los dos estaban tapados por el mismo agujero de
test.** El caso compartido reemplazaba una VISTA, y una vista se reemplaza en el
lugar en tres de los cuatro motores: el camino de borrar y volver a crear —el
que tiene todo el riesgo— solo se ejercitaba en SQLite.

El primero: **recrear un trigger en Postgres fallaba siempre.** La gramática es
`DROP TRIGGER nombre ON tabla`, donde el nombre es un identificador PELADO —el
esquema va del lado de la tabla— y Kaname lo escribía calificado. `DROP TRIGGER
"s"."t" ON "s"."tabla"` es un error de sintaxis, comprobado contra el servidor.

El segundo es peor porque no se arreglaba cambiando una línea: **en MySQL y
MariaDB el par DROP + CREATE no se podía ejecutar nunca.** El DSN lleva
`multiStatements=false` a propósito, así que las dos sentencias en una sola
cadena no son dos sentencias: son un error de sintaxis. Y como en esa familia lo
único que se reemplaza en el lugar son las vistas, eso era **toda** edición de
rutina o de trigger. Partir la cadena por el `;` al ejecutar no sirve: el cuerpo
de un procedimiento está lleno de puntos y comas.

La salida fue `Statement.Steps`: las sentencias que hay que mandar por separado,
en orden. `SQL` sigue siendo lo que se lee en la vista previa —las mismas, una
debajo de la otra— y los pasos son lo que corre. Las sabe quien armó la
sentencia, que es el único que puede saberlas.

**Dos notas de la vista previa decían cosas que no eran ciertas.** La de Postgres
prometía que el DROP se podía arrastrar con CASCADE y el renderizador nunca lo
escribía —ahora sí—. Y la de SQLite prometía atomicidad a secas: el DDL de SQLite
es transaccional, pero **solo si el apply corre en una transacción**, y con «una
sola transacción» destildado el tramo va sin ella y un CREATE que falla deja el
objeto borrado. Comprobado. Las dos notas ahora dicen la condición en vez de la
conclusión.

**Y la pantalla se saltaba la confirmación de producción.** Llamaba a `Stage`
directo en vez de pasar por `useStage`, así que contra una conexión marcada como
producción el «falta confirmar» de Go caía como texto en un aviso y no había
dónde escribir el nombre de la base: reemplazar un objeto era imposible. En
SQLite, donde toda edición es borrar y crear, eso era **cualquier** edición de
objeto. Es exactamente lo que el comentario de ese hook advierte —«una pantalla
nueva que prepare cambios queda protegida sin acordarse de nada»— y la pantalla
nueva se acordó mal.

Los tres chicos: la firma de una función se concatena en el `DROP FUNCTION` y
venía del frontend **sin validar**, así que un `Args` con un `;` adentro entraba
como sentencias extra —pgx manda el Exec por el protocolo simple—; ahora pasa por
la misma disciplina que un nombre de tipo, que no comprueba que exista sino que
no pueda ser otra cosa. Refrescar pisaba el texto sin guardar del editor, y
«refrescar» pasa después de cada apply, así que una vista a medio editar se
perdía por aplicar un cambio de otra pestaña. Y para una política o una
extensión el error mandaba a «borrarla y volver a crearla», consejo que llevaba
derecho a un segundo error distinto porque tampoco se sabe borrarlas.

**S17: un enum NO se edita como texto, y esa es toda la decisión.** La pantalla
podría mostrar su `CREATE TYPE … AS ENUM ('a','b')` y dejar editarlo, como
cualquier otro objeto. Sería consistente y estaría mal, porque sugiere que las
tres operaciones que ese texto permite —agregar, renombrar, sacar— cuestan lo
mismo. Comprobado contra PostgreSQL 18:

- **Agregar** es `ALTER TYPE … ADD VALUE`. No toca ninguna fila.
- **Renombrar** es `ALTER TYPE … RENAME VALUE`. Tampoco toca filas y sin embargo
  cambia lo que TODAS dicen, a la vez.
- **Sacar no existe.** `ALTER TYPE … DROP VALUE` es un error de sintaxis. Para
  sacar un valor hay que crear un tipo nuevo, reescribir cada columna que use el
  viejo y borrar el original — y eso falla a mitad de camino si alguna fila
  todavía tiene el valor que se quiere sacar.

Un editor de texto dejaría borrar una línea y apretar guardar, y lo que sigue
sería un DROP TYPE que el servidor rechaza porque hay columnas usándolo. La
lista con un botón por operación no ofrece lo que no se puede hacer, que es la
misma regla que sostiene todo el changeset: **una operación que no sabemos
expresar es una operación que la interfaz no ofrece**. Y lo que no se ofrece se
explica ahí mismo, no cuando alguien busque el botón que no está.

**La posición se elige al agregar, porque es la única oportunidad.** El orden de
un enum no es cosmético: en Postgres es el orden en que sus valores COMPARAN y
ORDENAN, así que un `ORDER BY estado` cambia de resultado según dónde entre el
valor nuevo. Y una vez agregado no se puede mover sin recrear el tipo entero. Por
eso la lista va numerada —el número es lo que un ORDER BY va a hacer— y el
combo dice «antes de cuál».

El test no mira la sentencia sino el CATÁLOGO: que escribamos `BEFORE 'alto'` no
prueba que el servidor lo haya puesto ahí. La inyección que lo pone en rojo es
ignorar la posición pedida, y el valor aparece al final.

**Un valor nuevo tiene que confirmarse antes de que algo lo use, y eso partió el
apply.** El review encontró que la justificación de poner el enum en la primera
fase —«una columna nueva puede tener como default un valor del enum»— era
exactamente lo que PostgreSQL prohibe: el valor se agrega dentro de la
transacción pero **no se puede usar hasta que ésta confirme**. Con todo el
changeset en un solo BEGIN —que es lo que la casilla «Una sola transacción»
promete y cumple contra Postgres— el par «agregar el valor» + «usarlo» daba
`unsafe use of new value` (SQLSTATE 55P04) y revertía el changeset entero.

De ahí salió `Statement.Aislada`: la sentencia que tiene que confirmarse SOLA,
antes de que siga el resto. `TramosDe` la corta a los dos lados aunque el motor
tenga DDL transaccional. No deja de ser todo o nada —sigue siendo una sola
sentencia, que es atómica—; lo que se pierde es agruparla, que es justo lo que
hacía falta.

**Y la primera medición de esto estuvo mal, de una forma que vale anotar.**
Probando con `psql -c "BEGIN; ALTER TYPE …; INSERT …; COMMIT;"` contra la 17 y la
18 funcionaba, así que la conclusión fue «la 17 lo relajó» y quedó escrita en
tres comentarios. Es falso: `psql -c` manda todas las sentencias en UN mensaje
de consulta simple, y ahí el servidor no se queja. Mandadas de a una —que es como
las manda Kaname— **fallan en las cuatro versiones que soportamos**. Lo que
descubrió el error no fue leer la documentación sino la inyección: sacar el
aislamiento tiró el test contra la 18, que según mi medición no tenía que fallar.
Una herramienta de línea de comandos no manda las sentencias como las manda la
aplicación, y medir con ella es medir otra cosa.

**Del review, tres cosas más que hacían mal la cuenta de lo que ya se
preparó.** La lista de valores que la pantalla muestra sale de la definición que
está EN LA BASE, y preparar un cambio no la toca. Así que agregar «medio» dos
veces entraba dos cambios idénticos —la comprobación de duplicados miraba una
lista que no había cambiado— y el segundo fallaba al aplicar con «enum label
already exists», tirando el changeset entero porque Postgres lo corre en una
transacción. Ahora la pantalla lee también lo que hay preparado y lo muestra
aparte, con borde punteado: un valor preparado y uno que ya existe son cosas
distintas, y uno de los dos todavía no se puede consultar.

El combo de la posición no se podía volver atrás. Con `estricto`, el valor solo
sale de la lista de opciones, y «Al final» era un *placeholder* y no una
opción: una vez elegido «antes de alto» no había forma de deshacerlo —ni
borrando el texto, ni con Escape— y se preparaba una posición que ya no se
quería. Para un enum eso no se corrige después.

Y lo escrito se limpiaba antes de saber que el cambio había entrado. El primer
arreglo fue peor que el problema: limpiaba cuando lo tipeado coincidía con algo
ya preparado, así que volver a escribir un valor preparado —para ver por qué no
se podía— hacía desaparecer lo tipeado y el aviso no llegaba a mostrarse nunca.
Se limpia lo que ESTE formulario envió, no cualquier cosa que coincida; lo
encontró mirar la pantalla, no un test. Contra
producción, `stage` abre el diálogo de confirmación y **devuelve sin haber
preparado nada**; limpiar ahí borraba lo tipeado mientras el diálogo seguía
arriba, y si alguien cancelaba, sin decir nada. Se limpia cuando el valor
aparece entre los preparados, que es lo único que prueba que entró.

Los tres chicos: la pantalla de pendientes no tenía etiqueta para los tipos
nuevos —salían con su nombre camelCase— y armaba la línea como
`addEnumValue · .humor`, con un punto colgando, porque un cambio de objeto no
cuelga de ninguna tabla; el renderizador usaba `literal()`, que convierte la
cadena vacía en el keyword NULL —correcto para sacar un comentario, sin sentido
para una etiqueta— y ahora usa el citador de cadenas a secas; y las etiquetas no
pasaban por el límite de 63 bytes que tiene todo otro nombre, así que las
rechazaba el servidor en vez del renderizador, después de haberlas dejado
preparar y revisar.

También se tipó el callback que prepara el cambio, que estaba como `unknown` con
un `as never` del otro lado. Eso borraba el contrato entero: escribir
`beforeValue` en vez de `before` compilaba igual y el campo se perdía en
silencio — y ese campo es la posición del valor, lo único que después no se
puede corregir.

**Los enums son de PostgreSQL y punto.** En MySQL y MariaDB un ENUM es un TIPO
DE COLUMNA y no un objeto del catálogo —vive en la definición de la columna— y
SQLite no los tiene. No hace falta ninguna capacidad nueva para que la pantalla
no aparezca ahí: el árbol no lista ninguno, porque el catálogo no tiene ninguno.

**S16 se parte en dos, y el orden no es casual: primero los dependientes.** La
pantalla del objeto ya existía en solo lectura, así que la lista de qué se rompe
se puede mostrar ahí y aporta sola. Al revés —el editor primero— habría un
momento con una acción destructiva en pantalla y sin el aviso que la hace
segura, aunque durara un commit.

**Una lista de dependientes vacía y «no se puede saber» son la misma lista y
significan lo contrario.** Es toda la razón de ser de `schema.Dependents`: sin
el campo `Unknown` el tipo sobraría y sería un `[]Object`. Vacía quiere decir
«reemplazá tranquilo, no rompés nada», que es exactamente lo que alguien mira
antes de apretar algo destructivo. Y de los cuatro motores, **solo Postgres
puede contestar de verdad**: MySQL sabe desde 8.0.13 y solo para vistas,
MariaDB no tiene `VIEW_TABLE_USAGE`, y SQLite no guarda dependencias en
absoluto. Los tres que no saben lo dicen, con el motivo, y la pantalla los
pinta distinto —recuadro punteado, no el fondo tranquilo del «nada»—.

En SQLite se evaluó buscar el nombre del objeto adentro del texto de los demás,
que es lo único que `sqlite_master` tiene. Se descartó porque es adivinar:
encontraría una coincidencia en un comentario o en una cadena, y se perdería una
vista que lo usa con otro alias. Una respuesta inventada es peor que ninguna
cuando lo que sigue es un DROP.

**La consulta de Postgres pasa por `pg_rewrite`, y ese salto es el que se
olvida.** Una vista NO depende de sus tablas directamente: depende a través de
su regla `_RETURN`. Escrita de la forma que parece obvia —`pg_depend` contra
`pg_class`— la consulta devuelve **vacío**, y vacío acá significa «no depende
nada de esto». Es la inyección del test: con ella, una vista de la que cuelgan
otra vista y una materializada reporta `map[]`. Solo se cuentan las
dependencias `deptype = 'n'`, que son las que un DROP hace fallar y un DROP
CASCADE arrastra; las automáticas —la secuencia de un `serial`, el índice de una
restricción— son parte del objeto que las creó. Eso es lo único que se excluye a
propósito: la primera versión dejaba afuera además clases ENTERAS sin querer, y
eso lo arregló el review —ver más abajo—.

La otra mitad de esa consulta son los triggers que llaman a una función, y es la
que vuelve peligroso reemplazar una función: borrarla y volver a crearla rompe
cada trigger que la use, y esos triggers están en otra pantalla.

**El caso compartido no exige la lista, exige la honestidad.** Pedirle a los
cuatro motores que nombren dependientes sería pedirle a tres lo que no pueden.
Lo que se exige es la invariante que sí vale para todos: o hay lista, o se dice
que no se puede saber **con el motivo**, y `Vacio()` nunca devuelve verdadero
con `Unknown` puesto — que es exactamente el «nada depende de esto» que el caso
existe para impedir.

**El review `high` encontró que esta unidad cometía su propio error.** El tipo
existe para impedir que una lista vacía mienta, y la primera versión mentía en
cuatro clases de objeto. `pg_depend` no apunta al objeto que uno tiene en la
cabeza sino al que lo implementa: una columna tipada con un enum se registra
como una fila de `pg_class` con `objsubid`, y el `nextval(…)` de una columna
como una de `pg_attrdef`. Ninguna de las dos entra por la rama de las vistas ni
por la de los triggers, así que un enum que una tabla usa —uno que el propio
servidor se niega a borrar— contestaba «Nada. Reemplazarlo no rompe ningún otro
objeto». Comprobado contra PostgreSQL 18 antes de tocar nada.

La salida fue una tercera rama con `pg_identify_object`, que nombra cualquier
dependiente sea de la clase que sea, y dejar la clase **cruda** tal como la dice
el catálogo —«table column», «default value»— en vez de traducirla a una de las
nuestras. Es el mismo criterio que la cobertura del volcado y que los grupos del
árbol: lo que no se conoce se muestra con su nombre, no se esconde. El test
tiene un candado: le pide al servidor un `DROP TYPE` y **exige que falle**, así
que si algún día dejara de haber dependencia el caso se rompe en vez de pasar
sin probar nada.

**El `default` de la búsqueda contestaba vacío para todo lo que no reconocía.**
El comentario justificaba solo a los triggers —de un trigger no cuelga nada, y
ahí «vacío» es la verdad— pero la rama se comía también las políticas, las
extensiones y lo que aparezca mañana. Un `DROP EXTENSION postgis` arrastra
cientos de objetos. Ahora el trigger es el único caso explícito de «vacío y
seguro» y todo lo demás dice que no se sabe.

**MySQL esconde filas por privilegio y no avisa.** `VIEW_TABLE_USAGE` devuelve
solo las vistas sobre las que la conexión tiene `SHOW VIEW`; sin el permiso no
da error, da menos filas — y menos filas, cuando llegan a cero, se leen como «no
depende nada». Se comprueba el privilegio GLOBAL y no el de la base del objeto,
porque una vista de otra base puede depender de esta tabla: tener todo sobre la
propia no alcanza para afirmar que la lista está completa. Si no se puede
afirmar, lo que se encontró se devuelve igual pero marcado como incompleto.

Eso obligó a rehacer el panel: **la lista y el aviso de incompleto son dos
preguntas, no un `if` encadenado**. Qué se encontró, y si se puede confiar en
que eso es todo. Se dan juntas más seguido de lo que parece, y un `else` entre
las dos habría tirado la lista para mostrar el aviso — o al revés.

**Y los tres estados no se distinguían, porque los tokens de color que usé no
existen.** Inventé `--surface`, `--surface-2`, `--text` y `--text-faint`; el
sistema define `--bg-panel`, `--bg-inset` y `--text-1/2/3`. Un `var()` que no
resuelve no falla: no pinta. Así que «no hay» y «no se puede saber» salían las
dos sobre fondo transparente, separadas apenas por un borde punteado, mientras
el comentario del archivo afirmaba que se distinguían por color y el del
componente hablaba de un verde tranquilo que no estaba en ninguna parte. Ahora
«no hay» usa `--success` de verdad. La regla de CLAUDE.md dice colores solo por
tokens, y esto es el modo de romperla que ningún linter ve: usar la sintaxis
correcta con un nombre inventado.

Los dos chicos: los nombres de trigger son únicos **por tabla** y no por
esquema, así que dos `auditar` sobre tablas distintas colisionaban en la misma
clave de React y salían como un renglón repetido —y la tabla es justamente lo
único que dice dónde ir a arreglarlo, así que ahora viaja y se muestra—; y el
error de MariaDB se reconoce por su NÚMERO (1109) y no por su texto, porque los
servidores traducen sus mensajes según `lc_messages` y contra uno en otro idioma
el camino honesto de «no se puede saber» se degradaba en un error rojo.

**Probadas en la pantalla las CUATRO combinaciones**, que es donde se ve si de
verdad son distinguibles —un test no puede comprobar que dos cosas se ven
distintas—:

| En pantalla | Dónde |
|---|---|
| Los nombra uno por uno, con su glifo | Postgres, vista de la que cuelgan otras |
| «Nada. Reemplazarlo no rompe ningún otro objeto», en verde | Postgres, vista sola |
| «No se puede saber» con el porqué, recuadro punteado | MariaDB, cualquier vista |
| La lista **y** «Y puede haber más» debajo | MySQL 9.7 con el usuario de los contenedores |

La cuarta pareció al principio que pedía un usuario restringido armado a mano, y
no: el usuario `kaname` del compose tiene `USAGE ON *.*` y todo solo sobre su
propia base, que es exactamente la condición. **Cualquier conexión MySQL que no
sea de un administrador cae ahí**, así que no es un caso raro —es el normal—, y
eso vuelve al aviso más importante de lo que parecía al escribirlo. Los dos
bloques salen con fondo distinto: ámbar sólido el de la lista, punteado el de la
advertencia.

**Los objetos viajan con el snapshot, y por qué.** El árbol podría cargarlos al
abrir cada grupo, y sería más barato. No se hace porque el buscador de arriba
filtra sobre TODO lo que hay: una lista que se carga al abrir no se puede buscar
sin abrirla, y escribir «pedidos» tiene que encontrar la vista
`pedidos_del_mes` con el grupo Vistas cerrado. Son solo nombres; la definición,
que sí puede pesar kilobytes, se pide de a una cuando alguien la abre.

Se llenan en `service`, después del `Introspect` de cada motor y no adentro de
los tres: es una implementación en vez de tres, y es exactamente la misma
llamada que usa la cobertura del volcado, así que el árbol y el archivo no
pueden discrepar sobre qué hay en la base.

**Que el catálogo no se pueda leer no rompe la conexión, pero tampoco se
calla.** Son dos errores opuestos y los dos son fáciles de cometer. Hacer fallar
un «Conectar» entero porque una consulta al catálogo falló es una regresión
sobre lo que venía funcionando —las tablas se leyeron bien y el árbol sirve—; y
tragarse el fallo haría que un esquema lleno de vistas se viera idéntico a uno
que no tiene ninguna. Queda en `Snapshot.ObjectsError` y el árbol lo muestra
arriba, en ámbar y no en rojo: falta una parte, no falló la pantalla.

**`type` se partió en enum, dominio y tipo compuesto.** Se comportan distinto y
se editan distinto: un enum se cambia agregando valores, un dominio es una
restricción sobre otro tipo, un compuesto es una forma de fila. Un árbol que
muestra los tres como «tipo» obliga a abrirlos para saber cuál es cuál. La
cobertura del volcado gana precisión gratis: donde decía «3 tipos» ahora dice
«2 enums y 1 dominio».

**El design system tenía el mismo problema que el backend, y se arregló igual.**
Los glifos de S00 declaraban su propia lista de clases —`matview` donde Go dice
`materializedView`— así que mostrar un objeto obligaba a traducir en cada
pantalla: una tabla de equivalencias que hay que acordarse de actualizar dos
veces, y que en el último lugar que la copie va a estar desactualizada. Ahora
los nombres son los que manda el backend. `table`, `index`, `primaryKey`,
`foreignKey`, `schema`, `query` y `erd` siguen siendo de la interfaz, porque no
vienen del catálogo.

**Los grupos arrancan cerrados y los esquemas abiertos.** No es inconsistencia:
un esquema tiene uno o dos hermanos, y los grupos son hasta doce por esquema.
Abiertos, empujan las tablas —que es lo que casi siempre se busca— fuera de la
pantalla. Buscar expande las dos cosas, porque esconder una coincidencia detrás
de un nodo cerrado es lo contrario de lo que se pidió.

**Dos bugs que solo aparecieron mirando la pantalla.** Los tests estaban en
verde y la interfaz decía «Editar **un** secuencia»: el artículo salía de un
caso especial escrito para la única palabra que se había probado. El género va
en la tabla de clases, al lado del nombre. Y el mensaje de un dominio llegaba
como «leer la definición: sin definición: kn_s05.positivo es un dominio, y
Kaname todavía solo escribe enums» —dos encabezados antes de la frase que
explica algo—. El servicio dejó de re-envolver, porque los mensajes de cada
motor ya nombran al objeto, y el texto del `ErrSinDefinicion` pasó a ser la
cláusula que cierra la frase en vez de un prefijo suelto. Al arreglarlo me pasé
para el otro lado —quedó diciendo lo mismo dos veces— y hubo que sacar la
duplicación: ahora es «kn_s05.positivo es un dominio: Kaname todavía no sabe
leer esta clase de objeto».

**Del review `high`, siete hallazgos, y tres importaban.** El peor lo
introduje al hacer que el árbol comparta `selected` entre tablas y objetos: el
esquema en curso se deducía partiendo el id por el primer punto, y el id de un
objeto es `view:public.v_ventas`, así que elegir una vista dejaba el esquema en
«view:public» y «Diagrama» abría una pestaña vacía contra un esquema inexistente
—y guardaba un layout bajo esa clave—. El esquema ahora se GUARDA al elegir en
vez de deducirse de un id cuya forma cambió.

El segundo: la pestaña del objeto no escuchaba el contador de «Refrescar», así
que recrear una vista desde el editor y refrescar dejaba la definición vieja en
pantalla, con la única salida de cerrar y reabrir. Es exactamente la regresión
para la que el contador se inventó en la grilla.

El tercero contradecía lo que esta sección dice dos párrafos más arriba.
`Objects` son ocho consultas al catálogo y **cortaba en la primera que fallara**,
devolviendo nada: la de los eventos va última y en algunas variantes de MariaDB
no se puede leer, así que por ella desaparecían del árbol las vistas, las
funciones y los triggers que ya se habían leído bien. Ahora los fallos se juntan,
el recorrido sigue, y el método devuelve **lo que alcanzó a leer junto con el
error**. Quien llama decide con las dos mitades: el árbol muestra lo que hay y
dice qué faltó, el volcado se niega a escribir un archivo incompleto.

Y cuatro chicos que igual se ven: el orden de los grupos del árbol **no era** el
de la cobertura del volcado aunque el comentario jurara que sí —se alinearon los
dos, y cada lista nombra a la otra—; el glifo de una secuencia y el de una
consulta decían los dos «SQ», indistinguibles en la barra de pestañas desde que
una secuencia se puede abrir; el aviso de error colgaba adentro del
`role="tree"`, donde un lector de pantalla lo descarta; y el contador del
esquema contaba solo tablas, así que buscar «ventas» mostraba «0 de 40» justo
encima de la vista que sí coincidía.

**Probado a mano contra Postgres 18 y MySQL 9.7**, con objetos de las siete
clases. La vista vuelve con su `WITH (security_invoker=true)`, el enum con sus
tres valores en orden de declaración, la secuencia sin su `last_value`, las dos
sobrecargas de `calcular` como dos nodos distinguibles con su propio cuerpo, y
el dominio con el mensaje que dice qué es. En MySQL la vista vuelve con su
`DEFINER`, como se decidió. MariaDB y SQLite quedaron cubiertos por los tests
—que corren el mismo código— pero no se miraron en la interfaz.

**Una sola lista de objetos, no dos.** La cobertura del volcado ya recorría el
catálogo de los cuatro motores buscando vistas, matviews, funciones,
procedimientos, triggers, políticas, tipos y secuencias, para poder decir con
nombre y apellido qué quedaba afuera del archivo. El árbol de S05 necesita
exactamente esa lista. Escribir un segundo método habría sido las mismas ocho
consultas al catálogo escritas dos veces por motor, y la segunda copia se
atrasa sin que nadie lo note. Así que `Conn.Uncovered` pasó a llamarse
`Conn.Objects` y **qué de eso sabe escribir el volcado se decide en `dump`**,
del lado que lo sabe. Es lo que hace barata la promesa de la sección del
volcado: cuando S16 haga que una vista se pueda volcar, la cobertura se achica
cambiando un filtro.

**Se borró `schema.Kind`.** La iteración 1 lo declaró entero —tablas, vistas,
matviews, funciones, procedures, triggers, enums, secuencias— «para no tener que
migrar el contrato del frontend en cada iteración». Ocho iteraciones después no
lo usaba **nadie**, ni una sola línea, y mientras tanto la cobertura del volcado
creó `schema.ObjectKind`, que sí está vivo en los tres motores y en el puente.
Eran dos vocabularios para lo mismo, y encima discrepaban: `matview` contra
`materializedView`, `enum` contra `type`. Justo antes de que el árbol empezara a
usar uno de los dos. Queda `ObjectKind`, el que tiene código detrás.

La lección no es «no declares de más». Es que **un modelo declarado por
adelantado y nunca ejecutado no está listo: está sin probar**, y a los ocho
meses compite con el que se escribió mirando el problema de verdad.

**Un objeto no se identifica con esquema y nombre.** Postgres permite
`demo.calcular(integer)` y `demo.calcular(text)`: dos objetos distintos con el
mismo nombre. Sin la firma salían como dos entradas idénticas en la cobertura
del volcado —«quedan afuera demo.calcular y demo.calcular»—, serían dos nodos
indistinguibles en el árbol, y no habría forma de pedir la definición de una sin
ambigüedad, que es justo lo que el editor necesita. `schema.Object` lleva ahora
`Args` con la firma de identidad, y la consulta de definición filtra por ella.
Es un bug que ya existía en el volcado y que apareció al mirar el mismo dato
para otra cosa.

**La definición se normaliza a un CREATE completo, y eso es una decisión.** Los
cuatro motores devuelven formas distintas: `pg_get_viewdef` da solo el SELECT,
`pg_get_functiondef` da el CREATE entero, `SHOW CREATE VIEW` trae el `DEFINER` y
el `ALGORITHM`, y `sqlite_master.sql` guarda el texto exacto con el que se
escribió el objeto. Todas se normalizan a un CREATE completo porque es lo único
que hace verdadera la promesa del editor: **lo que se ve es lo que se ejecuta**.
Mostrar un cuerpo y ejecutar otra cosa alrededor deja a la persona revisando un
texto que no es el que corre, y la revisión es la garantía de fondo de todo el
apply.

Dos consecuencias que se eligieron a conciencia. El `DEFINER=usuario@host` de
MySQL **se deja**: sacarlo daría un texto más portable y sería mentir, porque
una vista con DEFINER se comporta distinto de una sin él, y que ese usuario no
exista en otro servidor conviene verlo antes de mover el objeto. Y el texto de
SQLite vuelve **tal como se escribió**, sin comillas ni `IF NOT EXISTS`
agregados: uniformarlo sería cambiar el objeto de la persona por otro
equivalente.

Lo que Kaname todavía no sabe reconstruir —dominios, tipos compuestos, políticas
de RLS— devuelve un error que dice qué es, y no una cadena vacía. En un editor,
una definición vacía se ve igual que un objeto sin cuerpo, y guardarla lo
borraría.

**El test es el ciclo completo, y una inyección mostró por qué la segunda mitad
no es un extra.** `la definición de un objeto se puede volver a correr` crea una
vista, pide su definición, **borra la vista** y la recrea con lo que salió. Que
el texto «contenga CREATE» no probaría nada. Al inyectar el fallo más probable
—devolver el cuerpo de `pg_get_viewdef` sin su `CREATE VIEW … AS` adelante— el
`Exec` **no falla**: un SELECT pelado es una sentencia perfectamente válida, no
crea nada y devuelve sin error. Lo que lo caza es el `count(*)` de después.
Ejecutar sin error y hacer lo que se pidió no son lo mismo. La otra inyección
—elegir la columna de `SHOW CREATE` por posición en vez de por nombre— pone en
rojo a MySQL y MariaDB, y es la que justifica buscarla por nombre: `SHOW CREATE
VIEW` devuelve cuatro columnas, `FUNCTION` seis y `TRIGGER` siete, y MariaDB no
promete las mismas que MySQL.

**El review `high` encontró que «un CREATE completo» no lo era.**
`pg_get_viewdef` devuelve el SELECT y nada más: las opciones de la vista viven
en `reloptions`, aparte, y quedaban afuera. Comprobado contra PostgreSQL 18: una
vista creada `WITH (security_invoker = true, check_option = cascaded)` volvía
como un SELECT pelado. Las consecuencias son las dos peores de su clase, porque
las dos se ven idénticas a que todo esté bien:

- **`security_invoker` desaparecía**, así que la vista pasaba a correr con los
  permisos de su DUEÑO en vez de los de quien consulta. Alguien abre una vista
  endurecida en el editor, la guarda sin tocar nada, y queda una vista
  permeable con el mismo nombre y el mismo cuerpo.
- **`check_option` desaparecía**, así que la vista dejaba de rechazar las
  escrituras que se saldrían de ella: se aceptan, y la fila desaparece de la
  vista donde se escribió.

Se escriben tal cual vienen del catálogo, en el mismo `WITH (…)` que acepta el
CREATE. Postgres guarda `check_option=cascaded` como una reloption más, así que
traducirla a `WITH CASCADED CHECK OPTION` sería un caso especial que hay que
mantener, y esta forma cubre también las que aparezcan mañana. El test no mira
el texto: borra la vista, la recrea y le pregunta al CATÁLOGO, porque mirar el
texto probaría que escribimos las palabras, no que el motor las entendió.

**Y encontró que el caso compartido no podía cazar eso.** La tabla de la suite
no tenía filas, así que `count(*)` daba 0 para cualquier vista sobre ella: una
definición que perdiera el WHERE pasaba igual, y el mensaje de error prometía
—«no devuelve lo mismo»— algo que el test no miraba. Ahora hay tres filas y la
vista deja pasar una; con el WHERE perdido el caso dice `count(*) = "3"`.

**La firma de una función depende del `search_path` de la conexión que la
leyó.** `pg_get_function_identity_arguments` escribe `m demo.humor` con el path
por defecto y `m humor` después de un `SET search_path TO demo`. Como `Objects`
y `ObjectDefinition` son dos viajes a un POOL, pueden caer en conexiones
distintas —y basta un `SET search_path` corrido en el editor SQL, que se pega a
una sola conexión, para que los dos textos difieran—. La igualdad no encontraba
nada y el usuario veía «ya no está en la base» sobre una función que está. Con
una sola candidata la firma no hacía falta para empezar, así que se usa esa; con
varias no se adivina, se dice cuántas hay. De paso: la firma **incluye los
nombres de los parámetros y los modos**, no solo los tipos —la documentación del
campo decía lo contrario— y se deja así porque es la forma de identidad que usa
el propio motor en un `DROP FUNCTION`.

**Encontrado de paso, sin arreglar todavía:** una consulta contra una tabla que
no existe dice **«El servidor rechazó la conexión con la consulta»**. El camino
de las consultas (`query.go`) clasifica con `Classify`, que contesta «¿por qué no
llegué al servidor?», cuando el servidor contestó perfectamente y dijo que no. Es
el mismo bug que `stmterrors.go` documenta haber arreglado para el apply,
todavía vivo en el camino que más se usa: cada tipeo en el editor SQL. Va en su
propio commit, y en los cuatro motores.

### Iteración 7 — 2026-09-10

**Los valores de una fila nunca están en la SQL que se ejecuta.** Es la regla
de CLAUDE.md —«SQL siempre parametrizado»— aplicada al único lugar donde
tentaba no hacerlo: la vista previa. Un `UPDATE` que muestra `$1` no se puede
revisar, y la revisión es la garantía de fondo de todo el apply. La salida es
que cada cambio de datos sale en **dos formas del mismo renderizado**:
`Statement.SQL`, con los valores escritos como literales, para leer, copiar y
guardar como `.sql`; y `Statement.Bound`, con marcadores y los valores aparte,
que es lo único que corre. Las dos se escriben en el mismo recorrido —cada
pedazo de texto va a las dos, cada valor va como literal a una y como marcador
a la otra— para que no puedan diferir, y `Bound` lleva `json:"-"`: no cruza el
puente porque el frontend no ejecuta nada y los valores ya viajan en el cambio.

Se evaluó ejecutar la forma legible, como hace el DDL. Se descartó por dos
motivos que un test no ve: con `standard_conforming_strings` apagado —raro, pero
se puede poner por sesión— un `a\nb` escrito como literal se guarda con un salto
de línea, y con un charset multibyte viejo una barra invertida al final de un
valor puede comerse la comilla. Con parámetros, nada de eso existe. Y un test
sí ve la otra mitad: `TestLoQueCorreEsLaFormaConParametrosYNoLaLegible` mira el
CAMINO y no el resultado, porque desde afuera las dos formas dejan la misma
fila y los tests contra los motores seguirían verdes si esto cambiara.

**Comprobado contra los motores, no supuesto:** pgx v5 manda un `*string` en
formato texto para cualquier tipo de parámetro —`numeric`, `boolean`,
`timestamptz`, `jsonb`, `bytea` como `\x…`, `int[]` como `{1,2,3}`— y el
servidor lo interpreta según la columna; `nil` es NULL. Por eso todos los
valores de la grilla son texto: es lo que devuelve el servidor y lo que vuelve
a leer, sin que Go interprete nada en el medio. MySQL y SQLite hacen lo mismo
por afinidad.

**Un cambio de datos tiene que tocar exactamente una fila, y se comprueba.**
`engine.Conn` y `engine.Tx` ganan `Modify(ctx, sql, args) (filas, error)`
separado de `Exec`: son dos contratos, no uno con argumentos opcionales. Con el
conteo, el apply exige lo que `Bound.Rows` dice —1 para insertar, actualizar y
borrar por clave— y cualquier otro número es un fallo de datos que revierte el
tramo: **cero** es que otra sesión borró la fila o le cambió la clave desde que
se leyó, y **dos o más** es que la clave con la que se identificó la fila no era
clave. Sin esto, editar una celda de una fila ajena diría «aplicado» sobre
nada, y un `UPDATE` sobre una tabla sin clave única pisaría la segunda fila en
silencio. Los dos casos tienen test contra los cuatro motores, y en los cuatro
la edición buena que iba en el mismo tramo también vuelve atrás.

**MySQL y MariaDB cuentan filas cambiadas, no alcanzadas.** Un `UPDATE` que deja
el mismo valor que ya estaba reporta **cero** filas afectadas —comprobado en
MySQL 9.7 y MariaDB 12.3—, que con la regla de arriba se leería como «la fila
ya no está». `cfg.ClientFoundRows = true` en `mysql.Open` hace que cuenten las
alcanzadas, como Postgres y SQLite. Efecto colateral, aceptado y anotado: el
editor de SQL informa para un `UPDATE` las filas coincidentes y no las
cambiadas, como Postgres y no como el cliente de MySQL.

**Un cambio de datos es UNA fila.** `insertRow`, `updateRow` y `deleteRow`, con
`Values` (las columnas cargadas o cambiadas), `Key` (la clave primaria con el
valor que tenía al leerla) y `Previous` (lo que había, solo para que la
revisión pueda mostrar «apodo: juan → juanci»; no entra en ninguna sentencia).
Editar tres celdas de la misma fila es un cambio con tres `Values`, no tres
cambios: es una sentencia y se revisa como tal. Solo la clave primaria
identifica la fila —una tabla sin clave no se edita desde la grilla, como
dice el plan— y una clave nula va como `IS NULL`, que solo puede pasar en
SQLite por su compatibilidad histórica con `PRIMARY KEY` no `INTEGER`.

**Solo borrar una fila es destructivo.** Contra producción pide la misma
confirmación al preparar que borrar una columna. Editarla no: el valor viejo
está en `Previous` y se puede volver a escribir. Y para que diez filas
borradas no sean diez confirmaciones, `Session.StageMany` prepara una tanda
entera, todo o nada, con una sola palabra.

**El renderizado del DML es uno para los cuatro motores.** `internal/dml`
escribe las tres sentencias con un `Dialect` de cinco cosas —cómo se califica
la tabla, cómo se cita un identificador, cómo se cita un literal, cómo se
escribe el marcador y cómo se inserta una fila sin valores—. El DDL se escribe
en cada motor porque cada motor tiene su gramática; un `UPDATE` por clave es
el mismo en los cuatro, y escribirlo cuatro veces serían cuatro lugares donde
equivocar la comprobación de filas. MySQL no acepta `DEFAULT VALUES`: la fila
vacía es `() VALUES ()`. Y el literal legible de MySQL sigue el modo del
servidor —`NO_BACKSLASH_ESCAPES`—, igual que el DDL; la forma que corre no
depende de eso porque el valor va como parámetro.

**Los errores del propio servicio se clasifican antes que los del motor.** El
clasificador de cada motor solo entiende errores del driver y manda cualquier
otro al cajón de «no se pudo conectar» —el mismo bug de la iteración 5—. Se
comprobó inyectando: sin `clasificar`, la fila que otro borró se reportaba
como «No se pudo conectar con kaname@127.0.0.1:55432/kaname_test».

**Del `/code-review high` de esta unidad salieron cuatro cosas, las cuatro
legítimas:**

1. El hint de «la fila ya no está» decía «Nada quedó aplicado», y con varios
   tramos —MySQL con un DDL adelante— es mentira: el DDL ya commiteó. El hint
   ya no afirma nada sobre el resto; eso lo dice `ApplyResult` con `RolledBack`
   y el tramo que falló.
2. Un `INSERT` también puede alcanzar cero filas sin error —un trigger `BEFORE`
   que devuelve NULL, una regla `DO INSTEAD NOTHING`— y el mensaje culpaba a
   «otra sesión». `Bound.Op` distingue: el INSERT que no insertó se explica
   como tal.
3. **Un apply de puras filas tiraba el snapshot del esquema** y el frontend
   volvía a inspeccionar el catálogo entero por una celda editada: justo lo que
   CLAUDE.md prohíbe. `ApplyResult.SchemaChanged` dice si algún cambio de
   esquema quedó aplicado; solo entonces se invalida el snapshot y el shell
   relee el árbol. Si no, se recargan las pestañas y nada más.
4. **En SQLite, una reconstrucción de tabla apaga las claves foráneas de la
   transacción entera**, y los cambios de datos van en la misma. Un borrado de
   padre que depende de `ON DELETE CASCADE` no arrastra nada, deja huérfanas,
   y el `foreign_key_check` del cierre rechaza el apply completo — aunque el
   mismo borrado, solo, ande. Se evaluó partir el tramo (datos con claves
   encendidas, reconstrucción aparte) y se descartó por ahora: rompe el «todo
   o nada» de SQLite justo en la combinación que lo tiene, y el ensayo ya no
   podría correr la misma transacción que el apply. Como falla seguro y no
   deja nada a medias, la respuesta es un aviso en la revisión que dice qué
   pasa y qué hacer —aplicar primero el esquema y después los datos—, con un
   test que reproduce el caso y comprueba que no queda nada a medias.

**La grilla guarda lo editado hasta «Preparar», y lo convierte Go.** Lo que se
escribe en una celda no es un cambio: es texto que se deshace con un clic. Vive
en la pantalla, identificado por la POSICIÓN de la fila en la página cargada, y
recién al preparar viaja a `Session.StageGrid` como un volcado —las filas leídas
enteras, lo editado por nombre de columna— que Go convierte en `insertRow`,
`updateRow` y `deleteRow`. La conversión está en Go y no en TypeScript por la
regla de siempre —el contrato se prueba con tests de Go— y por dos cosas que
conviene decidir de ese lado: el **orden** de las columnas en la sentencia (un
mapa de TypeScript no lo tiene, y un `SET` que cambia de orden entre dos vistas
previas es un error de programa) y **qué identifica la fila**, que sale de la
fila como se LEYÓ aunque se haya editado la clave misma.

Identificar por posición es frágil a propósito: ordenar por otra columna vuelve
a leer desde el principio y las ediciones se pierden, así que la pantalla
pregunta antes. «Refrescar» y un apply las descartan sin preguntar, porque la
base ya cambió y lo editado era sobre la vieja. Identificarlas por clave y
sobrevivir a la recarga sería más cómodo y más mentiroso: si la fila cambió del
otro lado, lo que se está editando ya no es lo que se ve.

**«Por defecto» no es NULL ni cadena vacía.** Una celda sin cargar de una fila
nueva no va en el INSERT: la base pone su valor. Se muestra como `[default]` en
gris, sin itálica, para que no se confunda con `[null]`, y no se mezcla con las
filas leídas en el mismo arreglo de strings — no hay centinela que no pueda
chocar con un valor real, así que las filas nuevas se guardan aparte y la grilla
pregunta por (fila, columna) a una sola función que decide qué mostrar.

**Sin selector de enum todavía.** El diseño de S10 abre un desplegable con los
valores del enum al editar una celda `ENM`. Los enums no se introspectan como
objetos hasta S17 (iteración 8); hasta entonces una celda de enum se edita como
texto y el servidor valida. Anotado para cuando existan.

**La revisión de una fila mira la base, de a una fila.** S08 no se limita a
mostrar el diff: `Session.ReviewRow` corre comprobaciones que solo la base
puede contestar —si la clave sigue identificando una sola fila, si existe el
padre al que apunta cada clave foránea que se escribe, cuántas hijas arrastra un
borrado y qué les pasa según `ON DELETE`, qué columnas `NOT NULL` sin default
quedaron sin valor en un alta—. Cada una es un `SELECT COUNT(*)` parametrizado
(`engine.Conn.CountWhere`, escrito por `dml.CountWhere` con el mismo escritor
que las sentencias, así que los valores tampoco entran acá en el texto). Se
piden para la fila que se está mirando y no para todas al abrir: cien filas
abiertas de golpe serían quinientas consultas antes de leer nada, y la pantalla
muestra de a una. No reemplazan la comprobación del apply —la fila puede
cambiar entre la revisión y el COMMIT— pero contestan lo que quien revisa quiere
saber ahora, y las tres etiquetas dicen cosas distintas: «ok», «warn» es algo
que va a pasar y conviene saber (una cascada), «bad» es algo que va a FALLAR.
Con tests en los cuatro motores, incluidas las inyecciones de cascada trocada
por restrict y de padre nunca contado.

**`Previous` es la fila entera también en un update.** Antes llevaba solo las
columnas cambiadas; la revisión muestra todas y marca las tocadas, y con solo
las tocadas no podía. Las columnas y sus tipos salen de `Detail`, que
`ReviewRow` ya lee para las claves foráneas.

**Un susto que no fue nada, y una regla nueva.** Probando S08 a mano, un clic
por texto sobre la lista de conexiones no cambió la selección —la primera de
la lista era la de producción del usuario— y el «Conectar» siguiente conectó a
producción. Solo se leyó el esquema y se desconectó en el acto; nada se
escribió ni se preparó. La regla que queda: el script de conexión de las
pruebas manuales comprueba, ANTES de tocar «Conectar», que la seleccionada sea
la de prueba —`zz-prueba-*`— y que su entorno sea LOCAL o DEV, y si no, no
conecta. Un «Conectar» a ciegas no se vuelve a ejecutar.

**Del `/code-review high` de S08, cinco cosas, las cinco arregladas:**

1. Las comprobaciones miraban solo la base y no lo que la misma tanda hace
   antes: decían «no existe el padre» cuando el padre era la fila de arriba, y
   «el borrado va a fallar» cuando las hijas se borraban dos filas antes.
   Ahora la revisión recibe los cambios de datos que corren antes en el apply
   —`Ordered` conserva el orden de edición— y los cuenta.
2. En SQLite, `id INTEGER PRIMARY KEY` es un alias del rowid y se asigna
   solo; la introspección no lo marcaba y la revisión decía que la columna
   `NOT NULL` quedó sin valor. Ahora esa columna es identidad «by default».
3. Un apply desde otra pestaña recargaba todas las abiertas y **pisaba las
   ediciones sin preparar** de una tabla que no se tocó. Con ediciones, la
   pestaña no relee: avisa en la tira y ofrece «Releer», que pregunta.
4. El mensaje de «no alcanzó ninguna fila» culpaba siempre a otra sesión; un
   trigger `BEFORE` que devuelve NULL también da cero. Lo dice.
5. El aviso de las claves foráneas apagadas en SQLite se emitía aunque la
   casilla de transacción estuviera apagada, y en ese modo no aplica. Lo dice.

**MySQL mostraba una fecha que no se podía editar.** Salió de probar la grilla
a mano contra MySQL —la UI es la misma para los cuatro, pero lo que llega a la
grilla no—: `parseTime=true` en el DSN hacía que el driver convirtiera DATE y
DATETIME a `time.Time`, y `database/sql` las volvía a escribir en RFC 3339,
`2026-09-10T11:49:41Z`. Ese texto no es lo que dijo el servidor, y MySQL lo
**rechaza** si se le manda de vuelta (22007, «Incorrect datetime value»): editar
cualquier fecha, o cualquier fila cuya clave tuviera una, fallaba al aplicar. El
comentario del DSN decía que sin `parseTime` «la grilla mostraría bytes crudos»;
era falso: sin él la fecha llega como texto, `2026-09-10 11:49:41`, tal cual la
escribe el servidor y tal cual la vuelve a leer. Es la regla de
`query.Result` —el texto lo genera el servidor, Go no interpreta nada en el
medio— que en MySQL no se estaba cumpliendo. Con test en los cuatro servidores
de MySQL y MariaDB: lo leído es el texto del servidor y ese mismo texto vuelve a
entrar como parámetro.

**Del review `high` de la grilla, cinco hallazgos, los cinco arreglados:**

1. **Con la casilla de transacción apagada, la comprobación de filas no podía
   revertir.** Cada sentencia iba sola y en autocommit, así que un `UPDATE` que
   alcanzó dos filas ya estaba commiteado cuando el conteo lo descubría, y
   encima quedaba pendiente para volver a correrse. Ahora cada sentencia sigue
   yendo sola, pero una de DATOS va adentro de su propia transacción: el DML
   de los cuatro motores lo soporta y el conteo revierte de verdad. El test de
   la clave que alcanza dos filas corre con la casilla puesta y sin ella.
2. `StageGrid` **compara la clave que manda la grilla con la clave primaria del
   catálogo** antes de convertir nada. El comentario decía que lo hacía y no lo
   hacía.
3. Con la grilla en modo edición el doble clic ya no abría el visor de celda y
   no había otra forma de abrirlo: ahora es la primera entrada del menú.
4. Abrir y cerrar el editor sobre una celda NULL o «por defecto» sin escribir
   nada la convertía en cadena vacía: un editor sin tocar cancela; escribir y
   borrar todo sí es querer la cadena vacía.
5. Después de «Agregar fila» el foco quedaba en el botón y el Enter agregaba
   otra fila en vez de editar: la grilla se enfoca.

**El ensayo también sirve contra MySQL y MariaDB, mientras no haya esquema.**
Exigía DDL transaccional y por eso se negaba en esos dos siempre. Pero un
changeset de **puras filas** es un solo tramo transaccional en los cuatro
motores, así que ahí no hay nada que el ensayo no pueda revertir. Ahora
`CanDryRun` y `DryRun` miran si hay algún cambio de esquema incluido en vez de
mirar solo el motor, y la pantalla dice «todo o nada» en ese caso en vez de
«MySQL no revierte cambios de esquema» — que era verdad y no venía al caso. Con
un DDL adentro se siguen negando, con test.

**Los escritores de exportación van de a una fila.** `internal/export` expone
`Begin(columnas)`, `Row(valores)`, `End()`, y nada que reciba todas las filas
juntas. El resultado del editor las tiene en memoria y podría pasarlas de una
vez, pero S19 no: lee una tabla del motor a medida que la escribe, y una tabla
de dos millones de filas no pasa por un `[][]string`. Escribir hoy el formato
con la interfaz que mañana necesita el camino difícil es lo que hace que el
resultado del editor sirva de validación del formato, que era el motivo de
hacerlo primero. `End` es obligatorio: cierra el array de JSON, vacía el búfer
y termina el gzip; sin él el archivo queda cortado aunque no haya habido error.

**Cuatro formatos ahora; SQL y DDL, con S19.** CSV, JSON, JSON Lines y
Markdown. El diseño de S19 ofrece «JSON lines» y no un array; se agregan los
dos porque son el mismo objeto por fila con distinto marco y sirven a gente
distinta: el array es lo que se pega en un script o en un ticket y lo que
devuelve cualquier cliente de base; las líneas son lo que leen `jq`, DuckDB y
pandas de a pedazos sin cargar el archivo entero. «SQL inserts» y «Schema only»
del diseño necesitan una tabla y su DDL, no un resultado: van con S19.

**El CSV sigue las convenciones de `COPY … CSV`, y no usa `encoding/csv`.**
La cadena vacía va como `""` y NULL va como `\N` —o como nada, con la opción—,
así que las dos se distinguen en el archivo igual que en la grilla, y S18 las
va a leer de vuelta sin ambigüedad. Un valor que coincide con la marca de NULL
se cita (`"\N"`) para que no se lea como nulo; NULL nunca se cita, por lo
mismo. `encoding/csv` no sabe citar todo ni distinguir NULL de vacío, y las
dos cosas son opciones del diálogo. Además del diseño, una opción más: la
**marca de orden de bytes**, porque Excel en Windows abre un CSV sin marca con
la página de códigos del sistema y «señal» se vuelve «seÃ±al»; en castellano
eso no es un caso borde. Fin de línea `\n`, como `COPY`.

**JSON escribe tipos solo cuando el texto del servidor es inequívoco.** Una
columna numérica cuyo texto cumple la gramática de número de JSON va sin
comillas; `NaN` e `Infinity` no la cumplen y van como cadena. Una booleana va
como `true`/`false` si el texto es `t`/`f`, `true`/`false` o `1`/`0`. Una
columna `json`/`jsonb` va tal cual si `json.Valid` lo acepta. Todo lo demás es
cadena, incluidos los casos raros de los anteriores: nunca se emite algo que
no se pueda volver a leer. El diseño mostraba `"total":"128.40"` entre
comillas; se sigue en cambio lo que hace el propio servidor —`row_to_json` y
`JSON_OBJECT` emiten `numeric` y `DECIMAL` como número— para que la fila
exportada sea la misma que el visor va a mostrar como JSON cuando exista «ver
la fila entera». Los arrays de Postgres van como cadena (`{1,2}`) hasta que el
parser de literales de la unidad del visor exista; ahí pasan a ser arrays.
MySQL no tiene booleanos: `true` es `TINYINT` y sale `1`, que es también lo
que `JSON_OBJECT` hace. En MariaDB `JSON` es un alias de `LONGTEXT`, así que
un JSON guardado ahí sale como cadena, no embebido: es correcto según la regla
y se comprobó a mano. Y en SQLite una expresión sin tipo declarado —`select
1`— es `ClassOther` y sale como cadena; desde una tabla con `INTEGER`
declarado sale como número.

**Markdown: encabezado siempre, `NULL` legible, y lo que rompe la tabla se
escapa.** Una tabla sin encabezado no es una tabla, así que la opción de
omitirlo no aplica. NULL se escribe `NULL` y no `\N`, porque es un formato
para leer; con la opción, nada. La barra vertical va como `\|` y un salto de
línea como `<br>`, porque una fila vive en una línea. Las columnas numéricas
se alinean a la derecha con `--:`, como en la grilla. No hay tope de filas: el
diseño insinuaba cortar en cien, pero un corte silencioso en un export es
peor que un archivo largo, y la persona eligió el alcance.

**El archivo se escribe entero en un temporal y se renombra al final.**
`Exports.Save` crea el temporal en el mismo directorio, escribe, sincroniza y
recién entonces hace el rename; un fallo a mitad de camino —disco lleno,
permiso, un formato que no existe— no deja con el nombre elegido un archivo
cortado que se vería igual que uno entero, y no pisa el que ya había. Con
test que inyecta el fallo y mira que el archivo viejo siga entero. Las filas
del resultado viajan de vuelta por el puente para formatearse en Go: ya están
acotadas por el límite de filas de la conexión, y la vista previa manda solo
las cuatro primeras. Formatearlas en TypeScript habría duplicado los
escritores; escribir el archivo desde el navegador no se puede.

**«Copiar» en la barra de resultados es TSV con NULL vacío.** Es lo que una
planilla pega en celdas; `\N` ahí es ruido. Lo arma Go con el mismo escritor.
El portapapeles de Windows devuelve `\r\n` al leerlo de vuelta: es el sistema,
no el escritor, y Excel lo lee igual.

**El selector nativo de «guardar como» queda para probar a mano.** No se
maneja por CDP, igual que «Abrir archivo SQLite…». Se probó por CDP todo lo
demás en los cuatro motores —vista previa de cada formato, delimitadores,
opciones, copiar y leer el portapapeles, y cancelar el selector sin que
aparezca un error—, y el `Save` tiene sus tests. Intentar completar el
selector mandando teclas al sistema fue un error: las teclas fueron a la
ventana que el usuario estaba usando. Regla, anotada en memoria: **nunca
`SendKeys`, `AppActivate` ni nada que toque el foco del sistema**; lo que pase
por un diálogo nativo lo prueba el usuario.

**Probados a mano por el usuario el 2026-09-10**: el «guardar como» deja
`kn_cli.csv` y, con gzip tildado, `kn_cli.csv.gz`; el selector de carpeta del
alcance «todas las tablas» también.

Salió una cosa de ahí, y es de Wails y no nuestra: cancelar el selector escribe
`ERR Invalid dialog call: Dialog.SaveFile failed: error getting selection:
cancelled by user` en la consola de `wails3 dev`. La pantalla hace lo correcto
—cancelar no muestra ningún error, que es lo que arregló `d3b7b20`— porque
`lib/dialogos.ts` reconoce ese texto del lado del navegador. El `ERR` lo escribe
Wails de su lado ANTES, al clasificar la llamada como `InvalidDialogCallError`
(`pkg/errs/errors.go`), y no hay opción para apagarlo sin parchear Wails. No se
toca: es ruido de la consola de desarrollo, el build que se distribuye en
Windows no tiene consola atada, y no hay nada sensible en esa línea. Queda
anotado para que a nadie le parezca un bug nuestro la próxima vez que lo vea.

**Probado a mano en los cuatro motores el 2026-09-10.** Además de la fecha de
MySQL (arriba), salieron dos cosas: el texto de la transacción asustaba con un
límite que no aplicaba (arriba); y
un editor de celda que se cerraba y se volvía a abrir en el mismo tick
reutilizaba la instancia ya cerrada y lo que se escribía después no se
confirmaba —el editor ahora lleva una `key` que cambia con cada apertura—.

**Recorrer no es paginar: `Conn.Scan` es su propia costura.** La grilla usa
`Page`, que trae una página con `LIMIT/OFFSET`. Exportar no puede usar eso: sin
un orden total el paginado repite y saltea filas, y con él obliga al servidor a
releer y descartar todo lo anterior en cada página. Una sola consulta leída a
medida que llega no tiene ninguno de los dos problemas, así que `Scan` no tiene
`Limit` ni `Offset` —no es un descuido, es lo que lo distingue— y devuelve un
`RowStream` con el uso de `database/sql`: `Next` hasta que da false, después
`Err`, y `Close` siempre.

Dos detalles que se decidieron mirando el código y no suponiendo:

- **El cierre va adentro de `Next`, no solo en `Close`.** `NextRow` devuelve
  false tanto al terminar como al fallar, y cuál de las dos fue lo dice el
  cierre. Sin cerrar ahí, un `for Next() {}` seguido de `Err()` vería `nil`
  aunque el servidor hubiera cortado a la mitad — y un archivo cortado se ve
  igual que uno entero. Lo mismo con `sql.Rows.Scan`, que devuelve su error de
  vuelta y **no** lo deja en `rows.Err()`: hay que guardarlo.
- **`Row` devuelve una rebanada nueva por fila.** Se consideró reusarla, como
  hace `sql.RawBytes`, pero ahorra una asignación de las N+1 que cada fila paga
  igual —los valores hay que copiarlos— y a cambio deja una trampa para quien
  guarde una fila. No vale.

**En Postgres, `Scan` va por el protocolo SIMPLE, igual que `Run`.** Es el
hallazgo más caro de la unidad y casi se escapa: la primera versión usaba
`pool.Query`, que va por el protocolo extendido, donde pgx pide muchos tipos en
formato BINARIO y los decodifica a tipos de Go. Un `timestamptz` habría vuelto
como `time.Time` y habría que volver a darle formato acá — exactamente lo que
el resto del proyecto evita, y la misma fila habría salido distinta en la
grilla y en el archivo. Con el protocolo simple el servidor manda texto y se
copia tal cual. El caso de la batería lo comprueba comparando `Scan` con
`Page` columna por columna, y la inyección —formatear un valor en Go— lo pone
en rojo.

**No se usa `COPY … TO STDOUT`, aunque el plan lo proponía.** COPY entrega
bytes ya formateados en CSV o en texto: serviría para uno de los cuatro
formatos y obligaría a traducir cada opción del diálogo —delimitador, marca de
NULL, citado— a las de COPY, con dos formatos saliendo por caminos distintos y
la diferencia escondida. La ventaja que el plan le atribuía era contra
paginar con `LIMIT/OFFSET`, y eso ya no es lo que hacemos. Si algún día se
mide que el CSV lo justifica, el atajo entra adentro de `postgres.Scan` sin
que nadie más se entere.

**El diálogo no promete un número de filas que la exportación no controla.**
Salió de la prueba a mano: la pantalla de la tabla lee el conteo exacto al
abrirla y no lo vuelve a pedir sola, así que decía «0 filas» de una tabla que
para entonces ya tenía 1500 —la habíamos cargado desde otra pestaña—. Con ese
número, el botón habría dicho «Exportar 0 filas» y habría escrito 1500. Ahora,
con una tabla, el diálogo dice «la tabla entera» y el número exacto lo informa
Go al terminar, contando lo que realmente escribió. Con un resultado del editor
sí se sabe de antemano —son las filas que están en la grilla— y se muestra.

**«Copiar al portapapeles» no está cuando el origen es una tabla.** Una tabla
entera puede ser de gigabytes; ofrecerlo sería prometer algo que no se puede
cumplir.

**Una exportación se registra donde se registran las consultas.** Es una
consulta corriendo, así que `Exports` recibe el `Queries` y usa su mismo mapa
de cancelaciones: «Cancelar» corta una exportación con el mismo `runID` con el
que corta cualquier otra cosa. La pestaña de datos usa dos identificadores
—`:data` y `:export`— para que cancelar la exportación no corte la lectura de
la grilla.

**Un recorrido que falla a la mitad no se puede probar contra un motor real
cuando uno quiere.** Es el caso que decide si un archivo cortado se distingue
de uno entero, así que el bucle que pasa las filas al escritor está separado
(`volcarFlujo`) y se prueba con un `RowStream` falso que entrega dos filas y
después falla: lo escrito queda con el array de JSON **abierto**, no cerrado
como si estuviera entero. La primera inyección que escribí no ponía nada en
rojo —el test cancelaba antes de que el recorrido arrancara— y eso era
justamente un test que no podía fallar.

**La importación no adivina tipos: pregunta.** El paso de «validación» del
diseño lista fila por fila lo que va a fallar, con motivos como «invalid
timestamptz». Eso exigiría un verificador de tipos del lado de Kaname que
inevitablemente va a discrepar con el motor, y justo en los casos raros —el
formato de fecha, la coma decimal, el infinito—. En vez de eso, el ensayo hace
**la importación de verdad adentro de una transacción y la revierte**: la misma
idea que el ensayo de S15, y la respuesta la da el servidor, que es quien sabe.
Lo que sí se comprueba sin la base es lo que no depende de ella: las líneas con
otra cantidad de campos, y si el archivo tiene bytes que no son UTF-8.

Tres decisiones más de esa unidad:

- **No se convierten codificaciones, se avisan.** Convertir necesita saber DE
  QUÉ codificación —latin-1, windows-1252, big5— y adivinarlo mal escribe
  basura en la base sin que nadie se entere hasta mucho después. Hacerlo bien
  necesita `x/text`, que es una dependencia nueva y va en su propio commit.
- **La marca de orden de bytes se saltea.** Excel la escribe al guardar un CSV,
  y sin saltearla la primera columna se llama `<BOM>id` y no coincide con `id`
  de la tabla — el error que se ve es «no existe la columna id» sobre una tabla
  que la tiene. Con test.
- **Mil filas por sentencia, todo en UNA transacción.** Una por fila hace un
  viaje al servidor por fila; todas juntas se pasa del límite de parámetros
  —Postgres admite 65535, así que con veinte columnas el tope real está en 3276
  filas—. El caso que importa es el segundo lote: si el primero entró y el
  segundo falla, no queda nada. Con test, y la inyección —cada lote por su
  cuenta— deja las mil filas huérfanas.
- **«Saltear las que chocan» se escribe distinto en cada motor** —`ON CONFLICT
  DO NOTHING`, `INSERT IGNORE`, `INSERT OR IGNORE`— así que sale del dialecto.
  El default es abortar: es el único que no pierde información en silencio.
  «Actualizar las que ya están» del diseño queda pendiente, porque necesita
  saber por qué clave y armar el SET.

**La pantalla de S18, y tres candados que el contrato no tenía.**

El asistente es de cuatro pasos porque cada uno responde una pregunta que el
siguiente da por contestada, y el tercero es el que justifica que esto sea una
pantalla y no un botón. Al escribirlo aparecieron tres agujeros del lado de Go
—los tres del lado que escribe, así que los tres con test y con la inyección en
rojo—:

- **Importar es escribir, y contra producción escribir exige tipear el nombre
  de la base.** El contrato no lo pedía: cualquiera que llamara al binding
  escribía en producción sin confirmar. Se verifica en Go y no en el asistente,
  igual que en Apply: una comprobación que vive solo del lado de la interfaz no
  es una protección, es un cartel. **El ensayo también lo pide**, y no por
  simetría: inserta las filas de verdad antes de revertirlas, así que toma los
  mismos candados de la tabla y consume los valores de las secuencias, que un
  ROLLBACK no devuelve.
- **Solo lectura tiene TRES razones y solo se miraba una.** Se miraba el
  interruptor de la conexión, así que apuntar a una réplica se estrellaba con
  el error crudo del driver en vez de decir «el servidor es una réplica». Ahora
  usa `soloLectura`, la misma función que Apply.
- **«Se revirtió» era una suposición.** El ensayo revierte en un `defer` que
  ignoraba el error, y aun así el resultado decía que había revertido. Ahora el
  ensayo hace el ROLLBACK explícito, mirando lo que devuelve, y `RolledBack` es
  lo que ese ROLLBACK afirma. Si falla, lo dice: las filas pueden haber quedado.

Dos decisiones de la pantalla que no se deducen del diseño:

- **La ruta del archivo se puede escribir**, y el selector del sistema es la
  comodidad. Es lo mismo que hace el formulario de SQLite de S03, y por el
  mismo motivo: pegar una ruta que ya se tiene es más rápido que buscarla, y
  si el selector falla el asistente sigue sirviendo.
- **Lo que no empareja por nombre queda sin elegir**, en vez de asignarse por
  posición. Un archivo con las columnas en otro orden se importaría cruzado y
  en silencio, que es la peor forma de fallar: la importación diría que salió
  bien.

**El conteo del encabezado se pedía una sola vez.** Encontrado probando el
asistente a mano: después de importar, la grilla mostraba las filas nuevas y el
encabezado seguía diciendo «0 filas». No era de S18 —el conteo exacto se pedía
al montar la pestaña y nunca más, así que también quedaba viejo después de
«Actualizar» o de un apply que tocara la tabla—. Ahora se vuelve a pedir en los
tres momentos en que la tabla cambia de tamaño.

**El review `high` de esta unidad: siete hallazgos, y cuatro no eran de acá.**
Revisar el rango entero y no solo el diff nuevo es lo que volvió a encontrar
cosas de S09 y de S19, ya commiteadas. Los siete arreglados, con test y con la
inyección en rojo:

- **El gzip de «todas las tablas a un archivo» no comprimía.** El archivo salía
  con nombre `.sql.gz` y contenido en claro, así que `gunzip` se negaba a abrir
  una exportación que la app decía haber comprimido. Se apagaba tabla por tabla
  —bien, para no dejar varios miembros gzip pegados— y no se ponía nunca
  alrededor del conjunto, aunque el comentario dijera que lo ponía `guardar`.
  Es el caso de un comentario que describe lo que *debería* pasar y nadie
  comprueba: el test ahora descomprime y busca las dos tablas adentro.
- **Mil filas por lote se pasaba del límite de parámetros en una tabla ancha.**
  «Mil deja margen para tablas anchas» era exactamente al revés: el tope de
  65535 marcadores es POR SENTENCIA, así que a más columnas entran MENOS filas.
  Con 66 columnas mapeadas son 66.000 marcadores y el servidor rechaza el
  primer lote. El lote ahora es `min(1000, 65535/columnas)`, y el aviso de «el
  error puede estar en las N siguientes» usa el tamaño real del lote y no la
  constante.
- **Un CSV de más de 64 KiB se avisaba como que no era UTF-8.** El aviso mira un
  prefijo de 64 KiB y el corte cae en un byte cualquiera: un carácter de varios
  bytes partido ahí marcaba como inválido un archivo perfectamente bueno, y el
  asistente mandaba a rehacerlo. Se descarta la secuencia incompleta del final
  antes de mirar. **En el corte exacto la pregunta no tiene respuesta** —un
  `0xF1` ahí es a la vez el arranque de un carácter que sigue afuera y una eñe
  de latin-1— y se elige no avisar, porque el costo de los dos errores no es el
  mismo: el falso aviso manda a rehacer un archivo que está bien. Escrito en el
  test, para que no parezca un descuido.
- **El visor abierto en «La fila» dejaba la primera pestaña inalcanzable.** El
  modo inicial se reaplicaba en cada render cuando el elegido era 0, así que
  tocar la primera pestaña la ponía en 0 —que era otra vez la señal de «usá el
  inicial»— y volvía sola. Ahora `null` significa «nadie eligió todavía» y el
  inicial siembra una vez.
- **La ruta del archivo se recortaba para mirarlo y no para importarlo.** Una
  ruta pegada con un espacio al final —lo que pasa al copiarla de una terminal—
  encontraba las columnas en el paso 1 y fallaba después con «no se pudo
  abrir». Se recorta donde se usa y NO al tipear: recortar en cada tecla no
  dejaría escribir `C:\Program Files\…`.
- **El conteo filtrado quedaba viejo.** El arreglo del conteo del encabezado
  refrescaba solo el total, y con un filtro puesto el encabezado muestra el
  filtrado: importar filas que pasan el filtro dejaba «de N filas» con el
  número de antes.
- **Dos tablas distintas podían dar el mismo archivo.** `pedidos/2026` y
  `pedidos-2026` se limpian igual, así que la segunda pisaba a la primera y las
  dos se informaban como escritas: un archivo menos del que la app decía haber
  dejado, sin error y sin aviso. Los nombres se resuelven antes de escribir
  nada y la segunda lleva sufijo. Volver a exportar a la misma carpeta SÍ
  reemplaza lo que había, que es lo que se espera de «guardar acá otra vez».

**El visor: tres cosas que S09 debía desde la Iteración 2.**

- **El literal de un array se parte en Go, no en el frontend.** `{a,"b,c",NULL}`
  tiene comas adentro de comillas, escapes con barra invertida, y un `NULL` que
  no es lo mismo que la palabra `"NULL"`. Eso es lógica con casos borde, y
  lógica con casos borde sin tests es lógica rota que nadie ve. `ParsePostgresArray`
  devuelve valores **y** un vector de nulos, porque devolver solo cadenas
  perdería justamente esa diferencia. Un array de dos dimensiones se muestra por
  filas sin abrirlas: aplanarlo mentiría sobre la forma. MySQL tiene su propio
  caso —una columna SET es una lista separada por comas y nada más— y SQLite no
  tiene arrays, así que el visor lo muestra como texto.
- **La fila entera como JSON se arma acá y no con `row_to_json`**, que era lo
  que decía el plan. Tres motivos que aparecieron al escribirlo: la fila YA está
  leída, así que pedirla de nuevo es un viaje por nada; `row_to_json` es de
  Postgres y habría que escribir la consulta equivalente en los otros tres; y
  sobre todo el resultado tiene que coincidir con lo que sale al exportar en
  JSON — usando el MISMO escritor, coincide por construcción y no por cuidado.
  Sale como UN objeto y no como un array de uno, y en una sola línea: indentar
  exigiría volver a parsear los números, y ahí un `numeric` de 12.50 se
  convertiría en 12.5. El valor exacto vale más que la sangría.
- **Los botones del pie editan.** Es lo que el visor viene a resolver: un JSON
  o un texto de dos mil caracteres no se edita en una celda de una línea. El
  editor reemplaza al modo que muestra el valor tal cual y no a los demás
  —editar un array por su lista de elementos, o la fila entera, es otra cosa—,
  y lo que se escribe va al mismo estado de edición que la grilla, así que
  «Preparar» lo trata igual que un doble clic. Con el resultado de una consulta
  el visor sigue siendo de solo lectura, y lo dice: puede venir de varias
  tablas. El componente `Textarea` se agregó a `components/ui`, no inline.

**El formato SQL es el único lugar donde un valor de fila se escribe adentro de
la SQL.** Y es legítimo, porque esa SQL **no la ejecuta Kaname**: es un archivo
que alguien va a leer y, si quiere, correr en otro lado. Todo lo que Kaname
ejecuta sigue yendo con parámetros. Para que el archivo sirva hacían falta tres
cosas que no son obvias:

- **Citar como cita el motor.** Sale de la costura: `Conn.Quoting()` devuelve
  las tres funciones —tabla, identificador, literal— y `engine` no se entera de
  qué formatos hay. En MySQL el citado de literales depende del SERVIDOR
  —`NO_BACKSLASH_ESCAPES`—, así que se pide a la conexión y no a una función
  suelta.
- **Lotes de 500 filas por INSERT.** Ni una por fila —un millón de sentencias
  tarda una eternidad en volver a entrar— ni todas juntas, que se pasa del
  tamaño máximo de paquete y además deja un archivo de una sola línea de 200 MB
  que no se puede ni mirar. Es lo que usan los volcados de MySQL. El último lote
  se cierra con punto y coma aunque no esté lleno: sin eso el archivo no se
  puede correr, y se ve igual que uno completo.
- **Una tabla vacía no escribe nada.** Un `INSERT … VALUES` sin filas no es SQL
  válida.

**El resultado del editor no puede exportarse como SQL.** Una consulta puede ser
un join de tres tablas o un `select 1`: no hay a cuál insertar. El formato
declara `NeedsTable` y la interfaz no lo ofrece ahí — y el servicio lo rechaza
igual, porque lo que la interfaz no ofrece hoy lo puede ofrecer mañana por
error.

**Con varias tablas, si una falla se dice cuáles quedaron.** No se borran las
anteriores: son archivos enteros y correctos, y quien exportó puede querer
quedárselos. Con el formato que junta todo es al revés —el script se arma en un
temporal y solo toma el nombre elegido si terminó—, porque un volcado a medias
que parece completo es peor que ninguno.

**Los filtros: catorce operadores, y ninguno que no sepan los cuatro motores.**
`ILIKE` es de Postgres y las expresiones regulares las escribe cada uno a su
manera: ofrecer algo que falle en tres de cuatro es peor que no ofrecerlo. Lo
que hay son las comparaciones (`=`, `<>`, `<`, `<=`, `>`, `>=`), tres de texto
—contiene, empieza con, termina con—, nulo y no nulo, en la lista y no en la
lista, y entre. Se combinan **solo con Y**: mezclar Y con O necesita paréntesis,
y unos paréntesis que no se ven en la pantalla son una consulta que quien la
escribió no puede leer. Para eso está el editor SQL, al lado.

Tres decisiones que no se deducen del código:

- **Un comodín tecleado a mano no es un comodín.** Los tres operadores de texto
  se escriben con `LIKE`, y el patrón lo arma el renderizador escapando lo que
  la persona escribió: buscar «50%» busca «50%», no «todo lo que empieza con
  50». El carácter de escape es `!` y no `\`, que es lo que uno elegiría
  primero: la barra invertida es el escape por defecto de `LIKE` en Postgres y
  en MySQL, pero además es un escape de CADENA en MySQL salvo con
  `NO_BACKSLASH_ESCAPES`, así que `ESCAPE '\'` dependería de una variable del
  servidor. `!` no significa nada en ninguno de los cuatro.
- **«No es igual a» incluye las filas sin valor.** `<> 'ana'` deja afuera los
  NULL, que es lo que dice el estándar y lo que casi nadie espera: «no es igual
  a ana» sin la fila que no tiene nombre parece que faltan filas. Se emite
  `(col <> $1 OR col IS NULL)`.
- **El conteo de la tabla y el de lo filtrado son dos números distintos.** Se
  vio en la prueba: al usar uno solo, el encabezado pasó a decir «1 filas» de
  una tabla de 8 porque había un filtro puesto. El encabezado dice qué tan
  grande es la tabla y no puede encogerse porque alguien filtró; el contador de
  la barra dice cuántas pasan, y lo aclara con «filtradas».

**Y dos cosas que salieron de mirar la pantalla, no el código.** El constructor
no entra en la barra de arriba: las pestañas de estructura ya se comen el ancho
y quedaba en 53 píxeles, así que tiene su propia fila sobre la grilla, que es
donde el diseño de S07 lo pone. Y el `Combobox` necesitó dos cosas nuevas, que
se agregaron al componente en vez de resolverse en la pantalla: `label`, para
mostrar «es igual a» cuando el valor guardado es `eq` —un desplegable que diga
`eq` no le sirve a nadie—, y `estricto`, para las listas CERRADAS. Sin lo
segundo, teclear «contiene» en vez de elegirlo mandaba `contiene` al motor y
volvía con «operador de filtro desconocido». Fallaba bien, y esa parte está
bien; ofrecer un campo libre donde no lo hay es lo que estaba mal.

**Del `/code-review high` de la tabla en streaming, siete hallazgos, los siete
arreglados. Cuatro son de unidades anteriores.** Tres merecen quedar escritos
porque no son descuidos sino razonamientos que estaban mal:

1. **La vista previa y el guardado compartían el `runID`.** El registro de
   cancelaciones es un mapa por identificador: tocar una casilla durante un
   guardado largo registraba la vista previa encima y, al terminar ella,
   borraba la entrada — dejando la exportación corriendo sin nadie que pudiera
   cortarla. Y «Cancelar» ni siquiera cancelaba: cerraba el diálogo mientras el
   archivo se seguía escribiendo. Ahora la vista usa `<runID>:vista`, el botón
   corta de verdad mientras se guarda, y lo que quede a medio escribir se
   descarta solo porque el archivo definitivo aparece recién al renombrar.
   Además, mientras se escribe no se puede cambiar formato ni opciones: el
   pedido ya salió con los de antes.
2. **`postgres.Scan` pedía una SEGUNDA conexión al pool.** Se queda con una
   durante todo el volcado y encima llamaba a `resolverTiposDesconocidos` con
   el pool. Con el pool de dos de una conexión de solo lectura, dos
   exportaciones a la vez se esperaban una a la otra. El primer arreglo fue
   peor: resolver los tipos sobre la conexión ya tomada corrompe el protocolo,
   porque en ese momento está en medio de un result set. Lo correcto es
   resolverlos ANTES de abrir el recorrido, con la misma consulta y `limit 0`,
   sobre la misma conexión. Hay test con un pool de UNA conexión y un enum: la
   versión vieja se cuelga hasta que el contexto la corta.
3. **El vacío no puede ser un comodín de esquema.** `mismoEsquema` decía
   `a == "" || b == ""`, que resolvía bien el caso para el que se escribió
   —SQLite manda "" y el catálogo dice "main"— y de paso hacía coincidir
   cualquier cosa. MySQL admite claves foráneas ENTRE BASES: un alta pendiente
   sobre `clientes` de la base abierta contaba como el padre de una clave que
   apunta a `otra.clientes`, y la revisión mostraba un tilde verde sobre un
   padre que no iba a existir. Ahora el vacío se RESUELVE al nombre real —la
   base actual en MySQL, `main` en SQLite— y se compara exacto. Un «bad» de más
   molesta; un «ok» de más miente justo en la pantalla que existe para no
   mentir.

Los otros cuatro, más cortos:

4. **«Cargar 500 más» corría la selección de una fila nueva a una fila real.**
   La selección es un índice absoluto y las filas nuevas viven después de las
   cargadas: al agregar una página, ese índice pasaba a apuntar a una fila de
   la base, y «Borrar fila» preparaba un DELETE contra una fila que nadie
   eligió. Se corren la selección y la celda en edición al agregar.
5. **La revisión cacheaba los chequeos y no los tiraba nunca.** Cada uno
   depende de lo que la tanda hace antes, así que descartar el alta de un padre
   dejaba al hijo con su tilde verde hasta que el apply fallaba por esa clave.
   El caché se vacía cuando cambia la tanda, contando también los destildados.
6. **`descartar()` sin `try/catch`**: un Unstage que fallaba no decía nada.
7. **El CSV no neutralizaba fórmulas de planilla.** Un valor que empieza con
   `=`, `+`, `-` o `@` lo EJECUTA Excel al abrir el archivo, y citar no lo
   evita: la planilla mira el contenido del campo. Los valores de una celda son
   dato no confiable —lo dice CLAUDE.md— y este es el único camino que se los
   entrega a una planilla. Se agregó la opción, **apagada por defecto**: el
   apóstrofo que lo neutraliza CAMBIA el valor, y un archivo que se va a volver
   a importar tiene que decir lo que decía. Se enciende cuando el destino es
   una planilla, que es cuando el riesgo existe. Solo CSV: a JSON y Markdown no
   los ejecuta ninguna planilla.

**Del `/code-review high` de la exportación, seis hallazgos, los seis
arreglados. Ninguno alto ni medio**, y dos no eran de esta unidad sino de las
anteriores, que es exactamente para lo que sirve revisar el rango entero:

1. **El diagrama pintaba como alterada una tabla a la que solo se le editó una
   fila.** `erdStaged` decidía por descarte —«lo que no reconozco toca una
   columna»— y los tres tipos de datos caían en ese `default`. El ERD dibuja
   la forma de las tablas; una fila editada no la cambia, y marcarla decía que
   había un ALTER esperando donde no lo había. Ahora `pendientesDe` saltea los
   cambios de datos. La causa de fondo era que «qué tipos son de datos» estaba
   escrito **tres veces** —en el diagrama por descarte, y como lista en la
   revisión de filas y en Cambios pendientes—: se mudó a `lib/cambios.ts`, así
   el cuarto lugar que lo necesite no vuelve a inventarlo.
2. **La fila nueva se creaba fuera de la vista.** Con una página de 500 filas,
   «Agregar fila» la agregaba al final, enfocaba la grilla con `preventScroll`
   y no la mostraba: se veía solo el chip de abajo, y el Enter abría el editor
   sobre algo que no estaba en pantalla. Se va al fondo del scroller —las
   filas nuevas son siempre las últimas— en un efecto, que corre después del
   commit, cuando el alto virtual ya cuenta la fila.
3. **«Copiar» fallaba en silencio.** Sin `try/catch`, un rechazo de
   `Render` o del portapapeles quedaba sin manejar y el botón no hacía nada
   visible, que es la peor forma de fallar de un botón. Ahora dice «No se pudo
   copiar».
4. **La vista previa se quedaba con el spinner para siempre** si el formateo
   fallaba: el error iba al pie y el panel seguía «cargando». La vista previa
   tiene ahora su propio estado —pidiendo, lista, falló—, separado del de
   guardar, porque un fallo al formatear y uno al escribir el archivo no son
   lo mismo.
5. **Apretar «Elegir…» apenas se abre el diálogo escribía el archivo sin
   extensión.** La extensión la dice Go y tarda un viaje por el puente; hasta
   que llega, «Elegir…» y «Exportar» están deshabilitados en vez de proponer
   `consulta` a secas con un filtro `*`.
6. **La revisión de una fila excluida contaba mal lo que la tanda hace
   antes.** `ReviewRow` recorría `Ordered()`, que omite los excluidos, para
   juntar los cambios anteriores y cortaba al llegar al que se revisa: si el
   revisado estaba **excluido**, el corte no llegaba nunca y «antes» terminaba
   siendo la tanda entera, incluido lo que correría después. Se veía como un
   «lo pone esta misma tanda» sobre un padre insertado más tarde. Ahora
   recorre `List()` —que tiene a los excluidos y, como los cambios de datos
   van todos en la misma fase, conserva su orden de ejecución— y saltea los
   excluidos al juntar. Con test en los cuatro motores, y la inyección del
   `Ordered()` viejo lo pone en rojo.

**El túnel se cuenta antes que los demás impedimentos de `pg_dump`, y lo
encontró CI.** Los tres motivos por los que Kaname no corre la herramienta —no
está en el PATH, su versión no alcanza para el servidor, la conexión pasa por un
bastión— salían cada uno por su propio `return`, así que ganaba **el primero que
se comprobaba**, no el que importa. En mi máquina `pg_dump` y el servidor son de
la misma versión y el túnel quedaba como único impedimento; en CI hay un
`pg_dump` 16 contra servidores 17 y 18, ganaba la versión, y el bastión no se
mencionaba nunca. Ahora se averiguan todos los hechos primero —así el panel dice
la versión del servidor aunque la herramienta no esté— y un solo lugar decide
cuál se cuenta.

El orden no es arbitrario: **el túnel es el único impedimento que, ignorado, no
falla.** `pg_dump` correría contra lo que responda en ese host y puerto sin el
túnel —otra base— y escribiría un archivo de aspecto impecable. Los otros dos
fallan de frente. Un impedimento silencioso se cuenta antes que uno ruidoso.

La lección de testing es la que vale más que el arreglo: **el test dependía de
qué `pg_dump` estuviera instalado en la máquina que lo corre.** Verde acá, rojo
allá, y ninguna de las dos cosas decía nada del código. La decisión —el orden—
se separó en una función pura sobre los hechos ya averiguados, y se prueba con
los impedimentos armados a mano, incluida la combinación exacta de CI. La
inyección que la pone en rojo es mover el caso del túnel al final del `switch`,
y el mensaje que sale es palabra por palabra el que falló en CI.

**Y el review encontró que mi primer test del arreglo tampoco podía fallar.**
Había escrito una comprobación de que «un servidor cuya versión no se pudo leer
no habilita la comparación», creyendo que protegía el `si se sabe la versión`
que llevaba adelante el caso. No protegía nada: una versión que no se pudo leer
queda en `Mayor: 0` y `AlcanzaPara` compara con `>=`, así que contra el cero
cualquier herramienta alcanza y el caso no se dispara con guardia o sin ella.
Sacar el guardia dejaba el test igual de verde. La salida no fue reforzar el
test sino **borrar el guardia**, que era una segunda forma de decir lo mismo, y
mover la invariante a donde de verdad vive: `AlcanzaPara`. Ahí sí es
falsificable —cambiar el `>=` por `==` hace que un servidor desconocido acuse a
una herramienta impecable, con el mensaje absurdo «el servidor es 0»— y ese es
el test que quedó.

**El impedimento se cuenta de a uno; el consejo no.** Con bastión Y sin
`pg_dump` instalado, decir solo «abrí el reenvío con `ssh -L`» manda a alguien a
armar un túnel para descubrir recién ahí que no tiene la herramienta. El motivo
sigue siendo uno solo —el de fondo— pero el consejo agrega la otra mitad cuando
hace falta.

### Iteración 6 — 2026-09-09

**Reconstruir una tabla en SQLite borra las filas de las tablas hijas, en
silencio.** Es el hallazgo más caro de la iteración y el único con pérdida de
datos.

SQLite tiene cuatro `ALTER TABLE`: `RENAME TO`, `RENAME COLUMN`, `ADD COLUMN` y
`DROP COLUMN`. Todo lo demás —cambiar un tipo, exigir que una columna no sea
nula, sacar un valor por defecto, agregar un CHECK o una clave— se hace
reconstruyendo: tabla nueva, copiar, tirar la vieja, renombrar. Y ahí se juntan
tres hechos que por separado parecen inofensivos:

1. Con `foreign_keys` encendido, un `DROP TABLE` hace un DELETE implícito, así
   que dispara los `ON DELETE CASCADE` de las tablas que la referencian.
2. `PRAGMA foreign_keys` es un **no-op adentro de una transacción**. No falla,
   no avisa, no hace nada.
3. `PRAGMA foreign_key_check` después del rebuild devuelve **cero filas**,
   porque las hijas no quedaron huérfanas: quedaron borradas.

Comprobado: padre con dos filas, hija con dos filas y `ON DELETE CASCADE`,
reconstruir el padre → la hija queda en **0 filas, sin un solo error**.

La solución es el procedimiento que documenta el propio manual de SQLite, y lo
que obliga es a que las opciones de la transacción digan qué va adentro:
`engine.TxOptions{RebuildsTables: true}` apaga las claves ANTES del `BEGIN`,
corre `foreign_key_check` antes del `COMMIT` —y se niega a commitear si
encuentra algo— y las vuelve a encender al terminar. Es la misma semántica que
un `DEFERRABLE INITIALLY DEFERRED` de Postgres.

**`PRAGMA legacy_alter_table` no sirve para esto, aunque lo parezca.** La idea
natural para evitar el `DROP` peligroso es renombrar la tabla vieja para
liberar el nombre, y ese pragma promete que un `RENAME` no reescriba las
referencias. No lo cumple: con el pragma leyendo `1`, el `RENAME` reescribió
igual el `REFERENCES` de la tabla hija, y el `DROP` posterior se llevó sus
filas. Por eso el orden del rebuild es **crear la nueva con un nombre temporal,
copiar, tirar la vieja, renombrar la nueva**: así el nombre original nunca
cambia de dueño y ninguna otra tabla tiene que seguirlo.

**El rebuild se arma leyendo el texto del `CREATE TABLE`, no el catálogo.**
SQLite no expone los CHECK en ningún pragma ni en ninguna tabla del catálogo:
solo existen en `sqlite_schema.sql`. Armar la definición nueva desde los
pragmas —que es lo natural, y lo que hacen varias herramientas— perdería en
silencio los CHECK, los COLLATE, las cláusulas `ON CONFLICT` y el
`WITHOUT ROWID`. Es la misma clase de error que ya se decidió no cometer con
Atlas y las columnas VIRTUAL, así que hay un tokenizador propio —`ddltext.go`,
que respeta comillas, paréntesis y comentarios— y lo que el cambio no toca
vuelve byte por byte como estaba. Lo que no se entiende **no se reconstruye**:
una columna generada devuelve `ErrUnsupported` en vez de una definición
aproximada, y una tabla **virtual** también: FTS5 y R-Tree figuran como
`type='table'` igual que cualquier otra, y reconstruirlas con el procedimiento
normal las dejaría convertidas en tablas comunes —mismos nombres de columna,
sin el módulo— sin dar un solo error.

**SQLite no tiene un código de error por problema.** `SQLITE_ERROR` es 1 y ahí
caen, con el mismo número, la tabla que no existe, la columna repetida, el
índice que ya está y el error de sintaxis — comprobado contra 3.53.4, los
cuatro devuelven 1. Para ese caso, y solo para ese, la clasificación lee el
mensaje. Es justo lo que en Postgres y MySQL se evita a propósito; se hace
igual porque la alternativa es que la pantalla de apply diga «el motor rechazó
la sentencia» y nada más contra el único motor que no necesita servidor, que va
a ser el más usado para probar. Las restricciones son la mitad buena: ahí
`SQLITE_CONSTRAINT` sí tiene códigos extendidos y son precisos —275 CHECK, 787
FOREIGN KEY, 1299 NOT NULL, 1555 PRIMARY KEY, 2067 UNIQUE—.

**Los pragmas de sesión van en el DSN, no por `Exec`.** `database/sql` tiene un
pool: un `PRAGMA foreign_keys=ON` ejecutado con `db.Exec` se aplica a UNA
conexión y la consulta siguiente puede salir por otra. Comprobado: con el
pragma puesto así, seis inserts de filas huérfanas pasaron sin error. En el DSN
—`?_pragma=foreign_keys(1)`— el driver lo aplica a cada conexión que abre.

**SQLite acepta cualquier nombre de tipo.** `CREATE TABLE t (a noexistetipo)`
no da error: el tipo declarado define una AFINIDAD, no una restricción. Así que
la validación del tipo en el DDL es más importante acá que en los otros
motores, no menos: no hay ninguna comprobación del motor detrás de la nuestra.
Y por lo mismo la grilla lee todo como texto —una columna `integer` puede tener
un texto en una fila y un blob en la otra—.

**Lo que SQLite no tiene, y se dice en vez de fingirse:** comentarios de tabla y
de columna (devuelven `ErrUnsupported`), `ALTER TABLE ADD CONSTRAINT` —una
UNIQUE se agrega como índice único, que es el mismo mecanismo que usa el
motor—, `DROP TABLE ... CASCADE`, y estadísticas de filas salvo que alguien
haya corrido `ANALYZE`.

**El apply se parte en tramos, y el resultado lo dice.** `ApplyResult` suma
`Tramos` y `TramoFallido`, y `StatementResult.Applied` cambió de significado:
antes era «corrió sin error» y ahora es «quedó aplicada en la base». La
diferencia no es semántica —`olvidarAplicados` saca del changeset lo que figure
aplicado, así que con la definición vieja una sentencia que corrió y después se
revirtió se perdía como edición sin existir en la base—.

El mismo changeset da dos resultados distintos y los dos correctos: contra
Postgres y SQLite un tramo, se revierte todo y el changeset queda entero;
contra MySQL y MariaDB tantos tramos como sentencias de esquema, lo anterior al
fallo queda aplicado de verdad, y `RolledBack` es false porque no se revirtió
nada. Es lo que S15 tiene que mostrar en vez de la promesa fija de «todo o
nada».

**MariaDB 10.11 no conectaba, y es la LTS más vieja que declaramos soportar.**
`leerServerInfo` pedía `@@transaction_read_only` en la misma consulta que todo
lo demás, y esa variable llegó a MariaDB recién en 11.1.1 — antes se llamaba
`@@tx_read_only`, y MySQL 8.0 hizo el camino inverso: agregó la nueva y eliminó
la vieja. O sea que **no hay un solo nombre que sirva en los dos**. La conexión
entera fallaba con «Unknown system variable», que se mostraba como «el servidor
rechazó la conexión» y mandaba a revisar la contraseña.

Ahora se lee aparte, después de saber qué motor hay del otro lado, y no poder
leerlo no rompe la conexión: es un dato de la barra de estado.

Lo que dejó pasar el bug no fue el código sino el banco de pruebas: el
`docker-compose.test.yml` tenía una sola MariaDB. Ahora tiene **dos, a la vez**
—12.3 en el 53307 y 10.11 en el 53308— y las dos corren la batería entera.
Declarar una versión soportada y no correr un solo test contra ella es prometer
sin comprobar. Con la corrección, la 10.11 pasa los diecinueve casos.

**Lo que encontró el `/code-review` en `high`, y que los tests no.** Doce
hallazgos legítimos, todos reproducidos antes de tocar nada. Vale anotarlos
porque tienen una forma en común: ninguno se ve leyendo el código de a una
función.

- **Una vista encima de la tabla hacía imposible cualquier reconstrucción.**
  Desde 3.25 un `RENAME TO` vuelve a analizar el esquema entero, y en el medio
  del rebuild la tabla original ya no existe: `error in view vt: no such
  table`. Se arregla con `PRAGMA legacy_alter_table` — que resultó servir para
  esto y no para lo que se había supuesto antes, que era evitar que el rename
  reescribiera las referencias. Eso lo gobierna `foreign_keys`, no este pragma.
- **`ON DELETE SET NULL` y `ON DELETE SET DEFAULT` se partían en dos.** NULL y
  DEFAULT abren cláusula en cualquier otro lugar de la definición de una
  columna, y ahí no. Sacarle el valor por defecto a una columna con
  `ON DELETE SET DEFAULT` devolvía «sí, lo saqué» y dejaba `ON DELETE SET`: SQL
  rota y la acción de la clave destruida.
- **`Run` ejecutaba las sentencias dos veces.** El reintento con `Exec` cuando
  el `Query` fallaba volvía a correr lo que ya había corrido, porque el driver
  de SQLite ejecuta toda la cadena en una llamada. `INSERT …; SELECT roto;`
  dejaba dos filas. El reintento además no hacía falta: `QueryContext` acepta
  DML y devuelve cero columnas, que es cómo se distingue.
- **La validación de identificadores no validaba nada.** El mapa guardaba
  descripción→nombre y el bucle lo leía al revés, así que medía el largo de la
  descripción: un nombre de tabla de 300 caracteres pasaba, y uno con un salto
  de línea adentro también. Estaba en MySQL y se había copiado a SQLite.
- **El modo solo lectura protegía UNA conexión del pool.** `SET SESSION` vale
  para la conexión que lo recibió; abrir una tabla y después correr un DELETE
  en el editor sale por otra. Ahora la configuración de sesión va en un
  conector propio, así que cada conexión que el pool abre la recibe.
- **El límite de tiempo por sentencia estaba comentado y no implementado.** El
  campo se asignaba y no se leía en ninguna parte. Se implementa en el mismo
  conector, mandando las dos formas que existen —`max_execution_time` en
  milisegundos para MySQL, `max_statement_time` en segundos para MariaDB— y
  exigiendo que una haya funcionado.
- **El `DESC` del paginado se aplicaba solo a la última columna.** En los
  TRES motores. `ORDER BY a, b DESC` ordena por `a` ascendente; con una clave
  primaria compuesta el orden deja de ser total y el paginado repite y saltea
  filas.
- **La reconstrucción hacía retroceder el contador de `AUTOINCREMENT`.** El
  `DROP TABLE` se lleva la fila de `sqlite_sequence` y la copia la recrea con
  el máximo de las filas que quedaron: si se habían borrado las últimas, la
  próxima fila recibe un id que ya existió, que es exactamente lo único que
  `AUTOINCREMENT` promete que no pasa.
- **La ruta del archivo de SQLite iba sin escapar en un URI `file:`.** Una base
  en `…/notas#1/app.db` se cortaba en el `#` y —como se abre con
  `SQLITE_OPEN_CREATE`— SQLite **creaba un archivo vacío** llamado `notas` y lo
  abría. Sin error: Kaname mostraba una base vacía mientras la del usuario
  seguía intacta al lado.
- **Comentar una columna en MySQL le borraba el valor por defecto.** `MODIFY`
  reemplaza la definición entera y lo que no se repite se pierde.
- **El servicio despachaba todo a Postgres.** El editor ofrece los otros tres
  deshabilitados, pero el archivo de conexiones se edita a mano: una conexión
  con `engine = "mysql"` le entregaba a pgx un DSN que no es suyo y el error
  hablaba de credenciales. Una comprobación que vive solo del lado de la
  interfaz no es una protección.
- **`QuoteString` corrompía las barras invertidas bajo `NO_BACKSLASH_ESCAPES`.**
  El comentario decía que duplicarlas «es correcto en los dos modos»; no lo es,
  y un comentario que dice `C:\ruta` se guardaba con dos barras. No es una
  inyección —la comilla simple, que es lo único que puede cerrar el literal, se
  duplica igual en los dos modos— pero sí corrupción silenciosa. Se lee
  `@@sql_mode` al conectar.
- **El error 1792 no estaba clasificado**, y es justamente el que da una
  escritura en una conexión que Kaname abrió en modo solo lectura: el más
  probable de toda la lista en una conexión de producción. Salía como «el motor
  rechazó la sentencia».
- **`Redact` solo tapaba contraseñas en URIs `esquema://`**, que es la forma de
  Postgres. El DSN de MySQL no es un URI —`usuario:clave@tcp(host)/base`— así
  que la contraseña pasaba entera. No se encontró un camino por el que llegara
  a un log hoy, pero CLAUDE.md lo pone como requisito duro y la función
  prometía más de lo que hacía.
- **Y una comprobación de la batería que no podía fallar.** `r.Rows[0][0] ==
  r2.Rows[0][0]` compara `*string`: dos punteros de dos lecturas distintas
  nunca son iguales. Un `Page` que ignorara el `Offset` por completo pasaba.

Se agregó además `ON DELETE CASCADE` a la clave del fixture de la batería. Sin
él, inyectar la falla del rebuild daba un error de clave foránea —ruidoso— en
vez del borrado silencioso que el caso dice comprobar: pasaba por haber fallado
fuerte, no por estar protegido.

**La batería creció porque encontró un agujero en sí misma.** El ciclo de
`RenderDDL → ejecutar → releer el catálogo` cubría agregar, exigir no nulo,
renombrar y borrar una columna, pero no el valor por defecto. Ahí había un bug
que llevaba en el repositorio desde que se escribió el paquete de MySQL:
`SetDefault` leía `c.Expression` mientras Postgres —y ahora SQLite— leen
`c.Column.Default`, que es lo que exige `Validate`. Un cambio perfectamente
válido renderizaba `ALTER TABLE … SET DEFAULT ` y fallaba recién al aplicar,
con un error de sintaxis. Se agregaron los dos casos y se comprobó que fallan
con el bug puesto.

Y se agregó un caso más, «las filas sobreviven al cambio de esquema», que
modifica la tabla PADRE y cuenta las filas de la HIJA. El ciclo tocaba la
hija, que no es referenciada por nadie, así que no habría notado nunca el
borrado en cascada de SQLite.

**MySQL y MariaDB no solo no revierten el DDL: lo que hacen es peor.** Un DDL
en el medio de una transacción hace **commit implícito de todo lo anterior** y
deja la conexión fuera de la transacción, así que lo que venga después también
se commitea solo y el ROLLBACK final no revierte nada.

Comprobado contra 9.7.2 y 12.3.3, con esta secuencia:

```
BEGIN;
UPDATE t SET n='PRIMERO' WHERE id=1;   -- dato
ALTER TABLE t ADD COLUMN extra int;    -- commit implícito de lo de arriba
UPDATE t SET n='SEGUNDO' WHERE id=2;   -- ya fuera de la transacción
ROLLBACK;                              -- no revierte NADA
```

Los dos updates quedan aplicados. Es peor que «no hay DDL transaccional»,
porque la interfaz habría prometido «todo o nada» y la base habría aplicado
todo. Y es exactamente el escenario de la Iteración 7, donde el changeset
mezcla datos y esquema en un solo apply.

**Por eso el apply se parte en tramos y no manda todo en un BEGIN.** Con DDL
transaccional —Postgres, SQLite— el tramo es uno solo y la casilla «Una sola
transacción» cumple lo que promete. Sin él, cada corrida de sentencias de datos
consecutivas es un tramo transaccional de verdad y cada DDL queda solo. Un DDL
solo igual es todo o nada por AtomicDDL, así que lo único que se pierde es la
garantía de agrupar — y esa se perdía igual, con la diferencia de que ahora se
dice en vez de fingirse.

**La buena noticia para la Iteración 7:** el DML de InnoDB es transaccional de
verdad. Un changeset de puros cambios de datos —que es exactamente lo que
produce la grilla editable— es **un solo tramo transaccional en los cuatro
motores**. Editar celdas tiene la misma garantía contra MySQL que contra
Postgres. La limitación es solo del esquema.

---

**Las dos LTS de cada motor, no la última de cada uno.** El compose levanta
seis contenedores: PostgreSQL, MySQL 9.7 y 8.4, MariaDB 12.3 y 10.11, y el
servidor SSH.

El encabezado del compose ya decía «se prueba contra LTS: en septiembre de 2026
eso es 9.7 y 8.4», y de MySQL solo estaba la 9.7. Un comentario que declara una
cobertura que el archivo no tiene es peor que no tener el comentario: quien lo
lee deja de mirar.

Que esto importa está comprobado en el mismo proyecto y no en abstracto. La
10.11 de MariaDB **no conectaba en absoluto** —Kaname pedía
`@@transaction_read_only`, que llegó recién en 11.1.1— y se descubrió el día que
se levantó un contenedor con esa versión. Antes de eso, «MariaDB soportada
desde la 10.11» era una línea del README.

Las 8.4 y 10.11 son además las que están instaladas en **más** lugares que las
últimas: son las versiones de las bases de las que la gente se preocupa.

Los dos tests que dependen de la versión —modo solo lectura y límite de tiempo
por sentencia— corren contra los cuatro servidores y no contra dos. Son
justamente los que tocan variables de sesión, que es donde MySQL y MariaDB se
separan: `max_execution_time` en milisegundos contra `max_statement_time` en
segundos, y cada motor rechaza la del otro.

De paso, los subtests dejaron de llamarse con el DSN. El nombre de un subtest se
imprime, y un DSN lleva credenciales adentro; acá son de juguete, pero el hábito
de mandar un DSN a la salida es el que después manda uno de verdad.

---

**El ensayo de S15, y por qué abrir una transacción y revertirla no alcanza.**

El ensayo corre el changeset entero adentro de una transacción y la revierte.
Responde la pregunta que la vista previa **no puede** responder: la vista previa
dice qué SQL se va a mandar, y esa SQL puede ser impecable y fallar igual.
`SET NOT NULL` sobre una columna que tiene nulos es correcta como texto y la
rechaza el motor, porque el que decide no es la sintaxis sino los datos.

Lo que no era obvio: **un ensayo escrito de la forma directa mentiría**. Hay
errores que el motor no levanta en la sentencia sino recién al cerrar —las
claves `DEFERRABLE INITIALLY DEFERRED` de Postgres, y en SQLite el
`foreign_key_check` que cierra una reconstrucción de tabla—, así que una
transacción que se abre, corre todo y se revierte **nunca los ve**. El ensayo
diría «va a andar» y el apply fallaría en el COMMIT.

Por eso `engine.Tx` ganó `Verify`: dispara esas comprobaciones sin commitear.
Postgres corre `SET CONSTRAINTS ALL IMMEDIATE` y SQLite el `foreign_key_check`
que ya tenía; MySQL devuelve nil porque no tiene nada diferido. Está probado con
las dos mitades: con `Verify` la violación aparece, y **sin** `Verify` la misma
transacción se revierte sin un solo error —ese silencio es el bug—.

El ensayo pasa por el **mismo** `correrTramo` que el apply de verdad, con una
sola cosa distinta al final: `Verify` en lugar de `Commit`. Un ensayo que corre
por otro código prueba otro código.

Tres decisiones más, todas por el mismo criterio de no prometer de más:

- **No existe contra MySQL ni MariaDB.** Ahí «correr todo y revertir» no
  revierte nada, así que el botón aplicaría. `CanDryRun` sale de las
  capacidades de la conexión abierta, no del nombre del motor.
- **Pide la misma confirmación de producción que aplicar**, y esto es una
  corrección: al principio no la pedía, con el argumento de que escribir el
  nombre de la base es la puerta de «esto queda» y esto no queda. El argumento
  miraba la consecuencia equivocada. Un ensayo hace el MISMO trabajo y toma los
  MISMOS candados, así que contra una tabla grande de producción el corte de
  servicio es idéntico; lo único que cambia es lo que queda escrito después. Un
  botón que toma un ACCESS EXCLUSIVE en producción con un clic es justo lo que
  CLAUDE.md prohibe al pedir «confirmación extra en cualquier escritura».
- **El ensayo que sale bien no se pinta de verde.** En el resto de la pantalla
  el verde significa «quedó aplicado», y acá significa lo contrario. Dos verdes
  iguales con significados opuestos es cómo alguien cierra el diálogo creyendo
  que ya aplicó. Por lo mismo, el cartel de progreso dice «Ensayando N de M» y
  no «Aplicando», y el rojo de `.resultadoTitulo` pasó a colgar del bloque que
  falla en vez de la clase del título —la compartían, así que un ensayo correcto
  escribía «Nada aplicado» en rojo de error—.

- **`RolledBack` se pone después de revertir, no antes.** Estaba puesto en true
  al armar el resultado, mientras el ROLLBACK de verdad era el diferido y su
  error se descartaba. Es la única afirmación que el botón hace: no puede
  apoyarse en un error que nadie lee.

Y lo que el ensayo **no** promete, dicho en la propia pantalla: hace el mismo
trabajo que el apply —incluidas las reescrituras de tabla enteras— y toma los
mismos candados, así que contra una tabla grande sale lo mismo que aplicar y
después hay que aplicar igual; y entre el ensayo y el apply la base sigue viva.

---

**El campo «Comentario» del alta de columna se escribía y se perdía, y el bug de
las barras invertidas era imposible de ver.**

`SetColumnComment` estaba implementado de punta a punta —`COMMENT ON COLUMN` en
Postgres, `MODIFY` en MySQL, `ErrUnsupported` honesto en SQLite— y **ninguna
pantalla lo producía**. El vocabulario del changeset se armó completo por motor;
la puerta de entrada nunca se construyó. Peor: el diálogo de alta de columna SÍ
tenía un campo «Comentario», que viajaba adentro del `AddColumn` y que **ningún
motor renderiza**. Se escribía y desaparecía.

Ahora hay «Comentar…» en el menú contextual de la columna, y el comentario del
alta se arma como su **propio cambio** en vez de ir adentro del `AddColumn`:
Postgres necesita un `COMMENT ON` aparte de todos modos, un cambio produce una
sentencia, y así además aparece en la lista de pendientes.

**Y al probarlo apareció el bug que lo tapaba todo: `DEFAULT nuevo`, sin
comillas.** MySQL no tiene `COMMENT ON`, así que comentar una columna la
reescribe entera con `MODIFY` —hay que repetirle tipo, nulabilidad y default o
los pierde— y ese default sale del catálogo. Los dos motores lo entregan
distinto:

| | MySQL 9.7 | MariaDB |
|---|---|---|
| `varchar DEFAULT 'nuevo'` | `nuevo` **sin comillas** | `'nuevo'` |
| `int DEFAULT 5` | `5` | `5` |
| `DEFAULT CURRENT_TIMESTAMP` | `CURRENT_TIMESTAMP`, marcado en `extra` | `current_timestamp()`, sin marca |

O sea que en MySQL un literal de texto llega pelado, y al volver a escribirlo en
el DDL sale `DEFAULT nuevo` y el motor contesta 42000. `normalizarDefault` lo
deja escrito como SQL en la introspección, que es donde nace la diferencia: en
MariaDB no hay nada que decidir —ya viene como expresión— y en MySQL se cita lo
que no esté marcado como expresión, salvo que la columna sea numérica. Se cita
en el modo del servidor, no en el normal: con `NO_BACKSLASH_ESCAPES` duplicar la
barra guardaría dos, que es el mismo error de antes.

Comprobado el viaje entero desde la aplicación contra MySQL 9.7: el comentario
`C:\ruta\del\backup` queda en la base con **18 bytes**, una barra por cada una,
y el default sigue siendo `'nuevo'`.

---

**El editor parte las sentencias, y por eso los cuatro motores se comportan
igual.**

Lo que **no** era problema: `--`, `/* */` en varias líneas, al final de una
línea y sueltos al final del texto andan en los cuatro. Se comprobó contra los
cuatro y no contra los dos que estaban a mano, que es un error que ya se había
cometido en esta misma iteración.

Lo que sí difería era correr **varias sentencias de una vez**. El editor mandaba
el texto entero en una llamada y decidía el driver. Comprobado contando FILAS y
no resultados —con tres `SELECT` los tres casos se ven casi iguales, con tres
`INSERT` se ve lo único que importa—:

| | ¿corrían? | ¿se veían? |
|---|---|---|
| Postgres | las tres | tres resultados |
| MySQL / MariaDB | **ninguna**, error de sintaxis | — |
| SQLite | **las tres**, tres filas escritas | **un resultado** |

El tercero era el grave: `INSERT; INSERT; INSERT` escribía tres filas y la
pantalla mostraba una.

**Se descartó encender `multiStatements` en el driver de MySQL.** Es un
parámetro del DSN que vale para toda conexión y toda llamada, no solo para el
editor: a partir de ahí un `;` deja de ser el final de nada, y cualquier lugar
donde se concatene texto pasa de poder producir «una sentencia rara» a poder
producir «una sentencia rara **y las que le sigan**». La regla de CLAUDE.md
—SQL siempre parametrizado— existe para que eso sea imposible. Y compraba poco:
no arreglaba SQLite, que ya corría las tres.

**Se parte del lado del cliente**, en `query.Split`. Con eso los cuatro motores
hacen lo mismo, cada sentencia trae su tiempo y sus filas afectadas, y el fallo
dice cuál fue y **en qué línea** —que es lo que sirve: quien escribió el texto
está mirando números de línea, no contando sentencias—. Se corta en la primera
que falla: seguir daría una cascada de errores donde el primero es el único que
importa, y lo que ya corrió viaja igual en el lote.

No es un `strings.Split(sql, ";")`, y esa es toda la dificultad. Un punto y coma
vive también adentro de:

- una cadena, con `''` duplicada o `\'` según el modo del servidor;
- un identificador citado —comillas dobles, acento invertido en MySQL,
  corchetes en SQLite—;
- un comentario de línea, uno de bloque, o uno con `#` en MySQL;
- el cuerpo de una función de Postgres, entre `$$` o `$etiqueta$`;
- el cuerpo de un trigger o un procedimiento, entre `BEGIN` y `END`.

El último tiene una trampa que se llevó el rato: si `BEGIN` contara siempre
como apertura de bloque, el `BEGIN;` que abre una **transacción** nunca
encontraría su `END` y a partir de ahí no se partiría nada —sin ningún error
que lo delate—. Por eso solo abre bloque dentro de un `CREATE`/`ALTER` de
TRIGGER, PROCEDURE o FUNCTION.

El dialecto sale de la conexión y no del motor, porque una de las banderas
depende del SERVIDOR: con `NO_BACKSLASH_ESCAPES`, la barra invertida no escapa
nada y la cadena termina antes.

Y se cayeron dos capacidades que habían durado una hora: `MultiStatement` y
`ResultPerStatement` existían para describir en qué se diferenciaban los
motores, y partiendo del lado del cliente ya no se diferencian. Una capacidad
que vale lo mismo en los cuatro no es una capacidad.

---

**El editor SQL hablaba PostgreSQL contra los cuatro motores.** El `dialect` de
CodeMirror estaba fijo en `PostgreSQL` y la barra de estado decía «dialecto
PostgreSQL» estando conectado a MySQL 9.7. Lo encontró una prueba manual, no un
test: la costura de Go estaba bien, la pantalla no.

No es cosmético. El dialecto decide qué es palabra reservada, cómo se citan los
identificadores —acento invertido en MySQL, comilla doble en los otros—, si `#`
abre un comentario y qué ofrece el autocompletado. Ahora sale del motor de la
conexión abierta, y **MariaDB usa su propio dialecto**, que en CodeMirror no es
el de MySQL: divergen en palabras reservadas, igual que divergen los motores.

De paso, `nombreDeMotor` estaba copiado en dos pantallas y este cambio iba a
poner una tercera copia. Tres copias de un switch de cuatro casos es donde
alguien agrega un motor y arregla dos: se movió a `lib/motor.ts` junto con el
dialecto.

---

**El atajo «Abrir archivo SQLite…» termina en el editor, no en la base.**

`DraftSQLite` arma el borrador del lado de Go —motor, ruta y nombre sacado del
archivo— y lo devuelve **sin guardar**. Podría guardar y conectar de una: es un
clic menos. No lo hace porque la conexión se escribe en la libreta, y abrir un
archivo para mirarlo no debería dejar una entrada que la persona nunca vio.

El nombre se corta en los dos separadores a mano en vez de usar `filepath`.
`filepath` usa el separador del sistema donde corre el programa, así que en
Linux `filepath.Base` de una ruta de Windows devuelve la ruta entera —y la
libreta se sincroniza entre máquinas, además de que CI corre estos tests en
Linux—.

De paso se cayó un `TrimSpace` que ninguna inyección podía volver roja:
`Normalize` ya recorta el nombre. Una línea que no puede fallar es una línea que
sobra, por el mismo motivo por el que un test que no puede fallar es peor que no
tener test.

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

**La firma del autor va en About y en ningún otro lado.** Un enlace que aprieta
una persona no rompe la regla de no phone-home: la aplicación no hace ninguna
request, no manda identificador ni versión ni timing, y solo pasa si alguien
decide. Pero **la URL va pelada**: un `?ref=kaname&v=1.3` convertiría el clic en
telemetría —el servidor se enteraría de que alguien corre Kaname y cuál
versión—, que es justo lo que la regla prohíbe, disfrazado de enlace.

No son `<a href>`. Dentro de un webview eso **no** abre el navegador del
sistema: según cómo esté configurado WebView2, navega el webview en el lugar
—reemplaza la aplicación por una página web— o abre una ventana pelada. Va por
`Browser.OpenURL` de `@wailsio/runtime`, que se lo entrega al sistema operativo.
La aplicación nunca carga contenido remoto en su propio proceso.

**Está en las cuatro pantallas, y la animación no.** En la barra de estado del
workspace, del gestor de conexiones y de la bienvenida va la variante
**quieta**: el wordmark corto, el punto en color, la marca de GitHub, y nada que
se mueva. En About va la animada.

La distinción no es estética. Un texto que se teclea solo en el borde de la
vista, mientras alguien lee un plan de ejecución, es exactamente lo que la regla
de movimiento existe para evitar — y es además lo que haría que se leyera como
publicidad, porque **lo que llama la atención es el movimiento, no el hecho de
estar ahí**. Una firma quieta en una barra de estado es lo que tiene cualquier
herramienta y nadie la registra como un aviso.

En About sí se teclea, porque About es un destino: se abre a propósito, se mira
y se cierra, y no compite con nada. Ahí además se detiene cuando la ventana no
está visible: eso en una página web es cortesía, acá es que esta ventana queda
abierta horas.

En la barra va después del espaciador pero **antes** de las acciones. La esquina
sigue siendo de «Desconectar» y de «about», que es donde la mano ya las busca.

**Sin contador de estrellas de GitHub.** Sería llamar a `api.github.com` cada vez
que se abre About: GitHub aprendería la IP y que esa persona corre Kaname, cada
vez. Es phone-home con otro nombre. Hornear el número en el build lo deja
desactualizado, que es peor que no tenerlo. Queda el enlace solo.

**Cerrar pestañas con el botón del medio.** Lo que hace cualquier navegador o
editor. El `preventDefault` en `mousedown` no es opcional en Windows: sin él, el
sistema entra en modo autoscroll y deja el cursor de las flechitas dando vueltas.

**La marca, y por qué el ícono de Windows se adelantó.** `build/appicon.png` y
`build/windows/icon.ico` seguían siendo los de Wails, así que todo lo compilado
hasta la Iteración 9 iba a llevar la identidad equivocada. Y el test que el
propio kit define —el isotipo a 16px sobre taskbar clara y oscura— solo se
puede correr con un build real.

El `.ico` se arma **a mano con los cuatro cortes del kit**, no con
`wails3 generate icons`. Dos motivos: esa tarea reescala desde `appicon.png`, y
los tamaños chicos del kit son dibujos distintos —a 16px desaparece la varilla
horizontal, y de 16 a 32 el eje se dibuja alineado a la grilla de píxeles—; y
además genera solo seis miembros (`256,128,64,48,32,16`), sin el de **20**, que
es el que Windows usa en la barra de tareas, ni el de 24. El `.ico` quedó con
ocho. De 48 para arriba sí salen del maestro de 1024, que es lo que el kit
indica: ahí no se simplifica nada y solo cambia la resolución.

Por eso el build de Windows **ya no depende** de `common:generate:icons`: esa
tarea sobreescribe `windows/icon.ico`, y su flag `-windowsfilename` tiene ese
mismo valor por defecto, así que omitirlo no alcanzaba. El `.ico` pasa a ser un
artefacto commiteado, no generado.

**`wails3 task dev` fallaba por 400 milisegundos.** Abortaba con *«unable to
connect to frontend server»* aunque Vite arrancara bien. Wails espera al dev
server **10 intentos de 500 ms y después mata la aplicación**, y las dos cosas
—el host `localhost` y ese presupuesto— están escritas en su código, no son
configurables.

Se descartó primero la sospecha obvia: `vite.config.ts` ata el server a
`127.0.0.1` a propósito y `localhost` resuelve a `::1` primero en Windows. Pero
resuelve a **las dos**, y el cliente de Go hace fallback a IPv4 en 64 ms. No era
eso.

Era que `dev:frontend` traía `deps: install:frontend:deps`, o sea un
`pnpm install` de más: `wails3 dev` ya corre `wails3 build DEV=true` como paso
bloqueante, que instala las dependencias segundos antes. Medido, el arranque del
dev server tardaba **4607 ms contra 5000 de presupuesto** — fallaba en cuanto la
máquina estuviera algo ocupada, que es exactamente lo que pasa durante un build.
Sin la instalación repetida son **1312 ms**, y el margen pasa de 400 ms a 3,7 s.

**Movimiento: tres duraciones y una regla.** `--dur-fast` 90ms para menús y
desplegables, `--dur` 140ms para diálogos y paneles, `--dur-slow` 400ms para
destellos. La regla que las ordena: **se anima lo que cambia de existencia o de
posición, nunca lo que cambia de contenido ni lo que se está por accionar.**

Se adelantó una sola cosa, porque es un arreglo y no decoración: el **destello
del contador de cambios pendientes**. Preparás un cambio, el diálogo se cierra,
y lo único que pasa es que un número sube en la otra punta de la pantalla; sin
el destello esa señal se pierde. (La barra de progreso del apply, que también
estaba en la lista, ya tenía su transición: no hacía falta tocarla.)

Lo que queda para el pase de la Iteración 9: salida de los diálogos con
`@starting-style`, menús y desplegables, y los toasts de S24 —donde la
animación es funcional, porque aparecen sin que los pidas y en el borde de la
vista—. Lo que **no** se anima, y conviene que esté escrito para que nadie lo
«arregle» después:

- La grilla de resultados y cualquier lista virtualizada: las filas se
  reciclan, así que animar la entrada haría parpadear filas que solo cambiaron
  de posición.
- El lienzo del ERD mientras se arrastra o se recalcula el layout: va a 60fps
  contra la mano del usuario.
- Abrir y cerrar pestañas, y plegar los paneles laterales: animar el ancho
  reflowea de más. Si se intenta, se mide con el ERD abierto antes de aceptarlo.
- **Las confirmaciones destructivas.** Un modal rojo que entra suave se lee como
  menos serio que uno que aparece. La confirmación de producción tiene que
  aparecer, no llegar. Es la única pantalla donde la ausencia de animación es la
  decisión de diseño.

`prefers-reduced-motion` ya está resuelto global en `tokens.css`, así que todo
lo nuevo hereda el interruptor. Acá eso es más que accesibilidad: quien está
aplicando DDL contra producción tiene derecho a apagar cualquier cosa que
demore la respuesta.

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
*(2026-09-11: las tareas no se habían borrado —solo el directorio—; ver la
iteración 9, «Keychain, sockets y TOFU».)*

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
*(Resuelto el 2026-09-11: Apache 2.0, ver la iteración 9.)*

Consecuencia que aplica desde hoy: **si el repo se publica, se publica el
historial completo.** Un secreto commiteado ahora sigue en el historial aunque se
borre en el commit siguiente. La regla de no commitear credenciales, rutas de
claves ni datos de conexiones reales no es higiene: es irreversible.

### Checklist de pre-publicación

No es una lista para "algún día": es la condición para que el repo pase a
público. Nada se marca por confianza, todo con evidencia.

- [x] Auditoría del historial completo en busca de secretos, no solo del árbol
      actual. Job `secretos` con `gitleaks git --log-opts=--all` y
      `.gitleaks.toml`, 2026-09-11: 146 commits limpios, con dos reglas
      propias para las DSN y los `password = "…"` que las de fábrica no ven.
- [x] Verificar que ningún log, mensaje de error ni evento hacia el frontend
      contenga credenciales, connection strings ni valores de filas. Tabla de
      canales en la iteración 9 («Logs, errores y eventos sin
      credenciales»), con un test por canal, 2026-09-11.
- [x] Confirmar que los secretos viven solo en el keychain y que el estado
      local —TOML y JSON; no hay SQLite de estado— y el archivo de config no
      tienen ninguno. `TestLosSecretosVanAlKeychainYANingunArchivo` y
      `TestSiElKeychainFallaNoSeGuardaNadaEnNingunLado`, 2026-09-11.
- [x] Confirmar que la app no abre ningún socket en ninguna configuración.
      `sockets_test.go` (código y Taskfiles) y `scripts/sockets.ps1` contra el
      binario limpio, 2026-09-11: cero Listen, cero UDP, cero sockets de
      `kaname.exe`. Las tareas del modo servidor, que seguían en el
      Taskfile, borradas.
- [x] Revisar que `known_hosts` haga TOFU real y que no exista ninguna ruta con
      `InsecureIgnoreHostKey`. `TestElVerificadorSoloAceptaLaClaveQueSeAcepto`
      (seis casos) y la prohibición de `ssh.InsecureIgnoreHostKey` en
      `sockets_test.go`, 2026-09-11.
- [x] Tests de integración de los cuatro motores en verde. Batería completa
      con los seis contenedores, 2026-09-11: 1248 PASS, 0 FAIL, 4 SKIP con
      motivo; en CI, verde en cada push contra Postgres 18/17/16/14.
- [x] Elegir y agregar la licencia. Apache 2.0, 2026-09-11.

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
