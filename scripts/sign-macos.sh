#!/usr/bin/env bash
# Signs what scripts/build-macos.sh built with a Developer ID and the
# hardened runtime: the command-line tools and Spytify.app.
#
#   SPYTIFY_SIGN_IDENTITY="Developer ID Application: Name (TEAMID)" scripts/sign-macos.sh dist/macos
#
# With SPYTIFY_SIGN_IDENTITY=- it signs ad hoc, which checks the steps
# without a certificate. Notarising is scripts/notarize-macos.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
dir="${1:?usage: sign-macos.sh <folder with Spytify.app>}"
id="${SPYTIFY_SIGN_IDENTITY:?set SPYTIFY_SIGN_IDENTITY}"
ent=macos/Spytify.entitlements
ts=--timestamp
[ "$id" = - ] && ts=--timestamp=none
sign() { codesign --force --options runtime "$ts" --entitlements "$ent" --sign "$id" "$@"; }

for f in spytifyd spytify-cli spytify-tap spytify-ui; do
  [ -f "$dir/$f" ] && sign "$dir/$f"
done
# The bundle's helpers first, then the bundle, which seals them and its
# main executable. ffmpeg reads and writes files, and gets none of the
# app's entitlements.
app="$dir/Spytify.app"
for f in "$app"/Contents/MacOS/*; do
  case "$(basename "$f")" in
    spytify-ui) ;;
    ffmpeg) codesign --force --options runtime "$ts" --sign "$id" "$f" ;;
    *) sign "$f" ;;
  esac
done
sign "$app"
codesign --verify --strict --deep "$app"
echo "signed $app and the tools in $dir as $id"
