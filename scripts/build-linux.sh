#!/usr/bin/env bash
# Builds Spytify for Linux on this machine's architecture into dist/ (or the
# folder given): spytify-<version>-linux-<arch>.tar.gz, a .deb and an .rpm.
# Needs Go, and for the desktop app gcc with the OpenGL, X11 and Wayland
# headers (Debian: gcc libgl1-mesa-dev xorg-dev libwayland-dev
# libxkbcommon-dev wayland-protocols).
#
# The player recorded when none is chosen comes from SPYTIFY_DEFAULT_PROCESS,
# from the environment or an untracked build.env; without it the app asks
# for a player first.
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=/dev/null
[ -f build.env ] && . ./build.env
out="${1:-dist}"
# The release workflow sets SPYTIFY_VERSION; any other build is a
# development one after the latest tag (scripts/version.sh).
version="$(scripts/version.sh)"
arch="$(go env GOARCH)"
ldflags="-s -w -X github.com/spytify/spytify/pkg/proto.Version=v$version"
ldflags+=" -X github.com/spytify/spytify/internal/target.DefaultProcess=${SPYTIFY_DEFAULT_PROCESS:-}"
# Crash reports and anonymous stats go to the Sentry project in
# SPYTIFY_SENTRY_DSN; a build without it sends nothing.
ldflags+=" -X github.com/spytify/spytify/internal/telemetry.DSN=${SPYTIFY_SENTRY_DSN:-}"

dist="$out/spytify-$version-linux-$arch"
mkdir -p "$dist"
for cmd in spytifyd spytify-cli; do
  CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$dist/$cmd" "./cmd/$cmd"
done
CGO_ENABLED=1 go build -trimpath -ldflags "$ldflags" -o "$dist/spytify-ui" ./cmd/spytify-ui
# The app needs the newest glibc symbol it links, which comes from this
# machine's glibc.
glibc="$(objdump -T "$dist/spytify-ui" | grep -o 'GLIBC_[0-9.]*' | sort -uV | tail -n 1)"
glibc="${glibc#GLIBC_}"
cp README.md LICENSE "$dist/"
# The desktop entry and the icon at the sizes docks and menus draw it, laid
# out as under /usr/share, where the packages install them from here.
share="$dist/share"
mkdir -p "$share/applications" "$share/icons/hicolor"/{48x48,64x64,128x128,256x256}/apps
cp packaging/linux/org.spytify.ui.desktop "$share/applications/"
cp assets/logo.png "$share/icons/hicolor/128x128/apps/spytify.png"
cp assets/logo256px.png "$share/icons/hicolor/256x256/apps/spytify.png"
for size in 48 64; do
  go run ./internal/tools/genico -size "$size" -o "$share/icons/hicolor/${size}x$size/apps/spytify.png" assets/logo256px.png
done
tar -C "$out" -czf "$dist.tar.gz" "$(basename "$dist")"

# nfpm does not expand variables in file paths: fill the config in here.
config="$out/nfpm.yaml"
sed -e "s|\${VERSION}|$version|g" -e "s|\${ARCH}|$arch|g" -e "s|\${DIST}|$dist|g" -e "s|\${GLIBC}|$glibc|g" packaging/nfpm.yaml > "$config"
for pkg in deb rpm; do
  go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0 package --config "$config" --packager "$pkg" --target "$out/"
done
rm "$config"
echo "built v$version into $out/"
