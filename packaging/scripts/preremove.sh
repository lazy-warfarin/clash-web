#!/bin/sh
set -eu

if [ "${1:-}" = remove ]; then
  deb-systemd-invoke stop clash-web.service clash-web-helper.service >/dev/null || true
fi
