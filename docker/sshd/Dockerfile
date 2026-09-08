# Servidor SSH para los tests del túnel.
#
# Se construye una imagen propia en vez de usar una de terceros porque el túnel
# necesita AllowTcpForwarding, y la imagen que se probó primero
# (linuxserver/openssh-server) lo trae en `no` y con el `Include` comentado, así
# que no hay forma de sobrescribirlo desde afuera. Con una imagen propia el
# sshd_config es nuestro, está a la vista y se revisa en el diff.
#
# Todo lo de acá es descartable: credenciales de juguete, claves de host
# generadas en el build. Nada de esto debe parecerse a una configuración real.
FROM alpine:3.22

RUN apk add --no-cache openssh-server \
    && adduser -D -s /bin/sh kaname \
    && echo 'kaname:kaname' | chpasswd \
    && mkdir -p /home/kaname/.ssh \
    && chown -R kaname:kaname /home/kaname/.ssh \
    && chmod 700 /home/kaname/.ssh

# Las claves de host se generan en el build, no en el arranque: así el servidor
# presenta la misma huella mientras el contenedor viva, que es lo que permite
# probar que confiar en una clave y reconectar da "ya conocida" y no "primera
# vez". Un servidor que rota su clave en cada arranque haría pasar el test de
# clave cambiada por accidente y no por lo que se quiere probar.
RUN ssh-keygen -A

COPY sshd_config /etc/ssh/sshd_config

EXPOSE 2222

# -D no se desprende del terminal, -e manda los logs a stderr para que
# `docker compose logs` sirva de algo cuando un test de SSH falle.
CMD ["/usr/sbin/sshd", "-D", "-e"]
