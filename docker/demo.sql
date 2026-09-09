-- Esquemas de prueba para mirar Kaname con datos que se parezcan a algo.
--
-- No lo usa ningún test: los tests crean su propio esquema por caso y lo borran
-- al terminar. Esto es para probar A MANO —el diagrama, la estructura, el
-- changeset— sin tener que inventar tablas cada vez.
--
--   docker compose -f docker-compose.test.yml up -d --wait
--   docker exec -i kaname-postgres-1 psql -U kaname -d kaname_test < docker/demo.sql
--
-- Se puede correr las veces que haga falta: empieza borrando lo que dejó la
-- corrida anterior. La base de prueba vive en tmpfs, así que bajar el stack se
-- lleva todo y hay que volver a correrlo.

DROP SCHEMA IF EXISTS demo CASCADE;
DROP SCHEMA IF EXISTS aristas CASCADE;
DROP SCHEMA IF EXISTS otro CASCADE;

-- ============================================================== demo
-- Un esquema con las cosas que la pantalla de estructura tiene que saber
-- mostrar: identidad, columnas generadas, restricciones sin validar, índices de
-- todas las formas, triggers activos y deshabilitados.

CREATE SCHEMA demo;

CREATE TABLE demo.clientes (
  id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email  text NOT NULL,
  pais   char(2),
  creado timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE demo.clientes IS 'personas que compran';
COMMENT ON COLUMN demo.clientes.pais IS 'ISO 3166-1 alfa-2';

-- Índice funcional: la columna es una expresión, no un nombre de columna.
CREATE UNIQUE INDEX clientes_email_uq ON demo.clientes (lower(email));

-- Un enum propio de esta base: el selector de tipos lo tiene que ofrecer, y
-- ninguna lista escrita a mano lo tendría.
CREATE TYPE demo.estado AS ENUM ('pendiente', 'pagado', 'enviado', 'cancelado');

CREATE TABLE demo.pedidos (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  cliente_id bigint NOT NULL REFERENCES demo.clientes (id) ON DELETE RESTRICT ON UPDATE CASCADE,
  estado     demo.estado NOT NULL DEFAULT 'pendiente',
  unitario   numeric(10,2) NOT NULL,
  cantidad   integer NOT NULL DEFAULT 1,
  -- VIRTUAL es de PostgreSQL 18 y es una de las tres cosas que Atlas no sabe
  -- leer. Ver kaname-plan.md § 6.
  total      numeric(12,2) GENERATED ALWAYS AS (unitario * cantidad) VIRTUAL,
  moneda     char(3) NOT NULL DEFAULT 'EUR',
  puesto     timestamptz NOT NULL DEFAULT now(),
  enviado    timestamptz,
  nota       text,
  CONSTRAINT pedidos_total_no_negativo CHECK (unitario >= 0),
  CONSTRAINT pedidos_moneda_iso CHECK (moneda ~ '^[A-Z]{3}$')
);
COMMENT ON COLUMN demo.pedidos.unitario IS 'sin IVA';

-- Una restricción sin validar: las filas que ya estaban nunca se comprobaron.
ALTER TABLE demo.pedidos ADD CONSTRAINT pedidos_enviado_despues
  CHECK (enviado IS NULL OR enviado >= puesto) NOT VALID;

-- Y un NOT NULL sin validar, que también es de PostgreSQL 18.
ALTER TABLE demo.pedidos ADD CONSTRAINT pedidos_nota_nn NOT NULL nota NOT VALID;

CREATE INDEX pedidos_puesto_idx   ON demo.pedidos (puesto DESC);
CREATE INDEX pedidos_cliente_idx  ON demo.pedidos (cliente_id) INCLUDE (estado);
CREATE INDEX pedidos_abiertos_idx ON demo.pedidos (puesto) WHERE enviado IS NULL;
CREATE INDEX pedidos_sin_usar_idx ON demo.pedidos (moneda);

CREATE TABLE demo.envios (
  id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  pedido_id bigint REFERENCES demo.pedidos (id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  guia      text
);

-- Se apunta a sí misma: en el diagrama es el bucle.
CREATE TABLE demo.categorias (
  id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  nombre   text NOT NULL,
  padre_id bigint REFERENCES demo.categorias (id) ON DELETE SET NULL
);

-- Clave primaria compuesta y dos claves foráneas.
CREATE TABLE demo.items (
  pedido_id    bigint NOT NULL REFERENCES demo.pedidos (id) ON DELETE CASCADE,
  categoria_id bigint NOT NULL REFERENCES demo.categorias (id),
  cantidad     integer NOT NULL,
  PRIMARY KEY (pedido_id, categoria_id)
);

CREATE FUNCTION demo.tocar() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RETURN NEW; END $$;

CREATE TRIGGER pedidos_recalcular BEFORE INSERT OR UPDATE ON demo.pedidos
  FOR EACH ROW EXECUTE FUNCTION demo.tocar();
CREATE TRIGGER pedidos_auditar AFTER INSERT OR UPDATE OR DELETE ON demo.pedidos
  FOR EACH STATEMENT EXECUTE FUNCTION demo.tocar();
-- Deshabilitado: existe pero no corre. Tiene que verse distinto.
ALTER TABLE demo.pedidos DISABLE TRIGGER pedidos_auditar;

INSERT INTO demo.clientes (email, pais)
  SELECT 'c' || g || '@ejemplo.com', 'AR' FROM generate_series(1, 4182) g;
INSERT INTO demo.pedidos (cliente_id, unitario, cantidad, nota)
  SELECT 1 + (g % 4182), (g % 500)::numeric, 1 + (g % 3), 'nota ' || g
  FROM generate_series(1, 12481) g;
INSERT INTO demo.categorias (nombre) SELECT 'cat ' || g FROM generate_series(1, 40) g;
UPDATE demo.categorias SET padre_id = 1 + (id % 5) WHERE id > 5;

ANALYZE demo.clientes;
ANALYZE demo.pedidos;
ANALYZE demo.categorias;

-- ============================================================ aristas
-- Un esquema chico donde cada tabla existe para mostrar UNA forma de relación.
-- Sirve para comparar de un vistazo cómo se dibuja cada caso.

CREATE SCHEMA aristas;
CREATE SCHEMA otro;

CREATE TABLE aristas.a_padre (id bigint PRIMARY KEY, nombre text NOT NULL);

-- Obligatoria: la columna no admite nulos. Línea llena.
CREATE TABLE aristas.b_obligatoria (
  id       bigint PRIMARY KEY,
  padre_id bigint NOT NULL REFERENCES aristas.a_padre (id),
  detalle  text
);

-- Opcional: admite nulos, así que la fila puede no tener padre. Línea punteada.
CREATE TABLE aristas.c_opcional (
  id       bigint PRIMARY KEY,
  padre_id bigint REFERENCES aristas.a_padre (id),
  detalle  text
);

-- Cascada: borra del otro lado. Línea naranja.
CREATE TABLE aristas.d_cascada (
  id       bigint PRIMARY KEY,
  padre_id bigint NOT NULL REFERENCES aristas.a_padre (id) ON DELETE CASCADE,
  detalle  text
);

-- Las dos cosas a la vez.
CREATE TABLE aristas.e_opcional_cascada (
  id       bigint PRIMARY KEY,
  padre_id bigint REFERENCES aristas.a_padre (id) ON DELETE CASCADE,
  detalle  text
);

-- Bucle.
CREATE TABLE aristas.f_arbol (
  id       bigint PRIMARY KEY,
  nombre   text NOT NULL,
  padre_id bigint REFERENCES aristas.f_arbol (id) ON DELETE SET NULL
);

-- Clave compuesta: dos columnas de cada lado, emparejadas por posición.
CREATE TABLE aristas.g_compuesta_padre (
  region char(2) NOT NULL,
  codigo int NOT NULL,
  PRIMARY KEY (region, codigo)
);
CREATE TABLE aristas.h_compuesta_hija (
  id     bigint PRIMARY KEY,
  region char(2) NOT NULL,
  codigo int NOT NULL,
  FOREIGN KEY (region, codigo) REFERENCES aristas.g_compuesta_padre (region, codigo)
);

-- Dos claves entre las MISMAS dos tablas: dos líneas separadas.
CREATE TABLE aristas.i_doble (
  id      bigint PRIMARY KEY,
  autor   bigint NOT NULL REFERENCES aristas.a_padre (id),
  revisor bigint          REFERENCES aristas.a_padre (id)
);

-- Hacia otro esquema: no se dibuja, se cuenta en el panel.
CREATE TABLE otro.externa (id bigint PRIMARY KEY);
CREATE TABLE aristas.j_hacia_afuera (
  id         bigint PRIMARY KEY,
  externa_id bigint NOT NULL REFERENCES otro.externa (id)
);

ANALYZE aristas.a_padre;

-- ============================================================ para romper
-- Datos preparados para que ciertas operaciones FALLEN al aplicarse. Están para
-- comprobar que el error se explica bien y que la transacción revierte.

CREATE SCHEMA IF NOT EXISTS demo;

-- Tiene nulos: un SET NOT NULL sobre `apodo` tiene que fallar.
CREATE TABLE demo.con_nulos (
  id    bigint PRIMARY KEY,
  apodo text
);
INSERT INTO demo.con_nulos VALUES (1, 'ana'), (2, NULL), (3, 'beto');

-- Tiene repetidos: un índice único sobre `codigo` tiene que fallar.
CREATE TABLE demo.con_repetidos (
  id     bigint PRIMARY KEY,
  codigo text NOT NULL
);
INSERT INTO demo.con_repetidos VALUES (1, 'AAA'), (2, 'BBB'), (3, 'AAA');

-- Tiene huérfanos: una clave foránea de `huerfanos.cliente_id` a
-- `demo.clientes.id` tiene que fallar, porque el 999999 no existe.
CREATE TABLE demo.huerfanos (
  id         bigint PRIMARY KEY,
  cliente_id bigint NOT NULL
);
INSERT INTO demo.huerfanos VALUES (1, 1), (2, 999999);

SELECT 'listo: demo, aristas, otro' AS estado;
