#!/bin/sh
# Installs a built botpanel binary and the systemd units. Run as root from the repository root.
# It never generates or overwrites secrets: create keys with `botpanel keygen`.
set -eu
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
[ -x bin/botpanel ] || { echo "build first: make build" >&2; exit 1; }
install -m 0755 bin/botpanel /usr/local/bin/botpanel
install -d -m 0750 /etc/botpanel /var/lib/botpanel /var/backups/botpanel
install -d -m 0700 /etc/botpanel/keys
[ -e /etc/botpanel/botpanel.env ] || install -m 0640 deploy/systemd/botpanel.env.example /etc/botpanel/botpanel.env
install -m 0644 deploy/systemd/botpanel.service deploy/systemd/botpanel-backup.service deploy/systemd/botpanel-backup.timer /etc/systemd/system/
systemctl daemon-reload
cat <<MSG
Installed. Next steps:
  1. Create an encryption key:   BOTPANEL_KEY_DIR=/etc/botpanel/keys botpanel keygen
     Back the key up somewhere separate from the database backups.
  2. Create the first admin:     set -a; . /etc/botpanel/botpanel.env; set +a; botpanel create-admin you@example.com
  3. Start:                      systemctl enable --now botpanel botpanel-backup.timer
  4. Put a TLS reverse proxy in front of 127.0.0.1:8080 (WebSocket upgrade required).
MSG
