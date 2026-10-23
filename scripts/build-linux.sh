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
# SPYTIFY_VERSION (such as 2.0.0-alpha.1) is set by the release workflow.
version="${SPYTIFY_VERSION:-2.0.0-alpha+$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
arch="$(go env GOARCH)"
ldflags="-s -w -X github.com/spytify/spytify/pkg/proto.Version=v$version"
ldflags+=" -X github.com/spytify/spytify/internal/target.DefaultProcess=${SPYTIFY_DEFAULT_PROCESS:-}"

dist="$out/spytify-$version-linux-$arch"
mkdir -p "$dist"
for cmd in spytifyd spytify-cli; do
  CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$dist/$cmd" "./cmd/$cmd"
done
CGO_ENABLED=1 go build -trimpath -ldflags "$ldflags" -o "$dist/spytify-ui" ./cmd/spytify-ui
cp README.md LICENSE "$dist/"
tar -C "$out" -czf "$dist.tar.gz" "$(basename "$dist")"

# nfpm does not expand variables in file paths: fill the config in here.
config="$out/nfpm.yaml"
sed -e "s|\${VERSION}|$version|g" -e "s|\${ARCH}|$arch|g" -e "s|\${DIST}|$dist|g" packaging/nfpm.yaml > "$config"
for pkg in deb rpm; do
  go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0 package --config "$config" --packager "$pkg" --target "$out/"
done
rm "$config"
echo "built v$version into $out/"
