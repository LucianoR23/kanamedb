-- Dos bases con esquemas distintos a propósito, para probar S20 (comparar
-- esquemas) a mano. Se corre contra el Postgres de pruebas:
--
--   docker exec -i kaname-postgres-1 psql -U kaname -d kaname_test < docker/demo-drift.sql
--
-- Después, dos conexiones —una a `drift_origen` y otra a `drift_destino`— y
-- «Comparar esquemas…» en el gestor de conexiones. Lo que tiene que salir:
--
--   + tabla productos                (CREATE TABLE, sin los defaults: el catálogo no los trae)
--   + columna clientes.email         (ADD COLUMN)
--   + columna pedidos.producto_id    (ADD COLUMN)
--   + columna pedidos.nota           (ADD COLUMN)
--   + clave foránea pedidos → productos   (ADD CONSTRAINT; en el archivo va DESPUÉS del CREATE TABLE)
--   + vista v_pedidos                (sin sentencia: la definición se pide aparte)
--   ~ columna clientes.nombre        (NOT NULL en el origen, acepta NULL en el destino)
--   - columna pedidos.legacy_ref     (sin sentencia: borrar no se genera nunca)
--   - tabla viejo                    (sin sentencia)

DROP DATABASE IF EXISTS drift_origen;
DROP DATABASE IF EXISTS drift_destino;
CREATE DATABASE drift_origen;
CREATE DATABASE drift_destino;

\connect drift_origen

CREATE TABLE clientes (
    id     bigserial PRIMARY KEY,
    nombre text NOT NULL,
    email  text
);
CREATE TABLE productos (
    id     bigserial PRIMARY KEY,
    nombre text NOT NULL,
    precio numeric(10,2) NOT NULL DEFAULT 0
);
CREATE TABLE pedidos (
    id          bigserial PRIMARY KEY,
    cliente_id  bigint NOT NULL REFERENCES clientes (id),
    producto_id bigint REFERENCES productos (id),
    nota        text
);
CREATE VIEW v_pedidos AS SELECT * FROM pedidos;

\connect drift_destino

CREATE TABLE clientes (
    id     bigserial PRIMARY KEY,
    nombre text
);
CREATE TABLE pedidos (
    id         bigserial PRIMARY KEY,
    cliente_id bigint NOT NULL REFERENCES clientes (id),
    legacy_ref text
);
CREATE TABLE viejo (
    id int PRIMARY KEY
);
