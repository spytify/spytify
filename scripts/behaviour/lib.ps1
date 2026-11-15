# Behaviour gate (ci.yml): helpers shared by the Windows steps of
# the behaviour scan. Everything the steps keep goes under $Report, which
# the workflow uploads once the player's name is scrubbed out of it.
$ErrorActionPreference = 'Stop'
$script:Report = Join-Path $env:RUNNER_TEMP 'report'
foreach ($d in $Report, (Join-Path $Report 'runs'), (Join-Path $Report 'events'), (Join-Path $Report 'defender')) {
  New-Item -ItemType Directory -Force -Path $d | Out-Null
}
$script:Utf8 = New-Object System.Text.UTF8Encoding($false)

# The player's names, once windows-names.ps1 has read them. Until then no
# step prints what a program says: its output can carry the name.
$script:NamesFile = Join-Path $env:RUNNER_TEMP 'player-names.txt'
$script:Names = @()
# A file holding only "#none" means the release names no player: nothing to
# hide, and output may be printed.
$script:Loud = $false
if (Test-Path -LiteralPath $NamesFile) {
  $lines = @(Get-Content -LiteralPath $NamesFile | Where-Object { $_ })
  $script:Names = @($lines | Where-Object { -not $_.StartsWith('#') })
  $script:Loud = $lines.Count -gt 0
}

function Get-Stamp { [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ss.fffZ') }
function Write-Text([string]$Path, [string]$Text) { [System.IO.File]::WriteAllText($Path, $Text, $Utf8) }
function Add-Text([string]$Path, [string]$Text) { [System.IO.File]::AppendAllText($Path, $Text + "`n", $Utf8) }
function Add-Json([string]$Path, $Object) { Add-Text $Path ($Object | ConvertTo-Json -Compress -Depth 8) }

# A text with the player's names taken out, for the log.
function Protect([string]$Text) {
  foreach ($n in $Names) { $Text = [regex]::Replace($Text, [regex]::Escape($n), '[player]', 'IgnoreCase') }
  return $Text
}

function Mark([string]$Name, [string]$Detail = '') {
  Add-Json (Join-Path $Report 'marks.jsonl') ([ordered]@{ t = (Get-Stamp); mark = $Name; detail = $Detail })
  Write-Host "== $Name $(Protect $Detail)"
}

# One part of a step: what goes wrong inside is written down and the next
# part still runs. The block runs in a scope of its own, so what it leaves
# for later parts goes in $script: variables.
function Phase([string]$Name, [scriptblock]$Do) {
  Mark "phase: $Name"
  try { $null = & $Do } catch {
    Add-Json (Join-Path $Report 'phase-errors.jsonl') ([ordered]@{ t = (Get-Stamp); phase = $Name; error = $_.Exception.Message; at = $_.ScriptStackTrace })
    Write-Host "!! phase ${Name}: $(Protect $_.Exception.Message)"
  }
}

# Reads the names out of `spytify-cli info --json`, hides them in the job's
# log from here on, and keeps them for the scrub.
function Save-PlayerNames([string]$InfoJson) {
  try { $t = ($InfoJson | ConvertFrom-Json).target } catch { return $false }
  if (-not $t) { return $false }
  $found = New-Object System.Collections.Generic.List[string]
  foreach ($v in @($t.process, $t.mpris)) {
    foreach ($part in ([string]$v -split '[,;]')) {
      $part = $part.Trim()
      if ($part) { $found.Add($part); $found.Add(($part -replace '\.exe$', '')) }
    }
  }
  # A bundle id or a bus name is the name between words every app has.
  $common = 'com', 'org', 'net', 'app', 'client', 'desktop', 'player', 'music', 'www', 'mpris', 'mediaplayer2'
  foreach ($v in @($t.bundle_id, $t.mpris)) {
    foreach ($part in ([string]$v -split '[.,;]')) {
      $part = $part.Trim()
      if ($part -and $common -notcontains $part.ToLower()) { $found.Add($part) }
    }
  }
  $names = @($found | Where-Object { $_.Length -ge 4 } | Sort-Object -Unique | Sort-Object -Property Length -Descending)
  if ($names.Count -eq 0) { return $false }
  foreach ($n in $names) {
    $cap = $n.Substring(0, 1).ToUpper() + $n.Substring(1).ToLower()
    foreach ($v in @($n, $n.ToLower(), $n.ToUpper(), $cap) | Select-Object -Unique) { Write-Host "::add-mask::$v" }
  }
  [System.IO.File]::WriteAllLines($NamesFile, [string[]]$names, $Utf8)
  $script:Names = $names
  $script:Loud = $true
  return $true
}

# One argument as CreateProcess wants it on a command line.
function Format-Arg([string]$a) {
  if ($a -ne '' -and $a -notmatch '[\s"]') { return $a }
  $a = [regex]::Replace($a, '(\\*)"', '$1$1\"')
  $a = [regex]::Replace($a, '(\\+)$', '$1$1')
  return '"' + $a + '"'
}

function Stop-Tree([int]$Id) { cmd.exe /c "taskkill /PID $Id /T /F >nul 2>&1" }

# Stops the children of one program that carry another's name: the cmd.exe
# spytifyd starts to open the player waits here on a message box.
function Stop-Children([string]$Parent, [string]$Child) {
  $parents = @(Get-CimInstance Win32_Process -Filter "Name = '$Parent'" | ForEach-Object { $_.ProcessId })
  foreach ($c in @(Get-CimInstance Win32_Process -Filter "Name = '$Child'")) {
    if ($parents -contains $c.ParentProcessId) {
      Write-Host "   stopping ${Child}[$($c.ProcessId)], child of $Parent"
      Stop-Tree $c.ProcessId
    }
  }
}

# The runs are numbered across the steps of the job.
function Get-RunNo {
  $file = Join-Path $Report 'run-no.txt'
  $n = 0
  if (Test-Path -LiteralPath $file) { $n = [int](Get-Content -LiteralPath $file -Raw) }
  $n++
  Write-Text $file "$n"
  return $n
}

# Starts a program with CreateProcess, never through the shell: a shell
# start of a downloaded file can stop on a dialog nobody is there to click.
# Its output goes to runs\<n>-<name>.out and .err.
function Start-Tool {
  param(
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][string]$File,
    [string[]]$Arguments = @(),
    [hashtable]$Environment = @{},
    [string]$WorkingDirectory = $env:RUNNER_TEMP,
    [switch]$Console
  )
  $no = Get-RunNo
  $base = Join-Path $Report ('runs\{0:d3}-{1}' -f $no, $Name)
  $line = (@($Arguments) | ForEach-Object { Format-Arg $_ }) -join ' '
  $rec = [ordered]@{ n = $no; name = $Name; file = $File; args = $line; env = $Environment; started = (Get-Stamp) }
  $saved = @{}
  foreach ($k in $Environment.Keys) {
    $saved[$k] = [Environment]::GetEnvironmentVariable($k)
    [Environment]::SetEnvironmentVariable($k, [string]$Environment[$k])
  }
  $p = $null
  try {
    $sp = @{ FilePath = $File; PassThru = $true; RedirectStandardOutput = "$base.out"; RedirectStandardError = "$base.err"; WorkingDirectory = $WorkingDirectory }
    if ($line) { $sp.ArgumentList = $line }
    if ($Console) { $sp.NoNewWindow = $true }
    $p = Start-Process @sp
    # Reading the handle now keeps the exit code readable after the exit.
    $null = $p.Handle
    $rec.pid = $p.Id
  } catch {
    $rec.start_error = $_.Exception.Message
    Write-Host "!! $Name did not start: $(Protect $_.Exception.Message)"
  } finally {
    foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k]) }
  }
  return [pscustomobject]@{ Record = $rec; Process = $p; Out = "$base.out"; Err = "$base.err" }
}

# Waits for a program started by Start-Tool, stops it past the timeout and
# writes its record to runs.jsonl.
function Complete-Tool {
  param($Run, [int]$TimeoutSec = 60, [switch]$Quiet, [switch]$DiscardOutput, [switch]$StopNow)
  $rec = $Run.Record
  if ($Run.Process) {
    if ($StopNow) {
      if (-not $Run.Process.HasExited) { $rec.stopped = $true; Stop-Tree $Run.Process.Id }
      $null = $Run.Process.WaitForExit(10000)
    } elseif (-not $Run.Process.WaitForExit($TimeoutSec * 1000)) {
      $rec.timed_out = $true
      Stop-Tree $Run.Process.Id
      $null = $Run.Process.WaitForExit(10000)
    }
    $rec.ended = Get-Stamp
    try { $rec.exit = $Run.Process.ExitCode } catch { $rec.exit = $null }
  }
  foreach ($k in 'Out', 'Err') {
    $f = $Run.$k
    $size = 0
    if (Test-Path -LiteralPath $f) { $size = (Get-Item -LiteralPath $f).Length }
    $rec[$k.ToLower() + '_bytes'] = $size
    if ($DiscardOutput -and $k -eq 'Out' -and (Test-Path -LiteralPath $f)) { Remove-Item -LiteralPath $f -Force -ErrorAction SilentlyContinue }
  }
  Add-Json (Join-Path $Report 'runs.jsonl') $rec
  if (-not $Quiet) {
    $how = "exit $($rec.exit)"
    if ($rec.timed_out) { $how = "stopped after $TimeoutSec s" }
    if ($rec.stopped) { $how = 'stopped' }
    if ($rec.start_error) { $how = 'did not start' }
    Write-Host "-- $($rec.name) $(Protect $rec.args): $how, $($rec.out_bytes) bytes out, $($rec.err_bytes) bytes err"
    # What it said, only once the log hides the player's name.
    if ($Loud) {
      foreach ($f in $Run.Out, $Run.Err) {
        if (Test-Path -LiteralPath $f) {
          Get-Content -LiteralPath $f -TotalCount 40 -ErrorAction SilentlyContinue | ForEach-Object { Write-Host "   $(Protect $_)" }
        }
      }
    }
  }
  return $rec
}

function Invoke-Tool {
  param(
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][string]$File,
    [string[]]$Arguments = @(),
    [hashtable]$Environment = @{},
    [string]$WorkingDirectory = $env:RUNNER_TEMP,
    [int]$TimeoutSec = 60,
    [switch]$Console,
    [switch]$Quiet,
    [switch]$DiscardOutput
  )
  $run = Start-Tool -Name $Name -File $File -Arguments $Arguments -Environment $Environment -WorkingDirectory $WorkingDirectory -Console:$Console
  $null = Complete-Tool -Run $run -TimeoutSec $TimeoutSec -Quiet:$Quiet -DiscardOutput:$DiscardOutput
  return $run
}

# The processes worth a look at this moment, and the windows on screen (a
# dialog shows as a window of the process that owns it).
function Save-Processes([string]$Label) {
  $want = 'spytify|ffmpeg|powershell|cmd\.exe|rundll32|conhost|WerFault|smartscreen|MpCmdRun|consent|OpenWith|dllhost|explorer'
  $rows = @(Get-CimInstance Win32_Process | Where-Object { $_.Name -match $want } | ForEach-Object {
      [ordered]@{ pid = $_.ProcessId; ppid = $_.ParentProcessId; name = $_.Name; path = $_.ExecutablePath; cmd = $_.CommandLine }
    })
  $windows = @(Get-Process | Where-Object { $_.MainWindowTitle } | ForEach-Object {
      [ordered]@{ pid = $_.Id; name = $_.ProcessName; title = $_.MainWindowTitle }
    })
  Add-Json (Join-Path $Report 'snapshots.jsonl') ([ordered]@{ t = (Get-Stamp); label = $Label; processes = $rows; windows = $windows })
  $short = ($rows | Where-Object { $_.name -match 'spytify|ffmpeg|powershell|cmd\.exe|rundll32' } | ForEach-Object { "$($_.name)[$($_.pid)<$($_.ppid)]" }) -join ' '
  Write-Host "   processes at ${Label}: $short"
  # A message box about the player has its name in the title.
  if ($Loud) { foreach ($w in $windows) { Write-Host "   window: $($w.name)[$($w.pid)] '$(Protect $w.title)'" } }
}

# Every file of the extracted release: size, SHA-256, signature, and the
# mark Windows leaves on a download. A file that security software removed
# is the one missing from the second list.
function Get-AppFiles([string]$Dir) {
  $root = (Resolve-Path -LiteralPath $Dir).Path
  @(Get-ChildItem -LiteralPath $root -Recurse -File | ForEach-Object {
      $row = [ordered]@{ path = $_.FullName.Substring($root.Length + 1); bytes = $_.Length }
      try { $row.sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower() } catch { $row.sha256_error = $_.Exception.Message }
      if ($_.Extension -in '.exe', '.dll') {
        try {
          $sig = Get-AuthenticodeSignature -LiteralPath $_.FullName
          $row.signature = [string]$sig.Status
          if ($sig.SignerCertificate) { $row.signer = $sig.SignerCertificate.Subject }
        } catch { $row.signature = "error: $($_.Exception.Message)" }
        $v = $_.VersionInfo
        $row.version = [ordered]@{ product = $v.ProductName; file_version = $v.FileVersion; product_version = $v.ProductVersion; description = $v.FileDescription; company = $v.CompanyName; original = $v.OriginalFilename }
      }
      try { $row.zone = (Get-Content -LiteralPath $_.FullName -Stream Zone.Identifier -Raw -ErrorAction Stop) } catch { $row.zone = $null }
      $row
    })
}

# Defender's command-line tool: the newest platform copy when there is one.
function Get-MpCmdRun {
  $platform = 'C:\ProgramData\Microsoft\Windows Defender\Platform'
  if (Test-Path -LiteralPath $platform) {
    $newest = Get-ChildItem -LiteralPath $platform -Directory | Sort-Object Name -Descending | ForEach-Object { Join-Path $_.FullName 'MpCmdRun.exe' } | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    if ($newest) { return $newest }
  }
  return 'C:\Program Files\Windows Defender\MpCmdRun.exe'
}

# When the scan began (UTC, ISO), for the event log queries.
function Get-ScanStart {
  $file = Join-Path $Report 'scan-start.txt'
  if (Test-Path -LiteralPath $file) { return (Get-Content -LiteralPath $file -Raw).Trim() }
  return [DateTime]::UtcNow.AddHours(-2).ToString('yyyy-MM-ddTHH:mm:ss.fffZ')
}

# One Windows event log since $Since (UTC, ISO), as the XML wevtutil gives.
function Export-Log([string]$Name, [string]$Log, [string]$Since) {
  $out = Join-Path $Report "events\$Name.xml"
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = 'wevtutil.exe'
  $psi.Arguments = 'qe "{0}" "/q:*[System[TimeCreated[@SystemTime>=''{1}'']]]" /f:xml' -f $Log, $Since
  $psi.UseShellExecute = $false
  $psi.RedirectStandardOutput = $true
  $psi.CreateNoWindow = $true
  try {
    $p = [System.Diagnostics.Process]::Start($psi)
    $fs = [System.IO.File]::Create($out)
    try { $p.StandardOutput.BaseStream.CopyTo($fs) } finally { $fs.Close() }
    $p.WaitForExit()
    Write-Host ("   {0}: {1:n0} bytes (wevtutil exit {2})" -f $Log, (Get-Item -LiteralPath $out).Length, $p.ExitCode)
    if ($p.ExitCode -ne 0 -or (Get-Item -LiteralPath $out).Length -eq 0) { Remove-Item -LiteralPath $out -Force }
  } catch {
    Write-Host "   ${Log}: $($_.Exception.Message)"
  }
}
