# Kaname

Gestor de bases de datos de escritorio con diagrama ERD editable.
Editás el diagrama, Kaname te muestra el SQL que va a correr, y recién ahí lo aplicás.

Motores: **PostgreSQL** (principal), MySQL, MariaDB y SQLite.

> **Estado: Iteración 5 — ERD de escritura.** Se conecta a PostgreSQL directo o
> a través de un bastión SSH —con verificación de la clave del host y sin abrir
> ningún puerto local—, guarda la libreta de conexiones con los secretos en el
> keychain del sistema operativo, y trae editor SQL con autocompletado, grilla de
> resultados con paginado, estructura completa de cada tabla y diagrama ERD.
>
> **Y ya edita el esquema:** las ediciones se juntan en un changeset, se muestran
> como SQL antes de tocar nada, y se aplican en una transacción con progreso por
> sentencia. Contra producción hay que escribir el nombre de la base. Editar
> datos de las filas llega en la Iteración 7.
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
- **Sin phone-home.** Sin telemetría, sin update checks automáticos.

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

### Verificaciones

Los tests de integración necesitan los motores de prueba, que incluyen un
servidor SSH para el túnel:

```sh
docker compose -f docker-compose.test.yml up -d
```

Levanta PostgreSQL, MySQL y **dos** MariaDB, cada uno en un puerto corrido para
no chocar con una instalación local: **55432**, **53306**, **53307** (MariaDB
12.3) y **53308** (MariaDB 10.11, la LTS más vieja que la aplicación declara
soportar).

Las dos MariaDB a la vez no son exceso de celo. Probando solo la 12.3, la 10.11
no conectaba en absoluto —Kaname pedía `@@transaction_read_only`, que llegó
recién en 11.1.1— y nadie se enteraba. Declarar una versión soportada y no
correr un solo test contra ella es prometer sin comprobar.

**SQLite no está ahí y no hace falta levantarlo**: es un archivo, y sus tests
crean uno nuevo en el directorio temporal de cada caso. Por eso los de SQLite
nunca se saltean, y los otros tres sí cuando el motor no está escuchando. Ese
salteo es a propósito para trabajar sin Docker, pero en CI sería un test verde
que no probó nada: ponerle cualquier valor a `KANAME_REQUIRE_ENGINES` lo
convierte en un fallo.

Los cuatro motores corren **la misma batería** —`internal/engine/enginetest`—,
así que un motor pasa o no pasa contra los mismos 17 casos que los demás:

```sh
go test ./internal/postgres/ ./internal/mysql/ ./internal/sqlite/ -run TestSuite
```

Eso son cinco corridas de la misma batería: Postgres, MySQL, MariaDB 12.3,
MariaDB 10.11 y SQLite.

Por defecto levanta PostgreSQL 18. Para probar contra otra versión de la matriz,
`PG_VERSION` la elige — pero **hay que bajar el stack con `-v` antes de
cambiarla**:

```sh
docker compose -f docker-compose.test.yml down -v
PG_VERSION=17 docker compose -f docker-compose.test.yml up -d --wait
```

`--force-recreate` no alcanza: el contenedor nuevo puede quedarse con el
directorio de datos del anterior y Postgres aborta con *"database files are
incompatible with server"*. El `-v` es lo que lo borra. En CI no aparece porque
cada pata de la matriz corre en una máquina limpia.

`MYSQL_VERSION`, `MARIADB_VERSION` y `MARIADB_LTS_VERSION` hacen lo mismo para
los otros tres. Los defaults son MySQL 9.7, MariaDB 12.3 y MariaDB 10.11.

### Datos para probar a mano

Los tests crean y borran su propio esquema en cada caso, así que la base queda
vacía. Para mirar la aplicación con algo que se parezca a un esquema real:

```sh
docker exec -i kaname-postgres-1 psql -U kaname -d kaname_test < docker/demo.sql
```

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

```sh
gofmt -l .                         # formato de Go
go vet ./...                       # análisis estático
go test -p 1 ./...                 # tests (ver abajo por qué -p 1)
govulncheck ./...                  # CVEs alcanzables desde nuestro código
cd frontend && pnpm run typecheck  # tipos de TypeScript
```

`govulncheck` se instala con
`go install golang.org/x/vuln/cmd/govulncheck@v1.7.0`. Consulta `vuln.go.dev` al
correr; es una herramienta de desarrollo, la aplicación no hace ninguna llamada
de red por su cuenta.

El `-p 1` no es opcional cuando hay base: `go test` corre los binarios de cada
paquete **en paralelo**, y los de `internal/postgres` e `internal/service`
escriben en la misma base de pruebas. Sin serializar, un test que cuenta las
tablas visibles ve las que creó otro paquete y falla de manera intermitente.

Es lo mismo que corre CI en cada push.

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
.github/workflows/   CI: lint, typecheck y build win-x64 + win-arm64.
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

Windows x64 y arm64 se compilan hoy. Linux y macOS necesitan cgo, así que no se
cross-compilan desde Windows: van por CI en la Iteración 9.

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
