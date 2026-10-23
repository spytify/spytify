# CI only: gives a Windows runner, which has no sound card, an audio
# output by installing the Scream virtual audio driver, and starts the
# Windows Audio service.
$ErrorActionPreference = "Stop"
Set-Service -Name Audiosrv -StartupType Automatic
Start-Service -Name Audiosrv
$zip = Join-Path $env:RUNNER_TEMP "Scream3.6.zip"
$dir = Join-Path $env:RUNNER_TEMP "Scream"
Invoke-WebRequest https://github.com/duncanthrax/scream/releases/download/3.6/Scream3.6.zip -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $dir -Force
$sys = Get-ChildItem -Path $dir -Recurse -Filter Scream.sys | Where-Object { $_.FullName -match "x64" } | Select-Object -First 1
if (-not $sys) { $sys = Get-ChildItem -Path $dir -Recurse -Filter Scream.sys | Select-Object -First 1 }
$inf = Join-Path $sys.DirectoryName "Scream.inf"
$devcon = Get-ChildItem -Path $dir -Recurse -Filter "devcon*.exe" | Where-Object { $_.Name -match "x64" -or $_.Name -eq "devcon.exe" } | Select-Object -First 1
Write-Host "driver $inf, devcon $($devcon.FullName)"
# Trusting the driver's signer keeps Windows from asking before it installs.
$cert = (Get-AuthenticodeSignature $sys.FullName).SignerCertificate
$store = [System.Security.Cryptography.X509Certificates.X509Store]::new("TrustedPublisher", "LocalMachine")
$store.Open("ReadWrite")
$store.Add($cert)
$store.Close()
Push-Location $sys.DirectoryName
& $devcon.FullName install $inf "*Scream"
Pop-Location
Restart-Service -Name Audiosrv -Force
Get-CimInstance Win32_SoundDevice | Format-Table Name, Status
