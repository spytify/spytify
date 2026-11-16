#!/usr/bin/env bash
# Makes the compressed disk image of Spytify.app that CI and the release
# ship, with a link to /Applications to drag the app onto.
#
#   scripts/dmg-macos.sh dist/Spytify.app dist/Spytify-macos.dmg
#
# On GitHub's macOS runners hdiutil now and then fails with "Resource
# busy", something else holding the new image for a moment; it is tried
# again, up to five times.
set -euo pipefail
app="${1:?usage: dmg-macos.sh <Spytify.app> <image.dmg>}"
dmg="${2:?usage: dmg-macos.sh <Spytify.app> <image.dmg>}"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
# ditto keeps the signatures and the executable bits.
ditto "$app" "$stage/$(basename "$app")"
ln -s /Applications "$stage/Applications"
for try in 1 2 3 4 5; do
  if hdiutil create -volname Spytify -srcfolder "$stage" -ov -format UDZO "$dmg"; then
    exit 0
  fi
  [ "$try" = 5 ] && break
  echo "hdiutil failed (try $try of 5); trying again in $((try * 5)) s" >&2
  sleep $((try * 5))
done
exit 1
