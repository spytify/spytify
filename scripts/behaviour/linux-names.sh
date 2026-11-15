#!/usr/bin/env bash
# Behaviour gate (ci.yml). Reads the player's names the release
# carries so the log and the report can hide them.
#   linux-names.sh           the installed package
#   linux-names.sh tarball   the linux amd64 tarball of the release
set -uo pipefail
scan="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
daemon=/usr/bin/spytifyd
cli=/usr/bin/spytify-cli
if [ "${1:-}" = tarball ]; then
  tarball="$(ls "$RUNNER_TEMP"/assets/*linux*amd64*.tar.gz 2>/dev/null | head -n 1)"
  [ -n "$tarball" ] || { echo "no linux amd64 tarball"; exit 1; }
  tar -xzf "$tarball" -C "$work"
  daemon="$(find "$work" -type f -name spytifyd | head -n 1)"
  cli="$(find "$work" -type f -name spytify-cli | head -n 1)"
fi
export SPYTIFY_SOCKET="$work/s.sock" SPYTIFY_CONFIG="$work/config.toml" XDG_STATE_HOME="$work/state"
"$daemon" >"$work/d.out" 2>"$work/d.err" &
pid=$!
ok=0
for _ in $(seq 30); do
  sleep 1
  if "$cli" info --json >"$work/info.json" 2>"$work/c.err"; then ok=1; break; fi
done
"$cli" shutdown >/dev/null 2>&1
sleep 1
kill "$pid" 2>/dev/null
if [ "$ok" != 1 ]; then
  echo "the recorder never answered; bytes: $(wc -c <"$work/d.out") out, $(wc -c <"$work/d.err") err, $(wc -c <"$work/c.err") cli err"
  exit 1
fi
python3 -I "$scan/names.py" "$work/info.json" "$RUNNER_TEMP/player-names.txt"
status=$?
rm -rf "$work"
exit $status
