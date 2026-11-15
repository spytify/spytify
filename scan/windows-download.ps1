# Throwaway (scan-behaviour branch). Downloads the release into Downloads
# and extracts it there, with the mark a browser and Explorer leave, so
# Defender treats it as a file from the Internet.
. (Join-Path $PSScriptRoot 'lib.ps1')

$release = $env:RELEASE
$name = "spytify-${release}-windows-amd64"
$url = "https://github.com/spytify/spytify/releases/download/${release}/${name}.zip"
$downloads = Join-Path $env:USERPROFILE 'Downloads'
New-Item -ItemType Directory -Force -Path $downloads | Out-Null
$zip = Join-Path $downloads "${name}.zip"
$sums = Join-Path $env:RUNNER_TEMP 'SHA256SUMS'
$rec = [ordered]@{ url = $url; zip = $zip }

curl.exe -sSfL --retry 3 -o $sums "https://github.com/spytify/spytify/releases/download/${release}/SHA256SUMS"
$rec.sums_curl = $LASTEXITCODE
curl.exe -sSfL --retry 3 -o $zip $url
$rec.zip_curl = $LASTEXITCODE
Start-Sleep -Seconds 5
$rec.zip_exists = Test-Path -LiteralPath $zip
if ($rec.zip_exists) {
  try { $rec.sha256 = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLower() } catch { $rec.sha256_error = $_.Exception.Message }
  if (Test-Path -LiteralPath $sums) {
    $line = Get-Content -LiteralPath $sums | Where-Object { $_ -match [regex]::Escape("${name}.zip") } | Select-Object -First 1
    if ($line) { $rec.expected = ($line -split '\s+')[0].ToLower(); $rec.match = $rec.expected -eq $rec.sha256 }
  }
  try {
    Set-Content -LiteralPath $zip -Stream Zone.Identifier -Value "[ZoneTransfer]`r`nZoneId=3`r`nReferrerUrl=https://github.com/spytify/spytify/releases/tag/${release}`r`nHostUrl=${url}`r`n"
  } catch { $rec.zone_error = $_.Exception.Message }
  try {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $z = [System.IO.Compression.ZipFile]::OpenRead($zip)
    try { $entries = @($z.Entries | ForEach-Object { [ordered]@{ name = $_.FullName; bytes = $_.Length } }) } finally { $z.Dispose() }
    $entries | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $Report 'zip-entries.json') -Encoding utf8
  } catch { $rec.list_error = $_.Exception.Message }
  try { Expand-Archive -LiteralPath $zip -DestinationPath $downloads -Force } catch { $rec.expand_error = $_.Exception.Message }
}
Start-Sleep -Seconds 20

$app = Join-Path $downloads $name
$rec.app_exists = Test-Path -LiteralPath $app
$rec.zip_still_exists = Test-Path -LiteralPath $zip
if ($rec.app_exists) {
  # Explorer marks each extracted file with the zip it came from.
  $marked = 0
  foreach ($f in Get-ChildItem -LiteralPath $app -Recurse -File) {
    try { Set-Content -LiteralPath $f.FullName -Stream Zone.Identifier -Value "[ZoneTransfer]`r`nZoneId=3`r`nReferrerUrl=${zip}`r`n"; $marked++ } catch { }
  }
  $rec.marked = $marked
  Write-Text (Join-Path $Report 'app-dir.txt') $app
  Get-AppFiles $app | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Report 'files-before.json') -Encoding utf8
}
$rec | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $Report 'download.json') -Encoding utf8
Mark 'download' ("zip curl $($rec.zip_curl), hash match $($rec.match), app folder $($rec.app_exists), marked $($rec.marked), expand error: $($rec.expand_error)")
exit 0
