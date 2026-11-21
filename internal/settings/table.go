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
// The window's lexicon words it, by its key (lang.SettingTexts): its label,
// the note under it, and its values' names.
type Def struct {
	Key string
	Tab string // v1 tab the setting came from
	// Group is the card the setting sits in on the settings screen, which
	// lang.SettingGroups titles.
	Group   string
	Kind    Kind
	Default any
	Enum    []string
	Ints    []int64
	Min     float64
	Max     float64
	Ranged  bool
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
	{Key: "app.theme", Tab: "App", Group: "app", Kind: KindEnum, Enum: []string{"system", "light", "dark"}, Default: "system"},
	{Key: "app.notifications", Tab: "App", Group: "app", Kind: KindEnum, Enum: []string{"problems", "tracks", "off"}, Default: "problems"},
	{Key: "app.minimize_to_tray", Tab: "App", Group: "app", Kind: KindBool, Default: false},
	{Key: "app.autostart_recording", Tab: "App", Group: "app", Kind: KindBool, Default: false},
	{Key: "app.check_updates", Tab: "App", Group: "app", Kind: KindBool, Default: true},

	{Key: "output.dir", Tab: "Recorder", Group: "output", Kind: KindPath, Default: "~/Spytify"},
	{Key: "output.group_by", Tab: "Recorder", Group: "output", Kind: KindEnum, Enum: []string{"none", "artist", "artist/album", "artists", "artists/album"}, Default: "artist/album"},
	{Key: "output.playlist", Tab: "Recorder", Group: "output", Kind: KindBool, Default: false},
	{Key: "output.template", Tab: "Recorder", Group: "output", Kind: KindTemplate, Default: "{album_artist} - {title}", Advanced: true},
	{Key: "output.underscores", Tab: "Recorder", Group: "output", Kind: KindBool, Default: false, Advanced: true},
	{Key: "output.ascii_filenames", Tab: "Recorder", Group: "output", Kind: KindBool, Default: false, Advanced: true},
	{Key: "output.prefix", Tab: "Recorder", Group: "output", Kind: KindEnum, Enum: []string{"none", "counter", "album_track"}, Default: "none", Advanced: true},
	{Key: "output.counter_next", Tab: "Recorder", Group: "output", Kind: KindInt, Default: int64(1), Min: 0, Max: 999999999, Ranged: true, Advanced: true},
	{Key: "output.counter_width", Tab: "Recorder", Group: "output", Kind: KindInt, Default: int64(3), Min: 1, Max: 9, Ranged: true, Advanced: true},

	{Key: "encode.format", Tab: "Recorder", Group: "encoding", Kind: KindEnum, Enum: []string{"mp3", "aac", "flac", "wav"}, Default: "mp3"},
	{Key: "encode.bitrate_kbps", Tab: "Recorder", Group: "encoding", Kind: KindIntEnum, Ints: bitrates, Default: int64(160)},
	{Key: "tags.cover", Tab: "Tags", Group: "encoding", Kind: KindBool, Default: true, Advanced: true},
	{Key: "tags.cover_size", Tab: "Tags", Group: "encoding", Kind: KindEnum, Enum: []string{"small", "medium", "large"}, Default: "medium", Advanced: true},
	{Key: "tags.extra_to_subtitle", Tab: "Tags", Group: "encoding", Kind: KindBool, Default: false, Advanced: true},
	{Key: "tags.artist_separator", Tab: "Tags", Group: "encoding", Kind: KindString, Default: ", ", Advanced: true},

	{Key: "recorder.duplicates", Tab: "Recorder", Group: "recording", Kind: KindEnum, Enum: []string{"skip", "suffix", "overwrite"}, Default: "skip"},
	{Key: "recorder.skip_duplicates_forward", Tab: "Recorder", Group: "recording", Kind: KindBool, Default: false},
	{Key: "recorder.stop_at_recorded", Tab: "Recorder", Group: "recording", Kind: KindBool, Default: false},
	{Key: "recorder.single_track", Tab: "Recorder", Group: "recording", Kind: KindBool, Default: false},
	{Key: "recorder.stop_with_player", Tab: "Recorder", Group: "recording", Kind: KindBool, Default: false},
	{Key: "recorder.normalize", Tab: "Advanced", Group: "recording", Kind: KindBool, Default: false},
	{Key: "recorder.trim_silence", Tab: "Recorder", Group: "recording", Kind: KindBool, Default: true, Advanced: true},
	{Key: "recorder.silence_db", Tab: "Recorder", Group: "recording", Kind: KindFloat, Default: -50.0, Min: -90, Max: -20, Ranged: true, Advanced: true},
	{Key: "recorder.boundary_window_ms", Tab: "Recorder", Group: "recording", Kind: KindInt, Default: int64(300), Min: 20, Max: 2000, Ranged: true, Advanced: true},
	{Key: "recorder.pause_fade_db", Tab: "Advanced", Group: "recording", Kind: KindFloat, Default: 20.0, Min: 0, Max: 60, Ranged: true, Advanced: true},
	{Key: "recorder.session_timer", Tab: "Recorder", Group: "recording", Kind: KindDuration, Default: "0s", Advanced: true},
	{Key: "recorder.target_lufs", Tab: "Advanced", Group: "recording", Kind: KindFloat, Default: -14.0, Min: -30, Max: -5, Ranged: true, Advanced: true},
	{Key: "recorder.pause_on_disk_full", Tab: "Advanced", Group: "recording", Kind: KindBool, Default: true, Advanced: true},
	{Key: "player.mute_output", Tab: "Advanced", Group: "recording", Kind: KindBool, Default: false, Advanced: true},

	{Key: "recorder.mute_ads", Tab: "Ads", Group: "ads", Kind: KindBool, Default: false},
	{Key: "recorder.skip_short_untitled", Tab: "Advanced", Group: "ads", Kind: KindInt, Default: int64(60), Min: 0, Max: 600, Ranged: true, Advanced: true},

	{Key: "privacy.bug_tracker", Tab: "App", Group: "privacy", Kind: KindBool, Default: true},
}

// bitrates are the MP3 and AAC bitrates, in kbps.
var bitrates = []int64{128, 160, 256, 320}

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
