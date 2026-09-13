# Kaname en Android

Anotado el 2026-09-11 para no pensarlo de nuevo desde cero, cuando la
decisión era «no ahora». El 2026-09-12 se decidió arrancar —el motivo
concreto: corregir un valor desde el teléfono— y el paso 1, el spike, pasó su
puerta el mismo día (ver «Estado»). Sigue siendo un segundo producto sobre el
mismo núcleo, no una adaptación, y suma un frontend que cada cambio del núcleo
tiene que seguir soportando. Lo que sigue es qué viaja, qué no, cómo tiene
que ser para que valga la pena, y en qué orden se hace.

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

## Estado del paso 1 (2026-09-12): pasó

**El APK corre en un teléfono real.** Arranca, carga el frontend de escritorio
—apretado, como se esperaba—, la lista de conexiones aparece vacía y guardar
una conexión sin contraseña funciona: los bindings responden y la libreta se
escribió en el directorio privado de la app. La única pelea con el framework
fue de CI: el CLI `wails3` en Linux se compila con cgo contra GTK4/WebKitGTK
aunque el objetivo sea Android, y la plantilla genera los bindings con `-tags
android` en el host, donde el cgo de Android no compila; las dos cosas se
arreglaron en el workflow y en el Taskfile. El `.so` compiló con el NDK y
Gradle 9.2.1 + AGP 8.7.3 armaron el APK a la primera, en 5m21s de CI.

Tamaños del build de **debug** (`gcflags=-l`, sin `-w -s`): `libwails.so` 31 MB,
APK 45 MB. El workflow pasó a producción (`-trimpath -ldflags="-w -s"`), que
es lo que se distribuiría; el escritorio en producción pesa ~14 MB.

Lo que quedó hecho sin escribir una línea de UI:

- **Compila a `android/arm64` sin cgo** (`GOOS=android GOARCH=arm64
  CGO_ENABLED=0 go build ./...`) sin tocar nada: el núcleo era portable de
  verdad. Con cgo hace falta el NDK, y eso solo pasa en CI.
- **`build/android/`** viene de los assets embebidos en `wails3` (no hay
  comando que los emita; se copiaron del module cache) con estos cambios: el
  `applicationId` es `dev.kaname.app` —el paquete Java sigue siendo
  `com.wails.app` porque los símbolos JNI del `.so` llevan ese nombre—,
  `minSdk 30`, solo `arm64-v8a`, sin los permisos de cámara, ubicación,
  notificaciones y servicio en primer plano que la plantilla pide, y
  `allowBackup=false`. El `main_android.go` de la plantilla no se commitea:
  lo genera `wails3 android overlay:gen` fuera del árbol.
- **`appinfo` en Android** resuelve las rutas con
  `application.Mobile.StoragePath()` (`getFilesDir()`), en un archivo con
  build tag; en escritorio `appinfo` sigue sin saber de Wails. Sin eso la app
  moría en `main()`: `os.UserConfigDir` da `/sdcard/.config`, que desde API
  30 no se puede escribir.
- **Workflow `android`** en `ubuntu-latest`: JDK 17, NDK `26.3.11579264`
  pineado, `android:build` + `android:assemble:apk`, el APK como artifact.
- La versión del APK entra en `TestLaVersionEsLaMismaEnTodosLados`.
- Los dos abortos de `main()` pasan de `log.Fatalf` a `panic`: en la `.so`,
  `log` escribe al fd 2 —que no va a logcat— y `os.Exit` mata el proceso sin
  rastro; un panic lo manda el runtime a logcat con la pila.

Pendientes que el spike arrastra a propósito, para no pelearse con la
plantilla antes de saber si sirve:

- Los `mipmap-*` son el ícono de Wails. Los del brand kit se generan en el
  paso 4, cuando haya frontend móvil.
- `WailsForegroundService.java` y el código de cámara y ubicación de
  `WailsBridge.java` quedaron aunque el manifest ya no los declara: Kaname no
  los llama. En 2b se decidió **no** forkear el bridge —el vault es una clase
  aparte—, así que se quedan tal cual vienen de la plantilla.

## Estado del paso 2a (2026-09-12): hecho, falta probarlo en el teléfono

`secrets` ya no habla con `go-keyring` directamente: `Keyring` valida y
envuelve errores, y delega en un `almacen` de tres métodos. En escritorio el
almacén es `go-keyring` (`almacen_desktop.go`); en Android es
`application.Mobile.SecureSet/Get/Delete` (`almacen_android.go`), que Wails
implementa con `EncryptedSharedPreferences` y una clave AES del Keystore. El
adaptador (`almacen_movil.go`) no lleva build tag: se prueba en Windows con un
bridge falso —contrato completo, contraseñas hostiles, aislamiento entre
servicios y fallas del bridge sin filtrar la contraseña—. Wails tiene un solo
espacio de claves, así que el servicio va dentro de la clave con prefijo de
longitud: con un separador a secas, `("a", "b:c")` y `("a:b", "c")` serían la
misma entrada, y hay un test que lo demuestra.

2a duró un commit: dejó la interfaz `almacen` y el adaptador con sus tests,
que 2b reutiliza tal cual. El backend de Wails (`SecureSet/Get/Delete`, sin
biometría) ya no se usa.

## Estado del paso 2b (2026-09-12): escrito, falta el teléfono

El vault es propio y el bridge de Wails queda intacto —la opción (b)—:

- **`KanameVault.java`** (`dev.kaname.vault`): una clave AES-256 en el
  Keystore —StrongBox si hay, TEE si no— con `setUserAuthenticationRequired`
  y `setUserAuthenticationParameters(0, AUTH_BIOMETRIC_STRONG)`: **por uso**,
  cada `Cipher.init` exige pasar por un `BiometricPrompt` con `CryptoObject`.
  Solo biometría fuerte; el PIN del equipo no vale como respaldo. Una huella
  nueva invalida la clave (`setInvalidatedByBiometricEnrollment`); al
  detectarlo se borran la clave y todo lo cifrado, con un mensaje que lo dice.
  Lo que queda en disco (`shared_prefs/kaname_vault.xml`) es IV + texto
  cifrado en base64, con la clave de la entrada como AAD para que un blob no
  se pueda mover de una conexión a otra. `has` no descifra.
- **`KanameApp.java`**: la `Application`, declarada en el manifest. Le da al
  vault el `Context` y la activity en primer plano; es todo lo que hace.
- **`vault_android.go`** (`android && cgo`): el único JNI propio del proyecto.
  Define `JNI_OnLoad` —Wails no la tiene—, que se queda con la `JavaVM` y
  resuelve la clase al cargar la `.so`, así ningún Java tiene que llamar a Go.
  Todo cruza como `byte[]`, no como `String`: `NewStringUTF` usa UTF-8
  modificado y rompe con caracteres fuera del BMP (un emoji en la
  contraseña). El buffer del secreto se pisa con ceros antes de liberarse, de
  los dos lados. Implementa `almacenSeguro`, así que la composición de claves
  y sus tests son los mismos que con el bridge falso.
- **`Has` ya no lee**: el contrato `almacen` tiene `hay`, y hay un test que
  cuenta lecturas. Sin eso, abrir la lista de conexiones pediría el dedo una
  vez por conexión.

Del review `high` de este paso salieron cinco cosas, todas arregladas: `set`
ya no rehace la clave en silencio cuando la biometría cambió —falla con el
mismo mensaje que `get`, porque «guardado» taparía que el resto de las
contraseñas acaba de desaparecer—; el texto en claro se pisa con ceros también
si la persona cancela el prompt; si no hay clave pero sí entradas viejas, se
limpian antes de crear la nueva (nadie las iba a poder leer); la activity se
retiene hasta que se destruye, no hasta que se pausa (el propio prompt pausa la
activity en algunos equipos, y la segunda lectura —la del bastión— llegaría sin
activity); y ProGuard mantiene `KanameVault`, que se resuelve por `FindClass`
y no lo referencia nadie.

Cuántas veces pide el dedo, con el modelo por uso: conectar, una (dos si hay
túnel con contraseña de bastión). Guardar una conexión nueva, una. Reemplazar
una contraseña, dos: `recordarSecreto` lee la anterior para poder deshacer.
Si molesta, el knob es `setUserAuthenticationParameters(segundos, …)`: una
ventana de validez tras la autenticación, en vez de por uso. Se decide con el
teléfono en la mano, no antes.

Probado en el teléfono el 2026-09-12: guardar con contraseña pide el dedo
una vez, reabrir y conectar pide el dedo y conecta, cancelar da «Cancelado.».
**2b pasó.** Queda como comprobación opcional, con un build de debug: `adb
shell run-as dev.kaname.app cat shared_prefs/kaname_vault.xml` tiene que
mostrar base64 de IV y texto cifrado, nunca la contraseña.

## Estado del paso 3 (2026-09-12): escrito, falta el teléfono

Las rutas ya estaban desde el spike (`paths_android.go`). Lo que sumó este
paso:

- **Importar conexiones** no necesitó código: `ImportConnections` existe y el
  `Dialogs.OpenFile` de Wails en Android abre el selector del sistema y copia
  el archivo a la caché con una ruta real. Se verifica en el teléfono.
- **La clave privada SSH como contenido.** `tunnel.Secrets.PrivateKey`: si
  viene, se usa en lugar de leer `KeyPath`; hay un test de punta a punta que
  conecta con contenido contra una ruta inexistente y comprueba que sin
  contenido esa misma ruta falla. En el servicio, `SSHKeySecretID(id)` es el
  tercer secreto de una conexión —al lado de la contraseña y la frase de
  paso—: `Connections.SetSSHKey(id, pem)` valida con `ssh.ParseRawPrivateKey`
  (cifrada o no; lo que no parsea no llega al keychain), `ClearSSHKey` lo
  quita, la vista tiene `hasSSHKey`, `Delete` lo borra con los otros dos, y
  `Connect` y `Test` lo usan por el mismo `secretosDelTunel`. No es exclusivo
  de Android: el backend es uno; en escritorio el keychain tiene un tope de
  1024 bytes (Credential Manager) y el vault 16 KiB, cada almacén declara el
  suyo. Una ed25519 entra en los dos; una RSA de 4096 solo en el teléfono.
- **`FLAG_SECURE`** en `KanameApp.onActivityCreated`: la ventana no sale en
  capturas, grabaciones ni en la miniatura del selector de apps.
- **Bloqueo en segundo plano** (`movil_android.go`): con la activity parada
  más de dos minutos, la sesión se cierra. Volver y reconectar pide la
  contraseña al vault, y el vault pide el dedo: ese es el bloqueo, sin una
  pantalla que lo simule. `ActivityStopped` y no `Paused`, porque el propio
  `BiometricPrompt` pausa la activity. Dos minutos es una constante; se
  vuelve ajuste si hace falta.

El review `high` de este paso trajo tres cosas, las tres de fondo y las tres
arregladas: el timer del bloqueo usaba el reloj monotónico, que en Android se
congela mientras el equipo duerme —con la pantalla apagada, dos minutos podían
ser horas y al despertar Resumed cancelaba el timer con la sesión abierta—;
ahora la parada se anota en reloj de pared (`Round(0)`) y Resumed compara
cuánto pasó de verdad. El cierre usaba `Disconnect`, que es «desconectar a
mano»: tiraba el changeset y podía cortar un apply o una exportación a la
mitad; ahora comparte el camino de la inactividad (`cerrarPorKaname`: motivo,
changeset conservado, transacciones avisadas) y con una operación en curso no
corta y reintenta a los quince segundos. Y la clave guardada quedaba huérfana
si el túnel pasaba a contraseña o se apagaba; ahora `SaveWithSSH` la borra al
cambiar, y la vista la reporta con cualquier auth mientras el túnel esté
activo.

La prueba a mano de este paso se hace junto con la de la fase 4: con la
interfaz de escritorio apretada en el teléfono no se podía manejar.

## Estado del paso 4 (2026-09-12): escrito, en `feat/android-frontend`

Un solo frontend. `frontend/src/mobile/` vive al lado de las pantallas de
escritorio y comparte con ellas los bindings, `lib/` y los átomos de
`components/ui`; `main.tsx` elige por el user agent (Android) o por `?movil`
en la URL, para verla en la PC con las herramientas de desarrollo en modo
dispositivo. El flujo de conectar —inspección del bastión, TOFU, conexión—
salió de `App.tsx` a `lib/useConectar.tsx` y lo usan las dos interfaces: es la
parte de seguridad y no se duplica.

| Pantalla | Archivo | Qué hace |
|---|---|---|
| Conexiones | `Conexiones.tsx` | Tarjetas con entorno, URI y **qué credencial falta**; Borrar (con sus tres secretos), Credenciales y Conectar; importar desde el archivo de la PC. |
| Credenciales | `Credenciales.tsx` | Contraseña de la base, secreto del bastión y —con túnel por clave— la clave privada pegada como texto (`SetSSHKey`). Lo vacío queda como está. |
| Sesión | `Sesion.tsx` | Cuatro pestañas abajo: Tablas, SQL, Historial, Ajustes. Barra con nombre, `describe` y lavado rojo en producción. |
| Tablas | `Tablas.tsx` | El esquema como lista con buscador; ~filas, «sin clave primaria». Sin objetos de texto. |
| Tabla | `Tabla.tsx` | Filas como tarjetas de a 40, «cargar más», recargar; aviso sin clave primaria; «Nueva fila» si se puede escribir. |
| Fila | `Fila.tsx` | Ver entera; Editar (NULL, por defecto, deshacer por campo; la clave no se toca), Borrar…, Agregar…. Los tres: `StageGrid` → palabra de producción si Go la pide → **vista previa del SQL** → `Apply` con la huella. El changeset del teléfono es siempre esa fila. |
| SQL | `Consulta.tsx` | Área de texto, ejecutar/cancelar, el resultado en tarjetas, varias sentencias. Sin CodeMirror ni transacciones manuales. |
| Historial | `Historial.tsx` | El de esta conexión, filtrable; tocar repite en SQL. |
| Ajustes | `Ajustes.tsx` | Tema, borrar el historial de esta conexión, desconectar, versión, y el texto de qué cubre y qué no. |

Al volver del segundo plano la app pregunta si la sesión sigue
(`visibilitychange` → `Current`) y muestra el motivo que dejó Go si se cerró;
los bindings que devuelven un fallo en vez de lanzar (`TableData`, `Run`)
también lo preguntan.

Primera vuelta en el teléfono (2026-09-12): importar, credenciales,
conectar, tablas, fila, SQL e historial **funcionan**. Faltaba borrar una
conexión —se agregó a la tarjeta—, y el túnel SSH no se probó porque el
archivo de la clave no estaba en el teléfono. Pendiente de decidir: que Kaname
aparezca en «Abrir con» para el `.toml` exportado; hoy se importa desde
adentro. Wails no mira el intent de apertura en Android, así que sería Java
propio en `KanameApp.onActivityCreated` más un binding `PendingImport`, con la
trampa de que `.toml` no tiene tipo MIME y los exploradores lo mandan como
`text/plain` u `octet-stream`.

Segunda vuelta (2026-09-12): borrar funciona; el resto seguía funcionando.

## Estado del paso 5 (2026-09-12): hecho, salvo la vuelta por los cuatro motores

- **El candado de esquema en el núcleo.** `Session.soloDatos` —true en
  Android por `solodatos_android.go`, false en escritorio— hace que `Stage` y
  `StageMany` rechacen con `ErrSchemaLocked` cualquier cambio de `KindSchema`,
  antes de la confirmación de producción: no hay palabra que lo habilite. Una
  tanda mixta se rechaza entera. El editor SQL es la otra puerta:
  `esquemaEnElTelefono` rechaza `CREATE`, `ALTER`, `DROP`, `TRUNCATE`,
  `RENAME`, `COMMENT`, `GRANT`, `REVOKE` y `REINDEX` por el verbo de cada
  sentencia, antes de correr ninguna del lote —el mismo mecanismo y la misma
  fuerza que «Bloquear DROP y TRUNCATE»—. El test corre en cualquier
  plataforma encendiendo el campo, y se comprobó que falla si se saca el `if`.
- **Íconos del brand kit**: `build/android/iconos.py` genera el adaptativo
  (fondo `bg-app` + la marca como capa de frente) y el heredado (tile oscuro)
  desde `build/appicon.png`; los PNG se commitean. Lección: «--bg-app» dentro
  de un comentario XML es inválido —dos guiones seguidos— y aapt lo rechaza.
- **README**: sección «Android» con instalar, traer las conexiones, qué
  protege y qué no, cuántas veces pide el dedo, y cómo se construye.
- Los pendientes del spike que quedan a propósito: `WailsForegroundService` y
  el código de cámara y ubicación del bridge de Wails, sin uso y sin declarar
  en el manifest.

Falta: la vuelta a mano por los cuatro motores desde el teléfono (leer y
corregir una fila en Postgres, MySQL, MariaDB y SQLite) y decidir «Abrir con».

Lo que se prueba en el teléfono, en este orden:

1. Exportar dos conexiones desde la PC (una con túnel por clave), pasar el
   archivo, importar. La tarjeta dice qué falta.
2. Credenciales: contraseña (pide el dedo), y en la de túnel la clave pegada y
   la frase si tiene. La tarjeta pasa a «credenciales cargadas».
3. Conectar (pide el dedo). Con túnel, el diálogo TOFU la primera vez.
4. Tablas → una tabla → una fila → Editar un campo → Guardar… → vista previa →
   Aplicar. En producción, la palabra. La tarjeta se actualiza.
5. Nueva fila y Borrar…, lo mismo.
6. SQL: un `select` y un `update` a mano. Historial: tocar una y que vuelva a
   SQL.
7. Captura de pantalla bloqueada; miniatura negra en el selector.
8. Otra app y volver antes de dos minutos: sigue. Después: «Sesión cerrada»
   con el motivo, y reconectar pide el dedo.

## Cuándo

Se arrancó el 2026-09-12 con el uso concreto que este apartado pedía:
corregir un valor desde el teléfono. Lo que este documento sigue pidiendo es
lo que el proyecto ya hace: que nada de escritorio se meta en el núcleo — el
spike lo confirmó, compiló a Android sin tocar una línea.
