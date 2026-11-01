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
	{MethodSessionStart, SessionStartParams{}, SessionStartResult{}, "fails when a recording cannot start: the error says why, and core.info's checks (Logs > Diagnostics in the app) what to fix"},
	{MethodSessionStop, SessionStopParams{}, SessionStopResult{}, "lets the current track finish, unless now; a track ended by now can be saved with track.keep"},
	{MethodSessionStatus, nil, SessionStatusResult{}, "snapshot for late-joining clients"},
	{MethodSettingsGet, SettingsGetParams{}, SettingsValues{}, "every setting when keys is empty"},
	{MethodSettingsSet, SettingsSetParams{}, SettingsSetResult{}, "validated, persisted, then settings.changed fires"},
	{MethodSettingsReset, SettingsResetParams{}, Empty{}, "back to defaults, all keys or those under a prefix"},
	{MethodPlayerControl, PlayerControlParams{}, Empty{}, "play, pause, next, previous, restart (from 0:00) through the player's local interface"},
	{MethodPlayerLaunch, nil, Empty{}, "opens the player if it is not running: SPYTIFY_TARGET_LAUNCH, else the OS's way"},
	{MethodLogTail, LogTailParams{}, LogTailResult{}, "last N log lines"},
	{MethodEventsRecent, nil, EventsRecentResult{}, "the last 200 events, for a client that connects mid-session"},
	{MethodTrackKeep, TrackKeepParams{}, Empty{}, "saves a keepable take after all, as \"<title> (partial)\", or \"<title> (low quality)\" for one skipped as low_quality; fails once it is no longer keepable"},
	{MethodCoreShutdown, nil, Empty{}, "stops the session first"},
}

// Events is every notification the core broadcasts.
var Events = []EventSpec{
	{EventSessionState, SessionStateEvent{}, "every transition"},
	{EventTrackStarted, TrackStartedEvent{}, "boundary committed"},
	{EventTrackFinished, TrackFinishedEvent{}, "file closed and tagged"},
	{EventTrackSkipped, TrackSkippedEvent{}, "decision made: duplicate, too_short, ad, incomplete, seeked, low_quality, pause_fade"},
	{EventTrackKeepable, TrackKeepableEvent{}, "a take cut short, stopped, moved along its track bar, recorded in low quality or left with a pause's fades can be saved with track.keep, or no longer"},
	{EventLevel, LevelEvent{}, "10 Hz while recording"},
	{EventSettingsChanged, SettingsValues{}, "after any successful settings.set or settings.reset"},
	{EventTargetState, TargetStateEvent{}, "player process appears or exits"},
	{EventLog, LogEvent{}, "each log line, every level"},
	{EventError, ErrorEvent{}, "capture, encoder or disk failure, or a warning such as capture_low_quality; one without msg withdraws the warning of its code"},
}
