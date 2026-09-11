# Firmar el `.exe` de Windows sin pagar

Anotado el 2026-09-11 para hacerlo más adelante, cuando el repo lleve unos días
público y haya un release. No es parte del checklist de pre-publicación: los
binarios salen sin firmar, y el README dice qué avisa cada sistema.

## Por qué

Sin firma, SmartScreen frena el `.exe` la primera vez con «Windows protegió tu
PC» y hay que pasar por *Más información → Ejecutar de todos modos*. Con un
certificado que ya tiene reputación acumulada, ese aviso desaparece. Un
certificado OV comprado no la trae: la reputación se gana por certificado, con
descargas, y un certificado nuevo arranca de cero aunque cueste plata.

## La vía gratuita: SignPath Foundation

[SignPath Foundation](https://signpath.org) firma binarios de proyectos de
código abierto con su propio certificado, sin costo, a través de la plataforma
de SignPath.io. Verificado contra [sus términos](https://signpath.org/terms) el
2026-09-11:

- **Licencia OSI sin dual-licensing** para todos los componentes. Apache 2.0
  cumple.
- **Proyecto mantenido y ya lanzado.** Hace falta por lo menos un release
  publicado: hoy no hay ninguno. Primero `v0.1.0`.
- **Binarios construidos de forma verificable desde el código**, por un build
  automatizado. Nada de subir un `.exe` a mano. El job `build` de CI cumple.
- **MFA para todos los miembros del equipo.**
- **Una política de firma publicada** en el repo, con tres roles —autores,
  revisores, aprobadores— y quién los ocupa. No exige tamaño mínimo de equipo.
- **Atribución**: «Free code signing provided by SignPath.io, certificate by
  SignPath Foundation», en el README.
- Sin malware, herramientas de intrusión ni funciones que comprometan la
  privacidad del usuario. Lo del checklist de pre-publicación aplica directo.

Dos consecuencias para no sorprenderse:

1. **El certificado se emite a SignPath Foundation.** El «publicador» que
   muestra Windows es ese nombre, no «Luciano Rodriguez» ni «Kaname». Es el
   trato: a cambio, el certificado ya tiene la reputación de todos los
   proyectos que firma con él, que es exactamente lo que hace que SmartScreen
   deje de frenar.
2. **La firma pasa por su infraestructura.** El job de release sube el `.exe`
   sin firmar, SignPath lo firma en su pipeline y lo devuelve firmado; se
   publica ese. Es un tercero en el camino del binario, y por eso se anota:
   quien lo evalúe tiene que estar de acuerdo con eso.

## Orden

1. Repo público, unos días de uso real.
2. Tag `v0.1.0`: el job `release` deja el borrador con los seis archivos;
   publicarlo.
3. Escribir la política de firma (`CODE_SIGNING_POLICY.md` o similar) y
   activar MFA en la cuenta de GitHub si no está.
4. Aplicar en signpath.org. La aprobación tarda de días a semanas.
5. Con la aprobación: un paso en el job `release` que sube `kaname-win-*.exe`
   a SignPath (token en un secret de Actions, ID de organización en una
   variable) y espera el binario firmado; el `SHA256SUMS` se calcula sobre lo
   firmado. `scripts/sockets.ps1` y el resto siguen igual: la firma no cambia
   lo que el binario hace.

## macOS

No hay equivalente gratuito. Firmar y notarizar un `.app` requiere el Apple
Developer Program, USD 99 por año. Sin eso, Gatekeeper bloquea el `.app` y se
pasa con *Ajustes → Privacidad y seguridad → Abrir de todos modos*, que es lo
que el README explica. Se decide si aparecen usuarios de Mac.
