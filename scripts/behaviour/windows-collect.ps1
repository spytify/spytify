# Behaviour gate (ci.yml). What the system saw: a Defender scan of
# the app folder, its detections, the event logs, and what the app left.
. (Join-Path $PSScriptRoot 'lib.ps1')
$since = Get-ScanStart
$appFile = Join-Path $Report 'app-dir.txt'
$app = $null
if (Test-Path -LiteralPath $appFile) { $app = (Get-Content -LiteralPath $appFile -Raw).Trim() }

Phase 'defender scan' {
  if ($app -and (Test-Path -LiteralPath $app)) {
    $null = Invoke-Tool -Name 'defender-scan' -File (Get-MpCmdRun) -Arguments @('-Scan', '-ScanType', '3', '-File', $app, '-DisableRemediation') -TimeoutSec 600 -Console
  }
  try { Get-MpThreatDetection | Select-Object * -ExcludeProperty Cim* | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Report 'defender\detections.json') -Encoding utf8 } catch { }
  try { Get-MpThreat | Select-Object * -ExcludeProperty Cim* | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Report 'defender\threats.json') -Encoding utf8 } catch { }
  try { Get-MpComputerStatus | Select-Object * -ExcludeProperty Cim* | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $Report 'defender\status-end.json') -Encoding utf8 } catch { }
  try { Get-MpPreference | Select-Object * -ExcludeProperty Cim* | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $Report 'defender\preference-end.json') -Encoding utf8 } catch { }
}

Phase 'event logs' {
  $logs = [ordered]@{
    'sysmon' = 'Microsoft-Windows-Sysmon/Operational'
    'defender' = 'Microsoft-Windows-Windows Defender/Operational'
    'codeintegrity' = 'Microsoft-Windows-CodeIntegrity/Operational'
    'application' = 'Application'
    'system' = 'System'
    'powershell' = 'Microsoft-Windows-PowerShell/Operational'
    'windows-powershell' = 'Windows PowerShell'
    'firewall' = 'Microsoft-Windows-Windows Firewall With Advanced Security/Firewall'
    'smartscreen' = 'Microsoft-Windows-SmartScreen/Debug'
    'applocker-exe' = 'Microsoft-Windows-AppLocker/EXE and DLL'
    'applocker-script' = 'Microsoft-Windows-AppLocker/MSI and Script'
    'mitigations' = 'Microsoft-Windows-Security-Mitigations/KernelMode'
  }
  foreach ($k in $logs.Keys) { Export-Log $k $logs[$k] $since }
}

Phase 'what the app left' {
  $state = Join-Path $Report 'state'
  New-Item -ItemType Directory -Force -Path $state | Out-Null
  $start = [DateTime]::Parse($since).ToUniversalTime()
  $places = [ordered]@{
    localappdata = Join-Path $env:LOCALAPPDATA 'Spytify'
    appdata = Join-Path $env:APPDATA 'spytify'
    fyne = Join-Path $env:APPDATA 'fyne'
    music = Join-Path $env:USERPROFILE 'Music'
    home_spytify = Join-Path $env:USERPROFILE 'Spytify'
  }
  $listing = [ordered]@{}
  foreach ($k in $places.Keys) {
    $p = $places[$k]
    if (-not (Test-Path -LiteralPath $p)) { $listing[$k] = $null; continue }
    $listing[$k] = @(Get-ChildItem -LiteralPath $p -Recurse -Force -File -ErrorAction SilentlyContinue | ForEach-Object {
        $rel = $_.FullName.Substring($p.Length + 1)
        if ($k -ne 'music' -and $k -ne 'home_spytify' -and $_.Length -lt 2MB -and $_.Extension -in '.toml', '.json', '.log', '.txt', '.env', '.jsonl', '') {
          $dest = Join-Path $state ("$k-" + ($rel -replace '[\\/:]', '_'))
          Copy-Item -LiteralPath $_.FullName -Destination $dest -Force -ErrorAction SilentlyContinue
        }
        [ordered]@{ path = $rel; bytes = $_.Length; written = $_.LastWriteTimeUtc.ToString('o'); attributes = [string]$_.Attributes }
      })
  }
  $listing.temp = @(Get-ChildItem -LiteralPath $env:TEMP -Force -ErrorAction SilentlyContinue | Where-Object { $_.LastWriteTimeUtc -ge $start } | ForEach-Object { [ordered]@{ name = $_.Name; dir = $_.PSIsContainer; bytes = $(if ($_.PSIsContainer) { 0 } else { $_.Length }) } })
  $listing | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Report 'left-behind.json') -Encoding utf8

  $persist = [ordered]@{}
  foreach ($k in 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run', 'HKCU:\Software\Microsoft\Windows\CurrentVersion\RunOnce', 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Run') {
    try { $persist[$k] = (Get-ItemProperty -LiteralPath $k -ErrorAction Stop | Out-String) -split "`n" | Where-Object { $_ -match 'spytify' } } catch { $persist[$k] = $null }
  }
  $persist.startup = @(Get-ChildItem -LiteralPath ([Environment]::GetFolderPath('Startup')) -ErrorAction SilentlyContinue | ForEach-Object Name)
  $persist.tasks = @(Get-ScheduledTask -ErrorAction SilentlyContinue | Where-Object { ($_.TaskName + ($_.Actions | Out-String)) -match 'spytify' } | ForEach-Object TaskName)
  $persist.services = @(Get-CimInstance Win32_Service | Where-Object { $_.PathName -match 'spytify' } | ForEach-Object Name)
  $persist.firewall = @(Get-NetFirewallApplicationFilter -ErrorAction SilentlyContinue | Where-Object { $_.Program -match 'spytify' } | ForEach-Object Program)
  $persist | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $Report 'persistence.json') -Encoding utf8

  if ($app -and (Test-Path -LiteralPath $app)) {
    Get-AppFiles $app | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Report 'files-after.json') -Encoding utf8
  } else { Mark 'app folder' 'gone at the end' }
}
exit 0
