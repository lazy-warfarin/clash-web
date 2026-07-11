#!/bin/sh
set -eu

if ! getent group clash-web >/dev/null 2>&1; then
  addgroup --system clash-web
fi
if ! getent passwd clash-web >/dev/null 2>&1; then
  adduser --system --ingroup clash-web --home /var/lib/clash-web --no-create-home --shell /usr/sbin/nologin clash-web
fi

install -d -o clash-web -g clash-web -m 0750 /var/lib/clash-web /var/lib/clash-web/profiles /var/lib/clash-web/versions
install -d -o root -g clash-web -m 0750 /run/clash-web
chown root:root /usr/lib/clash-web/mihomo
chmod 0755 /usr/lib/clash-web/mihomo

systemctl daemon-reload >/dev/null 2>&1 || true
deb-systemd-helper enable clash-web-helper.service clash-web.service >/dev/null || true
deb-systemd-invoke restart clash-web-helper.service clash-web.service >/dev/null || true

echo "Clash Web is listening on port 8080."
echo "First-run password: sudo cat /var/lib/clash-web/bootstrap-password"
echo "Configure HTTPS before exposing the service to the public Internet."
