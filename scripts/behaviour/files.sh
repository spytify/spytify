#!/usr/bin/env bash
# Behaviour gate (ci.yml). Scans the files of the builds with ClamAV
# (potentially unwanted programs included) and the YARA Forge rules, and
# writes what matched to $RUNNER_TEMP/report for check.py.
#   files.sh <windows build dir> <linux build dir>
win="$1"
lin="$2"
report="$RUNNER_TEMP/report"
x="$RUNNER_TEMP/extracted"
rules="$RUNNER_TEMP/yara"
mkdir -p "$report" "$x/windows" "$x/linux" "$rules"
cp -r "$win/." "$x/windows/"
for f in "$lin"/*.deb; do dpkg-deb -x "$f" "$x/linux/$(basename "$f").d"; done
for f in "$lin"/*.tar.gz; do mkdir -p "$x/linux/$(basename "$f").d"; tar -xzf "$f" -C "$x/linux/$(basename "$f").d"; done

scripts/ci/apt-install.sh clamav clamav-freshclam yara >"$report/apt.log" 2>&1 || { echo "apt failed"; exit 1; }
sudo systemctl stop clamav-freshclam >/dev/null 2>&1
sudo freshclam >"$report/freshclam.log" 2>&1 || { echo "freshclam failed: the gate cannot judge"; exit 1; }
clamscan -r -i --detect-pua=yes --alert-exceeds-max=yes --max-filesize=2000M --max-scansize=2000M "$x" >"$report/clamscan.txt" 2>&1
echo "clamscan: exit $? ($(grep -c ' FOUND$' "$report/clamscan.txt") found)"

for set in core extended; do
  curl -sSfL --retry 3 -o "$rules/$set.zip" "https://github.com/YARAHQ/yara-forge/releases/latest/download/yara-forge-rules-$set.zip" || { echo "YARA Forge $set: download failed"; exit 1; }
  unzip -q -o "$rules/$set.zip" -d "$rules/$set"
  yara -w -r "$(find "$rules/$set" -name '*.yar' | head -n 1)" "$x" >"$report/yara-$set.txt" 2>"$report/yara-$set.err"
  echo "yara $set: exit $? ($(grep -c . "$report/yara-$set.txt") matches)"
done
# Paths in the report start at the extracted folder.
sed -i "s|$x/||" "$report"/clamscan.txt "$report"/yara-*.txt
exit 0
