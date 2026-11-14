---
layout: page
title: F.A.Q.
---

### [macOS or Windows will not open Spytify](#open-anyway)
{: #open-anyway}

Spytify is not notarised for macOS (_$99 USD/yr_) nor signed for Windows (_$129 USD/yr_), so each system warns the first time a version opens:

- **macOS:** allow it in System Settings > Privacy & Security > Open Anyway.
- **Windows:** when SmartScreen says "Windows protected your PC", click **More info**, then **Run anyway**. Spytify starts two programs of its own, the recorder `spytifyd.exe` and the capture helper `spytify-tap.exe`, and Windows can hold them without showing it: open each once from the Spytify folder and allow it the same way.

No such warning is known on Linux.

### [Security software blocks Spytify](#security-software)
{: #security-software}

Security software holds a version it does not know yet, until enough people have downloaded it. When its scan says a file looks suspicious but reports no threat, choose to run it anyway in its alert, then open `spytifyd.exe` and `spytify-tap.exe` once from the Spytify folder and allow them the same way. Otherwise, wait until the version becomes known, or build Spytify from its sources (see [Build and contribute](https://github.com/spytify/spytify#build-and-contribute)).

If it reports a virus or a threat, or deletes Spytify's files, we want to know so we can fix it: tell us in [GitHub issues](https://github.com/spytify/spytify/issues/new) or on [r/spytify](https://www.reddit.com/r/spytify/), with the name of the security software and of what it detected. The bug tracker cannot see a Spytify that never starts.

### [Nothing is recorded](#nothing-recorded)

Press **Diagnostics** in the Logs tab, or **Open diagnostics** on Record: the checks say what is wrong, such as "ffmpeg not found", and **Send to the developers** sends the checks that fail. When `spytifyd`, the recorder, is not running, Record says so and offers **Start the recorder**. If the application plays and no audio reaches Spytify for 10 seconds, the app also says where to look.

### [Where are my files?](#output-folder)

In `~/Spytify` (`C:\Users\<name>\Spytify` on Windows), grouped by artist and album, unless you chose another **Output folder** in Settings > Output. Up to 2.0.0-alpha.7 the default was `~/Music/Spytify`: an install that already records there keeps it. **Open folder** on Record opens it, and a row's magnifier shows its file there.

### [Tracks overlap, or a file starts with the end of the previous track](#tracks-overlap)

Turn off crossfade and gapless playback in the application: Spytify cuts between tracks in the audio, and two tracks mixed into each other cannot be separated again. Gapless albums are fine; they are cut at each track's length.

### [The track already playing was not recorded](#already-playing)

A file is a whole track, so recording starts with the next one. Press **Record this track from the start** to play it again from 0:00 and keep it.

### [A track is marked "track bar moved"](#track-bar-moved)

The position jumped while it played, so the file would not be the track as published, even if it lasted as long. Let tracks play through while recording, or press **Keep anyway** to save it with the jump.

### [The application plays my recording instead of the track](#local-files)

Some applications play your own music files in place of a streamed track when they match. Keep Spytify's output folder out of the folders the application scans.

### [Keep a track that was cut short](#keep-anyway)

The last track cut short (skipped, stopped with **Stop now**, or moved along its track bar) shows **Keep anyway** in the Record list until the next track is saved. It saves the part that played as "*title* (partial)", which never counts as the whole track: when it plays in full, it is recorded as usual. A track skipped as low quality is kept the same way, as "*title* (low quality)". The Library's filter shows these takes alone, to clean them up.

### [Tracks are skipped as low quality](#low-quality)

The application's output device runs below 44.1 kHz or in mono, so the tracks would sound like a phone call. A Bluetooth headset does this while an app uses its microphone. The warning says what to change: another output, or another microphone than the headset's.

### [Short items are skipped as ads](#ads)

Podcast episodes record like tracks. Items shorter than a minute without an album are taken for ads: set **Treat items shorter than N seconds with no album as ads** to 0 in Settings > Ads > Advanced to record them.

### [Keep the order of the recordings](#recording-order)

Set **File name prefix** to `counter` in Settings > Output > Advanced: each file starts with its number, which goes on from one recording to the next. **Next counter number** sets the next one: set it back to 1 for a new CD.

### [Is FLAC better than MP3?](#quality)

MP3 at 160 kbps is the default. FLAC and WAV avoid a second lossy pass, but streaming services already send compressed audio: a FLAC file is larger and sounds the same as the stream.

### [Spytify does not open its window](#no-window)

Spytify draws its window with OpenGL. On Windows, without a graphics driver offering it, Spytify draws with the copy of Mesa in its `opengl` folder, in software. On Linux, the packages bring Mesa's drivers; without them, the app says what to install.

### [macOS does not let Spytify record](#macos-permissions)

Spytify needs **System Audio Recording** and **Automation**. Turn them on in System Settings > Privacy & Security, then open Spytify again. A Spytify you built yourself asks again after each build. While the prompt waits for an answer, nothing is recorded and the level stays still; after 10 s with no audio, the app shows where to allow it.
