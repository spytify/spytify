# Contributing

Issues and pull requests are welcome. Run `gofmt` and `go test -race ./...` before you push: CI runs the tests with `-race`, builds every platform, runs the PipeWire scenarios, and runs the tests and the shipped executables on Windows.

A change to what the bug tracker sends or scrubs (`internal/telemetry`) updates [PRIVACY.md](PRIVACY.md) in the same pull request.

What the app does and how to use it is on the [site](https://spytify.github.io/spytify/v2/overview.html); this page is about the code.

## How it works

```text
Spytify app ─┐                ┌── capture ◀── the application's own audio stream
spytify-cli ─┴─ JSON-RPC ──▶ spytifyd ◀── now playing ◀── the application's media interface
                                  └──▶ ffmpeg ──▶ tagged files
```

- **Two processes.** `spytifyd` captures, splits, encodes and tags, and keeps recording with no window open. The desktop app ([Fyne](https://fyne.io)) and `spytify-cli` are clients over a local socket.
- **Capture of one process.** Linux: its PipeWire stream, found with `pw-dump`, recorded with `pw-record` (PulseAudio: `parec --monitor-stream`). macOS: `spytify-tap`, a Swift helper using a Core Audio process tap. Windows: `spytify-tap.exe`, a C++ helper using WASAPI process loopback on the process tree.
- **Now playing.** MPRIS on Linux, the application's AppleScript interface on macOS, the Windows media session on Windows. Nothing is looked up on the web.
- **Cuts.** A track change notification is only roughly on time. The track length gives the expected end, and the quietest 20 ms around it gives the sample.
- **Finishing.** Each track goes to a temporary WAV, is checked, trimmed, normalised, encoded to `<name>.part`, tagged, then renamed, so a half-written file never has the final name.

## Build

Go 1.26 or later.

```sh
go build -o bin/ ./cmd/spytifyd ./cmd/spytify-cli ./cmd/spytify-ui   # Linux
scripts/build-linux.sh                                               # Linux packages: dist/*.tar.gz, .deb, .rpm
scripts/build-macos.sh                                               # macOS: bin/Spytify.app, with ffmpeg, and the command-line tools
scripts/build-windows.sh                                             # Windows, cross-compiled: bin/windows/*.exe, ffmpeg.exe and Mesa's OpenGL
```

- `spytifyd` and `spytify-cli` are pure Go (`CGO_ENABLED=0` works). The desktop app needs cgo:
  - Linux: `gcc`, `libgl1-mesa-dev`, `xorg-dev`, `libwayland-dev`, `libxkbcommon-dev`, `wayland-protocols` (Debian names).
  - macOS: the Xcode command line tools, which also build `spytify-tap`.
  - Windows: MinGW-w64 (`x86_64-w64-mingw32-gcc`, `-g++` and `-windres`), from Linux or macOS; it also builds `spytify-tap.exe` and puts the icon and version information in `Spytify.exe` ([`windows/spytify.rc`](windows/spytify.rc)). 7-Zip (`7z` or `7zz`) unpacks Mesa into `opengl\`: with no graphics driver offering OpenGL, the app draws its window with Mesa's, in software. For that, `Spytify.exe` must not link `opengl32.dll`, which go-gl/gl does: the script builds it with a copy of go-gl/gl whose loader is [`windows/go-gl-procaddr.go.in`](windows/go-gl-procaddr.go.in), and `checkexe` fails the build otherwise. A plain `go build` of the app links it, and draws without Mesa.
  - The ffmpeg of the Windows zip and of the macOS app is built from source by [`scripts/build-ffmpeg.sh`](scripts/build-ffmpeg.sh) (it needs `make` and `nasm`; on a Mac, `brew install nasm`), with only the encoders, filters and formats Spytify runs. The Mac's is universal and runs from macOS 14.2, as the app. `SPYTIFY_NO_FFMPEG=1` leaves it out of either build. Code that runs ffmpeg for something new adds the component to the script's list, with a test that runs it, and runs the tests with that build: the script says how.
- What the app needs at run time (ffmpeg, PipeWire, the macOS permissions) is on the [Install](https://spytify.github.io/spytify/v2/install.html) page.
- The macOS app is ad-hoc signed: macOS asks for its permissions again after every rebuild, and another Mac refuses it until it is signed and notarised.

Build variables, read from the environment or an untracked `build.env`; CI takes them from repository variables:

| Variable | What it does | Without it |
| --- | --- | --- |
| `SPYTIFY_DEFAULT_PROCESS` | the application recorded by default: its bare name, matching `Name.app`, `Name.exe` or `name` | no default: choose it in Settings > App > Advanced > Player, or set `SPYTIFY_TARGET_PROCESS` |
| `SPYTIFY_SENTRY_DSN` | the Sentry project of the [bug tracker](PRIVACY.md) | nothing is sent and the switch is hidden; a crash shows a dialog to report by hand |

A version with `dev` or `+` in it, such as CI's, reports as the `development` environment. Run-time variables (`SPYTIFY_TARGET_*`, `SPYTIFY_FFMPEG`, `SPYTIFY_SOCKET`…) are on the [Advanced](https://spytify.github.io/spytify/v2/advanced.html#configuration) page.

## Protocol

Clients speak newline-delimited JSON-RPC 2.0 on the socket, or on TCP with `spytifyd --listen 127.0.0.1:7811`. Actions are requests; events are notifications sent to every client. Go clients import `github.com/spytify/spytify/pkg/proto`; others use [`pkg/proto/schema.json`](pkg/proto/schema.json), generated from the same types (`go generate ./pkg/proto`). The [Advanced](https://spytify.github.io/spytify/v2/advanced.html#protocol) page lists every method and event.

## Tests

```sh
go test -race ./...    # unit tests, the fake-player scenarios, the CLI end to end
```

- **Scenarios.** [`testdata/timelines`](testdata/timelines) drive the real core with a scripted player: notifications from 300 ms early to 800 ms late, gapless albums, ads, a sample-rate change, a pause, duplicates, skips, seeks. Each file must match its source by cross-correlation: peak within 2 ms of zero, residual under −50 dBFS. `spytifyd --fake <timeline>` plays one by hand.
- **Real PipeWire and MPRIS**, through `cmd/fakeplayer`; CI runs it in a container with no sound card:

  ```sh
  . scripts/pipewire-session.sh      # D-Bus, PipeWire, WirePlumber and a null sink
  go test -tags integration -v ./test/integration/
  ```

- **macOS capture**, against what `afplay` plays. It needs the System Audio Recording permission, so not in CI: `go test -tags macos_capture -v ./test/macos/`.
- **AppleScript**, against FakePlayer ([`macos/fakeplayer`](macos/fakeplayer)), a scriptable app that plays a timeline: `scripts/build-fakeplayer-macos.sh && SPYTIFY_FAKE_APP=$PWD/bin/FakePlayer.app go test -run TestJXAWithTheFakePlayer -v ./internal/meta/`.
- **Sentry**, against a fake server ([`internal/telemetry/fakesentry`](internal/telemetry/fakesentry)): with the bug tracker off, nothing may reach it; with Sentry out of reach, every call must return. Against a real test project: `SPYTIFY_SENTRY_LIVE_DSN=<dsn> go test -run TestLiveSentry -v ./internal/telemetry/`.
- **A real player.** `gate1` writes ten test tracks to play from the player's local files, then compares each recording with its track by the same rule:

  ```sh
  go run ./internal/tools/gate1 export -dir ~/Gate1   # then play them, recording to WAV or FLAC with normalize off
  go run ./internal/tools/gate1 check -dir ~/Gate1 -out ~/Music/Spytify
  ```

## Releases

Push a tag such as `2.0.0-alpha.1` (no `v`, like v1's tags), or run the **release** workflow from the Actions tab. It builds the Linux packages (amd64, arm64), the macOS disk image and the Windows zip, and drafts a GitHub release with their checksums; a manual run can publish it at once. A version with a `-` is a pre-release. Every CI run also keeps these builds on its summary page.

Signing turns on once the repository has the secrets, otherwise the files ship unsigned and the release notes say so:

- macOS, hardened runtime ([`scripts/sign-macos.sh`](scripts/sign-macos.sh)): `MACOS_CERT_P12` (a Developer ID Application certificate, base64), `MACOS_CERT_PASSWORD`, `MACOS_SIGN_IDENTITY`.
- macOS notarisation ([`scripts/notarize-macos.sh`](scripts/notarize-macos.sh)): `NOTARY_KEY` (an App Store Connect API key, base64), `NOTARY_KEY_ID`, `NOTARY_ISSUER`.
- Windows Authenticode ([`scripts/sign-windows.sh`](scripts/sign-windows.sh)): `WINDOWS_CERT_PFX` (base64), `WINDOWS_CERT_PASSWORD`.

## Site

The site is built by GitHub Pages from the `gh-pages` branch. `v2/settings.md` and the protocol section come from the code: `go run ./internal/tools/gendocs -out <gh-pages checkout>`.
