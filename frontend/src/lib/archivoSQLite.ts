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
}
