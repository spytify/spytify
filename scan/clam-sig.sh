#!/usr/bin/env bash
# Throwaway (scan-behaviour branch). Which ClamAV signature matches the
# app, and what it looks for.
report="$RUNNER_TEMP/report"
mkdir -p "$report"
sudo apt-get install -y -qq clamav clamav-freshclam unzip >/dev/null 2>&1
sudo systemctl stop clamav-freshclam >/dev/null 2>&1
sudo freshclam >/dev/null 2>&1
x="$RUNNER_TEMP/x"; mkdir -p "$x"
unzip -q "$RUNNER_TEMP/assets/spytify-${RELEASE}-windows-amd64.zip" -d "$x"
exe="$(find "$x" -name Spytify.exe | head -n 1)"
clamscan --detect-pua=yes --debug "$exe" 2>&1 | grep -iE 'pseudosigner|matched|signature|certificate|authenticode|pe:|security|virusname' | head -n 80 | tee "$report/clamscan-debug.txt"
sigtool --find-sigs 'Pseudosigner' 2>/dev/null | tee "$report/sigs.txt" | head -n 20
sigtool --find-sigs 'Pseudosigner-96' 2>/dev/null | sigtool --decode-sigs 2>/dev/null | tee "$report/decoded.txt" | head -n 60
echo "== $(grep -c . "$report/sigs.txt") Pseudosigner signatures"
exit 0
