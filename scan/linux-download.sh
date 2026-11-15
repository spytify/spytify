#!/usr/bin/env bash
# Throwaway (scan-behaviour branch). Downloads every asset of the release
# and checks it against SHA256SUMS.   linux-download.sh [dest]
set -euo pipefail
release="${RELEASE:?}"
dest="${1:-$RUNNER_TEMP/assets}"
base="https://github.com/spytify/spytify/releases/download/$release"
mkdir -p "$dest"
cd "$dest"
curl -sSfL --retry 3 -o SHA256SUMS "$base/SHA256SUMS"
awk '{ sub(/^\*/, "", $2); print $2 }' SHA256SUMS | while read -r name; do
  curl -sSfL --retry 3 -o "$name" "$base/$name"
done
sha256sum -c SHA256SUMS
ls -l
