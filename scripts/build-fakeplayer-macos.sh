#!/usr/bin/env bash
# Builds FakePlayer.app, the scriptable test player for macOS, into the
# folder given (bin/ by default). Tests find it through SPYTIFY_FAKE_APP:
#
#   scripts/build-fakeplayer-macos.sh && SPYTIFY_FAKE_APP=bin/FakePlayer.app go test ./internal/meta/
set -euo pipefail
cd "$(dirname "$0")/.."
out="${1:-bin}"
app="$out/FakePlayer.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp macos/fakeplayer/Info.plist "$app/Contents/Info.plist"
cp macos/fakeplayer/FakePlayer.sdef "$app/Contents/Resources/FakePlayer.sdef"
swiftc -O -target "$(uname -m)-apple-macos14.0" macos/fakeplayer/main.swift -o "$app/Contents/MacOS/FakePlayer"
codesign --force --sign - "$app" >/dev/null 2>&1
echo "built $app"
