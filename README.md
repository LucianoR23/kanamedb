# Kaname

Gestor de bases de datos de escritorio con diagrama ERD editable.
Editás el diagrama, Kaname te muestra el SQL que va a correr, y recién ahí lo aplicás.

Motores: **PostgreSQL** (principal), MySQL, MariaDB y SQLite.

> **Estado: Iteración 0 — esqueleto.** El proyecto compila, abre ventana y tiene
> CI, pero todavía no se conecta a ninguna base. Ver [`kaname-plan.md`](kaname-plan.md)
> para el plan completo y el registro de decisiones.

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

El binario queda en `bin/kaname.exe` (~10 MB).

> No uses `go build` directo. Se saltea el tag `production` —que deja el webview
> en modo desarrollo— y el `.syso` con ícono, manifest de DPI y metadata de versión.

### Verificaciones

```sh
gofmt -l .                         # formato de Go
go vet ./...                       # análisis estático
go test ./...                      # tests
cd frontend && pnpm run typecheck  # tipos de TypeScript
```

Es lo mismo que corre CI en cada push.

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

```
main.go              Punto de entrada. Registra servicios y crea la ventana.
build/               Assets e íconos por plataforma, Taskfiles de build.
  config.yml         Metadata del producto (nombre, versión, identificador).
frontend/
  src/               React 19 + TypeScript.
  bindings/          Generado por Wails. No se commitea.
  .npmrc             Filtro de supply chain. Leer antes de tocar.
.github/workflows/   CI: lint, typecheck y build win-x64 + win-arm64.
CLAUDE.md            Convenciones de código y reglas de seguridad.
kaname-plan.md       Plan por iteraciones y registro de decisiones.
```

## Stack

**Go 1.26** · **Wails v3** (ventana nativa, sin servidor HTTP) ·
**React 19** con React Compiler · **TypeScript 7** · **Vite 8** · **pnpm**

A medida que avancen las iteraciones se suman
[Atlas](https://atlasgo.io/) para introspección y diff de esquemas,
[pgx](https://github.com/jackc/pgx) y demás drivers,
[xyflow](https://reactflow.dev/) para el canvas del ERD,
[CodeMirror 6](https://codemirror.net/) para el editor SQL y
[glide-data-grid](https://grid.glideapps.com/) para la grilla de resultados.

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
