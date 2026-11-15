# Behaviour gate (ci.yml). Puts the build where a user's download lands,
# with the mark Edge and Explorer leave on a file from the Internet, so
# Defender treats it as one.
. (Join-Path $PSScriptRoot 'lib.ps1')

$app = Join-Path $env:USERPROFILE 'Downloads\spytify-ci'
New-Item -ItemType Directory -Force -Path (Split-Path $app) | Out-Null
Copy-Item -LiteralPath $env:SPYTIFY_BUILD -Destination $app -Recurse -Force
$marked = 0
foreach ($f in Get-ChildItem -LiteralPath $app -Recurse -File) {
  try {
    Set-Content -LiteralPath $f.FullName -Stream Zone.Identifier -Value "[ZoneTransfer]`r`nZoneId=3`r`nReferrerUrl=https://github.com/spytify/spytify/releases`r`n"
    $marked++
  } catch { }
}
Start-Sleep -Seconds 10
Write-Text (Join-Path $Report 'app-dir.txt') $app
Get-AppFiles $app | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Report 'files-before.json') -Encoding utf8
Mark 'placed' "$marked files in $app, marked as downloaded"
exit 0
