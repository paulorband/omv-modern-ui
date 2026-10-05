#!/usr/bin/env bash
# Builds the single omv-home binary with the React app embedded (SPEC-08 R08.1).
#   scripts/build.sh                 -> bin/omv-home (host platform)
#   GOOS=linux GOARCH=arm64 scripts/build.sh  -> cross-compile for a Raspberry Pi / ARM NAS
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "==> frontend"
# `npm ci` wipes node_modules, which fails on Windows while a dev server/editor holds native files open.
# Only do a clean install when there is nothing installed yet; otherwise a plain install is enough.
(
  cd "$root/frontend"
  if [ -d node_modules ]; then npm install --no-audit --no-fund; else npm ci; fi
  npm run build
)

echo "==> embed"
dist="$root/backend/internal/web/dist"
find "$dist" -mindepth 1 ! -name .gitkeep -delete
cp -r "$root/frontend/dist/." "$dist/"

echo "==> backend (${GOOS:-host}/${GOARCH:-host})"
out="$root/bin/omv-home"
[ "${GOOS:-}" = "windows" ] && out="$out.exe"
mkdir -p "$root/bin"
(cd "$root/backend" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out" ./cmd/omv-home)
echo "built $out"
