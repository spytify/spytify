#!/usr/bin/env bash
# Builds Spytify for Windows (x64) into bin/windows (or the folder given):
#   Spytify.exe       the desktop app
#   spytifyd.exe      the recorder, which the app starts
#   spytify-cli.exe   the command line
#   spytify-tap.exe   the capture helper (WASAPI process loopback)
#   ffmpeg.exe        the encoder, so nothing has to be installed: FFmpeg
#                     and LAME built from their sources with only what
#                     Spytify runs (scripts/build-ffmpeg.sh; SPYTIFY_NO_FFMPEG=1
#                     leaves it out)
#   opengl\           Mesa's OpenGL in software, which the app draws with
#                     when no graphics driver offers OpenGL: a build from
#                     pal1000/mesa-dist-win, pinned and checked
#                     (SPYTIFY_NO_MESA=1 leaves it out)
# Cross-compiles from Linux or macOS with Go and MinGW-w64
# (x86_64-w64-mingw32-gcc, -g++ and -windres; Debian: apt install
# mingw-w64), builds ffmpeg with make and nasm, and unpacks Mesa with 7-Zip
# (7z or 7zz; Debian: apt install 7zip). Spytify.exe carries the logo as its icon and its version.
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
tmp=$(mktemp -d)
# Nothing made here stays in the tree, where a later Windows build would
# link a stale .syso.
trap 'rm -rf "$tmp" cmd/spytify-ui/rsrc_windows_amd64.syso' EXIT

# shellcheck source=scripts/fetch.sh
. scripts/fetch.sh

# The app's icon and version information: windres compiles
# windows/spytify.rc into an object, which go build links from the
# package's folder. The tools that make the icon and check the result run
# here, so they build before GOOS is set.
go build -o "$tmp/" ./internal/tools/genico ./internal/tools/checkexe
"$tmp/genico" -o "$tmp/spytify.ico" assets/logo256px.png
IFS=. read -r major minor patch <<<"${version%%[-+]*}"
# windres passes -D to the preprocessor through a shell: the quotes of a
# string are escaped.
x86_64-w64-mingw32-windres -O coff -I "$tmp" \
  -D VERSION_MAJOR="$major" -D VERSION_MINOR="$minor" -D VERSION_PATCH="$patch" \
  -D "VERSION=\\\"$version\\\"" \
  -o cmd/spytify-ui/rsrc_windows_amd64.syso windows/spytify.rc

# Spytify.exe loads opengl32.dll itself, when Fyne first draws, rather than
# linking it, which would load Windows' before main: with no graphics
# driver offering OpenGL, the app loads Mesa's first (internal/ui/opengl).
# go-gl/gl links it for wglGetProcAddress alone. Its file doing so is
# swapped for windows/go-gl-procaddr.go.in, in a copy of the module go.mod
# pins, through a go.mod of the build's own. A new go-gl/gl needs the swap
# redone from its procaddr.go, and this checksum updated.
go mod download github.com/go-gl/gl
gl="$(go list -m -f '{{.Dir}}' github.com/go-gl/gl)"
if [ "$(sha256 "$gl/v2.1/gl/procaddr.go")" != 59ff0e5420ffab9704873974a71170df2f57db4e0fa035794160512790beecc5 ]; then
  echo "go-gl/gl's v2.1/gl/procaddr.go changed: redo windows/go-gl-procaddr.go.in from it" >&2
  exit 1
fi
mkdir "$tmp/go-gl"
cp "$gl/go.mod" "$tmp/go-gl/"
cp -R "$gl/v2.1" "$tmp/go-gl/"
chmod -R u+w "$tmp/go-gl"
cp windows/go-gl-procaddr.go.in "$tmp/go-gl/v2.1/gl/procaddr.go"
cp go.mod go.sum "$tmp/"
echo "replace github.com/go-gl/gl => $tmp/go-gl" >> "$tmp/go.mod"

export GOOS=windows GOARCH=amd64
for cmd in spytifyd spytify-cli; do
  CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$out/$cmd.exe" "./cmd/$cmd"
done
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
  go build -modfile "$tmp/go.mod" -trimpath -ldflags "$ldflags -H=windowsgui" -o "$out/Spytify.exe" ./cmd/spytify-ui
x86_64-w64-mingw32-g++ -O2 -Wall -Wextra -std=c++17 -static -municode \
  -o "$out/spytify-tap.exe" windows/spytify-tap/main.cpp -lole32 -luuid
"$tmp/checkexe" "$out/Spytify.exe"

# ffmpeg, beside spytifyd.exe, which looks there first after PATH.
if [ "${SPYTIFY_NO_FFMPEG:-}" != 1 ]; then
  scripts/build-ffmpeg.sh "$out" x86_64-w64-mingw32
fi

# Mesa's OpenGL, x64: opengl32.dll and the llvmpipe renderer it loads,
# libgallium_wgl.dll. Nothing loads them from opengl\ but Spytify.exe, and
# only with no graphics driver offering OpenGL.
mesa_url="https://github.com/pal1000/mesa-dist-win/releases/download/26.2.4/mesa3d-26.2.4-release-mingw.7z"
mesa_sha256="36a30a04d8cab6e2bea2d3c747c326d15024d9711dd80f45ef97ac6f2c3c5086"
if [ "${SPYTIFY_NO_MESA:-}" != 1 ]; then
  sevenzip="$(command -v 7z || command -v 7zz)" || { echo "Mesa's archive needs 7-Zip: 7z or 7zz" >&2; exit 1; }
  archive="$(fetch "$mesa_url" "$mesa_sha256")"
  mkdir -p "$out/opengl"
  "$sevenzip" e -y -bso0 -bsp0 -o"$out/opengl" "$archive" x64/opengl32.dll x64/libgallium_wgl.dll
  cp windows/mesa-LICENSES.txt "$out/opengl/LICENSES.txt"
  cat > "$out/opengl/README.txt" <<NOTE
opengl32.dll and libgallium_wgl.dll are Mesa 26.2.4, x64, as built for
Windows by pal1000/mesa-dist-win:
  $mesa_url
Their licenses are in LICENSES.txt. The source is at https://mesa3d.org
(tag mesa-26.2.4) and the build scripts at
https://github.com/pal1000/mesa-dist-win.
Spytify.exe loads them only when no graphics driver on the computer offers
OpenGL, and draws its window with them, in software (llvmpipe).
NOTE
fi

echo "built v$version into $out/: Spytify.exe spytifyd.exe spytify-cli.exe spytify-tap.exe$( [ -f "$out/ffmpeg.exe" ] && echo " ffmpeg.exe")$( [ -d "$out/opengl" ] && echo " opengl\\")"
