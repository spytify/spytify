# CI only: plays a 440 Hz tone in a loop until killed, so the capture
# helper has a process to record.
param([string]$Path = (Join-Path $env:RUNNER_TEMP "tone.wav"))
$rate = 48000; $seconds = 5; $frames = $rate * $seconds
$w = [System.IO.BinaryWriter]::new([System.IO.File]::Create($Path))
$w.Write([Text.Encoding]::ASCII.GetBytes("RIFF")); $w.Write([int](36 + $frames * 4))
$w.Write([Text.Encoding]::ASCII.GetBytes("WAVEfmt ")); $w.Write([int]16); $w.Write([int16]1); $w.Write([int16]2)
$w.Write([int]$rate); $w.Write([int]($rate * 4)); $w.Write([int16]4); $w.Write([int16]16)
$w.Write([Text.Encoding]::ASCII.GetBytes("data")); $w.Write([int]($frames * 4))
for ($i = 0; $i -lt $frames; $i++) {
  $s = [int16](16384 * [Math]::Sin(2 * [Math]::PI * 440 * $i / $rate))
  $w.Write($s); $w.Write($s)
}
$w.Close()
$player = New-Object System.Media.SoundPlayer $Path
$player.PlayLooping()
Start-Sleep -Seconds 120
