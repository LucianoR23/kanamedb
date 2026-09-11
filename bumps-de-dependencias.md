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
