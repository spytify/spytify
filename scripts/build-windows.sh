#!/usr/bin/env bash
# Builds Spytify for Windows (x64) into bin/windows (or the folder given):
#   Spytify.exe       the desktop app
#   spytifyd.exe      the recorder, which the app starts
#   spytify-cli.exe   the command line
#   spytify-tap.exe   the capture helper (WASAPI process loopback)
#   ffmpeg.exe        the encoder, so nothing has to be installed: an LGPL
#                     build from BtbN/FFmpeg-Builds, pinned and checked
#                     (SPYTIFY_NO_FFMPEG=1 leaves it out)
# Cross-compiles from Linux or macOS with Go and MinGW-w64
# (x86_64-w64-mingw32-gcc and -g++; Debian: apt install mingw-w64).
#
# The player recorded when none is chosen comes from SPYTIFY_DEFAULT_PROCESS,
# from the environment or an untracked build.env; without it the app asks
# for a player first.
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=/dev/null
[ -f build.env ] && . ./build.env
out="${1:-bin/windows}"
mkdir -p "$out"
# The release workflow sets SPYTIFY_VERSION; any other build is a
# development one after the latest tag (scripts/version.sh).
version="$(scripts/version.sh)"
ldflags="-s -w -X github.com/spytify/spytify/pkg/proto.Version=v$version"
ldflags+=" -X github.com/spytify/spytify/internal/target.DefaultProcess=${SPYTIFY_DEFAULT_PROCESS:-}"
# Crash reports and anonymous stats go to the Sentry project in
# SPYTIFY_SENTRY_DSN; a build without it sends nothing.
ldflags+=" -X github.com/spytify/spytify/internal/telemetry.DSN=${SPYTIFY_SENTRY_DSN:-}"

export GOOS=windows GOARCH=amd64
for cmd in spytifyd spytify-cli; do
  CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$out/$cmd.exe" "./cmd/$cmd"
done
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
  go build -trimpath -ldflags "$ldflags -H=windowsgui" -o "$out/Spytify.exe" ./cmd/spytify-ui
x86_64-w64-mingw32-g++ -O2 -Wall -Wextra -std=c++17 -static -municode \
  -o "$out/spytify-tap.exe" windows/spytify-tap/main.cpp -lole32 -luuid

# ffmpeg, beside spytifyd.exe, which looks there first after PATH.
ffmpeg_url="https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-10-23-13-04/ffmpeg-n9.0.2-35-g9a9b70bb8c-win64-lgpl-9.0.zip"
ffmpeg_sha256="c12c3e71f0055802c732b6b6fb1712c296b7a47461243a8f0454c2c5b25d50d1"
if [ "${SPYTIFY_NO_FFMPEG:-}" != 1 ]; then
  cache="${XDG_CACHE_HOME:-$HOME/.cache}/spytify"
  zip="$cache/$(basename "$ffmpeg_url")"
  mkdir -p "$cache"
  if [ ! -f "$zip" ]; then
    curl -fsSL -o "$zip.part" "$ffmpeg_url"
    mv "$zip.part" "$zip"
  fi
  sum="$( (sha256sum "$zip" 2>/dev/null || shasum -a 256 "$zip") | cut -d' ' -f1)"
  if [ "$sum" != "$ffmpeg_sha256" ]; then
    rm -f "$zip"
    echo "ffmpeg download does not match its checksum" >&2
    exit 1
  fi
  unzip -q -o -j "$zip" "*/bin/ffmpeg.exe" -d "$out"
  unzip -q -o -p "$zip" "*/LICENSE.txt" > "$out/ffmpeg-LICENSE.txt"
  cat > "$out/ffmpeg-README.txt" <<NOTE
ffmpeg.exe is FFmpeg n9.0.2, an LGPL build by BtbN/FFmpeg-Builds:
  $ffmpeg_url
Its license is in ffmpeg-LICENSE.txt. The source is at https://ffmpeg.org
(tag n9.0.2) and the build scripts at https://github.com/BtbN/FFmpeg-Builds.
Spytify runs it as a separate program to encode the recordings.
NOTE
fi

echo "built v$version into $out/: Spytify.exe spytifyd.exe spytify-cli.exe spytify-tap.exe$( [ -f "$out/ffmpeg.exe" ] && echo " ffmpeg.exe")"
