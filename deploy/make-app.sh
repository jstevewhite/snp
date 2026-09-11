#!/bin/bash
# make-app.sh — package bin/snp-desktop into a signed, notarized macOS
# .app bundle (spec §12).
#
# usage: make-app.sh [SIGN_IDENTITY] [NOTARY_PROFILE]
#
#   SIGN_IDENTITY    codesign identity (name, email, or SHA-1 hash).
#                    Default: ad-hoc ("-") if not given on the command
#                    line or via the SIGN_IDENTITY environment variable.
#                    An ad-hoc bundle cannot be notarized and is skipped.
#   NOTARY_PROFILE   notarytool keychain profile (xcrun notarytool
#                    store-credentials <profile>). When set with a real
#                    identity the bundle is submitted, stapled, and a
#                    distribution zip is written to build/snp.zip. Set
#                    empty to skip notarization.
#
# Produces build/snp.app: Contents/MacOS/snp-desktop (the binary built
# by `make desktop`), Contents/Info.plist, Contents/Resources/
# AppIcon.icns from deploy/appicon.png (PWA fallback). The bundle is
# signed with --force (hardened runtime, secure timestamp, entitlements)
# and verified with codesign --verify --deep --strict.
#
# Quiet on success; errors to stderr with a non-zero exit.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/bin/snp-desktop"
PLIST="$ROOT/deploy/Info.plist"
# App icon: prefer a dedicated 1024px source (deploy/appicon.png),
# falling back to the PWA icon so a build never fails for lack of one.
ICON_SRC="$ROOT/deploy/appicon.png"
if [ ! -f "$ICON_SRC" ]; then
  ICON_SRC="$ROOT/web/public/pwa-512x512.png"
fi
APP="$ROOT/build/snp.app"

if [ ! -x "$BIN" ]; then
  echo "make-app: $BIN not found — run 'make desktop' first" >&2
  exit 1
fi

# Codesign identity: argument > SIGN_IDENTITY env > ad-hoc.
IDENTITY="${1:-${SIGN_IDENTITY:--}}"
# Notary profile: argument > NOTARY_PROFILE env > none.
NOTARY_PROFILE="${2:-${NOTARY_PROFILE:-}}"

echo "make-app: bundling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/snp-desktop"
cp "$PLIST" "$APP/Contents/Info.plist"

# Icon: generate the AppIcon.icns iconset from ICON_SRC (deploy/
# appicon.png when present, else the PWA icon). A 1024px source is
# ideal; smaller ones are upscaled for the @2x slot.
TMP="$(mktemp -d /tmp/snp-icon.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
ICONSET="$TMP/AppIcon.iconset"
mkdir -p "$ICONSET"
# Scratch 1024px master lives OUTSIDE the iconset directory: iconutil
# rejects any file it does not recognize inside the .iconset.
big="$TMP/icon-1024.png"
sips -z 1024 1024 "$ICON_SRC" --out "$big" >/dev/null
for spec in "16" "32" "128" "256" "512"; do
  sips -z "$spec" "$spec" "$big" --out "$ICONSET/icon_${spec}x${spec}.png" >/dev/null
  d=$((spec * 2))
  sips -z "$d" "$d" "$big" --out "$ICONSET/icon_${spec}x${spec}@2x.png" >/dev/null
done
cp "$big" "$ICONSET/icon_512x512@2x.png"
# iconutil refuses to write into some sandboxed paths, so render to the
# temp dir and copy into the bundle.
iconutil -c icns "$ICONSET" -o "$TMP/AppIcon.icns"
cp "$TMP/AppIcon.icns" "$APP/Contents/Resources/AppIcon.icns"

# Sign. A Developer ID signs with the hardened runtime (--options
# runtime), a secure timestamp (notarization requires it), and the
# network-client entitlement in deploy/snp.entitlements (the desktop app
# makes outbound HTTPS calls itself — Ask-AI talks to the configured
# provider in-process). Ad-hoc ("-") skips all three.
if [ "$IDENTITY" = "-" ]; then
  codesign --force --deep --sign - "$APP"
else
  codesign --force --deep --options runtime --timestamp \
    --entitlements "$ROOT/deploy/snp.entitlements" \
    --sign "$IDENTITY" "$APP"
fi

codesign --verify --deep --strict "$APP"
echo "make-app: signed and verified ($APP)"

# Notarize + staple. Skipped for an ad-hoc signature (Apple rejects
# unsigned submissions) or when no profile is configured.
if [ "$IDENTITY" = "-" ]; then
  echo "make-app: ad-hoc signature — skipping notarization" >&2
elif [ -z "$NOTARY_PROFILE" ]; then
  echo "make-app: no notary profile — skipping notarization" >&2
else
  if ! command -v xcrun >/dev/null 2>&1; then
    echo "make-app: xcrun not found (Xcode command line tools are required for notarization)" >&2
    exit 1
  fi
  ZIP="$ROOT/build/snp.zip"
  echo "make-app: notarizing with profile '$NOTARY_PROFILE' (this can take a few minutes)"
  rm -f "$ZIP"
  ditto -c -k --keepParent "$APP" "$ZIP"
  set +e
  NOTARY_OUT="$(xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait 2>&1)"
  NOTARY_RC=$?
  set -e
  printf '%s\n' "$NOTARY_OUT"
  if [ "$NOTARY_RC" -ne 0 ] || ! printf '%s' "$NOTARY_OUT" | grep -q "status: Accepted"; then
    echo "make-app: notarization failed." >&2
    echo "  Check the profile with: xcrun notarytool history --keychain-profile $NOTARY_PROFILE" >&2
    echo "  Create/repair it with:  xcrun notarytool store-credentials $NOTARY_PROFILE --apple-id <id> --team-id <team> --password <app-specific-password>" >&2
    exit 1
  fi
  echo "make-app: stapling the notarization ticket"
  xcrun stapler staple "$APP"
  xcrun stapler validate "$APP"
  # Re-zip the stapled bundle so build/snp.zip is distribution-ready.
  rm -f "$ZIP"
  ditto -c -k --keepParent "$APP" "$ZIP"
  # Best-effort Gatekeeper assessment. spctl can still decline on the
  # build host (e.g. before the ticket propagates); stapler validate
  # above is the authoritative check.
  if [ -x /usr/sbin/spctl ] && ! /usr/sbin/spctl --assess --type execute -vv "$APP" 2>&1; then
    echo "make-app: note: spctl did not accept the app on this host (staple validation passed)" >&2
  fi
  echo "make-app: notarized and stapled ($APP, zip $ZIP)"
fi
