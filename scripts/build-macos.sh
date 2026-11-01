#!/usr/bin/env bash
# Builds Spytify for macOS into bin/ (or the folder given):
#   spytifyd, spytify-cli, spytify-tap   universal command-line binaries
#   Spytify.app                          the desktop app, with the daemon,
#                                        the capture helper and ffmpeg inside
#                                        it (scripts/build-ffmpeg.sh;
#                                        SPYTIFY_NO_FFMPEG=1 leaves ffmpeg out)
# Needs Go and the Xcode command line tools (swiftc, clang, lipo, codesign,
# sips, iconutil), and for ffmpeg make and nasm (brew install nasm).
#
# The player recorded when none is chosen comes from SPYTIFY_DEFAULT_PROCESS,
# a bare name such as "player", from the environment or an untracked
# build.env; without it the app asks for a player first.
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=/dev/null
[ -f build.env ] && . ./build.env
out="${1:-bin}"
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
trap 'rm -rf "$tmp"' EXIT

for cmd in spytifyd spytify-cli; do
  for arch in arm64 amd64; do
    CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$tmp/$cmd-$arch" "./cmd/$cmd"
  done
  lipo -create -output "$out/$cmd" "$tmp/$cmd-arm64" "$tmp/$cmd-amd64"
done

# Process taps exist from macOS 14.2. The Info.plist embedded in the binary
# carries the text macOS shows when it asks for System Audio Recording.
for arch in arm64 x86_64; do
  swiftc -O -target "$arch-apple-macos14.2" \
    -Xlinker -sectcreate -Xlinker __TEXT -Xlinker __info_plist -Xlinker macos/spytify-tap/Info.plist \
    macos/spytify-tap/main.swift -o "$tmp/spytify-tap-$arch"
done
lipo -create -output "$out/spytify-tap" "$tmp/spytify-tap-arm64" "$tmp/spytify-tap-x86_64"

# The UI links Cocoa and OpenGL through cgo.
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC="clang -arch arm64" MACOSX_DEPLOYMENT_TARGET=14.2 \
  go build -trimpath -ldflags "$ldflags" -o "$tmp/spytify-ui-arm64" ./cmd/spytify-ui
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="clang -arch x86_64" MACOSX_DEPLOYMENT_TARGET=14.2 \
  go build -trimpath -ldflags "$ldflags" -o "$tmp/spytify-ui-amd64" ./cmd/spytify-ui
lipo -create -output "$out/spytify-ui" "$tmp/spytify-ui-arm64" "$tmp/spytify-ui-amd64"

# ffmpeg goes in the app only, beside spytifyd, which looks there first
# after PATH. The command-line tools are often put on PATH, where a copy
# this small would stand in for a full ffmpeg.
if [ "${SPYTIFY_NO_FFMPEG:-}" != 1 ]; then
  for arch in arm64 x86_64; do
    scripts/build-ffmpeg.sh "$tmp/ffmpeg-$arch" "$arch-apple-macos14.2"
  done
  lipo -create -output "$tmp/ffmpeg" "$tmp/ffmpeg-arm64/ffmpeg" "$tmp/ffmpeg-x86_64/ffmpeg"
fi

# The app is put together aside and moved in whole: Finder, when it sees a
# bundle before its icon is written, keeps a blank icon for it.
app="$tmp/Spytify.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$out/spytify-ui" "$out/spytifyd" "$out/spytify-tap" "$out/spytify-cli" "$app/Contents/MacOS/"
if [ -f "$tmp/ffmpeg" ]; then
  cp "$tmp/ffmpeg" "$app/Contents/MacOS/"
  # codesign takes anything in Contents/MacOS for code: the license and the
  # README go with the other resources.
  cp "$tmp/ffmpeg-arm64/ffmpeg-LICENSE.txt" "$tmp/ffmpeg-arm64/ffmpeg-README.txt" "$app/Contents/Resources/"
fi
sed "s/@VERSION@/$version/g" macos/Info.plist > "$app/Contents/Info.plist"
iconset="$tmp/AppIcon.iconset"
mkdir -p "$iconset"
for size in 16 32 128 256; do
  sips -z $size $size macos/AppIcon.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z $double $double macos/AppIcon.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

for f in spytifyd spytify-cli spytify-tap spytify-ui; do
  codesign --force --sign - "$out/$f" >/dev/null 2>&1
done
for f in "$app"/Contents/MacOS/*; do
  [ "$(basename "$f")" = spytify-ui ] || codesign --force --sign - "$f" >/dev/null 2>&1
done
codesign --force --sign - "$app" >/dev/null 2>&1
rm -rf "$out/Spytify.app"
mv "$app" "$out/"
echo "built v$version into $out/: Spytify.app spytify-ui spytifyd spytify-cli spytify-tap"
