package lang

// Setting words a setting in Settings: its label, the note under it, and
// its values' names, in the order the settings table lists the values. The
// bitrate's are BitrateChoice for each of its bitrates.
type Setting struct {
	Label   string
	Note    string
	Choices []string
}

// SettingTexts word each setting of the settings table, by its key.
var SettingTexts = map[string]Setting{
	"app.theme":                        {Label: "Theme", Choices: []string{"System", "Light", "Dark"}},
	"app.notifications":                {Label: "Notifications", Note: "Only while the window is hidden in the tray.", Choices: []string{"Problems only", "Every saved track", "Off"}},
	"app.minimize_to_tray":             {Label: "Minimize to tray", Note: "Closing the window keeps Spytify running in the tray."},
	"app.autostart_recording":          {Label: "Start recording on launch"},
	"app.check_updates":                {Label: CheckForUpdates},
	"output.dir":                       {Label: "Output folder"},
	"output.group_by":                  {Label: "Group by folders", Note: "Main artist: the album's artist, so collaborations share one folder.", Choices: []string{"No folders", "Main artist", "Main artist / Album", "All artists", "All artists / Album"}},
	"output.playlist":                  {Label: "Write a playlist for each session", Note: "An .m3u8 in Playlists, in play order, with the tracks already recorded too."},
	"output.template":                  {Label: "File name template", Note: "Placeholders: {album_artist}, {artists}, {title}, {album}, {track}"},
	"output.underscores":               {Label: "Underscores instead of spaces"},
	"output.ascii_filenames":           {Label: "Remove diacritics from file names"},
	"output.prefix":                    {Label: "File name prefix", Choices: []string{"None", "Counter (001)", "Album track number (04)"}},
	"output.counter_next":              {Label: "Next counter number", Note: "It carries on from one recording to the next: set it back to 1 for a new CD."},
	"output.counter_width":             {Label: "Counter digits", Note: "3 makes 001. Past 999, the recording stops."},
	"encode.format":                    {Label: "Output format", Choices: []string{"MP3", "AAC", "FLAC", "WAV"}},
	"encode.bitrate_kbps":              {Label: "Bitrate", Note: "Not used by FLAC and WAV."},
	"tags.cover":                       {Label: "Embed album cover"},
	"tags.cover_size":                  {Label: "Cover size", Note: "A cover is shrunk to fit, never enlarged.", Choices: []string{"Small (300 px)", "Medium (640 px)", "Large (as it comes)"}},
	"tags.extra_to_subtitle":           {Label: "Move parenthesised extra title to subtitle", Note: `Moves "Live" from "Song (Live)" or "Song - Live" to the subtitle tag (MP3, FLAC).`},
	"tags.artist_separator":            {Label: "Multiple artists separator"},
	"recorder.duplicates":              {Label: "Already recorded tracks", Choices: []string{"Don't save again", "Save a copy", "Replace the file"}},
	"recorder.skip_duplicates_forward": {Label: "Skip to the player's next track when already recorded"},
	"recorder.stop_at_recorded":        {Label: "Stop the session at the first track already recorded", Note: "For a playlist that gets its new tracks at the top, played in order: the tracks after it are already recorded. Not for shuffle, a radio or a mix."},
	"recorder.single_track":            {Label: "Record a single track then stop"},
	"recorder.stop_with_player":        {Label: "Stop the session when the player stops or quits", Note: "A pause does not stop it, even when the player pauses at the end of a playlist."},
	"recorder.normalize":               {Label: "Normalize volume", Note: "One gain per track, never compressed. Off keeps the exact sound."},
	"recorder.trim_silence":            {Label: "Trim leading/trailing silence"},
	"recorder.silence_db":              {Label: "Silence threshold (dBFS)"},
	"recorder.boundary_window_ms":      {Label: "Boundary search window (ms)"},
	"recorder.pause_fade_db":           {Label: "Pause fade threshold (dB, 0 = off)", Note: "A paused take is marked (pause fade) when the cut is this far under the music."},
	"recorder.session_timer":           {Label: "Stop the session after (0s = never)"},
	"recorder.target_lufs":             {Label: "Target loudness (LUFS)"},
	"recorder.pause_on_disk_full":      {Label: "Pause player when disk is full"},
	"player.mute_output":               {Label: "Mute the player on this computer while recording (macOS, Linux)", Note: "The files are recorded as usual; only the speakers stay silent."},
	"recorder.mute_ads":                {Label: "Mute ads"},
	"recorder.skip_short_untitled":     {Label: "Treat items shorter than N seconds with no album as ads (0 = off)", Note: "Podcast episodes are always recorded; 0 records short clips too."},
	"privacy.bug_tracker":              {Label: TrackerSwitch},
}

// SettingGroups title the cards of Settings, by the group the settings table puts
// each setting in.
var SettingGroups = map[string]string{
	"app":       "App",
	"output":    "Output",
	"encoding":  "Encoding",
	"recording": Recording,
	"ads":       "Ads",
	"privacy":   "Privacy",
}
