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

Changed your mind? The last track cut short shows **Keep anyway** until the next track is saved: it saves the part that played as "*title* (partial)". A track skipped as [low quality](#low-quality) can be kept the same way, as "*title* (low quality)".

### [Tags and covers](#tags)

Title, artist, album, album artist, track and disc numbers and the cover come from what the application reports, and go into the file: ID3 for MP3, Vorbis comments for FLAC, MP4 tags for AAC, and a `.json` beside each WAV.

### [Formats](#formats)

MP3 at 160 kbps by default, or AAC, FLAC and WAV, each track trimmed of its leading and trailing silence (which can be turned off). A FLAC or WAV file avoids a second lossy pass but adds no quality: streaming services send compressed audio.

**Normalize volume** is off, so a recording keeps the application's exact sound. Turned on, it applies one gain per track, toward a loudness target, and never compresses; the Logs tab says when a track stays under the target.

Spytify records at the rate the system plays the application at, so nothing is converted twice. A device running above 48 kHz is written at half its rate, which keeps everything below 22 kHz. The track card shows the stream as it arrives, such as 44.1 kHz stereo, and says when the file is written at a lower rate.

### [Ads](#ads)

Ads and short untitled items are not recorded, and **Mute ads** silences the application while one plays. On macOS and Linux, **Mute the player on this computer** keeps the whole session silent while it records.

### [Low quality](#low-quality)

When the application's output device runs below 44.1 kHz or in mono, as a Bluetooth headset does while an app uses its microphone, the tracks would sound like a phone call. Spytify says so and what to change, and skips the tracks recorded that way; **Keep anyway** still saves the last one.

### [The Record screen](#record-screen)

The track being recorded sits on its cover's colours, with a badge that says whether it is recorded, the stream's rate and channels, and its progress along the bottom. Beside **Skip**, a meter shows each channel's level, L and R, while it records. Under it, the session's list says which tracks were saved, and why the others were skipped. When nothing is recorded, one line says why, with the one button that fixes it: open the player, **Play**, **Start the recorder** or **Open diagnostics**.

Light or dark, as the system is; **Theme** in Settings > App picks one.

### [Library](#library)

Every recorded file with its cover, sorted by newest, by name, or by album under a heading per album. The search finds an artist, a title or an album, and a filter shows the complete files, the partial takes **Keep anyway** saved, or the low-quality ones, together or alone, to clean them up. Each row's buttons show the file in its folder, play it, or move it to the system's trash (the Recycle Bin on Windows), where it can be put back: Spytify never deletes a file.

### [Keyboard](#keyboard)

- **⌘R** (Ctrl+R on Linux and Windows) presses Record or Stop, **⌘1** to **⌘5** show the tabs, and **⌘S** goes to the Library's or the Settings' search. Hold ⌘ or Ctrl to see every key on screen.
- **Tab** goes through each screen in the order it reads. On the tab bar, Left and Right show the tab before or after. A list is one stop: Up and Down move between its rows, Left and Right between a row and its buttons, and Space presses the button, or plays the row.
- **Escape** closes every dialog, as its Close, Cancel or No does.

### [Without a window](#headless)

`spytifyd` records on its own. Spytify's icon is in the tray or the menu bar from launch: with **Minimize to tray** on (Settings > App), closing the window keeps Spytify recording there (on a Linux desktop with no tray, closing quits), and it notifies you when a recording stops on its own. `spytify-cli` drives it from a terminal, `spytifyd --once` records one session without a socket, and `spytify-cli service install` starts it at login.
