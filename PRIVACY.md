# Privacy

Spytify has one switch for what it sends its developers: the **Sentry bug tracker** (Settings > Privacy). It is on by default and shown at the first launch, where you can untick it. Only official builds send anything: a build of your own has no bug tracker project (see `SPYTIFY_SENTRY_DSN` in [CONTRIBUTING.md](CONTRIBUTING.md#build)) and sends nothing.

## Never kept

Before a report leaves your computer, Spytify removes your computer's name, and turns your user name into `<user>`, your home folder into `~` and your output folder into `<output>` wherever they appear.

On Sentry's side, the project is set to:

- keep no IP address: Sentry reads the country from it first;
- remove any IP address or MAC address found anywhere in a report;
- mask user names in file paths;
- run the default scrubbers.

Your computer is known only by a code made from its machine ID for Spytify alone: the same after a reinstall, and of no use to any other app. Reports also carry the system, the version and the country.

## Sent while the switch is on

- Crashes of Spytify and of the recorder, with their trace, at the next launch, without asking.
- Problems that are not crashes, such as no sound reaching the recorder, a file that could not be encoded, or no graphics driver to draw the window, with the steps and clicks that led there. Each is counted; one computer in ten sends its details, once per version.
- Each launch: whether the recorder got ready, how long it took, the names of the checks that fail, and whether the window is drawn in software for want of a graphics driver.
- How far a new install got, once each: launched, recorder ready, first recording, first sound, first saved track.
- How long saving a track takes, for one track in a hundred.
- Recording counts, for every track: saved (with its format), skipped (with the reason, such as an ad or a duplicate) or kept by hand after being cut short, how its start was found and how far from where the player said the track changed. These carry no name of a track, an artist or an album.
- **Send to the developers** (Logs, when a check fails): the checks that fail and the end of the recorder's log, with an optional comment, once you have seen them.

The names of tracks, artists and albums do go where they appear in an error message or a log line.

Reports go to the developers' project on [Sentry](https://sentry.io), in the United States.

## When the switch is off

Nothing is sent or collected, not even in memory. **Send to the developers** is disabled, and a crash shows a dialog saying no report was sent, with the error and its trace, folded, to copy or to report on GitHub yourself.

## It never gets in the way

A report that cannot be made or sent is dropped, and at quit the app waits at most 2 seconds for what is queued.
