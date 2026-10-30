# shellcheck shell=bash
# Sourced by the build scripts that download what they ship.

sha256() { (sha256sum "$1" 2>/dev/null || shasum -a 256 "$1") | cut -d' ' -f1; }

# fetch downloads a pinned file into the cache once, checks it against its
# checksum and prints its path.
fetch() {
  local cache="${XDG_CACHE_HOME:-$HOME/.cache}/spytify" file
  file="$cache/$(basename "$1")"
  mkdir -p "$cache"
  if [ ! -f "$file" ]; then
    curl -fsSL -o "$file.part" "$1"
    mv "$file.part" "$file"
  fi
  if [ "$(sha256 "$file")" != "$2" ]; then
    rm -f "$file"
    echo "$(basename "$1") does not match its checksum" >&2
    return 1
  fi
  echo "$file"
}
