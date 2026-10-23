#!/usr/bin/env bash
# Builds spytifyd, spytify-cli and the spytify-tap capture helper for macOS
# as universal binaries (arm64 + x86_64) into bin/ (or the folder given).
# Needs Go and the Xcode command line tools (swiftc, lipo, codesign).
set -euo pipefail

cd "$(dirname "$0")/.."
out="${1:-bin}"
mkdir -p "$out"
version="v2.0.0-alpha+$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for cmd in spytifyd spytify-cli; do
  for arch in arm64 amd64; do
    CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath \
      -ldflags "-s -w -X github.com/spytify/spytify/pkg/proto.Version=$version" \
      -o "$tmp/$cmd-$arch" "./cmd/$cmd"
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

for f in spytifyd spytify-cli spytify-tap; do
  codesign --force --sign - "$out/$f" >/dev/null
done
echo "built $version into $out/: spytifyd spytify-cli spytify-tap"
