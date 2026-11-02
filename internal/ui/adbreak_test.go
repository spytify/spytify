package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

func adRow(id, title string, ms int64) model.Row {
	return model.Row{ID: id, Title: title, LengthMs: ms, Status: model.Skipped, Reason: proto.SkipAd}
}

// Each run of ads is one pill with the run's length; open, its ads follow
// it newest first, as the tracks are, numbered from the first one played,
// by their title only when the player gave them one.
func TestEachRunOfAdsIsOnePill(t *testing.T) {
	rows := []model.Row{
		adRow("a-5", "", 30000),
		{ID: "s-3", Title: "Wait", Status: model.Saved},
		adRow("a-4", "—", 24000),
		adRow("a-3", "New EP is out now!", 30000),
		adRow("a-2", "", 15000),
		{ID: "s-2", Title: "Reunion", Status: model.Saved},
		{ID: "s-1", Title: "Intro", Status: model.Skipped, Reason: proto.SkipDuplicate},
	}
	describe := func(items []sessionItem) []string {
		var out []string
		for _, it := range items {
			switch it.kind {
			case itemTrack:
				out = append(out, it.row.Title)
			case itemBreak:
				out = append(out, "pill "+it.brk+" "+fmtDuration(it.length))
			case itemAd:
				out = append(out, adName(it.n, it.row.Title)+" "+fmtDuration(it.length))
			}
		}
		return out
	}
	closed := []string{"pill a-5 0:30", "Wait", "pill a-2 1:09", "Reunion", "Intro"}
	if got := describe(sessionItems(rows, nil)); !slices.Equal(got, closed) {
		t.Errorf("closed: %q, want %q", got, closed)
	}
	open := []string{"pill a-5 0:30", "Wait", "pill a-2 1:09", "Ad #3 0:24", "Ad #2 · New EP is out now! 0:30", "Ad #1 0:15", "Reunion", "Intro"}
	if got := describe(sessionItems(rows, map[string]bool{"a-2": true})); !slices.Equal(got, open) {
		t.Errorf("open: %q, want %q", got, open)
	}
}

// feedAdBreak is a session where three ads played between two tracks, the
// second one titled, the third only a dash, as the player names some.
func feedAdBreak(t *testing.T, m *model.Model) {
	t.Helper()
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4242}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	track := func(id, title string, cover int, ms int64) {
		m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: id, Artist: "M83", Title: title, Album: "Hurry Up, We're Dreaming", LengthMs: ms, CoverURL: generatedCover(t, cover)}}))
		m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: id, Path: "/Users/listener/Music/Spytify/M83/" + title + ".flac", DurationMs: ms, Bytes: ms * 140}))
	}
	ad := func(id, title string, ms int64) {
		m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: id, Title: title, LengthMs: ms}}))
		m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: id, Reason: proto.SkipAd}))
	}
	track("s-001", "Intro", 1, 134000)
	track("s-002", "Reunion", 2, 236000)
	ad("a-001", "", 15000)
	ad("a-002", "New EP is out now!", 30000)
	ad("a-003", "—", 24000)
	track("s-003", "Wait", 3, 341000)
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-004", Artist: "M83", Title: "Midnight City", Album: "Hurry Up, We're Dreaming", LengthMs: 243000, CoverURL: generatedCover(t, 4)}}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -3, RMSDBFS: -14.4,
		Channels: []proto.ChannelLevel{{PeakDBFS: -3, RMSDBFS: -13}, {PeakDBFS: -4.5, RMSDBFS: -16.5}}}))
	m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: "session 20261102-120000 started", TS: time.Now().Format(time.RFC3339Nano)}))
}

// adPill is the drawn ad break's button, and its row.
func adPill(t *testing.T, r *recordScreen) (*rowButton, widget.ListItemID) {
	t.Helper()
	for _, s := range r.list.shown {
		for _, b := range rowButtons(s.row.content) {
			if strings.HasPrefix(b.Text, fmt.Sprintf(lang.AdBreak, "")) {
				return b, s.id
			}
		}
	}
	t.Fatalf("no ad break among rows %v", drawnIDs(r.list))
	return nil, 0
}

// Three ads in a row are one slim pill in the session list, "Advertisements
// 1:09", where they took three track rows; a click, or Space on its row,
// opens it to a slim line per ad, and closes it again.
func TestAnAdBreakIsOnePillThatOpens(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	a.win.Resize(fyne.NewSize(920, 760))
	feedAdBreak(t, m)
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	all := texts(a.win.Content())
	if !contains(all, fmt.Sprintf(lang.AdBreak, "1:09")) || contains(all, fmt.Sprintf(lang.SkippedFor, lang.ReasonAd)) || contains(all, "Ad #1") {
		t.Errorf("closed, the list shows %q", all)
	}
	pill, at := adPill(t, r)
	if at != 1 || len(r.items) != 4 {
		t.Errorf("the pill is row %d of %d, want 1 of 4: Wait, the pill, Reunion, Intro", at, len(r.items))
	}
	track, slim := rowAt(r.list, 0).box.Size().Height, rowAt(r.list, at).box.Size().Height
	if slim >= track/2 || pill.Size().Height > slim {
		t.Errorf("the pill's row is %v high, a track's %v, the pill %v", slim, track, pill.Size().Height)
	}
	// The break is a blue block, without a line from the tracks around it,
	// and the lines on each side of its pill are a deep blue.
	row := rowAt(r.list, at)
	if row.back.Hidden || !row.line.Hidden || !rowAt(r.list, at+1).line.Hidden {
		t.Error("the closed break is not a block of its own without lines at its ends")
	}
	rule := row.content.(*fyne.Container).Objects[1].(*fyne.Container).Objects[0].(*container.ThemeOverride).Content.(fyne.Widget)
	if got, want := theme.ColorForWidget(theme.ColorNameSeparator, rule), cardColors[colorAdRule]; got != want[0] && got != want[1] {
		t.Errorf("the line beside the pill is %v", got)
	}
	snapshot(t, a, "record-ad-break")

	pill.OnTapped()
	all = texts(a.win.Content())
	for _, want := range []string{"Ad #3", "Ad #2 · New EP is out now!", "Ad #1", "0:15", "0:30", "0:24"} {
		if !contains(all, want) {
			t.Errorf("open, %q is not in the list", want)
		}
	}
	if contains(all, "—") {
		t.Error("an ad titled with a dash shows it")
	}
	for id := at + 1; id <= at+3; id++ {
		if row := rowAt(r.list, id); row == nil || row.box.Size().Height != slim || row.back.Hidden {
			t.Errorf("ad line %d is not drawn %v high on the break's block", id, slim)
		}
	}
	if row := rowAt(r.list, at+4); row == nil || !row.line.Hidden {
		t.Error("a line parts the open break from the track under it")
	}
	snapshot(t, a, "record-ad-break-open")
	a.fa.Settings().SetTheme(spytifyTheme{mode: "light"})
	snapshot(t, a, "record-ad-break-open-light")

	c := a.win.Canvas()
	c.Focus(r.list)
	pressKey(c, fyne.KeyDown)
	pressSpace(c)
	if len(r.items) != 4 || contains(texts(a.win.Content()), "Ad #1") {
		t.Errorf("Space on the pill's row left %d lines", len(r.items))
	}
	c.Unfocus()
	snapshot(t, a, "record-ad-break-light")
}
