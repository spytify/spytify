// Package proto is the messaging contract between the spytify core daemon and
// its clients. Actions are JSON-RPC 2.0 requests sent by a client; events are
// JSON-RPC notifications the core broadcasts to every connected client. The
// wire framing is one JSON object per line.
package proto

//go:generate go run ../../internal/tools/genschema -o schema.json

import "encoding/json"

// Protocol is bumped whenever a method, event or payload changes shape.
const Protocol = 1

// Version is the core version, overridden at build time with
// -ldflags "-X github.com/spytify/spytify/pkg/proto.Version=v2.0.0-alpha.1"
// (scripts/version.sh). Without it, a build is a development one.
var Version = "v0.0.0-dev"

// Actions (client to core).
const (
	MethodCoreInfo      = "core.info"
	MethodCoreShutdown  = "core.shutdown"
	MethodSessionStart  = "session.start"
	MethodSessionStop   = "session.stop"
	MethodSessionStatus = "session.status"
	MethodSettingsGet   = "settings.get"
	MethodSettingsSet   = "settings.set"
	MethodSettingsReset = "settings.reset"
	MethodPlayerControl = "player.control"
	MethodPlayerLaunch  = "player.launch"
	MethodLogTail       = "log.tail"
	MethodEventsRecent  = "events.recent"
	MethodTrackKeep     = "track.keep"
)

// Events (core to clients).
const (
	EventSessionState    = "session.state"
	EventTrackStarted    = "track.started"
	EventTrackFinished   = "track.finished"
	EventTrackSkipped    = "track.skipped"
	EventTrackKeepable   = "track.keepable"
	EventLevel           = "level"
	EventSettingsChanged = "settings.changed"
	EventTargetState     = "target.state"
	EventLog             = "log"
	EventError           = "error"
)

// SessionState is the state of the recording session.
type SessionState string

const (
	StateIdle      SessionState = "idle"
	StateWaiting   SessionState = "waiting"
	StateRecording SessionState = "recording"
	StateAd        SessionState = "ad"
	StatePaused    SessionState = "paused"
	StateStopping  SessionState = "stopping"
)

// SkipReason says why a track was not saved.
type SkipReason string

const (
	SkipDuplicate  SkipReason = "duplicate"
	SkipTooShort   SkipReason = "too_short"
	SkipAd         SkipReason = "ad"
	SkipIncomplete SkipReason = "incomplete"
	// SkipSeeked is a track whose position jumped while it played: the
	// audio no longer matches the published track, whatever its length.
	SkipSeeked SkipReason = "seeked"
	// SkipLowQuality is a track recorded in a format too poor for music,
	// under 44.1 kHz or in mono, as from a Bluetooth headset in a call. It
	// can still be saved with track.keep.
	SkipLowQuality SkipReason = "low_quality"
	// SkipSilent is a track whose take held only silence, as when the
	// player is muted in its own volume: there is nothing to keep.
	SkipSilent SkipReason = "silent"
)

// PlayerCmd is a player transport command, for player.control.
type PlayerCmd string

const (
	CmdPlay     PlayerCmd = "play"
	CmdPause    PlayerCmd = "pause"
	CmdNext     PlayerCmd = "next"
	CmdPrevious PlayerCmd = "previous"
	// CmdRestart plays the current track again from its beginning, so a
	// session started in the middle of it can record it whole.
	CmdRestart PlayerCmd = "restart"
)

// TargetInfo is the resolved SPYTIFY_TARGET_* configuration.
type TargetInfo struct {
	Process  string `json:"process"`
	MPRIS    string `json:"mpris,omitempty"`
	BundleID string `json:"bundle_id,omitempty"`
	Launch   string `json:"launch,omitempty"`
}

// Check is one line of the capability probe shown before Record is enabled.
// An Advice check that fails only lowers quality: recording still works.
//
// The window words a check from its ID and Args, the facts its words take;
// a Detail that holds only a fact, as a path or an error, it shows as it
// comes. Name and Detail say it all in English, for the bug tracker and for
// a window older than the IDs.
type Check struct {
	ID     CheckID  `json:"id,omitempty"`
	Name   string   `json:"name"`
	OK     bool     `json:"ok"`
	Detail string   `json:"detail,omitempty"`
	Args   []string `json:"args,omitempty"`
	Advice bool     `json:"advice,omitempty"`
	// Facts say what the check found, without a name or a path, for the
	// bug tracker: where the output folder is, say.
	Facts map[string]string `json:"facts,omitempty"`
}

// CheckID names a check. The comment of each says what its Args hold, or
// what its Detail is when it has none.
type CheckID string

const (
	// CheckTarget is the player configured: its process names.
	CheckTarget CheckID = "target"
	// CheckPlayerRunning is the player running: its pid when it runs.
	CheckPlayerRunning CheckID = "player_running"
	// CheckFFmpeg is ffmpeg found; Detail: its version, or why not.
	CheckFFmpeg CheckID = "ffmpeg"
	// CheckEncoder is ffmpeg having encode.format's encoder: the encoder,
	// then the format.
	CheckEncoder CheckID = "encoder"
	// CheckPlayerHeard is the capture not hearing only silence while the
	// player played: the player muted in its own volume or in the system's
	// mixer, or, on macOS, a refused System Audio Recording permission.
	CheckPlayerHeard CheckID = "player_heard"
	// CheckOutputWritable is the output folder written to; Detail: the
	// folder, or why not. Without it, no recording starts (CodeOutputDir).
	CheckOutputWritable CheckID = "output_writable"
	// CheckMacOS is macOS 14.2 or later: the version.
	CheckMacOS CheckID = "macos"
	// CheckWindows is Windows 10 2004 or later: the build.
	CheckWindows CheckID = "windows"
	// CheckHelper is Windows' capture helper found: its path; Detail: why
	// not.
	CheckHelper CheckID = "capture_helper"
	// CheckTapHelper is macOS' capture helper found: its path; Detail: why
	// not.
	CheckTapHelper CheckID = "tap_helper"
	// CheckAppleScript is osascript found; Detail: why not.
	CheckAppleScript CheckID = "applescript"
	// CheckPowerShell is Windows PowerShell found: its path; Detail: why
	// not.
	CheckPowerShell CheckID = "powershell"
	// CheckCapture is a Linux capture found: pipewire or pulseaudio.
	CheckCapture CheckID = "capture"
	// CheckNoCapture is a system with no capture: the system.
	CheckNoCapture CheckID = "no_capture"
	// CheckSessionBus is the D-Bus session bus reached; Detail: why not.
	CheckSessionBus CheckID = "session_bus"
	// CheckSampleRates is PipeWire allowing 44.1 kHz, an advice: the
	// allowed rates, none when they are not known.
	CheckSampleRates CheckID = "sample_rates"
	// CheckFakePlayer is spytifyd --fake's player; Detail: its timeline.
	CheckFakePlayer CheckID = "fake_player"
)

type CoreInfoResult struct {
	Version  string     `json:"version"`
	Protocol int        `json:"protocol"`
	Target   TargetInfo `json:"target"`
	Platform string     `json:"platform"`
	Checks   []Check    `json:"checks"`
	Warnings []string   `json:"warnings,omitempty"`
}

type SessionStartParams struct {
	SingleTrack *bool `json:"single_track,omitempty"`
}

type SessionStartResult struct {
	SessionID string `json:"session_id"`
}

type SessionStopParams struct {
	// Now ends the current track where it is instead of letting it finish;
	// it is not saved, unless with track.keep.
	Now bool `json:"now,omitempty"`
}

type SessionStopResult struct {
	Recorded int  `json:"recorded"`
	Skipped  int  `json:"skipped"`
	Stopping bool `json:"stopping,omitempty"`
}

type Counters struct {
	Recorded int   `json:"recorded"`
	Skipped  int   `json:"skipped"`
	Ads      int   `json:"ads"`
	Bytes    int64 `json:"bytes"`
	Overruns int64 `json:"overruns"`
}

// Track is the metadata of a track as the session sees it.
type Track struct {
	ID       string `json:"id"`
	Artist   string `json:"artist"`
	Title    string `json:"title"`
	Album    string `json:"album,omitempty"`
	LengthMs int64  `json:"length_ms"`
	CoverURL string `json:"cover_url,omitempty"`
}

type SessionStatusResult struct {
	State     SessionState `json:"state"`
	SessionID string       `json:"session_id,omitempty"`
	Track     *Track       `json:"track,omitempty"`
	Counters  Counters     `json:"counters"`
}

type SettingsGetParams struct {
	Keys []string `json:"keys,omitempty"`
}

type SettingsValues struct {
	Values map[string]any `json:"values"`
}

type SettingsSetParams struct {
	Values map[string]any `json:"values"`
}

type SettingsSetResult struct {
	Applied  []string          `json:"applied"`
	Rejected map[string]string `json:"rejected"`
}

type SettingsResetParams struct {
	// Prefix limits the reset to keys under it, such as "output" or "recorder".
	Prefix string `json:"prefix,omitempty"`
}

type PlayerControlParams struct {
	Cmd PlayerCmd `json:"cmd"`
}

type LogTailParams struct {
	Lines int `json:"lines"`
}

type LogTailResult struct {
	Lines []LogEvent `json:"lines"`
}

type EventsRecentResult struct {
	Events []Event `json:"events"`
}

type Empty struct{}

// Event payloads.

type SessionStateEvent struct {
	State SessionState `json:"state"`
	// Playlist is, as the session ends, the .m3u8 written for it
	// (output.playlist), or the one its tracks still being saved go to.
	Playlist string `json:"playlist,omitempty"`
}

type TrackStartedEvent struct {
	Track
	// Source is the audio as the player delivers it.
	Source *AudioSource `json:"source,omitempty"`
	// Cut is how the recorder found where the track starts.
	Cut *TrackCut `json:"cut,omitempty"`
}

// TrackCut is how the recorder placed a track's start.
type TrackCut struct {
	// Kind is first (the session's first track), boundary (the track
	// before ended) or interrupt (it was cut short).
	Kind string `json:"kind"`
	// By is what placed the cut: silence, dip (two tracks faded into each
	// other), edge (a skip, mid-note), length (gapless, from the track
	// before's declared length) or notice (where the player said the track
	// changed, nothing better found); for a first track, first_sound or
	// notice.
	By string `json:"by"`
	// OffsetMs is how far the cut is from where the player said the track
	// changed: negative before it.
	OffsetMs int64 `json:"offset_ms"`
}

// AudioSource is the captured stream's format.
type AudioSource struct {
	SampleRate int `json:"sample_rate"`
	Channels   int `json:"channels"`
}

type TrackFinishedEvent struct {
	ID string `json:"id"`
	// Counter is the number this file got with output.prefix counter, in
	// its name; the next file gets one more (output.counter_next).
	Counter    int64  `json:"counter,omitempty"`
	Path       string `json:"path"`
	DurationMs int64  `json:"duration_ms"`
	Bytes      int64  `json:"bytes"`
	Tagged     bool   `json:"tagged"`
	TaggedBy   string `json:"tagged_by,omitempty"`
	// Timings is how long saving the file took, step by step.
	Timings *FinishTimings `json:"timings,omitempty"`
	// Partial is a take saved with track.keep: only the part that played,
	// or all of one recorded in low quality.
	Partial bool `json:"partial,omitempty"`
	// PauseFade is a take paused mid-track whose player faded out before
	// the pause and in after it: the pause is cut out, but the two fades
	// meet where it was, a dip that is heard. Its name ends in
	// " (pause fade)".
	PauseFade bool `json:"pause_fade,omitempty"`
}

// FinishTimings are the steps between the end of a track and its file.
type FinishTimings struct {
	Format   string `json:"format"`
	TrimMs   int64  `json:"trim_ms"`
	CoverMs  int64  `json:"cover_ms"`
	EncodeMs int64  `json:"encode_ms"`
	TagMs    int64  `json:"tag_ms"`
	TotalMs  int64  `json:"total_ms"`
}

type TrackSkippedEvent struct {
	ID     string     `json:"id"`
	Reason SkipReason `json:"reason"`
	// Path is the file a track skipped as already recorded has. While
	// that file is being saved by a take of this session, Of is that
	// take's ID instead, and its track.finished names the file.
	Path string `json:"path,omitempty"`
	Of   string `json:"of,omitempty"`
}

// TrackKeepableEvent says whether a skipped take can still be saved with
// track.keep. Only the last take cut short, stopped, moved along its track
// bar or recorded in low quality can: its audio is let go once another
// track is saved.
type TrackKeepableEvent struct {
	ID       string `json:"id"`
	Keepable bool   `json:"keepable"`
}

type TrackKeepParams struct {
	// ID is the take to save, from track.keepable; empty saves the one
	// there is.
	ID string `json:"id,omitempty"`
}

// LevelEvent is the level since the last one: over every channel, then
// each channel's in the stream's order, left then right for stereo.
type LevelEvent struct {
	PeakDBFS float64        `json:"peak_dbfs"`
	RMSDBFS  float64        `json:"rms_dbfs"`
	Channels []ChannelLevel `json:"channels,omitempty"`
}

type ChannelLevel struct {
	PeakDBFS float64 `json:"peak_dbfs"`
	RMSDBFS  float64 `json:"rms_dbfs"`
}

type TargetStateEvent struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// LogLevel is a log line's level.
type LogLevel string

const (
	LogError LogLevel = "error"
	LogInfo  LogLevel = "info"
	LogDebug LogLevel = "debug"
)

type LogEvent struct {
	Level LogLevel `json:"level"`
	Msg   string   `json:"msg"`
	TS    string   `json:"ts"`
}

// ErrorCode names a failure or a warning.
type ErrorCode string

const (
	// ErrorOutputDir is an output folder that cannot be written to, or
	// that went away during a session, which stops it.
	ErrorOutputDir    ErrorCode = "output_dir"
	ErrorDiskFull     ErrorCode = "disk_full"
	ErrorTempFile     ErrorCode = "temp_file"
	ErrorWrite        ErrorCode = "write"
	ErrorRingOverflow ErrorCode = "ring_overflow"
	// ErrorCaptureStalled is no audio from the capture while the player
	// plays.
	ErrorCaptureStalled ErrorCode = "capture_stalled"
	// ErrorCaptureSilent is only silence from the capture while the
	// player plays. Sent again with no Msg, it withdraws the warning, as
	// ErrorCaptureLowQuality does.
	ErrorCaptureSilent     ErrorCode = "capture_silent"
	ErrorCaptureLowQuality ErrorCode = "capture_low_quality"
	// A track's file failed on its way to the output folder.
	ErrorRead        ErrorCode = "read"
	ErrorEncode      ErrorCode = "encode"
	ErrorRename      ErrorCode = "rename"
	ErrorCounterFull ErrorCode = "counter_full"
)

// ErrorEvent is a failure or a warning, by its code. Msg is what the
// recorder found, in English, for the log and the bug tracker; the window
// words its known codes itself, from the code and the facts that follow.
type ErrorEvent struct {
	Code  ErrorCode `json:"code"`
	Msg   string    `json:"msg"`
	Fatal bool      `json:"fatal"`
	// Source is the capture's format, for capture_low_quality.
	Source *AudioSource `json:"source,omitempty"`
	// FreeMB is the space left in the output folder, in millions of bytes,
	// for disk_full.
	FreeMB *int64 `json:"free_mb,omitempty"`
}

// Event is a decoded notification: its method name and raw payload.
type Event struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Decode unmarshals the payload into v.
func (e Event) Decode(v any) error {
	return json.Unmarshal(e.Params, v)
}
