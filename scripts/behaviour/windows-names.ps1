# Behaviour gate (ci.yml). Reads the player's names the build carries,
# before anything else runs, so the report can hide them. Uses a copy of
# the build that is deleted afterwards.
. (Join-Path $PSScriptRoot 'lib.ps1')

# The build under test, as windows-build uploaded it.
$prep = Join-Path $env:RUNNER_TEMP 'prep'
New-Item -ItemType Directory -Force -Path $prep | Out-Null
$dir = Join-Path $prep 'build'
Copy-Item -LiteralPath $env:SPYTIFY_BUILD -Destination $dir -Recurse -Force

$env:SPYTIFY_SOCKET = Join-Path $prep 's.sock'
$env:SPYTIFY_CONFIG = Join-Path $prep 'config.toml'
$d = Start-Process -FilePath (Join-Path $dir 'spytifyd.exe') -WorkingDirectory $prep -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $prep 'd.out') -RedirectStandardError (Join-Path $prep 'd.err')
$null = $d.Handle

$json = Join-Path $prep 'info.json'
$got = $false
$codes = @()
for ($i = 0; $i -lt 30 -and -not $got; $i++) {
  Start-Sleep -Seconds 1
  $c = Start-Process -FilePath (Join-Path $dir 'spytify-cli.exe') -ArgumentList 'info --json' -WorkingDirectory $prep -PassThru -NoNewWindow `
    -RedirectStandardOutput $json -RedirectStandardError (Join-Path $prep 'c.err')
  $null = $c.Handle
  $null = $c.WaitForExit(15000)
  $codes += $c.ExitCode
  if ($c.ExitCode -eq 0 -and (Get-Item -LiteralPath $json).Length -gt 0) { $got = $true }
}

$ok = $false
if ($got) {
  $text = Get-Content -LiteralPath $json -Raw
  if (Save-PlayerNames $text) {
    $ok = $true
    Write-Host "$($Names.Count) player name(s) kept for the scrub"
  } else {
    # Parsed with a target that names nothing: nothing to hide.
    try { $t = ($text | ConvertFrom-Json).target } catch { $t = $null }
    if ($t) {
      Write-Text $NamesFile "#none`n"
      $ok = $true
      Write-Host 'the release names no player'
    }
  }
}

$s = Start-Process -FilePath (Join-Path $dir 'spytify-cli.exe') -ArgumentList 'shutdown' -WorkingDirectory $prep -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $prep 's.out') -RedirectStandardError (Join-Path $prep 's.err')
$null = $s.WaitForExit(15000)
$null = $d.WaitForExit(10000)
Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($prep) } | ForEach-Object { Stop-Tree $_.ProcessId }
Start-Sleep -Seconds 2

if (-not $ok) {
  $sizes = foreach ($f in 'd.out', 'd.err', 'info.json', 'c.err') {
    $p = Join-Path $prep $f
    if (Test-Path -LiteralPath $p) { "${f}=$((Get-Item -LiteralPath $p).Length)" } else { "${f}=none" }
  }
  Write-Host "could not read the player's names; cli exits: $($codes -join ','); bytes: $($sizes -join ' ')"
  exit 1
}

Remove-Item -LiteralPath $prep -Recurse -Force -ErrorAction SilentlyContinue
foreach ($p in (Join-Path $env:LOCALAPPDATA 'Spytify'), (Join-Path $env:APPDATA 'spytify')) {
  if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Recurse -Force -ErrorAction SilentlyContinue }
}
exit 0
