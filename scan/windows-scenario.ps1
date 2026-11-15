# Throwaway (scan-behaviour branch). A first day with the downloaded app,
# as a user would have it, while Defender and Sysmon watch: first launch,
# the checks, a recording try, the tray, a recording of a stand-in player,
# the capture helper alone, and a recording of the fake player.
. (Join-Path $PSScriptRoot 'lib.ps1')
$scan = $PSScriptRoot
$repo = Split-Path $scan -Parent

$appFile = Join-Path $Report 'app-dir.txt'
if (-not (Test-Path -LiteralPath $appFile)) { Mark 'scenario' 'no app folder: the download or the extraction did not finish'; exit 0 }
$script:App = (Get-Content -LiteralPath $appFile -Raw).Trim()
$script:Cli = Join-Path $App 'spytify-cli.exe'
$script:Daemon = Join-Path $App 'spytifyd.exe'
$script:Tap = Join-Path $App 'spytify-tap.exe'
$script:Ui = Join-Path $App 'Spytify.exe'
$script:Dsn = @{ SPYTIFY_SENTRY_DSN = 'http://scan@127.0.0.1:19090/1' }

# Not "Cli": PowerShell has an alias cli (Clear-Item), and aliases win.
function Run-Cli([string]$Label, [string[]]$Arguments, [int]$TimeoutSec = 30, [switch]$Quiet) {
  return Invoke-Tool -Name "cli-$Label" -File $Cli -Arguments $Arguments -WorkingDirectory $App -TimeoutSec $TimeoutSec -Console -Quiet:$Quiet
}

function Get-AppWindow { Get-Process -Name Spytify -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1 }

function Wait-AppWindow([int]$Seconds) {
  for ($i = 0; $i -lt $Seconds; $i++) {
    $w = Get-AppWindow
    if ($w) { return $w }
    Start-Sleep -Seconds 1
  }
  return $null
}

# Only the programs of the release: never the runner's own.
function Stop-App {
  Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($App, [StringComparison]::OrdinalIgnoreCase) } | ForEach-Object { Stop-Tree $_.ProcessId }
  Start-Sleep -Seconds 2
}

function Wait-Recorder([int]$Tries) {
  for ($i = 0; $i -lt $Tries; $i++) {
    $r = Run-Cli 'info-json' @('info', '--json') -Quiet
    if ($r.Record.exit -eq 0) { return $true }
    Start-Sleep -Seconds 2
  }
  return $false
}

# The scan's stand-in player: a looping tone played by Windows PowerShell.
function Start-Tone {
  $ready = Join-Path $env:RUNNER_TEMP 'tone.wav.playing'
  Remove-Item -LiteralPath $ready -Force -ErrorAction SilentlyContinue
  $t = Start-Tool -Name 'tone' -File 'powershell.exe' -Arguments @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', (Join-Path $repo 'scripts\ci\tone.ps1'))
  for ($i = 0; $i -lt 60; $i++) {
    if (Test-Path -LiteralPath $ready) { Mark 'tone' "playing, pid $($t.Record.pid)"; return $t }
    if ($t.Process -and $t.Process.HasExited) { break }
    Start-Sleep -Seconds 1
  }
  Mark 'tone' 'no sound'
  $null = Complete-Tool -Run $t -StopNow
  return $null
}

# A CLI command that may make the recorder open the player: cmd.exe then
# waits on a message box ("Windows cannot find ..."), which is looked at and
# closed.
function Cli-Opening([string]$Label, [string[]]$Arguments, [int]$TimeoutSec) {
  $run = Start-Tool -Name "cli-$Label" -File $Cli -Arguments $Arguments -WorkingDirectory $App -Console
  Start-Sleep -Seconds 8
  Save-Processes "$Label after 8 s"
  Stop-Children 'spytifyd.exe' 'cmd.exe'
  $null = Complete-Tool -Run $run -TimeoutSec $TimeoutSec
}

$sink = Start-Tool -Name 'sink' -File 'python' -Arguments @((Join-Path $scan 'sentry-sink.py'), '19090', (Join-Path $Report 'sentry-sink.jsonl'))
Save-Processes 'start'

Phase 'first launch' {
  $script:UiRun = Start-Tool -Name 'ui' -File $Ui -Environment $Dsn -WorkingDirectory $App
  $w = Wait-AppWindow 120
  if (-not $w) { Mark 'window' 'none after 120 s'; Save-Processes 'no window'; return }
  Mark 'window' "pid $($w.Id)"
  try {
    $gl = ($w.Modules | Where-Object ModuleName -eq 'opengl32.dll').FileName
    Mark 'opengl32.dll' $gl
  } catch { Mark 'opengl32.dll' "not readable: $($_.Exception.Message)" }
  Save-Processes 'window up'
  Start-Sleep -Seconds 30
  Save-Processes 'after 30 s idle'

  if (-not (Wait-Recorder 20)) {
    Mark 'recorder' 'the window did not start it within 40 s; starting it by hand'
    $script:DaemonRun = Start-Tool -Name 'spytifyd' -File $Daemon -WorkingDirectory $App
    $null = Wait-Recorder 15
  }
  $null = Run-Cli 'info' @('info')
  $null = Run-Cli 'status' @('status')
  $null = Run-Cli 'get' @('get')
  Cli-Opening 'launch' @('launch') 15
  Cli-Opening 'start' @('start') 60
  $null = Run-Cli 'status' @('status')
  $null = Run-Cli 'stop' @('stop')

  # Closing the window with "minimize to tray" on: the tray notice.
  $null = Run-Cli 'set-tray' @('set', 'app.minimize_to_tray=true')
  Start-Sleep -Seconds 3
  $w = Get-AppWindow
  if ($w) { $null = $w.CloseMainWindow() }
  for ($i = 0; $i -lt 6; $i++) { Start-Sleep -Seconds 2; Save-Processes "after close +$(2 * ($i + 1)) s" }
  Mark 'tray' "Spytify.exe still running: $(@(Get-Process -Name Spytify -ErrorAction SilentlyContinue).Count)"

  $null = Run-Cli 'shutdown' @('shutdown')
  Start-Sleep -Seconds 3
  Stop-App
  $null = Complete-Tool -Run $script:UiRun -StopNow
  if ($script:DaemonRun) { $null = Complete-Tool -Run $script:DaemonRun -StopNow }
}

Phase 'recording a stand-in player' {
  $env2 = $Dsn.Clone()
  $env2.SPYTIFY_TARGET_PROCESS = 'powershell'
  $script:UiRun = Start-Tool -Name 'ui-standin' -File $Ui -Environment $env2 -WorkingDirectory $App
  $w = Wait-AppWindow 120
  if (-not $w) { Mark 'window' 'none after 120 s'; return }
  if (-not (Wait-Recorder 20)) { Mark 'recorder' 'not answering'; return }
  $w = Get-AppWindow
  if ($w) { $null = $w.CloseMainWindow() }
  Start-Sleep -Seconds 3
  $script:Tone = Start-Tone
  if (-not $script:Tone) { return }
  $run = Start-Tool -Name 'cli-start-standin' -File $Cli -Arguments @('start') -WorkingDirectory $App -Console
  for ($i = 0; $i -lt 5; $i++) { Start-Sleep -Seconds 5; Save-Processes "recording +$(5 * ($i + 1)) s" }
  Stop-Children 'spytifyd.exe' 'cmd.exe'
  $null = Complete-Tool -Run $run -TimeoutSec 30
  $null = Run-Cli 'status' @('status')
  $null = Run-Cli 'info' @('info')
  # Stopped from outside the window while it is hidden: "Recording stopped".
  $null = Run-Cli 'stop' @('stop')
  Start-Sleep -Seconds 12
  Save-Processes 'after the stop'
  $null = Run-Cli 'shutdown' @('shutdown')
  Start-Sleep -Seconds 3
  Stop-App
  $null = Complete-Tool -Run $script:UiRun -StopNow
}

Phase 'the capture helper alone' {
  if (-not $script:Tone -or $script:Tone.Process.HasExited) { $script:Tone = Start-Tone }
  if (-not $script:Tone) { return }
  $tonePid = "$($script:Tone.Record.pid)"
  $t = Start-Tool -Name 'tap' -File $Tap -Arguments @('--pid', $tonePid) -WorkingDirectory $App -Console
  Start-Sleep -Seconds 6
  $null = Complete-Tool -Run $t -StopNow -DiscardOutput
  $null = Invoke-Tool -Name 'tap-get-volume' -File $Tap -Arguments @('--pid', $tonePid, '--get-volume') -WorkingDirectory $App -Console -TimeoutSec 15
  $null = Invoke-Tool -Name 'tap-set-volume' -File $Tap -Arguments @('--pid', $tonePid, '--set-volume', '0.25') -WorkingDirectory $App -Console -TimeoutSec 15
  $null = Invoke-Tool -Name 'tap-get-volume' -File $Tap -Arguments @('--pid', $tonePid, '--get-volume') -WorkingDirectory $App -Console -TimeoutSec 15
  $null = Invoke-Tool -Name 'tap-set-volume' -File $Tap -Arguments @('--pid', $tonePid, '--set-volume', '1') -WorkingDirectory $App -Console -TimeoutSec 15
}
if ($script:Tone) { $null = Complete-Tool -Run $script:Tone -StopNow }

Phase 'recording the fake player' {
  $timeline = Join-Path $repo 'testdata\timelines\basic.json'
  $base = @('--once', '--fake', $timeline, '--fake-realtime', '--idle-exit', '10s')
  $null = Invoke-Tool -Name 'spytifyd-fake' -File $Daemon -Arguments ($base + @('--events', (Join-Path $Report 'fake-events.jsonl'))) -WorkingDirectory $App -TimeoutSec 120 -Console
  # Into Music, a folder Controlled Folder Access guards.
  $music = Join-Path $env:USERPROFILE 'Music\SpytifyScan'
  $null = Invoke-Tool -Name 'spytifyd-fake-music' -File $Daemon -Arguments ($base + @('--events', (Join-Path $Report 'fake-events-music.jsonl'), '--set', "output.dir=$music")) -WorkingDirectory $App -TimeoutSec 120 -Console
  foreach ($d in (Join-Path $env:USERPROFILE 'Spytify'), $music) {
    if (Test-Path -LiteralPath $d) {
      $files = @(Get-ChildItem -LiteralPath $d -Recurse -Force -File | ForEach-Object { [ordered]@{ path = $_.FullName.Substring($d.Length + 1); bytes = $_.Length } })
      Mark 'output' "$d holds $($files.Count) file(s)"
      Add-Json (Join-Path $Report 'output-files.jsonl') ([ordered]@{ dir = $d; files = $files })
    } else { Mark 'output' "$d does not exist" }
  }
}

Phase 'small commands' {
  $null = Invoke-Tool -Name 'spytifyd-version' -File $Daemon -Arguments @('--version') -WorkingDirectory $App -Console
  $null = Run-Cli 'help' @('--help')
  $null = Run-Cli 'service-status' @('service', 'status')
}

Save-Processes 'end'
Stop-App
$null = Complete-Tool -Run $sink -StopNow -Quiet
exit 0
