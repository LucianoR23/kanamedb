import { Dialogs } from "@wailsio/runtime";

/**
 * elegirArchivoSQLite abre el selector de archivos del sistema operativo.
 *
 * El diálogo lo abre el sistema, no la página: el navegador no puede dar una
 * ruta de archivo, y una ruta es exactamente lo que SQLite necesita.
 *
 * Vive acá y no en la pantalla porque lo usan tres: el formulario de S03, el
 * atajo de S01 y el de S02. Tres copias de los filtros son tres lugares donde
 * agregar una extensión, y el que se olvide va a ser el que alguien use.
 *
 * Devuelve la cadena vacía si la persona cancela.
 */
export async function elegirArchivoSQLite(): Promise<string> {
  try {
    const ruta = await Dialogs.OpenFile({
      Title: "Elegir una base de SQLite",
      CanChooseFiles: true,
      // Se pueden elegir archivos que no estén en los filtros: las bases de
      // SQLite se llaman de cualquier forma, y muchas no tienen extensión.
      AllowsOtherFiletypes: true,
      Filters: [
        { DisplayName: "Bases de SQLite", Pattern: "*.db;*.sqlite;*.sqlite3;*.db3" },
        { DisplayName: "Todos los archivos", Pattern: "*" },
      ],
    });
    return ruta ?? "";
  } catch (err) {
    // Cancelar NO es un error, y era lo que se veía: cerrar el selector sin
    // elegir nada mostraba «Invalid dialog call: Dialog.OpenFile failed, error
    // getting selection: cancelled by user». La acción más común de un
    // selector de archivos terminaba en un cartel rojo en inglés.
    //
    // Wails lo manda como rechazo de la promesa, no como un valor: adentro es
    // `cfd.ErrorCancelled`, de un paquete `internal/` que no se puede importar,
    // así que del lado del navegador solo queda el texto.
    if (cancelado(err)) return "";
    // Lo que no sea cancelar sí es un fallo, y se cuenta en castellano en vez
    // de dejar pasar el mensaje del puente.
    throw new Error(
      "No se pudo abrir el selector de archivos del sistema. " +
        `Se puede escribir la ruta a mano en el campo «Archivo». (${textoDe(err)})`,
    );
  }
}

/** cancelado mira el texto porque es lo único que cruza el puente. */
function cancelado(err: unknown): boolean {
  return textoDe(err).toLowerCase().includes("cancelled by user");
}

function textoDe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
