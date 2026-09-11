# Automatizar los bumps de dependencias

Postergado el 2026-09-11 por decisión del usuario: lo que hay hoy alcanza por un
tiempo. Este archivo deja anotado qué hay, qué falta y qué tiene que cumplir la
herramienta que se elija, para no volver a pensarlo desde cero.

## Qué hay hoy

- **`govulncheck`** en CI, bloqueante. Solo vulnerabilidades alcanzables.
- **Job `deps`** en CI, informativo. Escribe en el resumen de la corrida qué
  dependencias directas (Go y frontend) tienen versión nueva. Nadie lo lee si
  nadie entra a mirar.
- **`minimum-release-age = 7` días** en `frontend/.npmrc`. pnpm rechaza paquetes
  publicados hace menos de una semana. No se desactiva.
- Reglas de `CLAUDE.md`: una dependencia por commit, versión exacta, changelog
  leído, y si arrastra transitivas se dice en el mensaje. Una versión nueva no
  es motivo para subir; un CVE o un bug que nos afecta, sí.

## Qué falta

Que el PR se abra solo. Lo demás —leerlo, decidir, mergear— sigue siendo de una
persona: **sin auto-merge**, nunca.

## Lo que la herramienta tiene que cumplir

Derivado de las reglas de arriba, no negociable:

1. **Un PR por dependencia.** Sin agrupar, salvo las transitivas que esa misma
   dependencia arrastra (`go.sum`, `pnpm-lock.yaml`).
2. **Versión exacta** en `package.json` — nunca reintroducir `^` ni `~`.
3. **Changelog o release notes en el cuerpo del PR**, para leerlo sin salir.
4. **Respetar los 7 días** de `minimum-release-age`. Si la herramienta no lo
   entiende, el PR va a fallar en `pnpm install --frozen-lockfile`, que es
   ruido pero no un agujero.
5. **CI completo en el PR**: `check`, `integration` y `build`. Un bump de driver
   se prueba contra los motores, no solo compilando.
6. `go mod tidy -diff` verde después del bump.
7. Sin tocar el toolchain de Go (`go` y `toolchain` en `go.mod`) ni Wails ni
   los drivers por su cuenta: esos se suben a mano, con el plan al día.

## Candidatas (a septiembre de 2026 — verificar en el momento)

| | Dependabot | Renovate |
|---|---|---|
| Infra | Ninguna: nativo de GitHub | App de Mend o self-hosted |
| pnpm | Sí (lockfile v9) | Sí |
| Los 7 días | `cooldown.default-days: 7` (3 por defecto desde 2026-07) | `minimumReleaseAge: 7 days` |
| Un PR por dependencia | Por defecto | Por defecto |
| Changelog en el PR | Release notes de GitHub | Release notes + changelog |
| Versión exacta | `versioning-strategy: increase` | `rangeStrategy: pin` |

Las dos cumplen las siete reglas por configuración. Si al llegar el momento no
apareció nada mejor, **Dependabot**, por no tener infra: es un archivo en
`.github/` y nada más. Con una salvedad: su `cooldown` y el `minimum-release-age`
de pnpm son dos relojes distintos que miran la misma fecha — se ponen los dos en
7 para que no discutan.

## Cuándo

Cuando el informe del job `deps` deje de leerse al cerrar cada iteración. Al
activar el bot, el job `deps` se borra: pasa a duplicarlo.

## Verificado el 2026-09-11, contra lo que tiene este repo

Lo que hay que cubrir: `go.mod` (8 dependencias directas), `frontend/package.json`
(pnpm, versiones exactas, `minimum-release-age=10080` en `.npmrc`), los
`uses:` de `.github/workflows/build.yml` (checkout, setup-go, setup-node,
pnpm/action-setup, upload/download-artifact, por tag mayor), los `go install
…@vX` del mismo workflow (govulncheck, gitleaks, wails3), y las imágenes de
`docker-compose.test.yml`.

| | Dependabot | Renovate (Mend Cloud, Community) |
|---|---|---|
| Quién lo corre | GitHub, dentro de la plataforma | **Mend**, en su infraestructura: una app de terceros con permiso de escritura sobre el repo |
| Costo | Ninguno | Ninguno, público o privado; 1 job concurrente, cada 4 horas, 30 min de tope |
| `cooldown` / `minimumReleaseAge` | Sí, por ecosistema; **3 días por defecto desde el 2026-07-14** — se sube a 7 | Sí |
| `go mod tidy` en el PR | Lo corre en cada actualización | `postUpdateOptions: gomodTidy` |
| Mayores de Go (cambio de import path) | No: a mano | Puede (`gomodUpdateImportPaths`); igual se revisa a mano |
| GitHub Actions | Sí (`github-actions`); actualiza el SHA si ya está pineado | Sí, y puede pinear a SHA solo |
| `go install …@vX` en el YAML | **No** | Sí, con un manager de regex propio |
| `docker-compose.test.yml` | Ecosistema `docker-compose` existe, pero las imágenes van con `${VAR:-9.7}` | Lo mismo: no esperar que lo toque |

Lo de las imágenes está bien así: la matriz de motores es una decisión (los
mínimos soportados, ver el README), no un bump.

**La diferencia que decide: quién ejecuta.** Dependabot es GitHub; Renovate
Cloud es Mend corriendo contra el repo desde afuera, con permiso de escribir.
Para un proyecto cuyo argumento es «ninguna dependencia llama a casa», sumar un
tercero con acceso de escritura para ahorrarse tres `go install` en un YAML no
cierra. Lo que Renovate hace y Dependabot no —los tres `go install` del
workflow— son herramientas de CI, no dependencias del binario: se revisan a
mano al cerrar cada iteración, como el toolchain de Go.

**Decisión recomendada: Dependabot**, con `cooldown.default-days: 7` en los
tres ecosistemas (`gomod`, `npm`, `github-actions`), `versioning-strategy:
increase`, sin `groups`, e `ignore` para `github.com/wailsapp/wails/v3`,
`@wailsio/runtime`, los tres drivers y `golang.org/x/crypto`, que se suben a
mano con el plan al día. Al activarlo se borra el job `deps`.

**Lo que un bump NO revisa solo.** El PR trae las release notes y CI en verde:
compila, pasa los tests, la matriz de integración y el typecheck. Eso agarra
una API que desapareció (no compila) y un cambio de comportamiento que un
test cubra. Lo que no agarra es una **deprecación**: el código sigue
compilando contra algo que en dos versiones no va a existir. Para eso hay que
agregar una señal que hoy no está: `staticcheck` en el job `check` (SA1019
marca cada uso de algo deprecado en Go) y, si algún día se quiere lo mismo en
TypeScript, `typescript-eslint` con `no-deprecated` —hoy el frontend no tiene
eslint y `tsc` no marca deprecaciones—. Y leer el changelog sigue siendo la
regla: es lo único que dice *por qué* cambió.

Fuentes: [opciones de Dependabot](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference),
[cooldown de 3 días por defecto](https://muddaser.com/dependabot-default-3-day-cooldown-dependabot-yml-guide/),
[`go mod tidy` en Dependabot](https://github.blog/changelog/2020-10-19-dependabot-go-mod-tidy-and-vendor-support/),
[Mend Renovate Cloud](https://docs.renovatebot.com/mend-hosted/overview/).
