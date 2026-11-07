package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// On Windows a track's cover is a temporary file the recorder's watcher
// deletes once the next track's cover is in (#765). coverRig plays tracks
// that way to a window recording into out.
type coverRig struct {
	t       *testing.T
	a       *App
	m       *model.Model
	out     string
	watcher string
	urls    []string
}

func newCoverRig(t *testing.T) *coverRig {
	t.Helper()
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	r := &coverRig{t: t, a: a, m: m, out: t.TempDir(), watcher: t.TempDir()}
	values := defaults()
	values["output.dir"] = r.out
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	a.render()
	return r
}

// play starts track n (from 1) with a cover of its own, as the watcher
// shows it: the previous track ends, and its cover file goes.
func (r *coverRig) play(n int) {
	r.t.Helper()
	cover := filepath.Join(r.watcher, fmt.Sprintf("spytify-cover-%d", n))
	writeCover(r.t, cover)
	r.urls = append(r.urls, "file://"+filepath.ToSlash(cover))
	if n > 1 {
		r.m.Apply(event(r.t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: trackID(n - 1), Path: filepath.Join(r.out, "x.mp3"), DurationMs: 1000, Bytes: 4}))
		if err := os.Remove(filepath.Join(r.watcher, fmt.Sprintf("spytify-cover-%d", n-1))); err != nil {
			r.t.Fatal(err)
		}
	}
	r.m.Apply(event(r.t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{
		ID: trackID(n), Artist: "Artiste", Title: fmt.Sprintf("Track %d", n), LengthMs: 1000, CoverURL: r.urls[n-1]}}))
	r.a.render()
	// The window's goroutines are done before the next track comes, as
	// they are in the minutes a track plays.
	r.settle()
}

// settle waits for the covers being read and copied.
func (r *coverRig) settle() {
	r.t.Helper()
	eventually(r.t, "the covers never settled", func() bool {
		r.a.covers.mu.Lock()
		loading := len(r.a.covers.pending)
		r.a.covers.mu.Unlock()
		r.a.copies.mu.Lock()
		copying := len(r.a.copies.pending)
		r.a.copies.mu.Unlock()
		return loading == 0 && copying == 0
	})
}

// shows says whether the Record list draws track n's cover once its row
// asked for it.
func (r *coverRig) shows(n int) bool {
	r.t.Helper()
	row := model.Row{ID: trackID(n), CoverURL: r.urls[n-1]}
	r.a.rowCover(row)
	r.settle()
	return r.a.rowCover(row).row != nil
}

// copied lists the copies in the output folder's .covers.
func (r *coverRig) copied() []string {
	entries, _ := os.ReadDir(filepath.Join(r.out, coverFolder))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func trackID(n int) string { return fmt.Sprintf("s-%03d", n) }

// Only the shown tab draws: tracks that play while Library shows still
// have their cover in the Record list (#765).
func TestCoversOfTracksPlayedOnAnotherTabShowInTheRecordList(t *testing.T) {
	r := newCoverRig(t)
	r.a.showTab(tabLibrary)
	r.play(1)
	r.play(2)
	r.a.showTab(tabRecord)
	r.a.render()
	for n := 1; n <= 2; n++ {
		if !r.shows(n) {
			t.Errorf("track %d has no cover in the Record list", n)
		}
	}
	if got := r.copied(); len(got) != 2 {
		t.Errorf("copies %v, want one per track", got)
	}
}

// The cover cache keeps only the covers drawn last; a row scrolled back to
// reads its cover again, from its copy (#765: an overnight list of 322 rows
// lost its first hours' covers). The cache keeps 10 here.
func TestTheOldestRowsOfALongSessionKeepTheirCover(t *testing.T) {
	r := newCoverRig(t)
	r.a.covers.max = 10
	for n := 1; n <= 15; n++ {
		r.play(n)
	}
	for n := 1; n <= 15; n++ {
		if !r.shows(n) {
			t.Errorf("track %d has no cover once scrolled back to", n)
		}
	}
}

// Clear list deletes the copies of the rows it clears; the track playing
// keeps its own.
func TestClearListDeletesTheCoversOfTheRowsItClears(t *testing.T) {
	r := newCoverRig(t)
	r.play(1)
	r.play(2)
	r.m.ClearRows()
	r.a.render()
	eventually(t, "the cleared row's cover was kept", func() bool { return len(r.copied()) == 1 })
	if got := r.copied(); filepath.Ext(got[0]) != ".png" || got[0][:len(got[0])-4] != trackID(2) {
		t.Errorf("copies %v, want the playing track's", got)
	}
}

// A new session starts a new list: the last session's copies go.
func TestANewSessionDeletesTheLastSessionsCovers(t *testing.T) {
	r := newCoverRig(t)
	r.play(1)
	r.play(2)
	r.m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	r.a.render()
	if got := r.copied(); len(got) != 2 {
		t.Fatalf("copies %v, want the ended session's two while its list shows", got)
	}
	r.m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	r.a.render()
	eventually(t, "the last session's covers were kept", func() bool { return len(r.copied()) == 0 })
}

// A window opening replays the session's tracks: their copies stay and
// show, as the watcher's files are long gone; copies of tracks it does not
// hold go.
func TestAWindowOpeningKeepsTheCoversOfTheTracksItReplays(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	out := t.TempDir()
	covers := filepath.Join(out, coverFolder)
	if err := os.MkdirAll(covers, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCover(t, filepath.Join(covers, "s-001.png"))
	writeCover(t, filepath.Join(covers, "old-007.png"))
	gone := "file://" + filepath.ToSlash(filepath.Join(t.TempDir(), "spytify-cover-1"))

	values := defaults()
	values["output.dir"] = out
	m.SetInfo(info())
	m.SetSettings(values)
	m.Replay([]proto.Event{
		event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}),
		event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-001", Title: "Track 1", LengthMs: 1000, CoverURL: gone}}),
	})
	m.SetConn(model.Connected, "")
	a.render()
	r := &coverRig{t: t, a: a, m: m, out: out, urls: []string{gone}}
	eventually(t, "a copy the list does not hold was kept", func() bool { return len(r.copied()) == 1 })
	if got := r.copied(); got[0] != "s-001.png" {
		t.Errorf("copies %v, want the replayed track's", got)
	}
	if !r.shows(1) {
		t.Error("the replayed track has no cover")
	}
}

// writeCover writes a small PNG at path.
func writeCover(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
