#!/usr/bin/env bash
# Prints the version a build gets. SPYTIFY_VERSION wins: the release
# workflow sets it. A build at a release tag is that release. Any other is
# a development build after the latest tag: its patch number moved on, and
# the number of commits since, such as 2.0.1-dev.12 twelve commits after
# 2.0.0-alpha.1. It sorts after the tag, so the update check never offers
# a build the release it was made after. v1's tags (1.x) are left out.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -n "${SPYTIFY_VERSION:-}" ]; then
  echo "$SPYTIFY_VERSION"
  exit
fi
if ! tag=$(git describe --tags --abbrev=0 --match '[0-9]*.[0-9]*.[0-9]*' --exclude '1.*' 2>/dev/null); then
  # No git or no tag, such as a source download: a version every release
  # sorts after.
  echo "0.0.0-dev.0"
  exit
fi
commits=$(git rev-list --count "$tag..HEAD")
if [ "$commits" = 0 ]; then
  echo "$tag"
  exit
fi
IFS=. read -r major minor patch <<<"${tag%%[-+]*}"
echo "$major.$minor.$((patch + 1))-dev.$commits"
