#!/bin/sh
# Después de instalar el .deb/.rpm: que el menú y el tema de íconos vean lo
# nuevo sin cerrar sesión. Ninguno de los tres es obligatorio para que la
# aplicación corra; si faltan, solo tarda más en aparecer en el menú.

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi

if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
fi

if command -v update-mime-database >/dev/null 2>&1; then
  update-mime-database -n /usr/share/mime || true
fi

exit 0
