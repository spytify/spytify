<p align="center"><img src="assets/logo.png" alt="Spytify logo" width="96"></p>

# Spytify v2 (alpha)

**Records any application's audio, splits it into tracks and tags them.**

> **Alpha.** Linux and macOS 14.2 or later, with a desktop app (`Spytify.app` on macOS, `spytify-ui` on Linux) or the command line. The Windows backend comes next (milestone M5 of [the plan](PLAN.md)). Settings, file names and the protocol may still change.

Spytify v2 is a rewrite in Go of [Spytify v1](https://github.com/jwallet/spy-spotify), the Windows recorder. You name the process to record; Spytify captures that process's audio stream only, cuts it into one file per track at a computed sample position, encodes with ffmpeg and writes tags. It never downloads anything and never names a player in its code: the player is whatever you put in `SPYTIFY_TARGET_PROCESS`.

## How it works

```
spytify-cli  ──actions──▶  spytifyd  ◀── PCM ── pw-record ◀── the player's stream (PipeWire)
             ◀──events───            ◀── now playing ────── MPRIS (D-Bus)
                                      ──▶ ffmpeg ──▶ tagged files
```

- **Two processes, one bus.** `spytifyd` does all capture, splitting, encoding and tagging, and keeps running without a window. Clients (`spytify-cli` today, a UI later) talk to it over newline-delimited JSON-RPC 2.0 on a Unix socket.
- **Per-process capture.** On PipeWire the daemon finds the player's output stream with `pw-dump` and records that stream alone with `pw-record`, so no other application can bleed in and no virtual cable is needed. PulseAudio falls back to `parec --monitor-stream`. On macOS a small helper, `spytify-tap`, records the player's processes with a Core Audio process tap.
- **Now playing.** MPRIS on Linux; on macOS the player's AppleScript interface (current track, player state, player position), polled every 250 ms.
- **Sample-accurate boundaries.** The player's MPRIS notification only says roughly when a track changed. The cut is placed from the audio: the declared track length gives the expected end, and the quietest 20 ms inside a ±300 ms window decides the exact sample. Gapless transitions fall back to the declared length. This fixes v1's split jitter (issue #359).
- **Two-pass finishing.** Each track goes to a temp WAV first; it is then checked against the minimum length, trimmed, loudness-normalised, encoded by ffmpeg to `<name>.part`, tagged and renamed. A half-written file never carries the final name.

## Requirements

Everywhere:

- `ffmpeg` on `PATH` (or `SPYTIFY_FFMPEG`), built with `libmp3lame` for MP3 output. It is the only runtime dependency.
- Crossfade and gapless playback turned off in the player: two mixed tracks cannot be un-mixed.

Linux:

- PipeWire 0.3.40 or later (`pw-record`, `pw-dump`), or PulseAudio (`parec`, `pactl`).
- A D-Bus session bus, and a player that exposes MPRIS (most Linux players do).

macOS:

- macOS 14.2 or later (process taps), and a player with an AppleScript dictionary (`current track`, `player state`, `next track`).
- `spytify-tap` next to `spytifyd` (the build script puts it there).
- Two permissions, asked once by macOS for the app you run `spytifyd` from (Terminal, iTerm, …): **System Audio Recording** (Privacy & Security > Screen & System Audio Recording) for the capture, and **Automation** for reading the player's now-playing. If you denied one, turn it on in System Settings and restart `spytifyd`.
- With `player.mute_output = true` the player is silent on your speakers while it is recorded.
- On PipeWire, let the graph run at 44.1 kHz as well as 48 kHz, or 44.1 kHz audio is resampled before it reaches the recorder. `spytify-cli info` checks it. Create `~/.config/pipewire/pipewire.conf.d/10-rates.conf` with:

  ```
  context.properties = {
      default.clock.allowed-rates = [ 44100 48000 ]
  }
  ```

  then restart PipeWire (`systemctl --user restart pipewire`).
- To build: Go 1.26 or later.

## Legal

Spytify is a general-purpose application audio recorder, in the same category as Audio Hijack, Loopback or OBS. This is not legal advice; it is how the project is built:

1. The name stays Spytify, but nothing in the code, the tagline or these docs names a player. You choose the process.
2. Spytify does not download, does not emulate any streaming protocol and does not circumvent DRM. It only records audio the operating system already delivers to your speakers.
3. The Spotify Web API tag source is optional, off by default, and uses keys you create. The default tag source is Last.fm.
4. There is no ad-blocking switch. A generic rule skips short items that carry no album (`recorder.skip_short_untitled`); `recorder.record_everything` turns it off for podcasts and radio.
5. You bring your own accounts. Recording may breach a service's terms, and an account ban is the realistic consequence; a throwaway account is recommended, as v1 already said.
6. Private-copy exceptions vary by country. Spytify does not promise that recording is legal where you live.

**For personal use of content you are entitled to listen to; you are responsible for the terms of the services you record.**

## Build

```sh
go build -o bin/ ./cmd/spytifyd ./cmd/spytify-cli ./cmd/spytify-ui   # Linux
scripts/build-macos.sh                                               # macOS: bin/Spytify.app, plus the command-line tools
```

`spytifyd` and `spytify-cli` are static Go (`CGO_ENABLED=0` works); the Linux backends call the PipeWire, PulseAudio and D-Bus tools at run time. The desktop app is built with [Fyne](https://fyne.io) and needs cgo: on Linux `gcc`, `libgl1-mesa-dev`, `xorg-dev`, `libwayland-dev`, `libxkbcommon-dev` and `wayland-protocols` (Debian names); on macOS the Xcode command line tools, which also build `spytify-tap`, written in Swift.

## Desktop app

The app is a client of `spytifyd`, like the CLI: closing the window does not stop a recording, and the tray menu (Start recording, Stop, Open window) keeps working while it is hidden (`app.minimize_to_tray`).

- **Record**: the track being recorded (cover, progress, level, Skip), then every file of the session with its status, Reveal and Play, and the session totals with Open folder. Before a session it shows the checklist from `core.info`.
- **Library**: every audio file in the output folder, newest first, with a search box.
- **Settings**: one card per group with the TOML key under each field, a search box, Reset group, and a **Player** card that sets `SPYTIFY_TARGET_*` in your user `.env` and restarts the daemon.
- **Logs** and **About**: the daemon's log by level, versions, the resolved target and the checks.

On macOS, open `bin/Spytify.app`. The first time, set the player in the Player card (process name and bundle id) and press **Save and restart spytifyd**; macOS then asks, once each, to let Spytify record system audio and to control the player. The app finds `ffmpeg` in `/opt/homebrew/bin` or `/usr/local/bin` even though apps opened from Finder have no shell `PATH`. The build is ad-hoc signed: macOS asks for the permissions again after each rebuild, and another Mac will refuse to open it until it is signed and notarised.

## Quick start

```sh
cp .env.example bin/.env      # then set SPYTIFY_TARGET_PROCESS to your player's process name
bin/spytifyd &                # logs the resolved target on its first line
bin/spytify-cli info          # versions and a checklist: player running, ffmpeg, capture, D-Bus, keyring
bin/spytify-cli set encode.format=flac output.dir=~/Music/Recordings
bin/spytify-cli start         # recording starts with the next track
bin/spytify-cli status
bin/spytify-cli tail -f       # follow the log
bin/spytify-cli stop          # the current track finishes first; --now discards it
```

On macOS, also set the player's bundle id in `.env`; `osascript -e 'id of app "<Player name>"'` prints it:

```sh
SPYTIFY_TARGET_PROCESS=<Player name>
SPYTIFY_TARGET_BUNDLE_ID=<bundle id>
```

The bundle id selects the player's processes, helpers included, and names it for AppleScript. Players report track lengths in milliseconds or in seconds; Spytify reads an integer as milliseconds and a fraction as seconds, and `SPYTIFY_TARGET_DURATION_UNIT=ms` or `s` overrides it.

Headless one-shot run, no socket, until the player stops or goes idle:

```sh
bin/spytifyd --once --set output.dir=/srv/rec --set encode.format=flac
```

`spytifyd --fake testdata/timelines/basic.json` plays a scripted timeline through the in-process fake player, on any OS, to try the CLI without a player.

### Commands

| `spytify-cli` | Action |
| --- | --- |
| `info` | `core.info`: versions, target, capability checklist |
| `start [--single-track]` | `session.start` |
| `stop [--now]` | `session.stop` |
| `status` | `session.status`: state, current track, counters |
| `set key=value...` / `get [key...]` / `reset [prefix]` | settings, validated by the daemon |
| `tail [-n N] [-f]` | log lines |
| `events` | every event as JSON lines |
| `control play\|pause\|next\|previous` | `player.control` through MPRIS |
| `spotify-connect` | OAuth for `tags.source = spotify_api` |
| `shutdown` | `core.shutdown` |

## Configuration

The player is identified by environment variables, read once at daemon start and logged on the first line. A `.env` file next to the binary, in the working directory, or in your user configuration folder (`~/.config/spytify/.env`, `~/Library/Application Support/spytify/.env` on macOS; the desktop app writes this one) is loaded if present.

| Variable | Used for | Default |
| --- | --- | --- |
| `SPYTIFY_TARGET_PROCESS` | process to record; a comma list for several packagings | none: the daemon refuses to start |
| `SPYTIFY_TARGET_MPRIS` | D-Bus name of the player (Linux) | `org.mpris.MediaPlayer2.<process>` |
| `SPYTIFY_TARGET_BUNDLE_ID` | bundle id of the player (macOS) | none: the process name is used |
| `SPYTIFY_TARGET_DURATION_UNIT` | `ms` or `s`, unit of the AppleScript track length (macOS) | guessed |
| `SPYTIFY_TAP` | path of `spytify-tap` (macOS) | next to `spytifyd`, then `PATH` |
| `SPYTIFY_TARGET_LAUNCH` | command that starts the player when a session starts | none |
| `SPYTIFY_CONFIG` | settings file | `~/.config/spytify/config.toml` |
| `SPYTIFY_SOCKET` | daemon socket | `$XDG_RUNTIME_DIR/spytify.sock` |
| `SPYTIFY_FFMPEG` | ffmpeg binary | `ffmpeg` on `PATH` |

Everything else is a setting in `config.toml`, changed with `spytify-cli set` and validated by the daemon; `spytify-cli get` lists them all. Secrets (Spotify client secret, Last.fm key) go to the OS keyring, never to `config.toml`. The full table, with the v1 setting each one replaces, is in [PLAN.md](PLAN.md#settings-carried-over-from-v1).

Snap and Flatpak builds of a player can run under a different process name and expose MPRIS only through their sandbox interfaces. List every process name in `SPYTIFY_TARGET_PROCESS`, and if `spytify-cli info` finds the process but no MPRIS name, check the sandbox's D-Bus permissions (`snap connections <snap>`, `flatpak info --show-permissions <app>`).

## Protocol

Clients speak newline-delimited JSON-RPC 2.0 on the socket (and on TCP with `spytifyd --listen 127.0.0.1:7811`). Actions are requests; events are notifications broadcast to every client.

```json
{"jsonrpc":"2.0","id":7,"method":"settings.set","params":{"values":{"encode.format":"flac"}}}
{"jsonrpc":"2.0","id":7,"result":{"applied":["encode.format"],"rejected":{}}}
{"jsonrpc":"2.0","method":"settings.changed","params":{"values":{"encode.format":"flac"}}}
```

Go clients import `github.com/spytify/spytify/pkg/proto`; others use [`pkg/proto/schema.json`](pkg/proto/schema.json), generated from the same types (`go generate ./pkg/proto`). The methods and events are listed in [PLAN.md](PLAN.md#messaging-contract).

## Tests

```sh
go test ./...                      # unit tests, the eight fake-player scenarios, the CLI end to end
```

The scenarios in [`testdata/timelines`](testdata/timelines) drive the real core with a scripted player: notifications from 300 ms early to 800 ms late, gapless albums, adverts, a sample-rate change, a pause, duplicates and a skip. Every file is compared with its source by cross-correlation: peak within 2 ms of zero and residual under −50 dBFS.

The same timelines run against real PipeWire and MPRIS with `cmd/fakeplayer`, which is what CI's gate job does:

```sh
. scripts/pipewire-session.sh      # D-Bus, PipeWire, WirePlumber and a null sink, no sound card needed
go test -tags integration -v ./test/integration/
```

On macOS, the capture helper is checked against what `afplay` plays (it needs the System Audio Recording permission, so it does not run in CI):

```sh
go test -tags macos_capture -v ./test/macos/
```

## License

MIT, see [LICENSE](LICENSE).
