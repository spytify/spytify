#!/usr/bin/env bash
# Builds Spytify for Windows (x64) into bin/windows (or the folder given):
#   Spytify.exe       the desktop app
#   spytifyd.exe      the recorder, which the app starts
#   spytify-cli.exe   the command line
#   spytify-tap.exe   the capture helper (WASAPI process loopback)
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
# SPYTIFY_VERSION (such as 2.0.0-alpha.1) is set by the release workflow.
version="${SPYTIFY_VERSION:-2.0.0-alpha+$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
ldflags="-s -w -X github.com/spytify/spytify/pkg/proto.Version=v$version"
ldflags+=" -X github.com/spytify/spytify/internal/target.DefaultProcess=${SPYTIFY_DEFAULT_PROCESS:-}"

export GOOS=windows GOARCH=amd64
for cmd in spytifyd spytify-cli; do
  CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$out/$cmd.exe" "./cmd/$cmd"
done
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
  go build -trimpath -ldflags "$ldflags -H=windowsgui" -o "$out/Spytify.exe" ./cmd/spytify-ui
x86_64-w64-mingw32-g++ -O2 -Wall -Wextra -static -municode \
  -o "$out/spytify-tap.exe" windows/spytify-tap/main.cpp -lole32 -luuid

echo "built v$version into $out/: Spytify.exe spytifyd.exe spytify-cli.exe spytify-tap.exe"
