---
layout: page
title: Features
---

### [One application, nothing else](#one-application)

Spytify records the audio of the application you choose, by its process, and of no other. A notification, a video in a browser or a call never ends up in a recording, and no virtual cable or output device has to be set up.

### [A file per track, cut at the sample](#a-file-per-track)

The cut between two tracks is placed in the audio, at the quietest moment around the track's expected end. Gapless albums are cut at the track's length. A pause is cut out at the sample, so it leaves neither a hole nor a click.

### [Whole tracks only](#whole-tracks)

A file is a whole track or nothing: a track that was skipped, stopped early or moved along its track bar is not kept, and a track already in the folder is skipped (or saved with a number, or overwritten, as you choose). **Skip to the next track when already recorded** moves the player on by itself. A whole track is kept whatever its length, intros and interludes included.

Changed your mind? The last track cut short shows **Keep anyway** until the next track is saved: it saves the part that played as "*title* (partial)".

### [Tags and covers](#tags)

Title, artist, album, album artist, track and disc numbers and the cover come from what the application reports, and go into the file: ID3 for MP3, Vorbis comments for FLAC, MP4 tags for AAC, and a `.json` beside each WAV.

### [Formats](#formats)

FLAC by default, or MP3, AAC and WAV, each track trimmed of its leading and trailing silence and loudness-normalised (both can be turned off). A FLAC or WAV file avoids a second lossy pass but adds no quality: streaming services send compressed audio. The track card shows the stream as it arrives, such as 44.1 kHz stereo.

### [Ads](#ads)

Ads and short untitled items are not recorded, and **Mute ads** silences the application while one plays. On macOS and Linux, **Mute the player on this computer** keeps the whole session silent while it records.

### [Library](#library)

Every recorded file, newest first, with its cover, a search box, Reveal and Play.

### [Without a window](#headless)

`spytifyd` records on its own: with **Minimize to tray** on (Settings > App), closing the window keeps Spytify recording in the tray or the menu bar, `spytify-cli` drives it from a terminal, `spytifyd --once` records one session without a socket, and `spytify-cli service install` starts it at login.
