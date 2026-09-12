# Kaname en Android

Anotado el 2026-09-11 para no pensarlo de nuevo desde cero. **Decisión: no
ahora.** Es un segundo producto sobre el mismo núcleo, no una adaptación, y
suma un frontend que cada cambio del núcleo tiene que seguir soportando. Lo que
sigue es qué viaja, qué no, cómo tendría que ser para que valga la pena, y en
qué orden se haría el día que haya un motivo concreto.

No sería una app de Play: un APK firmado con clave propia, instalado a mano.

## Qué viaja tal cual

El núcleo de Go es portable, y no por casualidad: la regla de que **la UI nunca
toca tipos del driver** hace que `schema.Snapshot`, `query.Result` y
`change.Change` sean todo lo que cruza el puente, y al backend no le importa
qué lo dibuja.

- Los cuatro drivers son Go puro (`pgx`, `go-sql-driver/mysql`,
  `modernc.org/sqlite`); compilan a `android/arm64`.
- El túnel SSH (`x/crypto/ssh`) y `known_hosts` con TOFU: iguales. Lo único de
  Windows, el agente con `go-winio`, ya está detrás de build tags
  (`agent_windows.go` / `agent_unix.go`).
- La libreta de conexiones, las preferencias, el historial y las guardadas
  son TOML y JSON en el directorio de la app: `appinfo.PathsIn` con otras dos
  raíces y listo. Exportar e importar conexiones —sin secretos— es cómo
  pasarían de la PC al teléfono.
- Las reglas de seguridad no cambian: sin sockets, sin phone-home, SQL
  parametrizada, confirmación en producción. Los tests estructurales de la
  raíz (`sockets_test.go`, `logs_test.go`) correrían igual.

## Qué no viaja

1. **El keychain.** `go-keyring` no tiene backend para Android: devuelve
   `ErrUnsupportedPlatform`, y la app —comprobado el 2026-09-11— se niega a
   guardar una conexión si no puede guardar su contraseña. Hay que escribir
   un backend nuevo del paquete `secrets` sobre el **Android Keystore**, por
   JNI. Es lo primero y lo más delicado: es donde vive la contraseña.
2. **La interfaz.** Kaname está diseñado para 1440×900, mínimo 1024×640, con
   mouse y teclado: el ERD con arrastre y aristas, la grilla editable celda
   por celda, CodeMirror con sus atajos. En un teléfono eso no se adapta; se
   rehace. Ver «Alcance».
3. **La infraestructura de build.** Wails v3 beta.17 trae soporte de Android
   (`application_android.go`), pero **exige cgo** (`//go:build android &&
   cgo`), y con eso Android SDK, NDK, Java y Gradle. Es la única plataforma
   del proyecto que necesitaría cgo. Es experimental: el riesgo de pelearse
   con el framework más que con el código propio es real, y el template de
   Android se sacó del repo en la iteración 0.
4. **El WebView** pasa a ser el Android System WebView, que actualiza Play.
   Igual que WebView2, no es nuestro y reporta a su dueño por su cuenta.

## Alcance: acotado por la pantalla, no todas las funciones

Un teléfono no es un lugar para editar un esquema. El Android que tiene
sentido es **leer, consultar y corregir filas**: lo que se hace con el pulgar
es cambiar un valor, no mover una tabla.

| Pantalla | Entra | Cómo |
|---|---|---|
| S01/S02 Conexiones | Sí | Lista, conectar, importar desde el archivo exportado en la PC. Sin editor completo: los campos del túnel y TLS se importan, no se escriben con el pulgar. |
| S04 Clave del host SSH | Sí | El mismo diálogo TOFU; es donde más importa no simplificar. |
| S05 Árbol del esquema | Sí | Tablas, columnas, índices, claves. Solo lectura. |
| S06 Editor SQL | Sí, reducido | Un área de texto con el historial a mano; los resultados como lista de tarjetas (una fila = una tarjeta), no una grilla. Paginado del núcleo tal cual. Acepta escrituras escritas a mano con la misma confirmación de producción que en escritorio. |
| Edición por fila | Sí | Tocar una tarjeta, editar un campo, confirmar: `UPDATE`, `INSERT` y `DELETE` de una fila por vez, por `Stage`/`Apply` del núcleo con el preview y la confirmación de escritorio. Sin edición masiva. |
| S21 Historial | Sí | Igual: filtra, repite. |
| S23 Ajustes | Mínimo | Tema, bloqueo, borrar historial. |
| ERD (lectura y edición), aplicar DDL, importar CSV, volcados, comparar esquemas, dumps | **No** | Son interacciones de escritorio, y un cambio de esquema no va en un dispositivo que se pierde. |

Regla de partida: **datos sí, esquema no, y lo decide el backend.** Un
`change.Change` de `KindSchema` se rechaza en `Stage` cuando la app corre en
Android, no se esconde en la UI: no hay forma de que la interfaz ofrezca lo que
el núcleo no acepta. El solo lectura por conexión y la confirmación de
producción siguen valiendo tal cual. Decidido el 2026-09-12; antes era «solo
lectura forzado».

## Seguridad: biometría, bien hecha

Wails beta.17 expone `BiometricAuthenticate(reason)`, que muestra el
`BiometricPrompt` y devuelve si la persona pasó. **Eso solo es una puerta de
UI**: si la contraseña está en un archivo, quien tiene el archivo no necesita
pasar por la puerta. Para que la biometría proteja algo, el secreto tiene que
ser **indescifrable sin ella**:

1. Una clave AES en el **Android Keystore** —hardware-backed donde el equipo
   lo tenga— creada con `setUserAuthenticationRequired(true)` y atada a
   biometría fuerte (`BIOMETRIC_STRONG`), con PIN del equipo como respaldo si
   se decide así.
2. Las contraseñas de las conexiones se cifran con esa clave y se guardan
   cifradas. Descifrar exige un `BiometricPrompt` con `CryptoObject`: el
   sistema no entrega la clave si la persona no pasó.
3. Ese es el backend Android del paquete `secrets`: mismo contrato `Set`,
   `Get`, `Has`, `Delete`, y el test que ya existe —«los secretos van al
   keychain y a ningún archivo»— tiene que pasar leyendo el directorio de la
   app byte a byte: lo que haya en disco es texto cifrado, y el centinela no
   aparece.
4. La clave privada SSH no es una ruta a un archivo del teléfono: es
   contenido, y va cifrado igual que una contraseña.
5. Bloqueo de la app al pasar a segundo plano, `FLAG_SECURE` para que la
   ventana no salga en capturas ni en el selector de apps, y portapapeles con
   los valores de fila: copiar sigue siendo del usuario, pero se avisa.

Lo que eso NO cubre, y hay que decirlo en el README de esa app: un teléfono
rooteado, y una persona a la que se le fuerza el dedo. Es el mismo límite que
tiene cualquier gestor de contraseñas en el mismo aparato.

## Lo que cambia del modelo de amenaza

- **El aparato se pierde.** La libreta no tiene secretos; el Keystore no
  entrega la clave sin biometría. Es la razón de la sección anterior.
- **La red no es la de la oficina.** Conectarse a producción desde datos
  móviles: `sslmode=verify-full` recomendado por defecto, túnel SSH cuando
  se pueda, y la confirmación de producción con el mismo peso que en escritorio.
- **La pantalla es pública.** En el tren, cualquiera lee una fila. Es un
  argumento para no mostrar valores hasta tocar, y para que cada escritura
  pase por su confirmación aunque sea de una sola fila.

## Infraestructura y esfuerzo

- SDK, NDK, JDK, Gradle; cgo; una clave de firma propia (`keytool`), que para
  instalar a mano alcanza y que hay que guardar como cualquier secreto.
- Un job de CI opcional en `ubuntu-latest` con el SDK; los tests de
  integración no cambian.
- Pruebas en dispositivo: un emulador no tiene Keystore con hardware.
- Estimación honesta: **semanas**, repartidas así, y en este orden porque
  cada paso puede matar el siguiente:
  1. Un «hola mundo» de Wails Android compilado y corriendo en el teléfono,
     para saber si el framework aguanta antes de escribir una línea propia.
  2. El backend `secrets` con Keystore + biometría, con su test.
  3. `appinfo.PathsIn` con las raíces de Android e importar conexiones.
  4. El frontend móvil: conexiones, árbol, consulta con tarjetas, historial,
     edición por fila desde la tarjeta.
  5. El candado de esquema en `Stage` con su test, y las pruebas a mano
     —leer y corregir una fila— contra los cuatro motores.

## Cuándo

Cuando haya un uso concreto que hoy no se puede hacer —«mirar una tabla desde
el teléfono» dicho por alguien que lo necesita— y después del 1.0.0 del
escritorio. Hasta entonces, lo único que este documento pide es lo que el
proyecto ya hace: que nada de escritorio se meta en el núcleo.
