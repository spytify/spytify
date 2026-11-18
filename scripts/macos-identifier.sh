#!/usr/bin/env bash
# Prints the code signing identifier of one of Spytify's macOS programs,
# by its file name. Signed without one, a program that has no Info.plist
# gets an identifier codesign makes up from its build, such as
# spytifyd-5555494498…, new at every build: macOS then takes each update
# for another program. scripts/build-macos.sh and scripts/sign-macos.sh
# sign with these.
set -euo pipefail
case "${1:?usage: macos-identifier.sh <program>}" in
  spytify-ui) echo org.spytify.ui ;;
  spytify-tap) echo org.spytify.tap ;;
  spytifyd | spytify-cli | ffmpeg) echo "org.spytify.$1" ;;
  *)
    echo "no identifier for $1" >&2
    exit 1
    ;;
esac
