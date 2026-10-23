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
// -ldflags "-X github.com/spytify/spytify/pkg/proto.Version=v2.0.0-alpha.1".
var Version = "v2.0.0-alpha.dev"

// Actions (client to core).
const (
	MethodCoreInfo       = "core.info"
	MethodCoreShutdown   = "core.shutdown"
	MethodSessionStart   = "session.start"
	MethodSessionStop    = "session.stop"
	MethodSessionStatus  = "session.status"
	MethodSettingsGet    = "settings.get"
	MethodSettingsSet    = "settings.set"
	MethodSettingsReset  = "settings.reset"
	MethodPlayerControl  = "player.control"
	MethodSpotifyConnect = "tags.spotify.connect"
	MethodTagsRetag      = "tags.retag"
	MethodLogTail        = "log.tail"
	MethodEventsRecent   = "events.recent"
)

// Events (core to clients).
const (
	EventSessionState    = "session.state"
	EventTrackStarted    = "track.started"
	EventTrackFinished   = "track.finished"
	EventTrackSkipped    = "track.skipped"
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
)

// Player transport commands accepted by player.control.
const (
	CmdPlay     = "play"
	CmdPause    = "pause"
	CmdNext     = "next"
	CmdPrevious = "previous"
	// CmdRestart plays the current track again from its beginning, so a
	// session started in the middle of it can record it whole.
	CmdRestart = "restart"
)

// TargetInfo is the resolved SPYTIFY_TARGET_* configuration.
type TargetInfo struct {
	Process  string `json:"process"`
	MPRIS    string `json:"mpris,omitempty"`
	BundleID string `json:"bundle_id,omitempty"`
	Launch   string `json:"launch,omitempty"`
}

// Check is one line of the capability probe shown before Record is enabled.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

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
	// Now discards the current track instead of letting it finish.
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
	// Prefix limits the reset to keys under it, such as "output" or "tags.spotify".
	Prefix string `json:"prefix,omitempty"`
}

type PlayerControlParams struct {
	Cmd string `json:"cmd"`
}

type SpotifyConnectResult struct {
	AuthURL string `json:"auth_url"`
}

type RetagParams struct {
	Paths []string `json:"paths,omitempty"`
}

type RetagResult struct {
	Queued int `json:"queued"`
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
}

type TrackStartedEvent struct {
	Track
}

type TrackFinishedEvent struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	DurationMs int64  `json:"duration_ms"`
	Bytes      int64  `json:"bytes"`
	Tagged     bool   `json:"tagged"`
	TaggedBy   string `json:"tagged_by,omitempty"`
}

type TrackSkippedEvent struct {
	ID     string     `json:"id"`
	Reason SkipReason `json:"reason"`
}

type LevelEvent struct {
	PeakDBFS float64 `json:"peak_dbfs"`
	RMSDBFS  float64 `json:"rms_dbfs"`
}

type TargetStateEvent struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

type LogEvent struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	TS    string `json:"ts"`
}

type ErrorEvent struct {
	Code  string `json:"code"`
	Msg   string `json:"msg"`
	Fatal bool   `json:"fatal"`
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
