# Postgres de pruebas con TLS encendido.
#
# La imagen oficial no trae certificado y arranca con `ssl = off`, así que
# hasta la iteración 9 los tests de Postgres corrían siempre en claro y la
# pestaña TLS de S03 no se podía probar contra nada: verify-ca con la raíz
# correcta, verify-full contra un autofirmado, el certificado que se muestra
# después de probar. Se construye una imagen propia que genera un certificado
# autofirmado EN EL BUILD —no en el repo: una clave privada commiteada es una
# clave privada filtrada, aunque sea de juguete— y lo deja donde el servidor lo
# lee.
#
# Vale para `localhost` y `127.0.0.1`, que es desde donde los tests conectan,
# así que verify-full con esta misma raíz cargada tiene que aceptar y con las
# raíces del sistema tiene que rechazar. Es la diferencia que se prueba.
#
# La versión sigue viniendo de PG_VERSION, como antes, para que CI corra la
# matriz igual.
ARG PG_VERSION=18
FROM postgres:${PG_VERSION}-alpine

RUN apk add --no-cache openssl \
    && mkdir -p /certs \
    && openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
        -days 3650 -subj "/CN=kaname-test-postgres/O=Kaname tests" \
        -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
        -keyout /certs/server.key -out /certs/server.crt \
    && chown postgres:postgres /certs/server.key /certs/server.crt \
    && chmod 600 /certs/server.key
