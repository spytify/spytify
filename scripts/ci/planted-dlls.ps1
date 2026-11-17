# CI only: checks that a program loads no DLL from its own folder, where
# one dropped beside it (in Downloads, say) would load in place of
# Windows' own (DLL search-order hijacking). It runs the program once and
# notes the System32 DLLs it loads, puts a copy of each beside it, runs it
# again and fails when a module comes from the program's folder.
#
#   planted-dlls.ps1 -Exe .\Spytify.exe -Window
#   planted-dlls.ps1 -Exe .\spytify-tap.exe -Arguments "--pid", 1234
param(
  [Parameter(Mandatory)] [string]$Exe,
  [string[]]$Arguments = @(),
  # Waits for the program's window; without it, the program runs 5 s.
  [switch]$Window,
  # Other processes the program starts, stopped with it.
  [string[]]$Also = @()
)
$ErrorActionPreference = "Stop"
$exe = (Resolve-Path $Exe).Path
$dir = Split-Path $exe
$name = [IO.Path]::GetFileNameWithoutExtension($exe)
$system32 = [Environment]::GetFolderPath("System")

# Runs the program and returns the paths of the modules it has loaded.
function Get-LoadedModules {
  $started = if ($Arguments.Count) {
    Start-Process $exe -ArgumentList $Arguments -PassThru -RedirectStandardOutput "$env:RUNNER_TEMP\$name.out" -RedirectStandardError "$env:RUNNER_TEMP\$name.err"
  } else {
    Start-Process $exe -PassThru -RedirectStandardOutput "$env:RUNNER_TEMP\$name.out" -RedirectStandardError "$env:RUNNER_TEMP\$name.err"
  }
  try {
    $p = $null
    if ($Window) {
      # The app may start a copy of itself, which opens the window.
      for ($i = 0; $i -lt 30 -and -not $p; $i++) {
        Start-Sleep -Seconds 1
        $p = Get-Process $name -ErrorAction SilentlyContinue | Where-Object MainWindowHandle -ne 0 | Select-Object -First 1
      }
      if (-not $p) { throw "no $name window within 30 s" }
    } else {
      Start-Sleep -Seconds 5
      $p = Get-Process -Id $started.Id -ErrorAction SilentlyContinue
      if (-not $p) { throw "$name exited within 5 s: $(Get-Content "$env:RUNNER_TEMP\$name.err" -Raw)" }
    }
    return @($p.Modules | ForEach-Object FileName)
  } finally {
    Get-Process (@($name) + $Also) -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
  }
}

$first = Get-LoadedModules
$fromSystem32 = $first | Where-Object { (Split-Path $_) -eq $system32 }
$planted = @()
try {
  foreach ($dll in $fromSystem32) {
    $copy = Join-Path $dir (Split-Path $dll -Leaf)
    if (-not (Test-Path $copy)) {
      Copy-Item $dll $copy
      $planted += $copy
    }
  }
  "$name loads $($fromSystem32.Count) DLLs from System32; a copy of each is beside it"
  $second = Get-LoadedModules
  $beside = $second | Where-Object { $_ -ne $exe -and (Split-Path $_) -eq $dir }
  if ($beside) {
    $beside | ForEach-Object { "  loaded from its folder: $_" }
    throw "$name loaded $(@($beside).Count) DLLs from its own folder instead of System32"
  }
  "$name loaded none of them: every DLL it loads comes from where Windows keeps it"
} finally {
  foreach ($copy in $planted) { Remove-Item $copy -Force -ErrorAction SilentlyContinue }
}
