#!/usr/bin/env bash
# Signs and notarises release files (a .dmg or .zip), then staples the
# ticket to the disk images. The App Store Connect API key comes from
# SPYTIFY_NOTARY_KEY (path to the .p8), SPYTIFY_NOTARY_KEY_ID and
# SPYTIFY_NOTARY_ISSUER; the signature from SPYTIFY_SIGN_IDENTITY.
#
#   scripts/notarize-macos.sh dist/Spytify-2.0.0-macos.dmg dist/spytify-2.0.0-macos-cli.zip
set -euo pipefail
key="${SPYTIFY_NOTARY_KEY:?set SPYTIFY_NOTARY_KEY}"
for f in "$@"; do
  case "$f" in *.dmg) codesign --force --timestamp --sign "${SPYTIFY_SIGN_IDENTITY:?}" "$f" ;; esac
  xcrun notarytool submit "$f" --key "$key" --key-id "${SPYTIFY_NOTARY_KEY_ID:?}" --issuer "${SPYTIFY_NOTARY_ISSUER:?}" --wait
  case "$f" in *.dmg) xcrun stapler staple "$f" ;; esac
done
