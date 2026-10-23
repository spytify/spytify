#!/usr/bin/env bash
# Builds Spytify for macOS into bin/ (or the folder given):
#   spytifyd, spytify-cli, spytify-tap   universal command-line binaries
#   Spytify.app                          the desktop app, with the daemon and
#                                        the capture helper inside it
# Needs Go and the Xcode command line tools (swiftc, clang, lipo, codesign,
# sips, iconutil).
set -euo pipefail

cd "$(dirname "$0")/.."
out="${1:-bin}"
mkdir -p "$out"
version="2.0.0-alpha+$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
ldflags="-s -w -X github.com/spytify/spytify/pkg/proto.Version=v$version"
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

app="$out/Spytify.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$out/spytify-ui" "$out/spytifyd" "$out/spytify-tap" "$out/spytify-cli" "$app/Contents/MacOS/"
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
for f in spytifyd spytify-cli spytify-tap; do
  codesign --force --sign - "$app/Contents/MacOS/$f" >/dev/null 2>&1
done
codesign --force --sign - "$app" >/dev/null 2>&1
echo "built v$version into $out/: Spytify.app spytify-ui spytifyd spytify-cli spytify-tap"
