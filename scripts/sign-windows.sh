#!/usr/bin/env bash
# Signs every .exe in a folder with Authenticode, through osslsigncode:
#
#   SPYTIFY_WINDOWS_PFX=cert.pfx SPYTIFY_WINDOWS_PFX_PASSWORD=... scripts/sign-windows.sh dist/spytify-2.0.0-windows-amd64
#
# SPYTIFY_TIMESTAMP_URL picks the timestamp server; set it empty to skip
# the timestamp (a test certificate, no network).
set -euo pipefail
dir="${1:?usage: sign-windows.sh <folder>}"
pfx="${SPYTIFY_WINDOWS_PFX:?set SPYTIFY_WINDOWS_PFX}"
ts="${SPYTIFY_TIMESTAMP_URL-http://timestamp.digicert.com}"
for f in "$dir"/*.exe; do
  args=(sign -pkcs12 "$pfx" -pass "${SPYTIFY_WINDOWS_PFX_PASSWORD:-}" -h sha256
    -n Spytify -i https://spytify.github.io/spytify/ -in "$f" -out "$f.signed")
  [ -n "$ts" ] && args+=(-ts "$ts")
  osslsigncode "${args[@]}" >/dev/null
  mv "$f.signed" "$f"
  echo "signed $f"
done
