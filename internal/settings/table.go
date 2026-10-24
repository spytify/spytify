// Package settings holds the validation table for every setting, and loads
// and saves config.toml.
package settings

// Kind is the type of a setting's value.
type Kind int

const (
	KindString Kind = iota
	KindPath
	KindBool
	KindInt
	KindFloat
	KindEnum
	KindIntEnum
	KindDuration
	KindTemplate
)

func (k Kind) String() string {
	return [...]string{"string", "path", "bool", "int", "float", "enum", "int enum", "duration", "template"}[k]
}

// Def describes one setting: where it shows, what it accepts, its default.
type Def struct {
	Key     string
	Tab     string // v1 tab the setting came from
	Group   string // card on the v2 settings screen
	Label   string
	Kind    Kind
	Default any
	Enum    []string
	Ints    []int64
	Min     float64
	Max     float64
	Ranged  bool
	Note    string
	// Advanced settings sit folded under their group's main ones.
	Advanced bool
}

// Table is the full list of settings, in the order the settings screen shows
// them. Keys, labels and defaults follow the plan's "Settings carried over
// from v1" table with the alpha patches applied: recorder.duplicates replaces
// skip_duplicates and overwrite, output.prefix replaces number_prefix, the
// ads switches are replaced by recorder.skip_short_untitled and
// recorder.mute_ads. Tags come from the player itself, so the web tag
// providers' settings are gone, and the UI is English only.
var Table = []Def{
	{Key: "app.theme", Tab: "App", Group: "App", Label: "Theme", Kind: KindEnum, Enum: []string{"system", "light", "dark"}, Default: "system"},
	{Key: "app.minimize_to_tray", Tab: "App", Group: "App", Label: "Minimize to tray", Kind: KindBool, Default: false},
	{Key: "app.autostart_recording", Tab: "App", Group: "App", Label: "Start recording on launch", Kind: KindBool, Default: false},
	{Key: "app.check_updates", Tab: "App", Group: "App", Label: "Check for updates", Kind: KindBool, Default: true},

	{Key: "output.dir", Tab: "Recorder", Group: "Output", Label: "Output folder", Kind: KindPath, Default: "~/Music/Spytify"},
	{Key: "output.group_by", Tab: "Recorder", Group: "Output", Label: "Group by folders", Kind: KindEnum, Enum: []string{"none", "artist", "artist/album"}, Default: "artist/album"},
	{Key: "output.prefix", Tab: "Recorder", Group: "Output", Label: "File name prefix", Kind: KindEnum, Enum: []string{"none", "counter", "album_track"}, Default: "none", Advanced: true},
	{Key: "output.underscores", Tab: "Recorder", Group: "Output", Label: "Add separators (_ instead of spaces)", Kind: KindBool, Default: false, Advanced: true},
	{Key: "output.template", Tab: "Recorder", Group: "Output", Label: "File name template", Kind: KindTemplate, Default: "{artist} - {title}", Note: "placeholders {artist}, {title}, {album}, {track}, {year}", Advanced: true},
	{Key: "output.counter_start", Tab: "Recorder", Group: "Output", Label: "Counter start value", Kind: KindInt, Default: int64(1), Min: 0, Max: 999999999, Ranged: true, Advanced: true},
	{Key: "output.counter_width", Tab: "Recorder", Group: "Output", Label: "Counter digits", Kind: KindInt, Default: int64(3), Min: 1, Max: 9, Ranged: true, Advanced: true},
	{Key: "output.ascii_filenames", Tab: "Recorder", Group: "Output", Label: "Remove diacritics from file names", Kind: KindBool, Default: false, Advanced: true},

	{Key: "encode.format", Tab: "Recorder", Group: "Encoding", Label: "Output format", Kind: KindEnum, Enum: []string{"mp3", "aac", "flac", "wav"}, Default: "flac"},
	{Key: "encode.bitrate_kbps", Tab: "Recorder", Group: "Encoding", Label: "Bitrate", Kind: KindIntEnum, Ints: []int64{128, 160, 256, 320}, Default: int64(160), Note: "ignored for flac and wav"},
	{Key: "tags.cover", Tab: "Tags", Group: "Encoding", Label: "Embed album cover", Kind: KindBool, Default: true, Advanced: true},
	{Key: "tags.cover_size", Tab: "Tags", Group: "Encoding", Label: "Cover size", Kind: KindEnum, Enum: []string{"small", "medium", "large"}, Default: "medium", Advanced: true},
	{Key: "tags.track_from_counter", Tab: "Tags", Group: "Encoding", Label: "Replace track number with counter", Kind: KindBool, Default: false, Advanced: true},
	{Key: "tags.extra_to_subtitle", Tab: "Tags", Group: "Encoding", Label: "Move parenthesised extra title to subtitle", Kind: KindBool, Default: false, Advanced: true},
	{Key: "tags.artist_separator", Tab: "Tags", Group: "Encoding", Label: "Multiple artists separator", Kind: KindString, Default: ", ", Advanced: true},

	{Key: "recorder.min_seconds", Tab: "Recorder", Group: "Recording", Label: "Minimum track length (seconds)", Kind: KindInt, Default: int64(30), Min: 0, Max: 3600, Ranged: true},
	{Key: "recorder.duplicates", Tab: "Recorder", Group: "Recording", Label: "Already recorded tracks", Kind: KindEnum, Enum: []string{"skip", "suffix", "overwrite"}, Default: "skip"},
	{Key: "recorder.skip_duplicates_forward", Tab: "Recorder", Group: "Recording", Label: "Skip to the next track when already recorded", Kind: KindBool, Default: false},
	{Key: "recorder.single_track", Tab: "Recorder", Group: "Recording", Label: "Record a single track then stop", Kind: KindBool, Default: false},
	{Key: "recorder.normalize", Tab: "Advanced", Group: "Recording", Label: "Normalize volume", Kind: KindBool, Default: true},
	{Key: "recorder.trim_silence", Tab: "Recorder", Group: "Recording", Label: "Trim leading/trailing silence", Kind: KindBool, Default: true, Advanced: true},
	{Key: "recorder.silence_db", Tab: "Recorder", Group: "Recording", Label: "Silence threshold (dBFS)", Kind: KindFloat, Default: -50.0, Min: -90, Max: -20, Ranged: true, Advanced: true},
	{Key: "recorder.boundary_window_ms", Tab: "Recorder", Group: "Recording", Label: "Boundary search window (ms)", Kind: KindInt, Default: int64(300), Min: 20, Max: 2000, Ranged: true, Advanced: true},
	{Key: "recorder.session_timer", Tab: "Recorder", Group: "Recording", Label: "Stop the session after (0s = never)", Kind: KindDuration, Default: "0s", Advanced: true},
	{Key: "recorder.target_lufs", Tab: "Advanced", Group: "Recording", Label: "Target loudness (LUFS)", Kind: KindFloat, Default: -14.0, Min: -30, Max: -5, Ranged: true, Advanced: true},
	{Key: "recorder.pause_on_disk_full", Tab: "Advanced", Group: "Recording", Label: "Pause player when disk is full", Kind: KindBool, Default: true, Advanced: true},

	{Key: "recorder.mute_ads", Tab: "Ads", Group: "Ads", Label: "Mute ads", Kind: KindBool, Default: false},
	{Key: "recorder.skip_short_untitled", Tab: "Advanced", Group: "Ads", Label: "Treat items shorter than N seconds with no album as ads (0 = off)", Kind: KindInt, Default: int64(60), Min: 0, Max: 600, Ranged: true, Advanced: true},
	{Key: "recorder.record_everything", Tab: "Advanced", Group: "Ads", Label: "Record everything (podcasts, short untitled items)", Kind: KindBool, Default: false, Advanced: true},
	{Key: "player.mute_output", Tab: "Advanced", Group: "Ads", Label: "Mute the player on this computer while recording (macOS, Linux)", Kind: KindBool, Default: false, Advanced: true},

	{Key: "privacy.anonymous_stats", Tab: "App", Group: "Privacy", Label: "Send anonymous stats to the developers to help fix bugs", Kind: KindBool, Default: false, Note: "counts, timings and the problems the app runs into; no IP address, names or paths"},
	{Key: "privacy.crash_reports", Tab: "App", Group: "Privacy", Label: "Crash reports", Kind: KindEnum, Enum: []string{"ask", "always", "never"}, Default: "ask", Note: "ask before sending, send without asking, or never send"},
}

var byKey = func() map[string]*Def {
	m := make(map[string]*Def, len(Table))
	for i := range Table {
		m[Table[i].Key] = &Table[i]
	}
	return m
}()

// Lookup returns the definition of a key.
func Lookup(key string) (*Def, bool) {
	d, ok := byKey[key]
	return d, ok
}
