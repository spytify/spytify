#!/usr/bin/env bash
# Throwaway (scan-behaviour branch). Scans every file of the release with
# free tools: ClamAV, the YARA Forge rules, capa, lintian and rpmlint, and
# the ELF hardening flags. Everything is best effort; outputs go to the
# report and only counts are printed.
scan="$(cd "$(dirname "$0")" && pwd)"
report="$RUNNER_TEMP/report"
x="$RUNNER_TEMP/extracted"
tools="$RUNNER_TEMP/tools"
mkdir -p "$report/files" "$x" "$tools"
r="$report/files"
note() { echo "== $*"; echo "$*" >>"$report/summary.txt"; }

sudo apt-get install -y -qq clamav clamav-freshclam yara lintian rpm rpmlint p7zip-full desktop-file-utils libcap2-bin cpio >"$r/apt.log" 2>&1
note "tools installed: exit $?"

cd "$RUNNER_TEMP/assets"
for f in *; do
  d="$x/$f.d"
  mkdir -p "$d"
  case "$f" in
    *.zip) unzip -q "$f" -d "$d" ;;
    *.deb) dpkg-deb -x "$f" "$d" ;;
    *.rpm) (cd "$d" && rpm2cpio "$RUNNER_TEMP/assets/$f" | cpio -idm --quiet --no-absolute-filenames) ;;
    *.tar.gz) tar -xzf "$f" -C "$d" ;;
    *.dmg) 7z x -y -o"$d" "$f" >/dev/null ;;
    *) rmdir "$d" ;;
  esac
done
note "extracted: $(find "$x" -type f | wc -l) files"

# ClamAV
sudo systemctl stop clamav-freshclam >/dev/null 2>&1
sudo freshclam >"$r/freshclam.log" 2>&1
note "freshclam: exit $?"
clamscan -r -i --detect-pua=yes --alert-exceeds-max=yes --max-filesize=2000M --max-scansize=2000M \
  "$RUNNER_TEMP/assets" "$x" >"$r/clamscan.txt" 2>&1
note "clamscan: exit $? ($(grep -c ' FOUND$' "$r/clamscan.txt") found)"
grep ' FOUND$' "$r/clamscan.txt" | sed "s|$RUNNER_TEMP/||" | tee -a "$report/summary.txt"

# YARA Forge
for set in core extended; do
  curl -sSfL --retry 3 -o "$tools/yara-$set.zip" "https://github.com/YARAHQ/yara-forge/releases/latest/download/yara-forge-rules-$set.zip" &&
    unzip -q -o "$tools/yara-$set.zip" -d "$tools/yara-$set"
  rules="$(find "$tools/yara-$set" -name '*.yar' | head -n 1)"
  if [ -n "$rules" ]; then
    yara -w -r "$rules" "$x" >"$r/yara-$set.txt" 2>"$r/yara-$set.err"
    note "yara $set: exit $? ($(wc -l <"$r/yara-$set.txt") matches)"
  else
    note "yara $set: no rules"
  fi
done

# capa
curl -sSfL --retry 3 -o "$tools/capa.zip" https://github.com/mandiant/capa/releases/download/v10.0.0/capa-v10.0.0-linux.zip &&
  unzip -q -o "$tools/capa.zip" -d "$tools/capa"
capa="$(find "$tools/capa" -type f -name capa | head -n 1)"
if [ -n "$capa" ]; then
  chmod +x "$capa"
  # One at a time: several at once ran the runner out of memory.
  find "$x" -type f \( -name 'spytify-tap.exe' -o -name 'spytify-cli.exe' -o -name 'spytifyd.exe' -o -name 'Spytify.exe' \) -path '*windows*' >"$tools/capa-list"
  find "$x" -type f -name spytifyd -path '*amd64.deb.d*' >>"$tools/capa-list"
  while read -r f; do
    base="$(basename "$(dirname "$f")")-$(basename "$f")"
    timeout 900 "$capa" -q "$f" >"$r/capa-$base.txt" 2>"$r/capa-$base.err"
    note "capa $base: exit $?"
  done <"$tools/capa-list"
  note "capa: $(ls "$r"/capa-*.txt 2>/dev/null | wc -l) reports"
else
  note "capa: not downloaded"
fi

# Packages
for deb in "$RUNNER_TEMP"/assets/*.deb; do
  b="$(basename "$deb")"
  lintian --no-tag-display-limit -I -E --pedantic "$deb" >"$r/lintian-$b.txt" 2>&1
  note "lintian $b: $(grep -cE '^[EW]:' "$r/lintian-$b.txt") errors and warnings"
  { dpkg-deb -I "$deb"; dpkg-deb -c "$deb"; } >"$r/dpkg-$b.txt" 2>&1
done
for rpm in "$RUNNER_TEMP"/assets/*.rpm; do
  b="$(basename "$rpm")"
  { rpm -qp --scripts "$rpm"; rpm -qpl "$rpm"; rpm -qpi "$rpm"; } >"$r/rpm-$b.txt" 2>&1
  rpmlint "$rpm" >"$r/rpmlint-$b.txt" 2>&1
  note "rpmlint $b: $(tail -n 1 "$r/rpmlint-$b.txt")"
done

# ELF hardening, capabilities, setuid, desktop file
{
  find "$x" -type f -exec sh -c 'head -c 4 "$1" | grep -q "ELF" && echo "$1"' _ {} \; | while read -r f; do
    echo "## ${f#$x/}"
    readelf -hW "$f" 2>/dev/null | grep -E 'Type:|Machine:'
    readelf -lW "$f" 2>/dev/null | grep -E 'GNU_RELRO|GNU_STACK|INTERP'
    readelf -dW "$f" 2>/dev/null | grep -E 'BIND_NOW|FLAGS|RUNPATH|RPATH'
  done
} >"$r/elf.txt"
getcap -r "$x" >"$r/getcap.txt" 2>&1
find "$x" -perm /6000 -type f >"$r/setuid.txt"
find "$x" -name '*.desktop' -exec desktop-file-validate {} \; >"$r/desktop-file-validate.txt" 2>&1
note "elf: $(grep -c '^## ' "$r/elf.txt") files, $(grep -c 'EXEC' "$r/elf.txt") not position-independent; setuid: $(wc -l <"$r/setuid.txt"); capabilities: $(grep -vc '^$' "$r/getcap.txt")"
exit 0
