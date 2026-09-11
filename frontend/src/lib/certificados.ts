import { Dialogs } from "@wailsio/runtime";
import { cancelado, textoDe } from "./dialogos";

/** Qué archivo se está eligiendo, para el título del selector y los filtros. */
export type TipoDeCertificado = "raiz" | "cliente" | "clave";

const TITULOS: Record<TipoDeCertificado, string> = {
  raiz: "Elegir el certificado raíz",
  cliente: "Elegir el certificado de cliente",
  clave: "Elegir la clave privada del cliente",
};

/**
 * elegirCertificado abre el selector de archivos del sistema para uno de los
 * tres archivos de la pestaña TLS.
 *
 * Como en el de SQLite, el diálogo lo abre el sistema: el navegador no puede
 * dar una ruta, y una ruta es lo que se guarda —el archivo se queda donde está—.
 * Devuelve la cadena vacía si la persona cancela; lanza solo si el selector
 * falló de verdad.
 */
export async function elegirCertificado(tipo: TipoDeCertificado): Promise<string> {
  try {
    const ruta = await Dialogs.OpenFile({
      Title: TITULOS[tipo],
      CanChooseFiles: true,
      // Los PEM se llaman de cualquier forma: la extensión es costumbre, no
      // formato. Los filtros ayudan a encontrarlo; no impiden elegir otro.
      AllowsOtherFiletypes: true,
      Filters:
        tipo === "clave"
          ? [
              { DisplayName: "Claves privadas", Pattern: "*.key;*.pem" },
              { DisplayName: "Todos los archivos", Pattern: "*" },
            ]
          : [
              { DisplayName: "Certificados", Pattern: "*.crt;*.pem;*.cer;*.ca-bundle" },
              { DisplayName: "Todos los archivos", Pattern: "*" },
            ],
    });
    return ruta ?? "";
  } catch (err) {
    if (cancelado(err)) return "";
    throw new Error(
      "No se pudo abrir el selector de archivos del sistema. " +
        `Se puede escribir la ruta a mano. (${textoDe(err)})`,
    );
  }
}

/**
 * elegirDestinoCertificado pide dónde guardar el certificado que presentó el
 * servidor, para cargarlo después como raíz. Vacío si se cancela.
 */
export async function elegirDestinoCertificado(nombreSugerido: string): Promise<string> {
  try {
    const ruta = await Dialogs.SaveFile({
      Title: "Guardar el certificado del servidor",
      Filename: nombreSugerido,
      Filters: [{ DisplayName: "Certificado PEM", Pattern: "*.crt;*.pem" }],
    });
    return ruta ?? "";
  } catch (err) {
    if (cancelado(err)) return "";
    throw new Error(
      `No se pudo abrir el diálogo de guardar del sistema. (${textoDe(err)})`,
    );
  }
}
