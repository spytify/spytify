---
layout: page
title: Coming from v1
---

Spytify v2 is a new program, written in Go, for Linux, macOS and Windows. It keeps what v1 did and drops what v2 no longer needs.

Every v1 setting and feature, and where it is in v2:

| v1 | v2 |
| --- | --- |
| Output path | **Output folder** (`output.dir`) |
| Audio device, or a virtual audio cable | Gone: v2 records the application's own audio stream, whatever device it plays on |
| Muting other apps while recording | Gone: no other app can be recorded. **Mute the player on this computer while recording** (`player.mute_output`) silences the player for you on macOS and Linux |
| Audio quality, 128 to 320 kbps | **Bitrate** (`encode.bitrate_kbps`), for MP3 and AAC |
| Audio format, WAV or MP3 | **Output format** (`encode.format`): MP3 by default, AAC, FLAC or WAV, all tagged |
| Minimal length | Gone: whole tracks of any length are kept and incomplete ones never are. **Keep anyway** saves the last track cut short |
| Mute ads | **Mute ads** (`recorder.mute_ads`); ads are never recorded |
| Record everything (podcasts) | Gone: podcasts record like tracks. Short items with no album record with **Treat items shorter than N seconds with no album as ads** at 0 |
| Delay next recording if sound is detected | Always on: each cut is placed in the quietest spot near the change of track (**Boundary search window**) |
| Timer for the recording session | **Stop the session after** (`recorder.session_timer`) |
| Counter in front of file names | **File name prefix** set to `counter` |
| Starting position of the counter | **Next counter number** (`output.counter_next`): the counter goes on from one recording to the next; set it back to 1 to start over |
| Group artists inside their own folder | **Group by folders**: by the main artist (`artist`, `artist/album`) or by all of them (`artists`, `artists/album`) |
| Replace spaces by underscores | **Underscores instead of spaces** (`output.underscores`) |
| Record already recorded tracks: skip, number or overwrite | **Already recorded tracks** (`recorder.duplicates`): `skip`, `suffix` or `overwrite` |
| Replace the track number tag by the counter | Gone: players sort by album first, so it never kept a playlist's order. The `counter` file name prefix keeps it where files are sorted by name |
| Move the extra title to the subtitle tag | **Move parenthesised extra title to subtitle** (`tags.extra_to_subtitle`) |
| Tags from Last.fm or the web API, with keys of your own | Tags from the application itself, cover included; nothing is looked up on the web. Players do not report a genre, so v2 writes none |
| Track changes read from the window title | The application's media interface: MPRIS, AppleScript, Windows media controls |
| English, French, Dutch and German | English |
| The recorded tracks list and the console | The **Record** tab, with the track playing and the session's list, and the **Logs** tab |
| | New: the **Library** tab, the light and dark themes, the tray, `spytify-cli`, and Linux and macOS |

Your v1 recordings stay where they are. v2 names and groups its files with its own settings, so it does not take v1 files for duplicates unless their names match.
