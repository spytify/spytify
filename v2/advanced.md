---
layout: page
title: Advanced
---

## [Command line](#command-line)

`spytifyd` records; the Spytify app and `spytify-cli` talk to it over a local socket.

```sh
spytifyd &                     # logs the recorded application on its first line
spytify-cli info               # versions and the startup checks
spytify-cli set encode.format=flac output.dir=~/Music/Recordings
spytify-cli start              # recording starts with the next track
spytify-cli status
spytify-cli tail -f            # follow the log
spytify-cli stop               # the current track finishes first; --now discards it
```

| `spytify-cli` | What it does |
| --- | --- |
| `info` | versions, recorded application, startup checks |
| `start [--single-track]` / `stop [--now]` / `status` | the recording session |
| `set key=value...` / `get [key...]` / `reset [prefix]` | [settings](settings.html), validated by the recorder |
| `tail [-n N] [-f]` / `events` | log lines / every event as JSON lines |
| `control play|pause|next|previous|restart` | transport; `restart` plays the current track again from 0:00 |
| `launch` | opens the application if it is not running |
| `service install|uninstall|status` | runs `spytifyd` at login and again after a crash (Linux, macOS) |
| `shutdown` | stops the recorder |

`spytifyd --once --set output.dir=/srv/rec` records one session without a socket, until the application stops or stays idle. `spytifyd --fake timeline.json` plays a scripted timeline through a built-in fake player, to try everything without an application.

### [Configuration](#configuration)

The recorded application is set with environment variables, read when the recorder starts, from the environment or a `.env` file: next to the binary, in the working folder, or in your configuration folder (`~/.config/spytify/.env`, `~/Library/Application Support/spytify/.env` on macOS, `%APPDATA%\spytify\.env` on Windows). The app's Player settings write that last one.

| Variable | Used for | Default |
| --- | --- | --- |
| `SPYTIFY_TARGET_PROCESS` | the application's name, or a comma list for several packagings | the build's default |
| `SPYTIFY_TARGET_MPRIS` | its D-Bus name (Linux) | `org.mpris.MediaPlayer2.<name>` |
| `SPYTIFY_TARGET_BUNDLE_ID` | its bundle id (macOS) | read from the installed app |
| `SPYTIFY_TARGET_DURATION_UNIT` | `ms` or `s`, the unit of its AppleScript track length (macOS) | guessed |
| `SPYTIFY_TARGET_LAUNCH` | a command that opens it | the system's own way |
| `SPYTIFY_TAP` | the capture helper `spytify-tap` (macOS, Windows) | next to `spytifyd`, then `PATH` |
| `SPYTIFY_CONFIG` | the settings file | `config.toml` in the configuration folder |
| `SPYTIFY_SOCKET` | the recorder's socket | in the runtime or state folder |
| `SPYTIFY_FFMPEG` | the ffmpeg binary | `ffmpeg` on `PATH`, then next to `spytifyd` and in the usual install folders |

Snap and Flatpak builds of an application can run under another process name, and expose MPRIS only through their sandbox. List every process name in `SPYTIFY_TARGET_PROCESS`. If `spytify-cli info` finds the process but no MPRIS name, check the sandbox's D-Bus permissions (`snap connections <snap>`, `flatpak info --show-permissions <app>`).

Everything else is a [setting](settings.html). Clients of their own speak the [protocol](#protocol).

## [Protocol](#protocol)

{% include v2-protocol.md %}
