-- Base de ejemplo para SQLite, para tener qué abrir con «Abrir archivo SQLite…».
--
-- Es el equivalente de demo.sql, que es de Postgres. No lo usa ningún test: los
-- tests de SQLite crean su archivo en un directorio temporal y lo borran. Esto
-- es para mirar la aplicación a mano.
--
-- Se arma con el sqlite3 de línea de comandos, o con Python, que lo trae:
--
--   sqlite3 demo.db < docker/demo-sqlite.sql
--   python -c "import sqlite3,sys;sqlite3.connect('demo.db').executescript(open('docker/demo-sqlite.sql',encoding='utf-8').read())"
--
-- Lo que hay adentro está elegido para que se vean las cosas donde SQLite es
-- distinto de los demás, que es justo lo que conviene mirar a mano:
--
--   * Una clave foránea con ON DELETE CASCADE. Reconstruir `autores` con las
--     claves encendidas borraría las filas de `libros` en silencio; el apply de
--     Kaname las apaga antes del BEGIN y corre foreign_key_check antes del
--     COMMIT. Ver § 6 del plan.
--   * Una columna generada, que la reconstrucción NO debe copiar.
--   * Un índice parcial y uno único, que se recrean después del rebuild.
--   * Un trigger, que también se recrea.
--   * Una vista, que es lo que obliga a `PRAGMA legacy_alter_table`.
--   * Tipos inventados —«plata», «cuando»— porque SQLite acepta cualquier
--     nombre de tipo: lo que manda es la afinidad, no el nombre.

PRAGMA foreign_keys = ON;

DROP VIEW IF EXISTS libros_por_autor;
DROP TABLE IF EXISTS prestamos;
DROP TABLE IF EXISTS libros;
DROP TABLE IF EXISTS autores;

CREATE TABLE autores (
  id        integer PRIMARY KEY AUTOINCREMENT,
  nombre    text    NOT NULL,
  pais      text,
  nacimiento cuando,
  CHECK (length(nombre) > 0)
);

CREATE TABLE libros (
  id         integer PRIMARY KEY AUTOINCREMENT,
  autor_id   integer NOT NULL REFERENCES autores (id) ON DELETE CASCADE,
  titulo     text    NOT NULL,
  isbn       text    UNIQUE,
  paginas    integer CHECK (paginas > 0),
  precio     plata   DEFAULT 0,
  -- Generada: la reconstrucción de tabla no la copia, la vuelve a declarar.
  precio_iva plata   GENERATED ALWAYS AS (precio * 1.21) VIRTUAL,
  publicado  integer NOT NULL DEFAULT 0
);

CREATE TABLE prestamos (
  id        integer PRIMARY KEY AUTOINCREMENT,
  libro_id  integer NOT NULL REFERENCES libros (id) ON DELETE CASCADE,
  quien     text    NOT NULL,
  desde     cuando  NOT NULL DEFAULT (date('now')),
  devuelto  cuando
);

-- Parcial: solo indexa lo que está afuera, que es lo que se busca.
CREATE INDEX prestamos_abiertos ON prestamos (libro_id) WHERE devuelto IS NULL;
CREATE INDEX libros_por_titulo ON libros (titulo);

CREATE TRIGGER libros_sin_titulo_vacio
BEFORE INSERT ON libros
FOR EACH ROW WHEN trim(NEW.titulo) = ''
BEGIN
  SELECT raise(ABORT, 'el título no puede estar vacío');
END;

CREATE VIEW libros_por_autor AS
SELECT a.nombre AS autor, count(l.id) AS cuantos
FROM autores a LEFT JOIN libros l ON l.autor_id = a.id
GROUP BY a.id, a.nombre;

INSERT INTO autores (nombre, pais, nacimiento) VALUES
  ('Silvina Ocampo', 'AR', '1903-07-28'),
  ('Ursula K. Le Guin', 'US', '1929-10-21'),
  ('Italo Calvino', 'IT', '1923-10-15'),
  ('Clarice Lispector', 'BR', '1920-12-10');

INSERT INTO libros (autor_id, titulo, isbn, paginas, precio, publicado) VALUES
  (1, 'La furia',                 '978-950-07-0001-1', 184, 12500, 1),
  (1, 'Autobiografía de Irene',   '978-950-07-0002-8', 160, 11800, 1),
  (2, 'La mano izquierda de la oscuridad', '978-84-450-0003-5', 320, 21900, 1),
  (2, 'Los desposeídos',          '978-84-450-0004-2', 400, 24500, 1),
  (3, 'Las ciudades invisibles',  '978-84-339-0005-9', 176, 15900, 1),
  (3, 'Si una noche de invierno un viajero', NULL,     280, 18700, 0),
  (4, 'La hora de la estrella',   '978-987-712-0006-3',  96,  9900, 1);

INSERT INTO prestamos (libro_id, quien, desde, devuelto) VALUES
  (1, 'ana',  '2026-08-01', '2026-08-20'),
  (3, 'beto', '2026-08-14', NULL),
  (5, 'caro', '2026-09-01', NULL),
  (5, 'ana',  '2026-06-02', '2026-06-30');
