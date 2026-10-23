# CI only: plays a 440 Hz tone in a loop until killed, so the capture
# helper has a process to record. With -Smtc it plays through Windows'
# MediaPlayer with a title, an artist and an album, so the media controls
# (SMTC) show a session to read.
param([string]$Path = (Join-Path $env:RUNNER_TEMP "tone.wav"), [switch]$Smtc)
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
if (-not $Smtc) {
  $player = New-Object System.Media.SoundPlayer $Path
  $player.PlayLooping()
  Start-Sleep -Seconds 120
  exit
}
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Media.Playback.MediaPlayer, Windows.Media.Playback, ContentType = WindowsRuntime]
$null = [Windows.Media.Playback.MediaPlaybackItem, Windows.Media.Playback, ContentType = WindowsRuntime]
$null = [Windows.Media.Core.MediaSource, Windows.Media.Core, ContentType = WindowsRuntime]
$null = [Windows.Media.MediaPlaybackType, Windows.Media, ContentType = WindowsRuntime]
$item = [Windows.Media.Playback.MediaPlaybackItem]::new([Windows.Media.Core.MediaSource]::CreateFromUri([Uri]::new($Path)))
$props = $item.GetDisplayProperties()
$props.Type = [Windows.Media.MediaPlaybackType]::Music
$props.MusicProperties.Title = "Runner Tone"
$props.MusicProperties.Artist = "Spytify CI"
$props.MusicProperties.AlbumTitle = "Runner"
$item.ApplyDisplayProperties($props)
$player = [Windows.Media.Playback.MediaPlayer]::new()
$player.IsLoopingEnabled = $true
$player.Source = $item
$player.Play()
Start-Sleep -Seconds 120
