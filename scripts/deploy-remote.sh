#!/usr/bin/env bash
# Builds OMV Home and installs it on a remote OMV machine over SSH, in one command.
#   scripts/deploy-remote.sh usuario@IP-DA-VM            (amd64, the usual PC / VirtualBox case)
#   scripts/deploy-remote.sh usuario@IP-DA-VM arm64      (Raspberry Pi and other ARM boxes)
#   SSH_PORT=2222 scripts/deploy-remote.sh usuario@localhost   (VirtualBox NAT with port forwarding)
# Needs: ssh/scp, Node, Go (Git Bash on Windows works). The remote user needs sudo.
set -euo pipefail
target="${1:?usage: scripts/deploy-remote.sh user@host [amd64|arm64]}"
arch="${2:-amd64}"
port="${SSH_PORT:-22}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v go >/dev/null || export PATH="/c/Program Files/Go/bin:$PATH"

GOOS=linux GOARCH="$arch" bash "$root/scripts/build.sh"

echo "==> copying to $target"
ssh -p "$port" "$target" 'rm -rf /tmp/omvhome && mkdir -p /tmp/omvhome'
scp -P "$port" -r "$root/deploy" "$root/bin/omv-home" "$target:/tmp/omvhome/"

echo "==> installing (sudo password may be asked)"
ssh -p "$port" -t "$target" 'cd /tmp/omvhome && sudo bash deploy/install.sh --binary ./omv-home'

host="${target#*@}"
echo
if [ "$port" != "22" ]; then host="localhost"; fi # NAT with port forwarding: reach it through the host
echo "Done. Open http://$host:8080 and sign in with your OMV administrator account."
echo "If OMV is not on port 80 there, set OMVHOME_OMV_URL in /etc/omv-home.env and restart omv-home."
