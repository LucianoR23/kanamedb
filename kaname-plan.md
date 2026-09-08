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

CodeMirror con autocompletado de esquema, ejecución con streaming y cancelación,
grilla solo lectura.

- **S06 SQL editor** — completa, incluyendo estado de streaming y cancelación.
- **S07 Results grid** — solo lectura: NULL vs string vacío, íconos de tipo,
  barra de sort/filter. Sin modo edición.
- **S09 Cell viewer** — completa.
- **S10 Table data tab** — con "Load more" y conteo de filas. Sin filter builder
  avanzado.

### Iteración 3 — SSH

Túnel integrado, `known_hosts` con TOFU, soporte ssh-agent.

- **S03** — tab SSH Tunnel.
- **S04 SSH host key verification** — completa, incluida la variante de host key
  cambiada.
- **S24** — variante "tunnel dropped / reconnect".

### Iteración 4 — ERD lectura

> **Verificar antes de empezar:** que Atlas introspeccione las features de
> esquema que agregó PostgreSQL 18 — columnas generadas `VIRTUAL`, restricciones
> temporales con `WITHOUT OVERLAPS` y `NOT NULL NOT VALID`. No hay confirmación
> en su documentación, y son exactamente el tipo de cosa que el differ ignora en
> silencio: si Atlas no las ve, cada re-inspección propone borrarlas. Se comprueba
> con una tabla de prueba, no leyendo el changelog.


Atlas inspect → modelo → canvas xyflow, auto-layout, posiciones persistidas por
conexión.

- **S12 ERD canvas** — sin edición: nodos con columnas e indicadores, edges con
  cardinalidad, minimapa, zoom, auto-layout, búsqueda, saved views, inspector,
  toggle de densidad.
- **S11 Table structure** — solo lectura (Columns, Indexes, FKs, Constraints,
  Triggers).

### Iteración 5 — ERD escritura

Edición en canvas → changeset pendiente → diff Atlas → preview SQL → aplicar →
re-inspeccionar.

- **S13 ERD edit interactions** — completa.
- **S14 Pending changes panel** — completa.
- **S15 SQL preview & apply** — DDL numerado, marcado de destructivos, progreso
  por sentencia. Sin banners MySQL/SQLite y sin dry run todavía.
- **S11** — editable, alimentando el changeset.
- **S24** — variante "write on Production".

**Hito usable: Postgres completo de punta a punta (~4 meses).**

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

Edición de celdas con preview de `UPDATE`/`DELETE`, import/export CSV.

- **S07** — modo edición: celdas modificadas, filas nuevas, filas marcadas para
  borrar, tablas sin PK en solo lectura con explicación.
- **S08 Data change review** — completa, incluida la variante de producción.
- **S18 CSV import wizard** — completa.
- **S19 Export dialog** — completa.

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

---

## 2. Stack

Versiones marcadas con ✅ están instaladas y verificadas en la máquina de
desarrollo (windows/arm64). El resto entra en la iteración que lo necesite.

### Instalado

| Pieza | Versión | Nota |
|---|---|---|
| Go | ✅ 1.26.0 | Un solo módulo. `CGO_ENABLED=0`, sin gcc |
| Wails | ✅ v3.0.0-beta.17 | Bindings Go↔JS, sin servidor HTTP |
| React | ✅ 19.2.8 | Con React Compiler 1.0.0, verificado activo |
| TypeScript | ✅ 7.0.2 | Puerto nativo en Go, es el `latest` de npm |
| Vite | ✅ 8.2.2 | `@vitejs/plugin-react` 6.1.1 |
| pnpm | ✅ 10.33.2 | No npm. Lockfile commiteado |

### Pendiente por iteración

- **Introspección / diff / plan:** `ariga.io/atlas` v1.3.0 (`sql/schema`,
  `sql/postgres`, `sql/mysql`, `sql/sqlite`), pineada. Verificado que no arrastra
  `cloud/` ni `cmd/`.
- **Drivers:** `pgx/v5` v5.11.0 (vía `stdlib` para Atlas),
  `go-sql-driver/mysql`, `modernc.org/sqlite` v1.58.0 (verificado en arm64).
- **SSH:** `golang.org/x/crypto/ssh` + `ssh/knownhosts`; `go-winio` para el pipe
  del agente de OpenSSH en Windows.
- **Keychain:** `zalando/go-keyring` (MIT).
- **Estado local** (historial, posiciones del ERD, config): SQLite en `%APPDATA%`,
  sin secretos.
- **Canvas ERD:** `@xyflow/react`.
- **Layout:** `@dagrejs/dagre`; ELK solo si dagre no alcanza.
- **Grilla:** CSS Grid propio, virtualizado con `@tanstack/react-virtual`
  v3.14.10, pineada exacta. Reemplaza a `glide-data-grid` — ver el registro de
  decisiones.
- **Editor:** CodeMirror 6 + `@codemirror/lang-sql`.
- **Estado UI:** Zustand.

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

### Iteración 2 — 2026-09-08

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
