// Package settings holds the validation table for every setting, loads and
// saves config.toml, and keeps secrets in the OS keyring.
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
	KindSecret
	KindTemplate
	KindLanguage
)

func (k Kind) String() string {
	return [...]string{"string", "path", "bool", "int", "float", "enum", "int enum", "duration", "secret", "template", "language"}[k]
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
}

// Table is the full list of settings. Keys, labels and defaults follow the
// plan's "Settings carried over from v1" table with the alpha patches applied:
// recorder.duplicates replaces skip_duplicates and overwrite, output.prefix
// replaces number_prefix, and the ads switches are replaced by the generic
// recorder.skip_short_untitled rule.
var Table = []Def{
	{Key: "output.dir", Tab: "Recorder", Group: "Output", Label: "Output folder", Kind: KindPath, Default: "~/Music/Spytify"},
	{Key: "output.group_by", Tab: "Recorder", Group: "Output", Label: "Group by folders", Kind: KindEnum, Enum: []string{"none", "artist", "artist/album"}, Default: "artist/album"},
	{Key: "output.prefix", Tab: "Recorder", Group: "Output", Label: "File name prefix", Kind: KindEnum, Enum: []string{"none", "counter", "album_track"}, Default: "none"},
	{Key: "output.underscores", Tab: "Recorder", Group: "Output", Label: "Add separators (_ instead of spaces)", Kind: KindBool, Default: false},
	{Key: "output.template", Tab: "Recorder", Group: "Output", Label: "File name template", Kind: KindTemplate, Default: "{artist} - {title}", Note: "placeholders {artist}, {title}, {album}, {track}, {year}"},
	{Key: "output.counter_start", Tab: "Recorder", Group: "Output", Label: "Counter start value", Kind: KindInt, Default: int64(1), Min: 0, Max: 999999999, Ranged: true},
	{Key: "output.counter_width", Tab: "Recorder", Group: "Output", Label: "Counter digits", Kind: KindInt, Default: int64(3), Min: 1, Max: 9, Ranged: true},
	{Key: "output.ascii_filenames", Tab: "Recorder", Group: "Output", Label: "Remove diacritics from file names", Kind: KindBool, Default: false},

	{Key: "encode.format", Tab: "Recorder", Group: "Encoding", Label: "Output format", Kind: KindEnum, Enum: []string{"mp3", "aac", "flac", "wav"}, Default: "mp3"},
	{Key: "encode.bitrate_kbps", Tab: "Recorder", Group: "Encoding", Label: "Bitrate", Kind: KindIntEnum, Ints: []int64{128, 160, 256, 320}, Default: int64(160), Note: "ignored for flac and wav"},

	{Key: "recorder.min_seconds", Tab: "Recorder", Group: "Recording", Label: "Minimum track length (seconds)", Kind: KindInt, Default: int64(30), Min: 0, Max: 3600, Ranged: true},
	{Key: "recorder.duplicates", Tab: "Recorder", Group: "Recording", Label: "Already recorded tracks", Kind: KindEnum, Enum: []string{"skip", "suffix", "overwrite"}, Default: "skip"},
	{Key: "recorder.skip_duplicates_forward", Tab: "Recorder", Group: "Recording", Label: "Skip to the next track when already recorded", Kind: KindBool, Default: false},
	{Key: "recorder.single_track", Tab: "Recorder", Group: "Recording", Label: "Record a single track then stop", Kind: KindBool, Default: false},
	{Key: "recorder.trim_silence", Tab: "Recorder", Group: "Recording", Label: "Trim leading/trailing silence", Kind: KindBool, Default: true},
	{Key: "recorder.silence_db", Tab: "Recorder", Group: "Recording", Label: "Silence threshold (dBFS)", Kind: KindFloat, Default: -50.0, Min: -90, Max: -20, Ranged: true},
	{Key: "recorder.boundary_window_ms", Tab: "Recorder", Group: "Recording", Label: "Boundary search window (ms)", Kind: KindInt, Default: int64(300), Min: 20, Max: 2000, Ranged: true},
	{Key: "recorder.session_timer", Tab: "Recorder", Group: "Recording", Label: "Session timer (stop after)", Kind: KindDuration, Default: "0s", Note: "0 = off"},
	{Key: "recorder.normalize", Tab: "Advanced", Group: "Recording", Label: "Normalize volume", Kind: KindBool, Default: true},
	{Key: "recorder.target_lufs", Tab: "Advanced", Group: "Recording", Label: "Target loudness (LUFS)", Kind: KindFloat, Default: -14.0, Min: -30, Max: -5, Ranged: true},
	{Key: "recorder.pause_on_disk_full", Tab: "Advanced", Group: "Recording", Label: "Pause player when disk is full", Kind: KindBool, Default: true},

	{Key: "recorder.skip_short_untitled", Tab: "Advanced", Group: "Ads and player", Label: "Skip items shorter than N seconds with no album", Kind: KindInt, Default: int64(60), Min: 0, Max: 600, Ranged: true, Note: "0 = off"},
	{Key: "recorder.record_everything", Tab: "Advanced", Group: "Ads and player", Label: "Record everything (podcasts, short untitled items)", Kind: KindBool, Default: false},
	{Key: "player.mute_output", Tab: "Advanced", Group: "Ads and player", Label: "Mute target player output (listen elsewhere)", Kind: KindBool, Default: false},

	{Key: "tags.source", Tab: "Tags", Group: "Tags", Label: "Media tags source", Kind: KindEnum, Enum: []string{"lastfm", "spotify_api", "none"}, Default: "lastfm"},
	{Key: "tags.spotify.client_id", Tab: "Tags", Group: "Tags", Label: "Spotify client id", Kind: KindString, Default: ""},
	{Key: "tags.spotify.client_secret", Tab: "Tags", Group: "Tags", Label: "Spotify client secret", Kind: KindSecret, Default: ""},
	{Key: "tags.spotify.redirect_uri", Tab: "Tags", Group: "Tags", Label: "Spotify redirect URI", Kind: KindString, Default: "http://127.0.0.1:7812/callback"},
	{Key: "tags.lastfm.api_key", Tab: "Tags", Group: "Tags", Label: "Last.fm API key", Kind: KindSecret, Default: "", Note: "built-in app key unless set"},
	{Key: "tags.lastfm.secret", Tab: "Tags", Group: "Tags", Label: "Last.fm shared secret", Kind: KindSecret, Default: "", Note: "only needed for signed calls"},
	{Key: "tags.track_from_counter", Tab: "Tags", Group: "Tags", Label: "Replace track number with counter", Kind: KindBool, Default: false},
	{Key: "tags.extra_to_subtitle", Tab: "Tags", Group: "Tags", Label: "Move parenthesised extra title to subtitle", Kind: KindBool, Default: false},
	{Key: "tags.cover", Tab: "Tags", Group: "Tags", Label: "Embed album cover", Kind: KindBool, Default: true},
	{Key: "tags.cover_size", Tab: "Tags", Group: "Tags", Label: "Cover size", Kind: KindEnum, Enum: []string{"small", "medium", "large"}, Default: "medium"},
	{Key: "tags.update_existing", Tab: "Tags", Group: "Tags", Label: "Re-tag existing files", Kind: KindBool, Default: false},
	{Key: "tags.artist_separator", Tab: "Tags", Group: "Tags", Label: "Multiple artists separator", Kind: KindString, Default: ", "},

	{Key: "app.language", Tab: "App", Group: "App", Label: "Language", Kind: KindLanguage, Default: "", Note: "empty = system"},
	{Key: "app.minimize_to_tray", Tab: "App", Group: "App", Label: "Minimize to tray", Kind: KindBool, Default: true},
	{Key: "app.autostart_recording", Tab: "App", Group: "App", Label: "Start recording on launch", Kind: KindBool, Default: false},
	{Key: "app.check_updates", Tab: "App", Group: "App", Label: "Check for updates", Kind: KindBool, Default: true},
	{Key: "app.log_level", Tab: "App", Group: "App", Label: "Log level", Kind: KindEnum, Enum: []string{"error", "info", "debug"}, Default: "info"},
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
