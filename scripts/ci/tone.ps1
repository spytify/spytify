# CI only: plays a 440 Hz tone in a loop until killed, so the capture
# helper has a process to record. With -Smtc it plays through Windows'
# MediaPlayer with a title, an artist and an album, so the media controls
# (SMTC) show a session to read. Writing the WAV takes seconds, more on a
# slow runner, and Windows starts the tone up to a second or more after
# PlayLooping returns: <Path>.playing appears once Windows meters the tone
# in this process's audio session (with -Smtc, once MediaPlayer is asked
# to play), to wait on. Without -Smtc it exits by itself after -Seconds.
param([string]$Path = (Join-Path $env:RUNNER_TEMP "tone.wav"), [switch]$Smtc, [int]$Seconds = 120)
$ready = "$Path.playing"
Remove-Item -ErrorAction Ignore $ready
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
  Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

[ComImport, Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")]
class MMDeviceEnumerator {}

// Only the methods called are typed; the others hold their place in the
// vtable.
[ComImport, Guid("A95664D2-9614-4F35-A746-DE8DB63617E6"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDeviceEnumerator {
  int EnumAudioEndpoints(int flow, int mask, out IMMDeviceCollection devices);
}

[ComImport, Guid("0BD7A1BE-7A1A-44DB-8397-CC5392387B5E"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDeviceCollection {
  int GetCount(out int n);
  int Item(int i, out IMMDevice device);
}

[ComImport, Guid("D666063F-1587-4E43-81F1-B948E807363F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDevice {
  int Activate(ref Guid iid, int context, IntPtr parameters, [MarshalAs(UnmanagedType.IUnknown)] out object client);
}

[ComImport, Guid("77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioSessionManager2 {
  int GetAudioSessionControl();
  int GetSimpleAudioVolume();
  int GetSessionEnumerator(out IAudioSessionEnumerator sessions);
}

[ComImport, Guid("E2F5BB11-0570-40CA-ACDD-3AA01277DEE8"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioSessionEnumerator {
  int GetCount(out int n);
  int GetSession(int i, [MarshalAs(UnmanagedType.IUnknown)] out object session);
}

[ComImport, Guid("bfb7ff88-7239-4fc9-8fa2-07c950be9c6d"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioSessionControl2 {
  int GetState();
  int GetDisplayName();
  int SetDisplayName();
  int GetIconPath();
  int SetIconPath();
  int GetGroupingParam();
  int SetGroupingParam();
  int RegisterAudioSessionNotification();
  int UnregisterAudioSessionNotification();
  int GetSessionIdentifier();
  int GetSessionInstanceIdentifier();
  int GetProcessId(out uint pid);
}

[ComImport, Guid("C02216F6-8C67-4B5B-9D00-D008E73E0064"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioMeterInformation {
  int GetPeakValue(out float peak);
}

public static class Heard {
  // Peak is the highest peak Windows meters now in the audio sessions of
  // process pid, on every active output: 0 while it plays nothing.
  public static float Peak(uint pid) {
    float max = 0;
    var devices = (IMMDeviceEnumerator)new MMDeviceEnumerator();
    IMMDeviceCollection list;
    // eRender, DEVICE_STATE_ACTIVE
    if (devices.EnumAudioEndpoints(0, 1, out list) != 0) return 0;
    int n;
    list.GetCount(out n);
    var iid = typeof(IAudioSessionManager2).GUID;
    for (int d = 0; d < n; d++) {
      IMMDevice device;
      object manager;
      IAudioSessionEnumerator sessions;
      // CLSCTX_ALL
      if (list.Item(d, out device) != 0 || device.Activate(ref iid, 23, IntPtr.Zero, out manager) != 0 ||
          ((IAudioSessionManager2)manager).GetSessionEnumerator(out sessions) != 0) {
        continue;
      }
      int count;
      sessions.GetCount(out count);
      for (int i = 0; i < count; i++) {
        object session;
        uint owner;
        float peak;
        if (sessions.GetSession(i, out session) != 0 || ((IAudioSessionControl2)session).GetProcessId(out owner) != 0 || owner != pid) {
          continue;
        }
        if (((IAudioMeterInformation)session).GetPeakValue(out peak) == 0 && peak > max) max = peak;
      }
    }
    return max;
  }
}
"@
  $player = New-Object System.Media.SoundPlayer $Path
  $player.PlayLooping()
  # The tone peaks at -6 dBFS, 0.5; -20 dBFS is 0.1. Up to 60 s, as long
  # as windows-smoke waits.
  for ($i = 0; $i -lt 600 -and [Heard]::Peak($PID) -lt 0.1; $i++) { Start-Sleep -Milliseconds 100 }
  if ($i -lt 600) { New-Item -ItemType File -Force $ready | Out-Null }
  Start-Sleep -Seconds $Seconds
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
New-Item -ItemType File -Force $ready | Out-Null
Start-Sleep -Seconds 120
