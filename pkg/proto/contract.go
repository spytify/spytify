package proto

// MethodSpec describes one action for documentation and the JSON schema.
type MethodSpec struct {
	Name   string
	Params any
	Result any
	Doc    string
}

// EventSpec describes one event.
type EventSpec struct {
	Name    string
	Payload any
	Doc     string
}

// Methods is every action the core answers.
var Methods = []MethodSpec{
	{MethodCoreInfo, nil, CoreInfoResult{}, "versions, resolved target and capability checks; first call on connect"},
	{MethodSessionStart, SessionStartParams{}, SessionStartResult{}, "fails if the target process is not running or ffmpeg is missing"},
	{MethodSessionStop, SessionStopParams{}, SessionStopResult{}, "lets the current track finish when past recorder.min_seconds, unless now"},
	{MethodSessionStatus, nil, SessionStatusResult{}, "snapshot for late-joining clients"},
	{MethodSettingsGet, SettingsGetParams{}, SettingsValues{}, "secrets come back masked"},
	{MethodSettingsSet, SettingsSetParams{}, SettingsSetResult{}, "validated, persisted, then settings.changed fires"},
	{MethodSettingsReset, SettingsResetParams{}, Empty{}, "back to defaults, all keys or those under a prefix"},
	{MethodPlayerControl, PlayerControlParams{}, Empty{}, "play, pause, next, previous, restart (from 0:00) through the player's local interface"},
	{MethodSpotifyConnect, nil, SpotifyConnectResult{}, "starts the OAuth loopback listener on the redirect URI"},
	{MethodTagsRetag, RetagParams{}, RetagResult{}, "not available in the alpha"},
	{MethodLogTail, LogTailParams{}, LogTailResult{}, "last N log lines"},
	{MethodEventsRecent, nil, EventsRecentResult{}, "the last 200 events, for a client that connects mid-session"},
	{MethodCoreShutdown, nil, Empty{}, "stops the session first"},
}

// Events is every notification the core broadcasts.
var Events = []EventSpec{
	{EventSessionState, SessionStateEvent{}, "every transition"},
	{EventTrackStarted, TrackStartedEvent{}, "boundary committed"},
	{EventTrackFinished, TrackFinishedEvent{}, "file closed and tagged"},
	{EventTrackSkipped, TrackSkippedEvent{}, "decision made: duplicate, too_short, ad, incomplete"},
	{EventLevel, LevelEvent{}, "10 Hz while recording"},
	{EventSettingsChanged, SettingsValues{}, "after any successful settings.set or settings.reset"},
	{EventTargetState, TargetStateEvent{}, "player process appears or exits"},
	{EventLog, LogEvent{}, "each log line at or above app.log_level"},
	{EventError, ErrorEvent{}, "capture, encoder or disk failure"},
}
