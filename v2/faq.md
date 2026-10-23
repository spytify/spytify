---
layout: page
title: F.A.Q.
---

### [Tracks overlap, or a file starts with the end of the previous track](#tracks-overlap)

Turn off crossfade and gapless playback in the application: Spytify cuts between tracks in the audio, and two tracks mixed into each other cannot be separated again. Gapless albums are fine; they are cut at each track's length.

### [The track already playing was not recorded](#already-playing)

A file is a whole track, so recording starts with the next one. Press **Record this track from the start** to play it again from 0:00 and keep it.

### [A track is marked "track bar moved"](#track-bar-moved)

The position jumped while it played, so the file would not be the track as published, even if it lasted as long. Let tracks play through while recording.

### [The application plays my recording instead of the track](#local-files)

Some applications play your own music files in place of a streamed track when they match. Keep Spytify's output folder out of the folders the application scans.

### [Podcasts or short items are skipped as ads](#ads)

Items shorter than a minute without an album are taken for ads. Turn on **Record everything** in Settings > Ads > Advanced, or lower that length.

### [Keep the order of the recordings](#recording-order)

Set **File name prefix** to `counter` in Settings > Output > Advanced: each file starts with its recording number, from **Counter start value**.

### [Is FLAC better than MP3?](#quality)

FLAC and WAV avoid a second lossy pass, but streaming services already send compressed audio: a FLAC file is larger and sounds the same as the stream.

### [macOS does not let Spytify record](#macos-permissions)

Spytify needs **System Audio Recording** and **Automation**. Turn them on in System Settings > Privacy & Security, then open Spytify again. A Spytify you built yourself asks again after each build.
