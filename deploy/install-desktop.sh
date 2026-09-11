#!/usr/bin/env bash
# install-desktop.sh — install the snp desktop app for the current user
# (spec §12). Linux; no root required.
#
# usage: install-desktop.sh [-b binary] [-p prefix]
#
#   -b binary   snp-desktop binary to install (default: <repo>/bin/snp-desktop)
#   -p prefix   install prefix                (default: $HOME/.local)
#
# Installs:
#   <prefix>/bin/snp-desktop
#   <prefix>/share/applications/snp.desktop
#   <prefix>/share/icons/hicolor/{192x192,512x512}/apps/snp.png
# then refreshes the desktop/icon caches when the tooling is present.
#
# The .desktop entry launches `snp-desktop` from PATH, so <prefix>/bin
# must be on it (the default on most distributions). Its StartupWMClass
# is `snp`, matching wails' linux ProgramName option in
# cmd/snp-desktop/platform_linux.go.
#
# Quiet on success; errors to stderr with a non-zero exit.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/bin/snp-desktop"
PREFIX="${HOME}/.local"

while getopts "b:p:" opt; do
  case "$opt" in
    b) BIN="$OPTARG" ;;
    p) PREFIX="$OPTARG" ;;
    *) echo "usage: install-desktop.sh [-b binary] [-p prefix]" >&2; exit 2 ;;
  esac
done

if [ ! -x "$BIN" ]; then
  echo "install-desktop: $BIN not found or not executable — run 'make desktop' first" >&2
  exit 1
fi

ICON192="$ROOT/web/public/pwa-192x192.png"
ICON512="$ROOT/web/public/pwa-512x512.png"
for f in "$ROOT/deploy/snp.desktop" "$ICON192" "$ICON512"; do
  if [ ! -f "$f" ]; then
    echo "install-desktop: missing $f" >&2
    exit 1
  fi
done

APPS="$PREFIX/share/applications"
ICONS="$PREFIX/share/icons/hicolor"
install -d "$PREFIX/bin" "$APPS" "$ICONS/192x192/apps" "$ICONS/512x512/apps"
install -m 0755 "$BIN" "$PREFIX/bin/snp-desktop"
install -m 0644 "$ROOT/deploy/snp.desktop" "$APPS/snp.desktop"
install -m 0644 "$ICON192" "$ICONS/192x192/apps/snp.png"
install -m 0644 "$ICON512" "$ICONS/512x512/apps/snp.png"

# Best-effort cache refresh: both tools are optional and their warnings
# (a per-user theme dir without index.theme, for instance) are noise.
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APPS" 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -t -f "$ICONS" 2>/dev/null || true
fi
