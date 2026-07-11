#!/bin/sh
set -eu

CONFIG="${CLASH_WEB_CONFIG:-/etc/clash-web/config.yaml}"

install -d -o clash-web -g clash-web -m 0750 \
  /var/lib/clash-web \
  /var/lib/clash-web/profiles \
  /var/lib/clash-web/versions
install -d -o root -g clash-web -m 0750 /run/clash-web
install -d -o root -g clash-web -m 0750 /var/lib/clash-web/runtime

# Keep the privileged supervisor in the clash-web group so both Unix sockets
# are reachable by the unprivileged web process. dumb-init forwards shutdown
# signals to the complete process group.
gosu root:clash-web /usr/bin/clash-web helper --config "$CONFIG" &

exec gosu clash-web:clash-web /usr/bin/clash-web serve --config "$CONFIG"
