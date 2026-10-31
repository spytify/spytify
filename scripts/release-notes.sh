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
set -euo pipefail

tag="$1"
repo="${GITHUB_REPOSITORY:-spytify/spytify}"
release="$(gh release view "$tag" --repo "$repo" --json assets,body)"

# GitHub renames some files as it stores them (a .deb's "~" becomes "."):
# the links take the names it gives back.
link() { echo "[$1](https://github.com/$repo/releases/download/$tag/$1)"; }
# In MiB, as GitHub shows the sizes.
size() { awk -v b="$1" 'BEGIN { m = b / 1048576; printf(m >= 10 ? "%.0f MB" : "%.1f MB", m) }'; }

# Each file, by the end of its name, and the system it is for, in the
# table's order.
systems=(
  "-windows-amd64.zip|Windows 10 (2004 or later) or 11"
  "-macos.dmg|macOS 14.2 or later"
  "_amd64.deb|Debian 12, Ubuntu 22.04 or later (x64)"
  "_arm64.deb|Debian 12, Ubuntu 22.04 or later (ARM)"
  ".x86_64.rpm|Fedora, openSUSE (x64)"
  ".aarch64.rpm|Fedora, openSUSE (ARM)"
  "-linux-amd64.tar.gz|Another Linux (x64)"
  "-linux-arm64.tar.gz|Another Linux (ARM)"
  "-macos-cli.zip|macOS, the command line alone"
)

assets="$(jq -r '.assets[] | "\(.name)\t\(.size)"' <<<"$release")"
rows=""
listed=" SHA256SUMS "
for s in "${systems[@]}"; do
  suffix="${s%%|*}"
  while IFS=$'\t' read -r name bytes; do
    if [[ -n "$name" && "$name" == *"$suffix" ]]; then
      rows+="| ${s#*|} | $(link "$name") | $(size "$bytes") |"$'\n'
      listed+="$name "
    fi
  done <<<"$assets"
done
# A file the list above does not know still gets its row.
while IFS=$'\t' read -r name bytes; do
  if [[ -n "$name" && "$listed" != *" $name "* ]]; then
    rows+="| Other | $(link "$name") | $(size "$bytes") |"$'\n'
  fi
done <<<"$assets"

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
echo "## Downloads"
echo
echo "| System | File | Size |"
echo "|---|---|---|"
printf '%s' "$rows"
echo
echo "The Windows zip comes with ffmpeg, and the .deb installs it. Elsewhere, install it first: \`brew install ffmpeg\` on macOS, or from the system's packages on Linux. $(link SHA256SUMS) has every file's checksum, and the [install page](https://spytify.github.io/spytify/v2/install.html) says more."
echo
jq -r .body <<<"$release"
