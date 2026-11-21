package core

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/core/naming"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/tag"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// controlLog writes down every command the session sends the player.
type controlLog struct {
	mu   sync.Mutex
	cmds []proto.PlayerCmd
}

func (l *controlLog) all() []proto.PlayerCmd {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.cmds)
}

type loggingNP struct {
	*fake.Backend
	log *controlLog
}

func (n loggingNP) Control(cmd proto.PlayerCmd) error {
	n.log.mu.Lock()
	n.log.cmds = append(n.log.cmds, cmd)
	n.log.mu.Unlock()
	return n.Backend.Control(cmd)
}

type loggingBackends struct {
	fakeBackends
	log *controlLog
}

func (b loggingBackends) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	return b.b, loggingNP{b.b, b.log}, nil
}

// endedAtRecorded is whether the session's last state said it ended at a
// track already recorded.
func endedAtRecorded(s *scenario) bool {
	at := false
	for _, ev := range s.events.all() {
		var st proto.SessionStateEvent
		if ev.Method == proto.EventSessionState && ev.Decode(&st) == nil && st.State == proto.StateIdle {
			at = st.AtRecorded
		}
	}
	return at
}

// A playlist that gets its new tracks at the top, played from the top:
// with recorder.stop_at_recorded, the session records the new ones, lets
// an ad by, and ends at the first track already recorded, the player
// paused, before the older ones play. It wins over skipping to the next
// track. Off, or with Suffix, where a known track is recorded again, the
// whole playlist plays as before (#758).
func TestTheSessionStopsAtTheFirstTrackAlreadyRecorded(t *testing.T) {
	cases := []struct {
		name       string
		settings   map[string]any
		saved      []string
		stopped    bool
		olderHeard bool
	}{
		{"on", map[string]any{"recorder.duplicates": "skip", "recorder.stop_at_recorded": true, "recorder.skip_duplicates_forward": true},
			[]string{"Harbor Song", "Afterglow"}, true, false},
		{"off", map[string]any{"recorder.duplicates": "skip"},
			[]string{"Harbor Song", "Afterglow", "Low Tide"}, false, true},
		{"suffix", map[string]any{"recorder.duplicates": "suffix", "recorder.stop_at_recorded": true},
			[]string{"Harbor Song", "Afterglow", "Harbor Song", "Low Tide"}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tl := loadTimeline(t, "ads")
			ad := tl.Items[1]
			tl.Items = []fake.Item{
				{Artist: "Willow Reyes", Title: "Harbor Song", Album: "Saltwater", LengthMs: 5000, GapMs: 500},
				ad,
				{Artist: "Willow Reyes", Title: "Afterglow", Album: "Saltwater", LengthMs: 5000, GapMs: 800},
				// Already recorded: the first of this session.
				{Artist: "Willow Reyes", Title: "Harbor Song", Album: "Saltwater", LengthMs: 5000, GapMs: 800},
				{Artist: "Willow Reyes", Title: "Low Tide", Album: "Saltwater", LengthMs: 5000, GapMs: 1000},
			}
			tl.Items[1].LengthMs = 8000
			tl.Expect, tl.Variants = nil, nil
			s := newScenario(t, tl, c.settings)
			sent := &controlLog{}
			s.core.backends = loggingBackends{fakeBackends: s.core.backends.(fakeBackends), log: sent}
			runWith(t, s, SessionOptions{EndOnStop: true})

			saved, skipped := outcome(t, s)
			if !slices.Equal(saved, c.saved) {
				t.Errorf("saved %v, want %v", saved, c.saved)
			}
			if skipped["Sponsored message"] != proto.SkipAd {
				t.Errorf("the ad was %q, want skipped as an ad", skipped["Sponsored message"])
			}
			if _, heard := skipped["Low Tide"]; heard || (slices.Contains(saved, "Low Tide") != c.olderHeard) {
				t.Errorf("Low Tide, after the track already recorded: saved %v, skipped %v", saved, skipped)
			}
			if got := endedAtRecorded(s); got != c.stopped {
				t.Errorf("the session said it ended at a track already recorded: %v, want %v", got, c.stopped)
			}
			cmds := sent.all()
			if paused := slices.Contains(cmds, proto.CmdPause); paused != c.stopped {
				t.Errorf("the player was sent %v; paused %v, want %v", cmds, paused, c.stopped)
			}
			if slices.Contains(cmds, proto.CmdNext) {
				t.Errorf("the player was sent next (%v): stopping wins over skipping", cmds)
			}
		})
	}
}

// A first track already recorded ends the session at once, with nothing
// saved: nothing in the playlist is new.
func TestAFirstTrackAlreadyRecordedEndsTheSessionWithNothingSaved(t *testing.T) {
	r := newRig(t, map[string]any{"recorder.duplicates": "skip", "recorder.stop_at_recorded": true}, SessionOptions{})
	r.alreadyRecorded("Artist", "A")
	playA(r)
	r.wait()
	if !r.ended {
		t.Fatal("the session did not end")
	}
	lines := r.lines()
	if len(r.jobs) > 0 {
		t.Errorf("saved %v, want nothing", r.jobs)
	}
	if !slices.Contains(lines, "state idle at_recorded") {
		t.Errorf("no idle state at a track already recorded in %v", lines)
	}
	waitFor(t, func() bool { return slices.Contains(r.sent(), proto.CmdPause) }, "player.control pause")
}

// Only a full take is a track already recorded: a file kept with a
// suffix, cut short, in low quality, faded by a pause or recorded from a
// low volume, is not, and its track is recorded again.
func TestAFileKeptWithASuffixIsNoTrackAlreadyRecorded(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	store := testStore(t, dir, map[string]any{"output.dir": out})
	v := store.Values()
	tr := meta.TrackInfo{Title: "Harbor Song", Artists: []string{"Willow Reyes"}, Album: "Saltwater", LengthMs: 5000}
	o := namingOptions(v)
	plan := naming.Build(namingTrack(tag.FromLocal(tr, tagOptions(v)), tr), o)
	folder := filepath.Join(out, plan.Dir)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	r := openRecords(out)
	for _, suffix := range []string{" (partial)", " (low quality)", " (pause fade)", " (low volume)"} {
		p := filepath.Join(folder, plan.Base+suffix+"."+o.Extension)
		if err := os.WriteFile(p, []byte("take"), 0o644); err != nil {
			t.Fatal(err)
		}
		if f, ok := r.find(tr, v); ok {
			t.Errorf("with only %q, the track is already recorded at %v", filepath.Base(p), f)
		}
	}
	full := filepath.Join(folder, plan.Base+"."+o.Extension)
	if err := os.WriteFile(full, []byte("take"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, ok := r.find(tr, v); !ok || f.path != full {
		t.Errorf("with the full take, found %v %v, want %s", f, ok, full)
	}
}
