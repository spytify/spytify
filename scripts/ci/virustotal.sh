#!/usr/bin/env bash
# CI only: sends each file to VirusTotal and writes, in the job's summary,
# how many engines detect it and as what. The release workflow runs it on
# the Windows programs of a draft, so a false detection is known, and
# reported to its vendor, before anyone downloads the version.
#   VIRUSTOTAL_API_KEY=... scripts/ci/virustotal.sh <file>...
#
# A free key allows 4 requests a minute: every request waits 16 s first.
# A file VirusTotal already knows is not sent again. Exits 1 when
# VirusTotal cannot be reached or gives no answer; detections are warnings.
set -euo pipefail

api="https://www.virustotal.com/api/v3"
summary="${GITHUB_STEP_SUMMARY:-/dev/stdout}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# vt URL [curl options]... leaves the answer in $tmp/answer and prints its
# HTTP status. Over the quota (429), it waits a minute and asks again.
vt() {
  local url="$1" code
  shift
  for try in 1 2 3 4 5; do
    sleep "${VT_WAIT:-16}"
    code="$(curl -sS -o "$tmp/answer" -w '%{http_code}' -H "x-apikey: $VIRUSTOTAL_API_KEY" "$@" "$url")"
    [ "$code" = 429 ] || break
    echo "VirusTotal quota reached (try $try), waiting a minute" >&2
    sleep "${VT_QUOTA_WAIT:-60}"
  done
  echo "$code"
}

fail() {
  echo "::error title=VirusTotal::$1" >&2
  exit 1
}

# Over 32 MB, a file goes to an upload URL VirusTotal gives for it.
send() {
  local file="$1" url="$api/files" code
  if [ "$(wc -c <"$file")" -gt $((32 << 20)) ]; then
    code="$(vt "$api/files/upload_url")"
    [ "$code" = 200 ] || fail "no upload URL for $(basename "$file") (HTTP $code)"
    url="$(jq -r .data "$tmp/answer")"
  fi
  code="$(vt "$url" -F "file=@$file")"
  [ "$code" = 200 ] || fail "$(basename "$file") was not sent (HTTP $code)"
  jq -r .data.id "$tmp/answer"
}

# The engines' verdicts on one file, once its analysis is done, as a
# {stats, results} object.
verdicts() {
  local file="$1" sha="$2" code id status
  code="$(vt "$api/files/$sha")"
  if [ "$code" = 200 ] && [ "$(jq '.data.attributes.last_analysis_results | length' "$tmp/answer")" -gt 0 ]; then
    jq '.data.attributes | {stats: .last_analysis_stats, results: .last_analysis_results}' "$tmp/answer"
    return
  fi
  [ "$code" = 200 ] || [ "$code" = 404 ] || fail "no answer about $(basename "$file") (HTTP $code)"
  id="$(send "$file")"
  # An analysis takes a few minutes; 20 minutes at most.
  for _ in $(seq 40); do
    code="$(vt "$api/analyses/$id")"
    [ "$code" = 200 ] || fail "no analysis of $(basename "$file") (HTTP $code)"
    status="$(jq -r .data.attributes.status "$tmp/answer")"
    if [ "$status" = completed ]; then
      jq '.data.attributes | {stats, results}' "$tmp/answer"
      return
    fi
    sleep "${VT_POLL_WAIT:-14}"
  done
  fail "the analysis of $(basename "$file") did not end in 20 minutes"
}

{
  echo "## VirusTotal"
  echo
  echo "| File | Detections | Detected as | Report |"
  echo "|---|---|---|---|"
} >>"$summary"
for file in "$@"; do
  name="$(basename "$file")"
  sha="$(sha256sum "$file" | cut -d' ' -f1)"
  v="$(verdicts "$file" "$sha")"
  found="$(jq '.stats.malicious + .stats.suspicious' <<<"$v")"
  engines="$(jq '.stats.malicious + .stats.suspicious + .stats.undetected + .stats.harmless' <<<"$v")"
  as="$(jq -r '[.results[] | select(.category == "malicious" or .category == "suspicious") | "\(.engine_name): \(.result // .category)"] | join(", ")' <<<"$v")"
  echo "| \`$name\` | $found / $engines | ${as:-none} | [report](https://www.virustotal.com/gui/file/$sha) |" >>"$summary"
  if [ "$found" -gt 0 ]; then
    echo "::warning title=VirusTotal::$name: $found of $engines engines detect it ($as)"
  fi
done
