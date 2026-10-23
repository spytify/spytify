---
layout: page
title: Coming from v1
---

Spytify v2 is a new program, written in Go, for Linux, macOS and Windows. It keeps what v1 did and drops what v2 no longer needs.

| v1 | v2 |
| --- | --- |
| A virtual audio cable or a chosen output device | Gone: v2 records the application's own audio stream |
| Muting other apps while recording | Gone: no other app can be recorded |
| Track changes read from the window title | The application's media interface: MPRIS, AppleScript, Windows media controls |
| Tags from web services, with keys of your own | Tags from the application itself; nothing is looked up on the web |
| Recording settings in one window | Settings by section, with the rare ones under Advanced; the same keys in `config.toml` and on the command line |
| Skip, record again or overwrite duplicates | The same three choices, in **Already recorded tracks** |
| Recording order number | **File name prefix** set to `counter` |
| Mute ads | **Mute ads**, and ads are never recorded |
| English and French | English |

Your v1 recordings stay where they are. v2 names and groups its files with its own settings, so it does not take v1 files for duplicates unless their names match.
