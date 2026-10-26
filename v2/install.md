---
layout: page
title: Install
---

Every release has a build per system on the [releases page]({{ site.repository_download }}). Spytify also needs **ffmpeg**, which encodes the files.

### [Linux](#linux)

- Install the `.deb` (Debian, Ubuntu) or the `.rpm` (Fedora, openSUSE), or unpack the `.tar.gz` anywhere.
- It needs PipeWire 0.3.40 or later (`pw-record`, `pw-dump`) or PulseAudio (`parec`, `pactl`), a D-Bus session, and an application that exposes MPRIS, as most Linux players do.
- On PipeWire, let the graph run at 44.1 kHz as well as 48 kHz, or 44.1 kHz audio is resampled before it is recorded. Create `~/.config/pipewire/pipewire.conf.d/10-rates.conf`:

  ```text
  context.properties = {
      default.clock.allowed-rates = [ 44100 48000 ]
  }
  ```

  then run `systemctl --user restart pipewire`.
- The packages install a user service: `systemctl --user enable --now spytifyd` keeps the recorder running from login.

### [macOS](#macos)

- macOS 14.2 or later. Open the `.dmg` and drag Spytify to Applications.
- The app is not notarised yet: the first time, macOS refuses to open it. Allow it in System Settings > Privacy & Security > Open Anyway.
- macOS asks once for two permissions: **System Audio Recording**, for the capture, and **Automation**, to read what is playing. If you said no, turn them on in System Settings > Privacy & Security and open Spytify again.
- Install ffmpeg with [Homebrew](https://brew.sh): `brew install ffmpeg`. Spytify finds it in `/opt/homebrew/bin` and `/usr/local/bin`.

### [Windows](#windows)

The Windows build is a first draft, being tested.

- Windows 10 version 2004 or later, or Windows 11. Unzip the release anywhere and open `Spytify.exe`.
- The zip comes with `ffmpeg.exe`, an LGPL build (see `ffmpeg-README.txt`). One installed with winget, Chocolatey or Scoop works too.
- The application must show what it plays in the Windows media controls (the volume flyout).

### [Choose the application](#choose-the-application)

Official builds come with a default application. In a build of your own, open **Settings > App > Advanced > Player**, enter the application's name (its executable, without `.app` or `.exe`), and press **Save and restart spytifyd**. The [command line page](advanced.html#configuration) lists the variables behind it.
