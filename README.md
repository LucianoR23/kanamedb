# Kaname

Gestor de bases de datos de escritorio con diagrama ERD editable.
Editás el diagrama, Kaname te muestra el SQL que va a correr, y recién ahí lo aplicás.

Motores: **PostgreSQL** (principal), MySQL, MariaDB y SQLite. No hace falta
una versión concreta: cada motor tiene un **mínimo**, y de ahí para arriba
cualquiera. El criterio es el mismo en los cuatro —la más vieja con soporte
oficial vigente—, la app lo verifica al conectar y lo dice si no alcanza.

| Motor | Mínimo | Probado en cada corrida | Por qué ahí |
|---|---|---|---|
| PostgreSQL | **14** | 18, 17, 16 y 14 en CI | En 13 y anteriores `reltuples` vale 0 para una tabla nunca analizada, y el árbol diría «0 filas» para una de millones. |
| MySQL | **8.4** LTS | 9.7 y 8.4 | La 8.0 salió de soporte premier en abril de 2026. |
| MariaDB | **10.11** LTS | 12.3 y 10.11 | La más vieja de las series mantenidas. |
| SQLite | — | 3.53, la que va adentro | El motor viene **embebido** en Kaname: no hay nada que instalar. Abre cualquier archivo `.db`/`.sqlite` que SQLite 3 lea. |

> **Estado: Iteración 7 — la grilla editable.** Habla con los cuatro motores
> —PostgreSQL, MySQL, MariaDB y SQLite— directo o a través de un bastión SSH,
> con verificación de la clave del host y sin abrir ningún puerto local, y con
> TLS con la semántica de libpq en los tres de servidor: modo, raíz y
> certificado de cliente por ruta, y el certificado que presentó el servidor a
> la vista después de probar. Cada conexión tiene además su search_path, su
> nombre de aplicación, el tamaño del pool y una SQL de sesión que corre en
> cada conexión al abrirla. Guarda
> la libreta de conexiones con los secretos en el keychain del sistema
> operativo —agrupadas por carpeta: un proyecto, con su local, su dev y su
> producción adentro— y las exporta e importa en ese mismo formato, sin
> ningún secreto y con vista previa. Trae editor SQL con autocompletado,
> formateo por dialecto y plan de ejecución —el motor dice cómo va a correr
> la sentencia bajo el cursor, sin correrla—, grilla con paginado, estructura
> completa de cada tabla y diagrama ERD.
>
> **Edita el esquema y las filas.** Las ediciones se juntan en un changeset, se
> muestran como SQL antes de tocar nada y se aplican en una transacción con
> progreso por sentencia; los valores de una fila viajan siempre como
> parámetros, nunca escritos en la SQL que corre. Contra producción hay que
> escribir el nombre de la base. La grilla filtra por columna —columna,
> operador y valor, varias condiciones combinadas— y lo que se lee se puede
> exportar a CSV, JSON, JSON Lines o Markdown: el resultado del editor, o una
> tabla entera con el filtro que tenga puesto, que se escribe a medida que se
> lee y por eso no depende de que entre en memoria. Y al revés: un CSV entra en
> una tabla con un asistente que ensaya la importación de verdad —adentro de
> una transacción, y la revierte— antes de escribir nada. El volcado escribe la
> estructura, los datos o los dos, y **dice con nombre y apellido qué deja
> afuera**: un export de esquema que se olvida de una vista se ve idéntico a uno
> correcto, y esa es la diferencia entre un archivo que sirve y uno que engaña.
> Para el volcado completo de PostgreSQL arma la línea de `pg_dump` y la corre
> si la versión alcanza. **Compara dos conexiones** —dev contra producción—
> y escribe la migración que alinearía la segunda: lee los dos catálogos, no
> ejecuta nada, nunca genera un borrado, y dice con la misma claridad qué no
> miró. Se construye para **Windows, Linux y macOS** desde CI, con la marca
> de cada sistema y sin firma; ver [Releases](#releases).
> Ver [`kaname-plan.md`](kaname-plan.md) para el plan y el registro de decisiones.

---

## Por qué

Los gestores existentes te dejan editar el esquema a ciegas o te obligan a escribir
el DDL a mano. Kaname parte del diagrama: movés una columna, agregás una FK, y antes
de tocar la base ves el plan de migración numerado, con los cambios destructivos
marcados. Contra producción, eso no es una comodidad — es la diferencia entre un
`ALTER` y un incidente.

## Principios

- **Sin sockets.** La app no expone ningún puerto. Todo va por bindings de Wails.
  Un servidor en `127.0.0.1` es alcanzable desde cualquier pestaña del navegador.
- **Los secretos viven en el keychain del SO.** Nunca en disco, nunca en logs.
- **Nada se aplica sin preview.** El DDL se muestra antes de ejecutarse, siempre.
- **Sin phone-home.** Sin telemetría, sin update checks automáticos. Lo único
  que sale a internet por su cuenta es el botón «Buscar actualizaciones» de los
  ajustes, y solo cuando lo apretás: un GET a la API de releases de GitHub, sin
  query string, sin cookies y sin la versión instalada —la comparación se hace
  del lado de la app—. No descarga ni instala nada.

  Eso es lo que hace Kaname. Lo que hace **el runtime de WebView2** que dibuja
  la ventana en Windows es otra cosa, y conviene saberlo: es un componente
  del sistema —lo instala y lo actualiza Windows, como a Edge— y al arrancar
  abre dos conexiones HTTPS a Microsoft por su cuenta, con la app sin haber
  conectado a nada. Es el runtime reportando diagnóstico y buscando
  configuración, igual que Edge. No lleva nada de lo que la ventana muestra:
  los datos de diagnóstico de Edge no incluyen contenido de página, y las URLs
  que navega Kaname son internas (`wails://`). No se apaga desde la app: se
  probaron los flags de Chromium para eso compilados en el binario
  (`--disable-background-networking`, `--disable-component-update`,
  `--disable-domain-reliability`, `--metrics-recording-only`, `--no-pings`) y
  las dos conexiones siguen ahí, así que no están. Lo que sí lo gobierna es el
  ajuste de Windows *Privacidad y seguridad → Diagnóstico y comentarios →
  Enviar datos de diagnóstico opcionales*. En Linux (WebKitGTK) y macOS
  (WKWebView) no pasa. `scripts/sockets.ps1` lo muestra cada vez que se corre,
  para que no haya que creerlo.

---

## Dónde guarda sus cosas

Ninguno de estos archivos tiene contraseñas: viven en el keychain del sistema
—Credential Manager, Keychain, Secret Service; en Android, almacenamiento
cifrado con una clave del Keystore—. La pantalla **Ajustes** muestra la ruta
del archivo de preferencias y **About** las de todos. En Windows y Android el
directorio de estado es el mismo que el de configuración; en Linux y macOS no.

| Archivo | Qué es | ¿Conviene sincronizarlo? |
|---|---|---|
| `connections.toml` | La libreta de conexiones, con la carpeta de cada una | Sí |
| `consultas.json` | Las consultas guardadas con nombre | Sí |
| `layouts/` | Dónde quedó cada tabla en el diagrama | Sí |
| `known_hosts` | Claves públicas de bastiones SSH aceptadas | Es tu decisión |
| `config.toml` | Preferencias de la aplicación (S23) | Da igual |
| `historial.json` | Qué consultas corriste **en esta máquina** | No |

---

## Desarrollo

### Requisitos

| | Versión | |
|---|---|---|
| [Go](https://go.dev/dl/) | 1.26+ | Sin cgo: no hace falta gcc |
| [Node.js](https://nodejs.org/) | 24+ | |
| [pnpm](https://pnpm.io/installation) | 10+ | `corepack enable pnpm` |
| WebView2 Runtime | — | Ya viene en Windows 11 |

Además, el CLI de Wails, pineado a la versión que usa el proyecto:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.17
```

Asegurate de que `$(go env GOPATH)/bin` esté en el `PATH`.

### Correr en modo desarrollo

```sh
cd frontend && pnpm install && cd ..
wails3 task dev
```

Recarga en caliente del frontend y rebuild del binario al tocar Go.

### Compilar

```sh
wails3 task build                  # arquitectura de la máquina
wails3 task build ARCH=amd64       # win-x64
wails3 task build ARCH=arm64       # win-arm64
```

El binario queda en `bin/kaname.exe` (~14 MB: pgx, el cliente SSH y el keychain).

> No uses `go build` directo. Se saltea el tag `production` —que deja el webview
> en modo desarrollo— y el `.syso` con ícono, manifest de DPI y metadata de versión.

En **Linux** y **macOS** el mismo `wails3 task build` compila nativo —hace falta
un compilador de C: GTK4 y WebKitGTK en Linux, Cocoa en macOS entran por cgo—,
y el empaquetado tiene su tarea por sistema:

```sh
# Linux (Ubuntu 24.04+ / Debian 13+; el backend es GTK4 + WebKitGTK 6.0)
sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev
wails3 task build
wails3 task linux:create:appimage   # bin/kaname-x86_64.AppImage, se copia y anda
wails3 task linux:create:deb        # bin/kaname.deb, con sus dependencias declaradas
wails3 task linux:create:rpm        # bin/kaname.rpm

# macOS (Xcode con las command line tools)
wails3 task darwin:package:universal   # bin/kaname.app, arm64 + x86_64
```

Un build hecho en la propia máquina no pasa por SmartScreen ni por Gatekeeper:
esos avisos son para lo que se **descarga**. Ver [Releases](#releases).

**Android** es un spike (ver `kaname-android.md`) y se construye solo en CI:
el workflow `android` —a mano desde Actions, o en cada push a `spike/android`—
deja `kaname-android-arm64.apk` como artifact, firmado con el keystore de
debug, para instalar con `adb install`. El Taskfile de Android de Wails solo
resuelve el NDK en Linux/macOS x86_64, y es la única plataforma que necesita
cgo. Para correrlo en una máquina Linux hacen falta JDK 17, el SDK con
`platforms;android-35`, `build-tools;35.0.0` y `ndk;26.3.11579264`:

```sh
wails3 task android:build ARCH=arm64    # overlay + bindings + frontend + libwails.so
wails3 task android:assemble:apk        # Gradle; deja bin/kaname.apk
```

Los íconos no se generan en el build: el `.ico` de Windows, los nueve PNG de
hicolor de Linux y el `.icns` de macOS vienen del brand kit y están
commiteados. La excepción es macOS 26, cuyo ícono es por capas: en un Mac con
Xcode 26, `common:generate:icons` compila `build/appicon.icon` con `actool` a
`Assets.car` durante el build. Sin él, queda el `.icns` plano.

La **versión** se escribe a mano en siete lugares —`internal/appinfo`,
`build/config.yml`, `build/windows/info.json`, los dos `Info.plist`,
`build/linux/nfpm/nfpm.yaml` y `build/android/app/build.gradle`— y `go test ./`
avisa si alguno quedó atrás. No
uses `wails3 task common:update:build-assets` para sincronizarlos: regenera esos
archivos desde el template y pisa lo que se editó a propósito.

### Verificaciones

Los tests de integración necesitan los motores de prueba, que incluyen un
servidor SSH para el túnel:

```sh
docker compose -f docker-compose.test.yml up -d
```

Levanta PostgreSQL, **dos** MySQL y **dos** MariaDB, cada uno en un puerto
corrido para no chocar con una instalación local: **55432**, **53306** (MySQL
9.7), **53309** (MySQL 8.4), **53307** (MariaDB 12.3) y **53308** (MariaDB
10.11). Las dos segundas de cada motor son las LTS anteriores, que son las que
están instaladas en más lugares que las últimas.

Las cuatro a la vez no son exceso de celo. Probando solo la 12.3 de MariaDB, la
10.11 no conectaba en absoluto —Kaname pedía `@@transaction_read_only`, que
llegó recién en 11.1.1— y nadie se enteraba. Declarar una versión soportada y no
correr un solo test contra ella es prometer sin comprobar.

**SQLite no está ahí y no hace falta levantarlo**: es un archivo, y sus tests
crean uno nuevo en el directorio temporal de cada caso. Por eso los de SQLite
nunca se saltean, y los otros tres sí cuando el motor no está escuchando. Ese
salteo es a propósito para trabajar sin Docker, pero en CI sería un test verde
que no probó nada: ponerle cualquier valor a `KANAME_REQUIRE_ENGINES` lo
convierte en un fallo.

Los cuatro motores corren **la misma batería** —`internal/engine/enginetest`—,
así que un motor pasa o no pasa contra los mismos casos que los demás:

```sh
go test ./internal/postgres/ ./internal/mysql/ ./internal/sqlite/ -run TestSuite
```

Eso son seis corridas de la misma batería: Postgres, las dos MySQL, las dos
MariaDB y SQLite.

Para probar **a mano** contra un solo motor no hace falta tener los seis
corriendo: los servicios se paran y se vuelven a levantar de a uno. Ojo con
Postgres: corre sobre `tmpfs`, así que pararlo borra lo que se haya creado a
mano (los tests no lo notan: crean lo suyo).

```sh
docker compose -f docker-compose.test.yml stop postgres mysql mysql-lts mariadb-lts
docker compose -f docker-compose.test.yml start postgres
```

La batería de Go sí los necesita a todos: con `KANAME_REQUIRE_ENGINES=1` un
motor apagado es un test rojo, no uno salteado.

Por defecto levanta PostgreSQL 18. Para probar contra otra versión de la matriz,
`PG_VERSION` la elige — pero **hay que bajar el stack con `-v` antes de
cambiarla**:

```sh
docker compose -f docker-compose.test.yml down -v
PG_VERSION=17 docker compose -f docker-compose.test.yml up -d --wait
```

El Postgres de pruebas **es una imagen propia** (`docker/postgres/Dockerfile`)
sobre la oficial: le agrega TLS encendido con un certificado autofirmado que se
genera en el build, para que la pestaña TLS de S03 tenga contra qué probarse
—verify-full lo rechaza con las raíces del sistema y lo acepta con él mismo
cargado como raíz—. `up` la construye sola la primera vez; después de cambiar
`PG_VERSION` conviene `docker compose -f docker-compose.test.yml build postgres`
para que el build tome la versión nueva. MySQL 9.7, 8.4 y MariaDB 12.3
generan su certificado solos; MariaDB 10.11 no ofrece TLS, y el test lo
comprueba contra lo que el servidor dice de la sesión en vez de suponerlo.

`--force-recreate` no alcanza: el contenedor nuevo puede quedarse con el
directorio de datos del anterior y Postgres aborta con *"database files are
incompatible with server"*. El `-v` es lo que lo borra. En CI no aparece porque
cada pata de la matriz corre en una máquina limpia.

`MYSQL_VERSION`, `MYSQL_LTS_VERSION`, `MARIADB_VERSION` y `MARIADB_LTS_VERSION`
hacen lo mismo para los otros cuatro. Los defaults son MySQL 9.7 y 8.4, y
MariaDB 12.3 y 10.11.

### Datos para probar a mano

Los tests crean y borran su propio esquema en cada caso, así que la base queda
vacía. Para mirar la aplicación con algo que se parezca a un esquema real:

```sh
docker exec -i kaname-postgres-1 psql -U kaname -d kaname_test < docker/demo.sql
```

Y una base de SQLite, que es un archivo y no necesita contenedor. Sirve para
probar «Abrir archivo SQLite…», que si no pide tener una a mano:

```sh
sqlite3 kaname-demo.db < docker/demo-sqlite.sql
# sin el cliente de sqlite3, Python lo trae:
python -c "import sqlite3;sqlite3.connect('kaname-demo.db').executescript(open('docker/demo-sqlite.sql',encoding='utf-8').read())"
```

Para **comparar esquemas** hacen falta dos bases que difieran a propósito.
`docker/demo-drift.sql` crea `drift_origen` y `drift_destino` en el mismo
Postgres de pruebas, con la lista de lo que tiene que salir en el encabezado del
archivo; después son dos conexiones y «Comparar esquemas…» en el gestor:

```sh
docker exec -i kaname-postgres-1 psql -U kaname -d kaname_test < docker/demo-drift.sql
```

Adentro hay lo que conviene mirar en SQLite y no en los otros: una clave foránea
con `ON DELETE CASCADE` —reconstruir la tabla padre con las claves encendidas
borraría las filas de la hija en silencio—, una columna generada que la
reconstrucción no debe copiar, un índice parcial, un trigger y una vista, que es
lo que obliga a `PRAGMA legacy_alter_table`.

Deja tres esquemas: **`demo`** con identidad, columnas generadas, restricciones
sin validar, índices de todas las formas y triggers activos y deshabilitados;
**`aristas`** con una tabla por cada forma de relación, para comparar cómo se
dibuja cada una; y tres tablas preparadas para que ciertas operaciones **fallen**
al aplicarse, y poder ver que el error se explica y que la transacción revierte:

| Tabla | Qué hacerle | Por qué falla |
|---|---|---|
| `demo.con_nulos` | exigir que `apodo` no sea nula | la fila 2 tiene NULL |
| `demo.con_repetidos` | índice único sobre `codigo` | `'AAA'` está dos veces |
| `demo.huerfanos` | clave foránea de `cliente_id` a `demo.clientes.id` | el cliente 999999 no existe |

Se puede correr las veces que haga falta. La base vive en tmpfs, así que bajar el
stack se lleva todo y hay que volver a correrlo.

**Lo que solo se prueba a mano.** Los selectores de archivo son del sistema
operativo, no de la página: no se pueden manejar por herramientas y ningún test
los cubre. Son **tres**, y conviene pasarlos una vez por release:

1. **«Abrir archivo SQLite…»**, desde S01 y S02.
2. **El «guardar como» de Exportar…**, que está en la barra de resultados del
   editor y en la de una tabla: elegir destino, guardar, y comprobar que el
   archivo tiene la extensión del formato elegido —incluido el `.gz` cuando se
   tilda comprimir—.
3. **El selector de carpeta de Exportar…**, que aparece con el alcance «todas
   las tablas» en cualquier formato que no sea INSERTs: elegir la carpeta y
   comprobar que quedó un archivo por tabla, con el nombre de cada una.

El de **Importar…** no está en la lista porque la ruta del archivo también se
puede escribir, así que ese camino se prueba desde la aplicación.

Todo lo demás de la exportación (formatos, opciones, vista previa, copiar, y que
una tabla salga entera y no solo lo cargado en la grilla) sí se prueba desde la
aplicación y desde los tests.

```sh
gofmt -l .                         # formato de Go
go vet ./...                       # análisis estático
staticcheck ./...                  # lo que vet no mira, deprecaciones incluidas
go test -p 1 ./...                 # tests (ver abajo por qué -p 1)
govulncheck ./...                  # CVEs alcanzables desde nuestro código
cd frontend && pnpm run typecheck  # tipos de TypeScript
gitleaks git --log-opts=--all --redact --config .gitleaks.toml  # secretos en TODO el historial
```

`govulncheck` se instala con
`go install golang.org/x/vuln/cmd/govulncheck@v1.7.0`. Consulta `vuln.go.dev` al
correr; es una herramienta de desarrollo, la aplicación no hace ninguna llamada
de red por su cuenta.

`staticcheck` se instala con `go install honnef.co/go/tools/cmd/staticcheck@v0.8.1`.
Está porque un bump de dependencia compila y pasa los tests aunque use algo
deprecado; SA1019 lo pone en rojo. Las comprobaciones apagadas y el motivo
están en `staticcheck.conf`.

`gitleaks` se instala con `go install github.com/zricethezav/gitleaks/v8@v8.30.1`
y revisa los commits, no solo el árbol: publicar el repo publica el historial, y una contraseña
commiteada y borrada al commit siguiente sigue en `git log -p`. Las reglas
están en `.gitleaks.toml` —las de fábrica más dos propias, porque las de
fábrica dejan pasar `postgres://usuario:contraseña@host`— y CI comprueba
primero que esas reglas detectan una muestra, y recién después escanea. Una
contraseña de mentira nueva en un test se agrega a la lista blanca del
`.gitleaks.toml` por su valor, con el motivo al lado.

El `-p 1` no es opcional cuando hay base: `go test` corre los binarios de cada
paquete **en paralelo**, y los de `internal/postgres` e `internal/service`
escriben en la misma base de pruebas. Sin serializar, un test que cuenta las
tablas visibles ve las que creó otro paquete y falla de manera intermitente.

Es lo mismo que corre CI en cada push.

`go test .` —el paquete de la raíz— incluye además los tests estructurales:
que ningún paquete abra un socket ni ignore la clave de un host SSH, que
ningún Taskfile compile el modo servidor de Wails, que la aplicación no
loguee ni emita eventos ni configure el logger de Wails —que en Debug escribe
los argumentos de cada binding—, que el frontend no inyecte HTML ni escriba en
la consola, que la versión sea la misma en todos lados. Para la parte que un
test no puede ver —lo que el binario
abre de verdad cuando corre, WebView2 incluido— está `scripts/sockets.ps1`:
lanza `bin/kaname.exe`, espera, lista cada socket del árbol de procesos y
falla si alguno escucha o si `kaname.exe` abrió alguno. Solo Windows por ahora.

> En un clon recién bajado, `go vet` y `go test` fallan con
> `pattern all:frontend/dist: no matching files found`. No es un error tuyo: el
> paquete `main` embebe el frontend construido y `frontend/dist/` es un artefacto
> que no se commitea. Corré `wails3 task build` una vez, o —si solo querés los
> tests de Go— alcanza con `mkdir -p frontend/dist && touch frontend/dist/.gitkeep`.
> Es lo que hace CI antes de compilar.

### Regenerar los bindings

Los bindings son el contrato entre Go y TypeScript. Se regeneran solos en
`wails3 task build`, pero si agregás o cambiás un servicio de Go y querés que el
frontend lo vea sin buildear todo:

```sh
wails3 generate bindings -clean=true -ts -i
```

No se commitean: están en `.gitignore`.

---

## Estructura

Módulo Go: `github.com/LucianoR23/kanamedb`.

```
main.go              Punto de entrada. Registra servicios y crea la ventana.
build/               Assets e íconos por plataforma, Taskfiles de build.
  config.yml         Metadata del producto (nombre, versión, identificador).
frontend/
  src/
    styles/          Tokens de diseño. La única fuente de color de la app.
    components/ui/   Componentes base de S00. Nada de elementos nativos.
    components/erd/  Nodo y arista del diagrama. DOM, no canvas: usan los tokens.
    screens/         Pantallas Sxx.
    lib/             Utilidades chicas.
  bindings/          Generado por Wails. No se commitea.
  .npmrc             Filtro de supply chain. Leer antes de tocar.
internal/            Paquetes de Go. Cada servicio expuesto al frontend vive acá.
design/             Artboards bajados de Claude Design. No se commitea.
scripts/             sockets.ps1: lo que el binario abre de verdad cuando corre.
.gitleaks.toml       Reglas de gitleaks: las de fábrica más las DSN y `password = "…"`.
staticcheck.conf     Qué comprobaciones corre staticcheck y por qué falta una.
.github/dependabot.yml  Un PR por dependencia con versión nueva; nunca mergea solo.
firma-de-codigo.md   Cómo firmar el .exe gratis (SignPath Foundation), para cuando toque.
kaname-android.md    Qué sería un Kaname para Android y por qué todavía no.
.github/workflows/   CI: lint, typecheck, secretos en el historial, integración,
                     builds de los tres sistemas y release en borrador con tag.
CLAUDE.md            Convenciones de código y reglas de seguridad.
kaname-plan.md       Plan por iteraciones y registro de decisiones.
```

## Stack

**Go 1.26** · **Wails v3** (ventana nativa, sin servidor HTTP) ·
**React 19** con React Compiler · **TypeScript 7** · **Vite 8** · **pnpm** ·
CSS Modules sobre variables CSS · Inter y JetBrains Mono autohospedadas

Más [pgx](https://github.com/jackc/pgx) para PostgreSQL,
[CodeMirror 6](https://codemirror.net/) para el editor SQL y
[TanStack Table](https://tanstack.com/table) + [Virtual](https://tanstack.com/virtual)
sobre una grilla de CSS Grid propia. El canvas del ERD usa
[xyflow](https://reactflow.dev/) con [dagre](https://github.com/dagrejs/dagre)
para el auto-layout.

La introspección del esquema es SQL propia contra el catálogo, no
[Atlas](https://atlasgo.io/). Atlas se evalúa para el *diff* de esquemas en la
Iteración 5: hoy no sabe leer tres features de PostgreSQL 18 y una de ellas falla
en silencio. El detalle, con el programa que lo comprueba, está en la sección 6
de [`kaname-plan.md`](kaname-plan.md).

## Dependencias: dos cosas para saber

**No agregues paquetes sin justificar qué ganan.** El binario se distribuye
copiando y pegando; cada dependencia se paga en tamaño y en superficie de ataque.

**`frontend/.npmrc` rechaza paquetes publicados hace menos de 7 días**
(`minimum-release-age`). Es defensa contra releases comprometidas. Si pnpm te
bloquea una versión, bajá a la anterior elegible en vez de desactivar el filtro.
La única excepción es `@wailsio/runtime`, que tiene que coincidir exacto con la
versión del módulo Go.

## Plataformas

Windows x64 y arm64, Linux x64 y macOS universal (arm64 + x86_64). Linux y
macOS necesitan cgo, así que no se cross-compilan desde Windows: los construye
CI. Linux pide GTK4 + WebKitGTK 6.0 (Ubuntu 24.04 / Debian 13 o más nuevos);
macOS, 12 o más nuevo. Android (arm64, API 30+) es un spike: se construye en
CI y se instala a mano; ver `kaname-android.md`.

## Releases

Un tag `v*` hace que CI construya los tres sistemas y deje un **release en
borrador** con seis archivos y su `SHA256SUMS`. El tag tiene que ser la versión
de `internal/appinfo/appinfo.go` —el job `release` lo comprueba— y se hace
sobre un commit que ya está en `main` y en verde:

```sh
# git tag -a v0.1.0 -m "Kaname 0.1.0"
# git push origin v0.1.0
# Después del push de main en verde. 0.1.0 y no 1.0.0 porque en semver 1.0.0 promete una API estable, y ni Wails v3 salió de beta ni el formato de la libreta está congelado: el 0.x avisa que puede cambiar.
```

Lo que deja:

| Archivo | Qué es |
|---|---|
| `kaname-win-x64.exe`, `kaname-win-arm64.exe` | Un ejecutable suelto. Se copia y anda; WebView2 viene con Windows 11. |
| `kaname-linux-x64.AppImage` | Se copia, se le da permiso de ejecución y anda. |
| `kaname-linux-x64.deb`, `kaname-linux-x64.rpm` | Instalan en `/usr/bin` con el `.desktop` y los íconos, y declaran las dependencias. |
| `kaname-mac-universal.zip` | `Kaname.app`, arm64 + x86_64. |

El borrador se publica a mano después de mirar las notas. **Nada está
firmado**, y eso tiene una consecuencia por sistema:

- **Windows**: SmartScreen avisa la primera vez —*Más información → Ejecutar de
  todos modos*—. Comprobá el `SHA256SUMS` antes.
- **macOS**: desde macOS 15 Gatekeeper bloquea el `.app`; se abre desde
  *Ajustes → Privacidad y seguridad → Abrir de todos modos*. O `xattr -d
  com.apple.quarantine Kaname.app` en la terminal.
- **Linux**: nada que pasar. No hay cadena de confianza equivalente.

Quien clona el repo y compila no ve ninguno de esos avisos. Firmar —USD 99 al
año en macOS, un certificado OV en Windows— está evaluado en `kaname-plan.md` y
no decidido.

## Diseño

Las 26 pantallas viven en un proyecto de Claude Design, no en el repo. Se bajan
con `DesignSync` al directorio `design/`, que está en `.gitignore`: una copia
commiteada se desactualiza y termina mintiendo.

Reglas que salen de S00 Foundations y valen para toda la UI:

- **Todo lo que entra o sale de una base va en mono.** Identificadores, tipos,
  valores, SQL, tiempos, rutas. Lo que dice la app va en Inter.
- **Los colores salen de los tokens.** Ningún componente escribe un literal. Si
  falta un color, se agrega a `tokens.css`.
- **Nada de elementos nativos.** `<select>` y compañía ignoran el tema en
  Windows. Se usan los componentes de `components/ui`.
- **NULL y la cadena vacía se ven distinto.** `[null]` en itálica y gris; la
  cadena vacía, una celda en blanco. Confundirlos es un bug de datos esperando.

## Licencia

[Apache 2.0](LICENSE). Con concesión explícita de patentes y sin que un fork
pueda usar el nombre «Kaname» como propio. El aviso de copyright va en
[`NOTICE`](NOTICE).
