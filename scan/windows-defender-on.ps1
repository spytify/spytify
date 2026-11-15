# Throwaway (scan-behaviour branch). The runner image switches Defender off;
# this puts back what a home PC has (cloud protection, behaviour monitoring,
# scripts, downloads), plus the enterprise rules in audit mode, and proves
# with two harmless test samples that it is really on.
param([string]$CloudBlockLevel = 'Default')
. (Join-Path $PSScriptRoot 'lib.ps1')
Write-Text (Join-Path $Report 'scan-start.txt') (Get-Stamp)

function Save-Defender([string]$Label) {
  try { Get-MpComputerStatus | Select-Object * -ExcludeProperty Cim* | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $Report "defender\status-$Label.json") -Encoding utf8 } catch { Write-Host "   status ${Label}: $($_.Exception.Message)" }
  try { Get-MpPreference | Select-Object * -ExcludeProperty Cim* | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $Report "defender\preference-$Label.json") -Encoding utf8 } catch { Write-Host "   preference ${Label}: $($_.Exception.Message)" }
}

function Change([string]$Title, [scriptblock]$Action) {
  $row = [ordered]@{ t = (Get-Stamp); change = $Title; ok = $true; error = $null }
  try { $null = & $Action } catch { $row.ok = $false; $row.error = $_.Exception.Message }
  Add-Json (Join-Path $Report 'defender\settings.jsonl') $row
  if ($row.ok) { Write-Host "   ${Title}: ok" } else { Write-Host "   ${Title}: FAILED: $($row.error)" }
}

Save-Defender 'before'

$pref = Get-MpPreference
foreach ($kind in 'ExclusionPath', 'ExclusionProcess', 'ExclusionExtension') {
  foreach ($v in @($pref.$kind)) {
    if ($v) { Change "remove $kind $v" { $p = @{ $kind = $v }; Remove-MpPreference @p } }
  }
}

$settings = [ordered]@{
  DisableRealtimeMonitoring = $false
  DisableBehaviorMonitoring = $false
  DisableBlockAtFirstSeen = $false
  DisableIOAVProtection = $false
  DisableScriptScanning = $false
  DisableArchiveScanning = $false
  DisableIntrusionPreventionSystem = $false
  DisablePrivacyMode = $false
  DisableScanningNetworkFiles = $false
  SignatureDisableUpdateOnStartupWithoutEngine = $false
  ScanAvgCPULoadFactor = 50
  MAPSReporting = 'Advanced'
  SubmitSamplesConsent = 'SendSafeSamples'
  PUAProtection = 'Enabled'
  CloudBlockLevel = $CloudBlockLevel
  CloudExtendedTimeout = 50
  EnableControlledFolderAccess = 'AuditMode'
  EnableNetworkProtection = 'AuditMode'
}
foreach ($k in $settings.Keys) {
  $v = $settings[$k]
  Change "$k = $v" { $p = @{ $k = $v }; Set-MpPreference @p }
}

$asr = @(
  '56a863a9-875e-4185-98a7-b882c64b5ce5', '7674ba52-37eb-4a4f-a9a1-f0f9a1619a2c', 'd4f940ab-401b-4efc-aadc-ad5f3c50688a',
  '9e6c4e1f-7d60-472f-ba1a-a39ef669e4b2', 'be9ba2d9-53ea-4cdc-84e5-9b1eeee46550', '01443614-cd74-433a-b99e-2ecdc07bfc25',
  '5beb7efe-fd9a-4556-801d-275e5ffc04cc', 'd3e037e1-3eb8-44c8-a917-57927947596d', '3b576869-a4ec-4529-8536-b80a7769e899',
  '75668c1f-73b5-4cf0-bb93-3ecf5cb7cc84', '26190899-1602-49e8-8b27-eb1d0a1ce869', 'e6db77e5-3df2-4cf1-b95a-636979351e5b',
  'd1e49aac-8f56-4280-b9ba-993a6d77406c', '33ddedf1-c6e0-47cb-833e-de6133960387', 'b2b3f03d-6a65-4f7b-a9c7-1c7ef74a9ba4',
  'c0033c00-d16d-4114-a5a0-dc9b3a7d2ceb', 'a8f5898e-1dc8-49a9-9878-85004b8a61e6', '92e97fa1-2edf-4476-bdd6-9dd0b4dddc7b',
  'c1db55ab-c21a-4637-bb3f-a12568109d35'
)
foreach ($id in $asr) {
  Change "ASR $id audit" { Add-MpPreference -AttackSurfaceReductionRules_Ids $id -AttackSurfaceReductionRules_Actions AuditMode }
}

$atp = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows Advanced Threat Protection'
if (Test-Path -LiteralPath $atp) {
  Change 'ForceDefenderPassiveMode = 0' { Set-ItemProperty -LiteralPath $atp -Name ForceDefenderPassiveMode -Value 0 -Type DWord }
}
Change 'script block logging' {
  $k = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\PowerShell\ScriptBlockLogging'
  New-Item -Path $k -Force | Out-Null
  Set-ItemProperty -LiteralPath $k -Name EnableScriptBlockLogging -Value 1 -Type DWord
}

foreach ($k in 'HKLM\SOFTWARE\Policies\Microsoft\Windows Defender', 'HKLM\SOFTWARE\Policies\Microsoft\Windows Advanced Threat Protection') {
  $text = (cmd.exe /c "reg query `"$k`" /s 2>&1") -join "`n"
  Add-Text (Join-Path $Report 'defender\policies.txt') "== $k`n$text`n"
}
try { Get-Service WinDefend | Select-Object Name, Status, StartType | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $Report 'defender\service.json') -Encoding utf8 } catch { }

for ($i = 0; $i -lt 60; $i++) {
  try { if ((Get-MpComputerStatus).RealTimeProtectionEnabled) { break } } catch { }
  Start-Sleep -Seconds 1
}

$mp = Get-MpCmdRun
$null = Invoke-Tool -Name 'defender-update' -File $mp -Arguments @('-SignatureUpdate') -TimeoutSec 300 -Console
$null = Invoke-Tool -Name 'defender-maps' -File $mp -Arguments @('-ValidateMapsConnection') -TimeoutSec 120 -Console

# Positive control 1: the antivirus test file, put together here so that no
# script holds it whole.
$probe = Join-Path $env:USERPROFILE 'Downloads\scan-probe.com'
New-Item -ItemType Directory -Force -Path (Split-Path $probe) | Out-Null
$eicar = 'X5O!P%@AP[4\PZX54(P^)7CC)7}$' + 'EICAR-STANDARD-ANTIVIRUS-TEST-FILE' + '!$H+H*'
$verdict = 'untouched within 30 s'
try {
  [System.IO.File]::WriteAllText($probe, $eicar, [System.Text.Encoding]::ASCII)
  for ($i = 1; $i -le 30; $i++) {
    Start-Sleep -Seconds 1
    if (-not (Test-Path -LiteralPath $probe)) { $verdict = "removed after $i s"; break }
    try { $null = [System.IO.File]::ReadAllBytes($probe) } catch { $verdict = "read refused after $i s"; break }
  }
} catch { $verdict = "write refused: $($_.Exception.Message)" }
Remove-Item -LiteralPath $probe -Force -ErrorAction SilentlyContinue

# Positive control 2: the script scanning test string in Windows PowerShell.
$amsi = Invoke-Tool -Name 'amsi-probe' -File 'powershell.exe' -Arguments @('-NoProfile', '-NonInteractive', '-Command', "Invoke-Expression ('AMSI Test Sample: ' + '7e72c3ce-861b-4339-8740-0ac1484c1386')") -TimeoutSec 60 -Quiet
# Its output echoes the test string, so Defender may refuse to let it be
# read: that counts as blocked too. The file is not kept either way.
$amsiBlocked = $false
foreach ($f in $amsi.Err, $amsi.Out) {
  try {
    if ((Test-Path -LiteralPath $f) -and ((Get-Content -LiteralPath $f -Raw -ErrorAction Stop) -match 'malicious content')) { $amsiBlocked = $true }
  } catch { $amsiBlocked = $true }
  Remove-Item -LiteralPath $f -Force -ErrorAction SilentlyContinue
}
Add-Json (Join-Path $Report 'defender\positive-control.json') ([ordered]@{ t = (Get-Stamp); eicar = $verdict; amsi_blocked = $amsiBlocked; cloud_block_level = $CloudBlockLevel })
Write-Host "== positive controls: test file $verdict; script scanning blocked: $amsiBlocked"

Save-Defender 'after-on'
try {
  $s = Get-MpComputerStatus
  foreach ($k in 'AMRunningMode', 'AMServiceEnabled', 'AntivirusEnabled', 'RealTimeProtectionEnabled', 'BehaviorMonitorEnabled', 'IoavProtectionEnabled', 'OnAccessProtectionEnabled', 'IsTamperProtected', 'AntivirusSignatureVersion', 'AMEngineVersion', 'AMProductVersion') {
    Write-Host "   ${k}: $($s.$k)"
  }
} catch { Write-Host "   status: $($_.Exception.Message)" }
exit 0
