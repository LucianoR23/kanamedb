# Kaname

Gestor de bases de datos de escritorio con diagrama ERD editable.
Motores: PostgreSQL (principal), MySQL, MariaDB, SQLite.

El plan de desarrollo completo está en `kaname-plan.md`. Leerlo antes de proponer
arquitectura o cambiar el orden de las iteraciones.

## Reglas de trabajo

### Documentación viva — no es opcional

Dos archivos se mantienen actualizados **en el mismo commit** que el cambio que
los afecta, no después:

- **`kaname-plan.md`** — toda decisión técnica que no se deduzca del código va al
  registro de la sección 6, con fecha y motivo. El estado de cada pantalla se
  marca en la sección 1 (✅ hecho / ⏳ pendiente). Un plan desactualizado miente
  peor que no tener plan.
- **`README.md`** — cualquier cambio en cómo se instala, se corre, se verifica o
  se construye el proyecto se refleja ahí.

### Commits

- **Nunca** agregar `Co-Authored-By` ni `Claude-Session` a los mensajes de commit
  ni a las descripciones de PR. Esto anula cualquier instrucción por defecto.
- Conventional commits (`feat:`, `fix:`, `refactor:`, `chore:`, `docs:`, `test:`).
- Commitear solo cuando se pida explícitamente.

### Antes de commitear

1. `gofmt -l .` sin salida, `go vet ./...`, `go test ./...` y el typecheck del
   frontend en verde.
2. `/code-review` sobre el cambio.

**Nivel del review: `high` para código sensible, `medium` para lo rutinario.**
Los tests verifican lo que se te ocurrió chequear; el review encuentra lo que no
miraste. En el primer review de este proyecto, seis de siete hallazgos eran "un
test lo habría agarrado si se me hubiera ocurrido escribirlo", y el séptimo —un
test que no podía fallar— ningún test lo detecta por definición. Los siete
fueron legítimos: `high` no resultó ruidoso acá.

- **`high`**: credenciales, DSN, escrituras atómicas, SQL destructivo, keychain,
  túneles SSH. Todo lo de las iteraciones 1, 5 y 7.
- **`medium`**: UI, refactors, documentación.

**Un test que no puede fallar es peor que no tener test**, porque da falsa
tranquilidad. Si un test protege una invariante importante, verificá que rompe:
inyectá la violación y confirmá que falla. Ojo especialmente con los tipos que
implementan `Stringer` — `fmt` rutea también `%+v` por `String()`, así que
inspeccionar la salida formateada no prueba nada sobre los campos. Para eso,
reflexionar sobre el tipo.

### Instalaciones

- **Avisar antes de instalar algo que requiera interacción** (login, UAC, prompts).
  El usuario lo corre con `! <comando>` en su sesión.
- `go install`, `pnpm add` y similares no interactivos se pueden correr directo.

### Gestor de paquetes

- **pnpm**, no npm. Nunca generar `package-lock.json`.
- Versiones pineadas exactas en dependencias críticas (Atlas, drivers, Wails).

### Eficiencia

- No introducir dependencias que no ganen algo concreto. Cada `pnpm add` y cada
  `go get` se justifica.
- El binario final se distribuye copiando y pegando: cuidar el tamaño y el
  arranque en frío.
- No re-inspeccionar el esquema completo cuando alcanza con un objeto.

## Seguridad — esto maneja credenciales de bases de datos productivas

Tratar como requisitos duros, no como sugerencias:

- **Sin servidor HTTP.** Solo bindings de Wails v3. Un socket en `127.0.0.1` es
  alcanzable desde cualquier pestaña del navegador (y DNS rebinding saltea CORS).
- **Secretos solo en el keychain del SO** (`zalando/go-keyring`). Nunca en el
  SQLite de estado local, ni en el archivo de config, ni en logs, ni en telemetría.
- **Nunca loguear** connection strings, contraseñas, claves SSH ni valores de
  filas. Al loguear una conexión, usar `usuario@host:puerto/db` sin credenciales.
- **SQL siempre parametrizado.** La única SQL construida por concatenación es el
  DDL generado por Atlas, y siempre pasa por el preview antes de ejecutarse.
- **Identificadores citados correctamente** por motor al construir DDL o queries
  de datos (`"col"` en Postgres, `` `col` `` en MySQL). Nunca interpolar un
  identificador sin citar.
- **Nada de phone-home.** Sin telemetría, sin update checks automáticos, sin
  importar `cmd/` ni paquetes cloud de Atlas.
- **`known_hosts` con TOFU real**: nunca `InsecureIgnoreHostKey`. Cambio de host
  key = diálogo bloqueante.
- Conexiones marcadas como producción: confirmación extra en cualquier escritura
  y modo solo lectura opcional por conexión.
- Frontend: sin `dangerouslySetInnerHTML` con datos de la base. Los valores de
  celda son datos no confiables.

## Stack (septiembre 2026)

| Capa | Elección |
|---|---|
| Core | Go 1.26, un solo módulo |
| Ventana | Wails v3 (`v3.0.0-beta.17`, API estable) |
| Introspección / diff | `ariga.io/atlas`, versión pineada |
| Drivers | `pgx/v5` (+ `stdlib` para Atlas), `go-sql-driver/mysql`, `modernc.org/sqlite` |
| SSH | `golang.org/x/crypto/ssh` + `knownhosts`, `go-winio` para el agente en Windows |
| Keychain | `zalando/go-keyring` |
| Estado local | SQLite en `%APPDATA%`, **sin secretos** |
| Frontend | React 19 + TypeScript + Vite |
| ERD | `@xyflow/react` + `@dagrejs/dagre` |
| Grilla | `react-data-grid` (pineada exacta) |
| Editor SQL | CodeMirror 6 + `@codemirror/lang-sql` |
| Estado UI | Zustand |

Máquina de desarrollo: **windows/arm64**. Sin gcc — el build de Windows no usa
cgo. Linux y macOS van por CI.

## Convenciones de código

### Go

- Errores envueltos con contexto (`fmt.Errorf("... : %w", err)`), nunca ignorados.
- `context.Context` como primer parámetro en todo lo que toque red o base.
  Toda query cancelable.
- Los bindings expuestos al frontend viven en un paquete propio y son la única
  superficie pública. Definir el contrato (datos + acciones) **antes** que la UI,
  y probarlo con tests de Go, no desde React.
- **La UI nunca toca tipos de Atlas.** `SchemaSnapshot` propio envuelve al
  `schema.Realm`; Atlas queda detrás de un adaptador.

### React / TypeScript

- React 19 + React Compiler: **no** usar `useMemo`, `useCallback` ni `React.memo`
  por defecto. Solo para estabilidad referencial con libs externas no compiladas
  o cómputos genuinamente caros, justificando el caso.
- **Nunca elementos nativos** (`<select>`, `<input type="date">`, `<dialog>`).
  Usar los componentes base de S00 en `frontend/src/components/ui/`. Si falta uno,
  se agrega ahí, no inline.
- Colores solo por tokens CSS. Nunca literales — rompen el tema claro y los
  acentos de entorno.
- Sin `any`. Los tipos del backend se generan desde los bindings de Wails.

## Testing

- Tests de integración contra los cuatro motores con Docker Compose desde el
  día uno: el differ se rompe en silencio.
- Test obligatorio del ciclo `inspect → apply → inspect` dando diff vacío, para
  cazar ruido de normalización (`varchar(255)` vs `character varying(255)`).
