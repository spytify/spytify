# Behaviour gate (ci.yml). Runs the Sigma rules Hayabusa ships over
# the event logs of the scan. The logs stay on the runner: only the
# timeline is kept.
. (Join-Path $PSScriptRoot 'lib.ps1')
$since = Get-ScanStart
$evtx = Join-Path $env:RUNNER_TEMP 'evtx'
New-Item -ItemType Directory -Force -Path $evtx | Out-Null
$logs = [ordered]@{
  'sysmon' = 'Microsoft-Windows-Sysmon/Operational'
  'defender' = 'Microsoft-Windows-Windows Defender/Operational'
  'security' = 'Security'
  'system' = 'System'
  'application' = 'Application'
  'powershell' = 'Microsoft-Windows-PowerShell/Operational'
  'windows-powershell' = 'Windows PowerShell'
  'codeintegrity' = 'Microsoft-Windows-CodeIntegrity/Operational'
  'firewall' = 'Microsoft-Windows-Windows Firewall With Advanced Security/Firewall'
}
foreach ($k in $logs.Keys) {
  wevtutil.exe epl "$($logs[$k])" (Join-Path $evtx "$k.evtx") "/q:*[System[TimeCreated[@SystemTime>='$since']]]" 2>$null
  Write-Host "   ${k}: wevtutil exit $LASTEXITCODE"
}

# Defender takes Hayabusa's rule files for threats; the logs are already
# exported, so the scan is no longer watching.
$dir = Join-Path $env:RUNNER_TEMP 'hayabusa'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
try { Add-MpPreference -ExclusionPath $dir } catch { Write-Host "   exclusion: $($_.Exception.Message)" }
$zip = Join-Path $dir 'hayabusa.zip'
curl.exe -sSfL --retry 3 -o $zip 'https://github.com/Yamato-Security/hayabusa/releases/download/v4.4.0/hayabusa-4.4.0-win-x64.zip'
if ($LASTEXITCODE -ne 0) { Mark 'hayabusa' "download failed (curl exit $LASTEXITCODE)"; exit 1 }
Expand-Archive -LiteralPath $zip -DestinationPath $dir -Force
$exe = Get-ChildItem -LiteralPath $dir -Recurse -Filter 'hayabusa*.exe' | Select-Object -First 1
if (-not $exe) { Mark 'hayabusa' 'no exe in the zip'; exit 1 }
$home2 = $exe.DirectoryName
$null = Invoke-Tool -Name 'hayabusa-help' -File $exe.FullName -Arguments @('help') -WorkingDirectory $home2 -Console -Quiet
if (-not (Test-Path -LiteralPath (Join-Path $home2 'rules'))) {
  $null = Invoke-Tool -Name 'hayabusa-update-rules' -File $exe.FullName -Arguments @('update-rules') -WorkingDirectory $home2 -TimeoutSec 300 -Console -Quiet
}
$r = Invoke-Tool -Name 'hayabusa' -File $exe.FullName -Arguments @('dfir-timeline', '-d', $evtx, '-t', 'jsonl', '-o', (Join-Path $Report 'hayabusa.jsonl'), '-w', '-q', '-K', '-C', '-U') -WorkingDirectory $home2 -TimeoutSec 600 -Console -Quiet
Mark 'hayabusa' "exit $($r.Record.exit), timeline $(if (Test-Path -LiteralPath (Join-Path $Report 'hayabusa.jsonl')) { (Get-Item -LiteralPath (Join-Path $Report 'hayabusa.jsonl')).Length } else { 0 }) bytes"
exit 0
