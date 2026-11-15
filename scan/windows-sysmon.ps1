# Throwaway (scan-behaviour branch). Installs Sysmon with scan\sysmon.xml,
# or, when this Sysmon refuses the newest event types, without them.
. (Join-Path $PSScriptRoot 'lib.ps1')

$dir = Join-Path $env:RUNNER_TEMP 'sysmon'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$zip = Join-Path $dir 'Sysmon.zip'
curl.exe -sSfL --retry 3 -o $zip 'https://download.sysinternals.com/files/Sysmon.zip'
if ($LASTEXITCODE -ne 0) { Mark 'sysmon' "download failed (curl exit $LASTEXITCODE)"; exit 1 }
Expand-Archive -LiteralPath $zip -DestinationPath $dir -Force
$exe = Join-Path $dir 'Sysmon64.exe'
$sig = Get-AuthenticodeSignature -LiteralPath $exe
if ($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notmatch 'O=Microsoft Corporation') {
  Mark 'sysmon' "signature not trusted: $($sig.Status) $($sig.SignerCertificate.Subject)"
  exit 1
}

$config = Join-Path $PSScriptRoot 'sysmon.xml'
$r = Invoke-Tool -Name 'sysmon-install' -File $exe -Arguments @('-accepteula', '-i', $config) -TimeoutSec 180 -Console
$used = 'full'
if ($r.Record.exit -ne 0) {
  $null = Invoke-Tool -Name 'sysmon-uninstall' -File $exe -Arguments @('-u', 'force') -TimeoutSec 120 -Console
  $text = Get-Content -LiteralPath $config -Raw
  foreach ($t in 'FileDelete', 'ClipboardChange', 'ProcessTampering', 'FileDeleteDetected', 'FileBlockExecutable', 'FileBlockShredding', 'FileExecutableDetected') {
    $text = [regex]::Replace($text, "(?s)<RuleGroup[^>]*>\s*<$t\b.*?</RuleGroup>", '')
  }
  $text = $text -replace 'schemaversion="4\.90"', 'schemaversion="4.22"'
  $reduced = Join-Path $dir 'sysmon-reduced.xml'
  Write-Text $reduced $text
  $r = Invoke-Tool -Name 'sysmon-install-reduced' -File $exe -Arguments @('-accepteula', '-i', $reduced) -TimeoutSec 180 -Console
  $used = 'reduced'
}
if ($r.Record.exit -ne 0) { Mark 'sysmon' 'not installed'; exit 1 }
wevtutil.exe sl Microsoft-Windows-Sysmon/Operational /ms:268435456
Mark 'sysmon' "installed with the $used config"
exit 0
