import { useEffect, useState } from "react";
import type { TypeOption } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type { ComboOption } from "../components/ui";

/**
 * Los tipos que se pueden elegir para una columna, leídos del catálogo.
 *
 * Vive acá y no en cada diálogo porque los pide más de uno —el alta de columna y
 * el alta de tabla—, y son la misma lista: dos copias se desincronizarían el día
 * que una filtre distinto que la otra.
 *
 * Si la lectura falla se devuelve una lista vacía y no un error: el campo sigue
 * sirviendo como texto libre. Quedarse sin poder crear una columna porque no se
 * pudo leer el catálogo sería peor que perder la comodidad de elegir.
 */
export function useColumnTypes(): { tipos: TypeOption[]; opciones: ComboOption[] } {
  const [tipos, setTipos] = useState<TypeOption[]>([]);

  useEffect(() => {
    let vigente = true;
    void SessionSvc.ColumnTypes()
      .then((ts) => {
        if (vigente) setTipos(ts ?? []);
      })
      .catch(() => {
        if (vigente) setTipos([]);
      });
    return () => {
      vigente = false;
    };
  }, []);

  return { tipos, opciones: tipos.map(aOpcion) };
}

/** Un tipo del catálogo como opción del desplegable, con lo propio marcado. */
function aOpcion(t: TypeOption): ComboOption {
  return {
    value: t.name,
    ...(t.builtIn ? {} : { tag: t.kind === "base" ? "de esta base" : t.kind }),
    ...(t.comment ? { title: t.comment } : {}),
  };
}
