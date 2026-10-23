// Package model is the state of the spytify UI: what the screens show,
// built only from the core's results and events. It knows nothing of the
// toolkit, so every rule here is tested without a window.
package model

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

// Conn is the state of the connection to spytifyd.
type Conn int

const (
	Disconnected Conn = iota
	Connecting
	Connected
)

// RowStatus is where a track is in its life.
type RowStatus string

const (
	Recording RowStatus = "Recording"
	Saving    RowStatus = "Saving"
	Saved     RowStatus = "Saved"
	Tagged    RowStatus = "Tagged"
	Skipped   RowStatus = "Skipped"
)

// Row is one track of the session list.
type Row struct {
	ID         string
	Artist     string
	Title      string
	Album      string
	LengthMs   int64
	CoverURL   string
	Started    time.Time
	Status     RowStatus
	Reason     proto.SkipReason
	Path       string
	DurationMs int64
	Bytes      int64
	TaggedBy   string
}

// Done reports whether the row's file is closed (saved or skipped).
func (r Row) Done() bool { return r.Status == Saved || r.Status == Tagged || r.Status == Skipped }

// StatusText is the chip label: "Tagged", "Skipped: duplicate".
func (r Row) StatusText() string {
	if r.Status == Skipped && r.Reason != "" {
		return "Skipped: " + reasonText(r.Reason)
	}
	return string(r.Status)
}

func reasonText(r proto.SkipReason) string {
	switch r {
	case proto.SkipTooShort:
		return "too short"
	case proto.SkipIncomplete:
		return "incomplete"
	case proto.SkipSeeked:
		return "track bar moved"
	}
	return string(r)
}

// Totals are the session footer counts.
type Totals struct {
	Saved   int
	Skipped int
	Ads     int
	Bytes   int64
}

const maxLogs = 1000

// State is everything the screens render. A State returned by Snapshot is
// a copy the caller owns.
type State struct {
	Conn      Conn
	ConnError string
	Info      proto.CoreInfoResult
	HaveInfo  bool
	Session   proto.SessionState
	SessionID string
	Current   *Row
	Rows      []Row // newest first
	Level     proto.LevelEvent
	Target    proto.TargetStateEvent
	// TargetKnown is set once a target.state event arrived; until then
	// the core.info check is all there is.
	TargetKnown bool
	Settings    map[string]any
	Logs        []proto.LogEvent
	LastError   *proto.ErrorEvent
}

// Recording reports whether a session runs.
func (s State) Recording() bool { return s.Session != "" && s.Session != proto.StateIdle }

// Totals counts the session's rows.
func (s State) Totals() Totals {
	var t Totals
	rows := s.Rows
	if s.Current != nil {
		rows = append([]Row{*s.Current}, rows...)
	}
	for _, r := range rows {
		switch {
		case r.Status == Saved || r.Status == Tagged:
			t.Saved++
			t.Bytes += r.Bytes
		case r.Status == Skipped && r.Reason == proto.SkipAd:
			t.Ads++
			t.Skipped++
		case r.Status == Skipped:
			t.Skipped++
		}
	}
	return t
}

// Model holds the state and tells listeners when it changes.
type Model struct {
	mu        sync.Mutex
	s         State
	now       func() time.Time
	listeners []func()
	// levels hear the 10 Hz level events, which change nothing else; the
	// OnChange listeners do not, so the meter does not redraw the window.
	levels []func()
	// cleared are the rows the user cleared; a replay of the core's recent
	// events must not bring them back.
	cleared map[string]bool
}

// New returns an empty, disconnected model.
func New() *Model {
	return &Model{s: State{Session: proto.StateIdle, Settings: map[string]any{}}, now: time.Now, cleared: map[string]bool{}}
}

// ClearRows empties the list of finished tracks; the current one stays.
func (m *Model) ClearRows() {
	m.update(func(s *State) {
		for _, r := range s.Rows {
			m.cleared[r.ID] = true
		}
		s.Rows = nil
	})
}

// OnChange registers fn, called after every change, outside the lock.
func (m *Model) OnChange(fn func()) {
	m.mu.Lock()
	m.listeners = append(m.listeners, fn)
	m.mu.Unlock()
}

func (m *Model) update(fn func(s *State)) {
	m.mu.Lock()
	fn(&m.s)
	listeners := append([]func(){}, m.listeners...)
	m.mu.Unlock()
	for _, l := range listeners {
		l()
	}
}

// Snapshot copies the state.
func (m *Model) Snapshot() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.s
	if s.Current != nil {
		c := *s.Current
		s.Current = &c
	}
	s.Rows = append([]Row(nil), s.Rows...)
	s.Logs = append([]proto.LogEvent(nil), s.Logs...)
	s.Settings = make(map[string]any, len(m.s.Settings))
	for k, v := range m.s.Settings {
		s.Settings[k] = v
	}
	if s.LastError != nil {
		e := *s.LastError
		s.LastError = &e
	}
	s.Info.Checks = append([]proto.Check(nil), s.Info.Checks...)
	return s
}

// SetConn records the connection state; leaving Connected keeps what the
// screens show but marks it stale.
func (m *Model) SetConn(c Conn, err string) {
	m.update(func(s *State) { s.Conn, s.ConnError = c, err })
}

func (m *Model) SetInfo(info proto.CoreInfoResult) {
	m.update(func(s *State) { s.Info, s.HaveInfo = info, true })
}

// SetStatus applies a session.status snapshot.
func (m *Model) SetStatus(st proto.SessionStatusResult) {
	m.update(func(s *State) {
		s.Session, s.SessionID = st.State, st.SessionID
		if st.Track != nil && (s.Current == nil || s.Current.ID != st.Track.ID) {
			s.Current = &Row{ID: st.Track.ID, Artist: st.Track.Artist, Title: st.Track.Title, Album: st.Track.Album,
				LengthMs: st.Track.LengthMs, CoverURL: st.Track.CoverURL, Started: m.now(), Status: Recording}
		}
	})
}

// SetSettings replaces the settings values.
func (m *Model) SetSettings(values map[string]any) {
	m.update(func(s *State) {
		s.Settings = make(map[string]any, len(values))
		for k, v := range values {
			s.Settings[k] = v
		}
	})
}

// SetLogs replaces the log lines (from log.tail).
func (m *Model) SetLogs(lines []proto.LogEvent) {
	m.update(func(s *State) {
		s.Logs = append([]proto.LogEvent(nil), lines...)
		trimLogs(s)
	})
}

func trimLogs(s *State) {
	if len(s.Logs) > maxLogs {
		s.Logs = s.Logs[len(s.Logs)-maxLogs:]
	}
}

// Replay applies a burst of events, such as events.recent on connect.
func (m *Model) Replay(events []proto.Event) {
	m.update(func(s *State) {
		for _, ev := range events {
			if ev.Method != proto.EventLog {
				m.apply(s, ev)
			}
		}
	})
}

// Apply folds one event into the state.
func (m *Model) Apply(ev proto.Event) {
	if ev.Method == proto.EventLevel {
		m.mu.Lock()
		_ = ev.Decode(&m.s.Level)
		levels := append([]func(){}, m.levels...)
		// Audio is back: the warning that none arrived no longer holds.
		cleared := m.s.LastError != nil && m.s.LastError.Code == "capture_stalled"
		if cleared {
			m.s.LastError = nil
		}
		listeners := append([]func(){}, m.listeners...)
		m.mu.Unlock()
		for _, l := range levels {
			l()
		}
		if cleared {
			for _, l := range listeners {
				l()
			}
		}
		return
	}
	m.update(func(s *State) { m.apply(s, ev) })
}

// OnLevel registers fn for level events.
func (m *Model) OnLevel(fn func()) {
	m.mu.Lock()
	m.levels = append(m.levels, fn)
	m.mu.Unlock()
}

// Live is the current track, the session state and the level, without
// copying the rest of the state.
func (m *Model) Live() (current *Row, session proto.SessionState, level proto.LevelEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.s.Current != nil {
		c := *m.s.Current
		current = &c
	}
	return current, m.s.Session, m.s.Level
}

func (m *Model) apply(s *State, ev proto.Event) {
	switch ev.Method {
	case proto.EventSessionState:
		var e proto.SessionStateEvent
		if ev.Decode(&e) != nil {
			return
		}
		if s.Session == proto.StateIdle && e.State != proto.StateIdle {
			// A new session starts a new list.
			s.Rows, s.Current, s.LastError = nil, nil, nil
		}
		s.Session = e.State
		// Waiting means the core follows no item: what was playing ended.
		if e.State == proto.StateIdle || e.State == proto.StateWaiting {
			m.retire(s)
		}
	case proto.EventTrackStarted:
		var e proto.TrackStartedEvent
		if ev.Decode(&e) != nil {
			return
		}
		// A reconnecting client can see a start twice: once replayed, once
		// live.
		if m.cleared[e.ID] || (s.Current != nil && s.Current.ID == e.ID) {
			return
		}
		for _, r := range s.Rows {
			if r.ID == e.ID {
				return
			}
		}
		m.retire(s)
		s.Current = &Row{ID: e.ID, Artist: e.Artist, Title: e.Title, Album: e.Album, LengthMs: e.LengthMs,
			CoverURL: e.CoverURL, Started: m.now(), Status: Recording}
	case proto.EventTrackFinished:
		var e proto.TrackFinishedEvent
		if ev.Decode(&e) != nil {
			return
		}
		m.edit(s, e.ID, func(r *Row) {
			r.Status, r.Path, r.DurationMs, r.Bytes, r.TaggedBy = Saved, e.Path, e.DurationMs, e.Bytes, e.TaggedBy
			if e.Tagged && e.TaggedBy != "" && e.TaggedBy != "local" {
				r.Status = Tagged
			}
		})
	case proto.EventTrackSkipped:
		var e proto.TrackSkippedEvent
		if ev.Decode(&e) != nil {
			return
		}
		m.edit(s, e.ID, func(r *Row) { r.Status, r.Reason = Skipped, e.Reason })
	case proto.EventLevel:
		_ = ev.Decode(&s.Level)
	case proto.EventTargetState:
		if ev.Decode(&s.Target) == nil {
			s.TargetKnown = true
			// core.info checked once, on connect; keep its answer current.
			for i := range s.Info.Checks {
				if s.Info.Checks[i].Name == "player running" {
					s.Info.Checks[i].OK = s.Target.Running
					s.Info.Checks[i].Detail = "not running"
					if s.Target.Running {
						s.Info.Checks[i].Detail = fmt.Sprintf("pid %d", s.Target.PID)
					}
				}
			}
		}
	case proto.EventSettingsChanged:
		var e proto.SettingsValues
		if ev.Decode(&e) == nil {
			for k, v := range e.Values {
				s.Settings[k] = v
			}
		}
	case proto.EventLog:
		var e proto.LogEvent
		if ev.Decode(&e) == nil {
			s.Logs = append(s.Logs, e)
			trimLogs(s)
		}
	case proto.EventError:
		var e proto.ErrorEvent
		if ev.Decode(&e) == nil {
			s.LastError = &e
		}
	}
}

// retire moves the current track into the list: it is being finished
// unless an event already said how it ended.
func (m *Model) retire(s *State) {
	if s.Current == nil {
		return
	}
	r := *s.Current
	if r.Status == Recording {
		r.Status = Saving
	}
	s.Rows = append([]Row{r}, s.Rows...)
	s.Current = nil
}

// edit changes the row with id, current or listed. A skip of the current
// track (an advert, a duplicate) keeps it current: it is still playing.
func (m *Model) edit(s *State, id string, fn func(*Row)) {
	if s.Current != nil && s.Current.ID == id {
		fn(s.Current)
		if s.Current.Status != Skipped {
			m.retire(s)
		}
		return
	}
	for i := range s.Rows {
		if s.Rows[i].ID == id {
			fn(&s.Rows[i])
			return
		}
	}
}

// Elapsed is how long the current track has been recording.
func (m *Model) Elapsed(r Row) time.Duration {
	if r.Started.IsZero() {
		return 0
	}
	d := m.now().Sub(r.Started)
	if r.LengthMs > 0 && d > time.Duration(r.LengthMs)*time.Millisecond {
		return time.Duration(r.LengthMs) * time.Millisecond
	}
	return d
}

// SortRows orders a copy of rows for the library: "date", "artist" or "title".
func SortRows(rows []Row, by string) []Row {
	out := append([]Row(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		switch by {
		case "artist":
			if out[i].Artist != out[j].Artist {
				return out[i].Artist < out[j].Artist
			}
			return out[i].Title < out[j].Title
		case "title":
			return out[i].Title < out[j].Title
		}
		return out[i].Started.After(out[j].Started)
	})
	return out
}
