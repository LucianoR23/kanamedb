import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";

/**
 * Después de un error de un binding, averigua si fue porque la sesión ya no
 * está —la cerró el bloqueo en segundo plano o la inactividad— y en ese caso
 * avisa con el motivo que dejó Go. Devuelve true si la sesión se había
 * cerrado: quien llama no muestra el error, porque el aviso ya lo explica.
 */
export async function siLaSesionSeCerro(onCerrada: (motivo: string) => void): Promise<boolean> {
  try {
    const v = await SessionSvc.Current();
    if (v.connected) return false;
    onCerrada(v.closedReason ?? "");
    return true;
  } catch {
    return false;
  }
}
