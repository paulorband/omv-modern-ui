#!/usr/bin/env bash
# Installs OMV Home as a systemd service on Debian/OpenMediaVault (SPEC-08 R08.3).
# It never touches nginx or any OMV file. Idempotent. Usage:
#   sudo ./install.sh [--binary PATH]     install or upgrade
#   sudo ./install.sh --uninstall         remove service and binary (keeps app data)
# DESTDIR=/tmp/x ./install.sh             stage into a directory (no systemctl) - used by tests
set -euo pipefail

DESTDIR="${DESTDIR:-}"
BIN_DIR="$DESTDIR/usr/local/bin"
UNIT_DIR="$DESTDIR/etc/systemd/system"
ENV_FILE="$DESTDIR/etc/omv-home.env"
DATA_DIR="$DESTDIR/var/lib/omv-home"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
binary="$here/../bin/omv-home"
uninstall=0

while [ $# -gt 0 ]; do
  case "$1" in
    --uninstall) uninstall=1 ;;
    --binary) shift; binary="${1:?--binary needs a path}" ;;
    -h|--help) sed -n '2,7p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

if [ -z "$DESTDIR" ] && [ "$(id -u)" -ne 0 ]; then
  echo "run as root (sudo)" >&2; exit 1
fi
ctl() { [ -n "$DESTDIR" ] || systemctl "$@"; }

if [ "$uninstall" -eq 1 ]; then
  ctl disable --now omv-home.service 2>/dev/null || true
  rm -f "$UNIT_DIR/omv-home.service" "$BIN_DIR/omv-home"
  ctl daemon-reload
  echo "OMV Home removed. App data kept in $DATA_DIR (and your OMVHOME_APPDATA_DIR); config kept in $ENV_FILE."
  exit 0
fi

[ -f "$binary" ] || { echo "binary not found: $binary (run scripts/build.sh first)" >&2; exit 1; }
if [ -z "$DESTDIR" ]; then
  command -v docker >/dev/null || echo "WARNING: docker not found. Install it with the OMV compose plugin (omv-extras); apps need Docker." >&2
  docker compose version >/dev/null 2>&1 || echo "WARNING: 'docker compose' (v2) not found; the app store will be disabled." >&2
fi

install -d -m 755 "$BIN_DIR" "$UNIT_DIR" "$DATA_DIR"
install -m 755 "$binary" "$BIN_DIR/omv-home"
install -m 644 "$here/omv-home.service" "$UNIT_DIR/omv-home.service"
if [ ! -f "$ENV_FILE" ]; then
  install -m 600 "$here/omv-home.env.example" "$ENV_FILE"
fi

ctl daemon-reload
ctl enable --now omv-home.service
ctl restart omv-home.service
echo "OMV Home installed. Open http://<server-ip>:8080 and sign in with your OMV administrator account."
