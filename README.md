[![Spytify](assets/logo-wordmark.png)](https://spytify.github.io/spytify/v2/overview.html)

[![Build](https://github.com/spytify/spytify/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/spytify/spytify/actions/workflows/ci.yml?query=branch%3Amain)
[![Tests](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fspytify%2Fspytify%2Fbadges%2Ftests.json)](https://github.com/spytify/spytify/actions/workflows/ci.yml?query=branch%3Amain)
[![Latest release](https://img.shields.io/github/v/release/spytify/spytify?include_prereleases&label=version)](https://github.com/spytify/spytify/releases)
[![Downloads](https://img.shields.io/github/downloads/spytify/spytify/total.svg?color=yellow&label=downloads)](https://github.com/spytify/spytify/releases)
[![Sub Reddit](https://img.shields.io/reddit/subreddit-subscribers/spytify.svg?label=r%2Fspytify)](https://www.reddit.com/r/spytify)
[![Donate](https://img.shields.io/badge/support-donate-ff69b4)](https://spytify.github.io/spytify/v2/donate.html)

**Records an application's audio, splits it into tracks and tags them.**

![Spytify recording a session: the current track with its progress and level, and the files already saved](assets/recording.png)

Spytify records the sound one application plays, nothing else, and saves each track as its own file, cut at the right sample and tagged with the title, artist, album and cover the application reports. It runs on Linux and macOS 14.2 or later; a first Windows build is being tested.

**[Download](https://github.com/spytify/spytify/releases)** · [Install](https://spytify.github.io/spytify/v2/install.html) · [Documentation](https://spytify.github.io/spytify/v2/overview.html) · [FAQ](https://spytify.github.io/spytify/v2/faq.html)

## A recorder, not a downloader

Spytify records what your computer already plays, like Audio Hijack or OBS. It downloads nothing, emulates no streaming protocol and circumvents no DRM.

Downloaders, such as the tools built on librespot, pull files through a service's own protocol; those are what services go after with legal action. Recording can break a service's terms, and what the service may do then is close your account. It is rare and has never happened to the maintainer, but use a separate free account. This is not legal advice: see [Legal](https://spytify.github.io/spytify/v2/legal.html).

## Privacy

Spytify's bug tracker, Sentry, is on by default. Untick it at the first launch or in Settings > Privacy, and nothing is sent at all.

- **Sent:** crashes, errors and recording counts, with the system, the version and the country: what it takes to fix a bug and to know where it happened.
- **Never sent:** your computer's name, your user name, your home folder or your IP address. Sentry reads the country from the IP address and does not keep it.
- Your computer is known only by a code made from its machine ID, for Spytify alone.

Everything the bug tracker sends: [PRIVACY.md](PRIVACY.md).

## Build and contribute

Building from source, the architecture, the tests and releases: see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT, see [LICENSE](LICENSE).
