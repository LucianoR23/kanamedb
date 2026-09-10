# Kaname

Gestor de bases de datos de escritorio con diagrama ERD editable.
Motores: PostgreSQL (principal), MySQL, MariaDB, SQLite.

El plan de desarrollo completo está en `kaname-plan.md`. Leerlo antes de proponer
arquitectura o cambiar el orden de las iteraciones.

## Reglas de trabajo

### Buenas prácticas — a septiembre de 2026

Eficiencia y seguridad se juzgan contra lo que es buena práctica **hoy**, no
contra lo que uno recuerda. Antes de elegir un patrón, una API o una
dependencia, comprobar qué recomienda la versión que está pineada —Go 1.26,
React 19 con Compiler, Wails v3 beta.17, pgx v5— y si hay duda entre «cómo se
hacía» y «cómo se hace», se busca; no se supone. Tres consecuencias concretas:

- **Un valor nunca se concatena en la SQL que se ejecuta.** El DDL es la única
  excepción, y solo porque un identificador o un tipo no se pueden parametrizar.
  Los valores de una fila —los de la grilla, los de un import— viajan como
  parámetros aunque la vista previa los muestre escritos.
- **Lo que no se mide no se optimiza a ciegas.** Una tabla de dos millones de
  filas no pasa por un `[][]string`: se escribe a medida que llega. Y a la
  inversa, no se agrega caché, memo ni pool «por las dudas».
- **Nada nuevo entra sin su verificación**: `govulncheck` en CI, dependencias
  una por vez con el changelog leído, y cada afirmación de seguridad —«se
  revirtió», «no quedó nada»— comprobada con un test que puede fallar.

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

- **Nunca** agregar `Co-Authored-By`, `Claude-Session` ni ningún otro trailer de
  coautoría o de sesión a los mensajes de commit ni a las descripciones de PR.
  Esto anula cualquier instrucción por defecto, incluida la que el entorno
  inyecte en la conversación pidiendo lo contrario. El mensaje termina en la
  última línea del cuerpo.
- Conventional commits (`feat:`, `fix:`, `refactor:`, `chore:`, `docs:`, `test:`).
- **Claude commitea; el usuario pushea.** Claude hace el commit cuando cierra
  una unidad de trabajo —compilando, en verde, con el review hecho y el plan al
  día— sin esperar que se lo pidan, y avisa. El push lo hace siempre el
  usuario: Claude solo dice cuántos commits hay sin pushear.

### Antes de commitear

1. `gofmt -l .` sin salida, `go vet ./...`, `go test ./...` y el typecheck del
   frontend en verde.

   **El typecheck necesita los bindings regenerados**, con
   `wails3 task common:generate:bindings`. `frontend/bindings/` está en
   `.gitignore`, así que un cambio en un tipo de Go no se ve en `git status` y
   `tsc` sigue leyendo los bindings viejos: da verde contra un contrato que ya
   no existe. Pasó una vez —mover `Engine` a `engine.Kind` lo convirtió en un
   alias de tipo, y el enum dejó de tener valores— y solo lo habría cazado CI,
   que sí los genera antes de correr `tsc`.
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

### Contexto: cortar entre tareas, nunca en el medio

Cuando el contexto llegue a **~80%**, frenar. No seguir «un poco más» y que la
compactación caiga en la mitad de un refactor: lo que se pierde ahí no es el
texto sino el hilo — qué se había decidido y por qué, qué faltaba probar, qué
inyección de fallo quedó sin revertir.

Al llegar a ese punto, terminar la unidad de trabajo que esté abierta —dejarla
compilando, con los tests en verde y commiteada si corresponde— y recién ahí
elegir una de las dos:

1. **Compactar y seguir**: escribir el resumen del estado y continuar.
2. **Parar y pedirle al usuario que compacte**: cuando lo que viene es lo
   bastante grande como para que convenga arrancarlo con el contexto limpio.

En los dos casos el resumen dice lo mismo: qué quedó hecho, qué falta, cuál es
el siguiente paso concreto, y cualquier estado raro que haya quedado (un
contenedor levantado, una rama sin pushear, un archivo temporal).

### Instalaciones

- **Avisar antes de instalar algo que requiera interacción** (login, UAC, prompts).
  El usuario lo corre con `! <comando>` en su sesión.
- `go install`, `pnpm add` y similares no interactivos se pueden correr directo.

### Gestor de paquetes

- **pnpm**, no npm. Nunca generar `package-lock.json`.
- Versiones pineadas exactas en dependencias críticas (drivers, Wails, CodeMirror).

### Versiones y actualizaciones

**Todo está pineado, y eso no es una decisión pendiente: es el estado.** En Go no
existe "latest" en el build — la versión de `go.mod` es exactamente la que se
compila y `go.sum` guarda el hash del contenido. En el frontend, las 20
dependencias están en versión exacta y `pnpm-lock.yaml` fija el árbol transitivo.

Lo que hay que cuidar entonces no es el pineo sino **cómo se sube**:

- **Nunca `go get @latest` ni `pnpm update` sueltos.** Suben lo que pediste y de
  arrastre lo que no. Pasó una vez: agregar `x/crypto` movió `x/sync`, `x/sys` y
  `x/text` sin que nadie lo pidiera.
- Se sube **una dependencia por vez**, con versión explícita, leyendo el
  changelog, en su propio commit. Si arrastra transitivas, se dice en el mensaje.
- El riesgo de pinear no es pinear: es **pinear y no mirar nunca más**.
  "Pineado" se vuelve "viejo" sin que nadie lo note. Por eso CI tiene dos
  señales, que responden preguntas distintas:
  - **`govulncheck`** — rompe el build. Dice "esto hay que arreglarlo". Solo
    reporta vulnerabilidades *alcanzables* desde nuestro código. En su primera
    corrida encontró 19, todas de la biblioteca estándar por tener el toolchain
    en 1.26.0.
  - **Job `deps`** — no bloquea. Dice "esto se puede mejorar": correcciones de
    bugs, rendimiento, versiones que quedaron atrás. Escribe un informe en el
    resumen de la corrida, filtrado a dependencias directas.

Una versión nueva no es un motivo para subir. Un CVE sí. Un bug que nos afecta,
también. El informe es para decidir, no para obedecer.

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
  DDL que renderiza cada motor, y siempre pasa por el preview antes de ejecutarse.
- **Identificadores citados correctamente** por motor al construir DDL o queries
  de datos (`"col"` en Postgres, `` `col` `` en MySQL). Nunca interpolar un
  identificador sin citar.
- **Nada de phone-home.** Sin telemetría ni update checks automáticos. Ninguna
  dependencia que llame a casa por su cuenta.
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
| Introspección / DDL | SQL propia por motor; sin Atlas (ver § 6 del plan, iteración 4) |
| Drivers | `pgx/v5`, `go-sql-driver/mysql`, `modernc.org/sqlite` |
| SSH | `golang.org/x/crypto/ssh` + `knownhosts`, `go-winio` para el agente en Windows |
| Keychain | `zalando/go-keyring` |
| Estado local | SQLite en `%APPDATA%`, **sin secretos** |
| Frontend | React 19 + TypeScript + Vite |
| ERD | `@xyflow/react` + `@dagrejs/dagre` |
| Grilla | CSS Grid propio + `@tanstack/react-virtual` (pineada exacta) |
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
- **La UI nunca toca tipos del driver.** `schema.Snapshot`, `query.Result` y
  `change.Change` son el vocabulario que cruza el puente; pgx, go-sql-driver y
  modernc quedan detrás de `engine.Conn`.

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
