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

- **S00 Foundations** — tokens CSS (dark + light, acentos de entorno), tipografía,
  espaciado y componentes base: botones, inputs, tabs, badges, tree row, celda de
  grilla, dialog, toast, context menu.
- **S05 Main workspace shell** — shell vacío: sidebar, tab strip, status bar, panes
  redimensionables. Sin contenido real.
- **S25 About** — completa.

### Iteración 1 — Conexiones

Manager de conexiones, keychain, conectar a Postgres, árbol de esquema.

- **S01 Welcome** — sin la opción "Open SQLite file".
- **S02 Connection manager** — completa.
- **S03 Connection editor** — solo tab General. SSH, TLS y Advanced quedan como
  placeholders deshabilitados.
- **S05** — árbol de esquema con tablas de Postgres únicamente.
- **S24 Confirmation dialogs** — solo variante "connection error".

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

MySQL, MariaDB y SQLite por el mismo pipeline.

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

- **Core:** Go 1.25+, un solo módulo
- **Ventana nativa:** Wails v3 (bindings Go↔JS, sin servidor HTTP)
- **Introspección / diff / plan:** `ariga.io/atlas` (`sql/schema`, `sql/postgres`,
  `sql/mysql`, `sql/sqlite`), versión pineada
- **Drivers:** `pgx/v5` (vía `stdlib` para Atlas), `go-sql-driver/mysql`,
  `modernc.org/sqlite`
- **SSH:** `golang.org/x/crypto/ssh` + `ssh/knownhosts`; `go-winio` para el pipe
  del agente de OpenSSH en Windows
- **Keychain:** `zalando/go-keyring` (MIT)
- **Estado local** (historial, posiciones del ERD, config): SQLite en `%APPDATA%`,
  sin secretos
- **Frontend:** React 19 + TypeScript + Vite
- **Canvas ERD:** `@xyflow/react`
- **Layout:** `@dagrejs/dagre`; ELK solo si dagre no alcanza
- **Grilla:** `glide-data-grid`
- **Editor:** CodeMirror 6 + `@codemirror/lang-sql`
- **Estado UI:** Zustand
- **CI:** GitHub Actions, matriz windows/linux/macos, `goreleaser` o script propio

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

- Atlas CLI hace update check; la librería no. No importes `cmd/` ni paquetes de
  cloud.
- WebView2 (Windows) se actualiza y reporta a Microsoft por su cuenta, y no lo
  controlás desde la app. Es el precio de no embeber Chromium.
- El resto (xyflow, CodeMirror, glide-data-grid, Wails, pgx) no hace phone-home.
- Verificá que `modernc.org/sqlite` compile en windows/arm64 con tu versión antes
  de comprometerte.
- Mirá la actividad del repo de glide-data-grid: bajó bastante.

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

## Cómo ejecutar cada iteración

1. Pasarle a Claude Code solo los `design/Sxx-*` de esa iteración, diciendo
   explícitamente qué parte de la pantalla entra y qué no.
2. Antes de la UI, definir el contrato: qué datos muestra la pantalla y qué
   acciones dispara. Eso es el binding Go de la iteración; se prueba desde un
   test, no desde React.
3. Recién después, implementar la pantalla contra ese binding, con datos reales.
4. La iteración cierra cuando la usás con una base tuya, no cuando "se ve igual al
   diseño".
