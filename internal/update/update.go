// Package update consulta si hay una versión de Kaname más nueva que la que
// está corriendo.
//
// # Esto es lo único de la aplicación que sale a internet por su cuenta
//
// Y por eso las reglas son estrechas. CLAUDE.md prohíbe el phone-home: sin
// telemetría y **sin chequeos automáticos**. Lo que hay acá es un botón.
//
//   - Se consulta SOLO cuando alguien aprieta. No hay temporizador, no corre al
//     arrancar, no corre después de conectar. Nada llama a `Check` salvo la
//     pantalla de ajustes.
//   - La consulta es un GET sin query string, sin cookies y sin ningún
//     identificador. El `User-Agent` es la palabra «Kaname» sola: NO lleva la
//     versión, que es el único dato que el servidor podría juntar. La
//     comparación se hace acá, con lo que ya sabemos.
//   - Solo se lee `tag_name` y `html_url` de la respuesta. Nada se ejecuta,
//     nada se descarga, nada se instala: la actualización la hace la persona.
//
// La respuesta viene de internet, así que se la trata como lo que es: un
// tamaño máximo, un timeout y redirecciones que no pueden salir del host.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// endpoint es la API de releases del repositorio.
//
// Se pide la última publicada y no la lista entera: es una respuesta y no una
// página de cincuenta.
const endpoint = "https://api.github.com/repos/LucianoR23/kanamedb/releases/latest"

// maxRespuesta es cuánto se lee de la respuesta antes de cortar.
//
// Un JSON de release con notas largas no llega a 100 KB. El tope está porque
// del otro lado hay un servidor que no controlamos, y `io.ReadAll` sobre un
// cuerpo infinito es memoria hasta que el proceso muere.
const maxRespuesta = 256 << 10

// timeout corta la consulta. Es un botón: quien lo apretó está mirando.
const timeout = 10 * time.Second

// Result es lo que la pantalla muestra.
//
// `Problem` y `Latest` son excluyentes a propósito: o se pudo consultar o no.
// Un resultado con las dos cosas dejaría a la pantalla decidiendo a cuál
// creerle.
type Result struct {
	// Current es la versión que está corriendo.
	Current string `json:"current"`

	// Latest es la última publicada, o vacío si no se pudo saber.
	Latest string `json:"latest"`

	// Newer dice si Latest es más nueva que Current.
	//
	// Es false también cuando no se pudieron comparar: «no sé» y «estás al día»
	// se ven distinto en pantalla gracias a Comparable, y confundirlos haría
	// que una versión nueva pase desapercibida.
	Newer bool `json:"newer"`

	// Comparable dice si las dos versiones se pudieron comparar.
	Comparable bool `json:"comparable"`

	// URL es dónde está la versión publicada.
	URL string `json:"url"`

	// CheckedAt es cuándo se consultó, en RFC 3339.
	CheckedAt string `json:"checkedAt"`

	// Problem es por qué no se pudo consultar, en castellano. Vacío si salió
	// bien.
	Problem string `json:"problem"`
}

// Checker consulta el endpoint.
type Checker struct {
	// base es el endpoint. Es un campo y no una constante solamente para que
	// los tests puedan apuntarlo a un servidor local: nada lo cambia en
	// producción.
	base   string
	client *http.Client
	ahora  func() time.Time
}

// New arma el checker que se usa de verdad.
func New() *Checker {
	return &Checker{
		base:  endpoint,
		ahora: time.Now,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Una redirección a otro host convertiría este botón en «mandá
				// una petición a donde diga la respuesta anterior».
				if len(via) > 0 && req.URL.Host != via[0].URL.Host {
					return fmt.Errorf("la respuesta redirige a otro servidor (%s)", req.URL.Host)
				}
				if len(via) >= 5 {
					return fmt.Errorf("demasiadas redirecciones")
				}
				return nil
			},
		},
	}
}

// Check consulta la última versión publicada.
//
// Nunca devuelve error: un problema de red es parte del resultado. Quien
// aprieta el botón necesita leer qué pasó, y un error del puente de Wails
// llegaría al frontend como una excepción sin la versión actual adentro.
func (c *Checker) Check(ctx context.Context, actual string) Result {
	res := Result{
		Current:   actual,
		CheckedAt: c.ahora().UTC().Format(time.RFC3339),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base, nil)
	if err != nil {
		res.Problem = "no se pudo armar la consulta: " + err.Error()
		return res
	}
	// GitHub exige un User-Agent. Va sin la versión: es el único dato de esta
	// máquina que la petición podría llevar, y no hace falta para nada.
	req.Header.Set("User-Agent", "Kaname")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.client.Do(req)
	if err != nil {
		res.Problem = "no se pudo consultar: " + err.Error()
		return res
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		res.Problem = "todavía no hay ninguna versión publicada."
		return res
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		res.Problem = "el servidor pidió esperar antes de volver a consultar."
		return res
	case resp.StatusCode != http.StatusOK:
		res.Problem = fmt.Sprintf("el servidor contestó %d.", resp.StatusCode)
		return res
	}

	cuerpo, err := io.ReadAll(io.LimitReader(resp.Body, maxRespuesta))
	if err != nil {
		res.Problem = "no se pudo leer la respuesta: " + err.Error()
		return res
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
	}
	if err := json.Unmarshal(cuerpo, &release); err != nil {
		res.Problem = "la respuesta no se entendió."
		return res
	}
	if release.Draft || strings.TrimSpace(release.TagName) == "" {
		res.Problem = "todavía no hay ninguna versión publicada."
		return res
	}

	res.Latest = strings.TrimSpace(release.TagName)
	// La URL se acepta solo si es https: es la que la pantalla le va a pasar al
	// navegador del sistema, y viene de una respuesta de red.
	if u := strings.TrimSpace(release.HTMLURL); strings.HasPrefix(u, "https://") {
		res.URL = u
	}

	cmp, ok := Comparar(res.Latest, actual)
	res.Comparable = ok
	res.Newer = ok && cmp > 0
	return res
}

// Comparar dice si `a` es más nueva que `b`: 1, 0 o −1, y si se pudo comparar.
//
// Entiende `v1.2.3`, con o sin la `v`, y trata un sufijo —`1.2.3-rc1`— como
// ANTERIOR a la versión limpia, que es lo que dice semver y lo que espera
// cualquiera: una release candidate no es más nueva que la final.
//
// Cuando no puede —una de las dos no es un número de versión— devuelve false en
// vez de adivinar. Adivinar acá significa decir «estás al día» sin saberlo.
func Comparar(a, b string) (int, bool) {
	na, pa, oka := partir(a)
	nb, pb, okb := partir(b)
	if !oka || !okb {
		return 0, false
	}

	for i := 0; i < len(na) || i < len(nb); i++ {
		// Una versión más corta se completa con ceros: 1.2 es 1.2.0, y sin
		// esto «1.2» y «1.2.0» compararían como distintas.
		va, vb := 0, 0
		if i < len(na) {
			va = na[i]
		}
		if i < len(nb) {
			vb = nb[i]
		}
		if va != vb {
			if va > vb {
				return 1, true
			}
			return -1, true
		}
	}

	switch {
	case pa == "" && pb == "":
		return 0, true
	case pa == "":
		// Sin sufijo es la versión final, y la final gana.
		return 1, true
	case pb == "":
		return -1, true
	default:
		return compararPrelanzamiento(pa, pb), true
	}
}

// compararPrelanzamiento ordena dos sufijos como manda semver.
//
// NO es una comparación de cadenas, y la diferencia importa: `rc10` es MENOR que
// `rc9` alfabéticamente, así que publicar una rc10 mientras corre una rc9 se
// reportaba como «tenés la última» — justo lo que este paquete existe para que
// no pase. Semver dice: se parte por puntos, los identificadores numéricos se
// comparan como números, los demás como texto, un numérico va antes que uno
// alfanumérico, y con todo igual gana el que tiene más identificadores.
//
// `rc10` no es numérico —es texto y un número pegados— así que el caso real
// sigue cayendo en la comparación de texto. Por eso lo que lo arregla de verdad
// es el desempate de abajo: si un identificador es un prefijo del otro, gana el
// más largo cuando lo que sigue son dígitos. Con `rc9` y `rc10`: prefijo común
// `rc`, después `9` contra `10`, y ahí se comparan como números.
func compararPrelanzamiento(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if c := compararIdentificador(pa[i], pb[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(pa) > len(pb):
		return 1
	case len(pa) < len(pb):
		return -1
	default:
		return 0
	}
}

func compararIdentificador(a, b string) int {
	na, erra := strconv.Atoi(a)
	nb, errb := strconv.Atoi(b)
	switch {
	case erra == nil && errb == nil:
		return signo(na - nb)
	case erra == nil:
		// Numérico antes que alfanumérico, como manda semver.
		return -1
	case errb == nil:
		return 1
	}

	// Los dos son alfanuméricos. Se parte en el primer dígito para que `rc9` y
	// `rc10` se comparen por el número y no por la letra: es la forma que usa
	// todo el mundo para las release candidates.
	la, da := letrasYNumero(a)
	lb, db := letrasYNumero(b)
	if la == lb && da >= 0 && db >= 0 {
		return signo(da - db)
	}
	return strings.Compare(a, b)
}

// letrasYNumero parte `rc10` en "rc" y 10. Devuelve −1 si no termina en dígitos.
func letrasYNumero(s string) (string, int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return s, -1
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return s, -1
	}
	return s[:i], n
}

func signo(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

// partir separa `v1.2.3-rc1` en [1 2 3] y "rc1".
//
// Los metadatos de build —lo que va después de un `+`, como
// `1.0.0+20260911`— se DESCARTAN, no se tratan como prelanzamiento. Semver es
// explícito: no cuentan para la precedencia. Tratándolos como sufijo, una build
// con fecha quedaba ordenada por DEBAJO de la misma versión sin fecha.
func partir(v string) ([]int, string, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, "", false
	}

	sufijo := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		sufijo = v[i+1:]
		v = v[:i]
	}

	partes := strings.Split(v, ".")
	nums := make([]int, 0, len(partes))
	for _, p := range partes {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, "", false
		}
		nums = append(nums, n)
	}
	return nums, sufijo, true
}
