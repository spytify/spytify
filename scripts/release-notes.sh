#!/usr/bin/env bash
# Prints the notes of the release TAG, which the release workflow has just
# made with every file and GitHub's list of changes: what the files lack (a
# signature, notarisation), which file each system takes, then that list.
# The highlights are written above them by hand, before publishing.
#
#   scripts/release-notes.sh TAG
#
# Reads the release with gh, from GITHUB_REPOSITORY (spytify/spytify by
# default). NOTARIZED=true and SIGNED=true say the macOS app is notarised
# and the Windows files signed.
#
# The files are named, not linked: a draft's download links answer 404, and
# GitHub lists the files under the notes anyway.
set -euo pipefail

tag="$1"
repo="${GITHUB_REPOSITORY:-spytify/spytify}"
body="$(gh release view "$tag" --repo "$repo" --json body --jq .body)"

# What the files lack, a paragraph each: nothing once they are signed and
# notarised.
lacks=()
[ "${NOTARIZED:-}" = true ] || lacks+=("The macOS app is not notarised: the first time, allow it in System Settings > Privacy & Security > Open Anyway.")
[ "${SIGNED:-}" = true ] || lacks+=("The Windows files are not signed, so SmartScreen warns before the first run. Click \"More info\", then select \"Run anyway\".")
if [ ${#lacks[@]} -gt 0 ]; then
  echo "> [!NOTE]"
  for i in "${!lacks[@]}"; do
    [ "$i" -eq 0 ] || echo ">"
    echo "> ${lacks[$i]}"
  done
  echo
fi
cat <<'NOTES'
## Downloads

Take your system's file from the Assets below:

- Windows 10 (2004 or later) or 11: the `-windows-amd64.zip`
- macOS 14.2 or later: the `-macos.dmg`
- Debian 12, Ubuntu 22.04 or later: the `.deb`, `_amd64` on x64, `_arm64` on ARM
- Fedora, openSUSE: the `.rpm`, `x86_64` on x64, `aarch64` on ARM
- Another Linux: the `-linux-amd64.tar.gz` on x64, `-linux-arm64.tar.gz` on ARM
- macOS, the command line alone: the `-macos-cli.zip`

The Windows zip and the macOS app come with ffmpeg, and the .deb installs it. Elsewhere, install it first: `brew install ffmpeg` for the macOS command line, or from the system's packages on Linux. `SHA256SUMS` has every file's checksum, and the [install page](https://spytify.github.io/spytify/v2/install.html) says more.

NOTES
echo "$body"
