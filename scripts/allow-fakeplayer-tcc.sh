#!/usr/bin/env bash
# CI only: lets osascript and FakePlayer send FakePlayer Apple events
# without the Automation prompt nobody can answer on a runner, by writing
# the consent into the user's TCC database. Needs sudo and a runner image
# where that database is writable.
#
#   scripts/allow-fakeplayer-tcc.sh <FakePlayer.app>
set -euo pipefail
app="${1:?usage: allow-fakeplayer-tcc.sh <FakePlayer.app>}"
db="$HOME/Library/Application Support/com.apple.TCC/TCC.db"
tmp="$(mktemp -d)"
# blob prints a code signature's designated requirement as hex.
blob() {
  codesign -d -r- "$1" 2>&1 | sed -n 's/^#* *designated => //p' | csreq -r- -b "$tmp/req"
  xxd -p "$tmp/req" | tr -d '\n'
}
target="$(blob "$app")"
now="$(date +%s)"
row() {
  sudo sqlite3 "$db" "INSERT OR REPLACE INTO access (service, client, client_type, auth_value, auth_reason, auth_version, csreq, indirect_object_identifier_type, indirect_object_identifier, indirect_object_code_identity, flags, last_modified) VALUES ('kTCCServiceAppleEvents', '$1', $2, 2, 3, 1, X'$3', 0, 'org.spytify.fakeplayer', X'$target', 0, $now);"
}
row org.spytify.fakeplayer 0 "$target"
row /usr/bin/osascript 1 "$(blob /usr/bin/osascript)"
sudo sqlite3 "$db" "SELECT client, indirect_object_identifier, auth_value FROM access WHERE service = 'kTCCServiceAppleEvents' AND indirect_object_identifier = 'org.spytify.fakeplayer';"
# tccd reads the database again when it starts.
pkill -x tccd || true
rm -rf "$tmp"
