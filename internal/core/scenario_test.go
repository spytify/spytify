package core

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// baseSettings make scenario output comparable sample by sample: WAV, no
// loudness change, no network.
var baseSettings = map[string]any{
	"encode.format":        "wav",
	"recorder.normalize":   false,
	"recorder.min_seconds": 1,
	"tags.cover":           false,
	"output.group_by":      "none",
}

type fakeBackends struct{ b *fake.Backend }

func (f fakeBackends) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	return f.b, f.b, nil
}
func (f fakeBackends) Probe() []proto.Check { return nil }

type recorder struct {
	mu      sync.Mutex
	events  []proto.Event
	onEvent func(proto.Event)
}

func (r *recorder) add(method string, payload any) {
	raw, _ := payload.(json.RawMessage)
	ev := proto.Event{Method: method, Params: raw}
	r.mu.Lock()
	r.events = append(r.events, ev)
	hook := r.onEvent
	r.mu.Unlock()
	if hook != nil {
		hook(ev)
	}
}

func (r *recorder) all() []proto.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]proto.Event(nil), r.events...)
}

type scenario struct {
	core   *Core
	player *fake.Player
	out    string
	events *recorder
	logs   *bytes.Buffer
}

func newScenario(t *testing.T, tl *fake.Timeline, extra map[string]any) *scenario {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	store, err := settings.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	overrides := map[string]any{"output.dir": out}
	for _, m := range []map[string]any{baseSettings, tl.Settings, extra} {
		for k, v := range m {
			overrides[k] = v
		}
	}
	if rej := store.Override(overrides); len(rej) > 0 {
		t.Fatalf("settings rejected: %v", rej)
	}
	player, err := fake.NewPlayer(tl, false)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log, _ := NewLogger("", &syncWriter{w: logs})
	c := New(Options{
		Env:         target.Env{Target: target.Target{ProcessNames: []string{"fakeplayer"}}, FFmpeg: "ffmpeg", StateDir: filepath.Join(dir, "state")},
		Store:       store,
		Log:         log,
		Backends:    fakeBackends{fake.NewBackend(player)},
		FindProcess: func(target.Target) (target.Process, bool) { return target.Process{PID: 4242, Name: "fakeplayer"}, true },
	})
	rec := &recorder{}
	c.SetBroadcaster(rec.add)
	return &scenario{core: c, player: player, out: out, events: rec, logs: logs}
}

type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *scenario) run(t *testing.T, tl *fake.Timeline) {
	t.Helper()
	for _, a := range tl.Actions {
		cmd := a.Cmd
		s.player.At(a.AtMs, func() {
			if err := s.core.Control(cmd); err != nil {
				t.Errorf("player.control %s: %v", cmd, err)
			}
		})
	}
	if _, err := s.core.StartSession(SessionOptions{EndOnStop: true}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.core.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatalf("session did not end; log:\n%s", s.logs)
	}
}

func loadTimeline(t *testing.T, name string) *fake.Timeline {
	t.Helper()
	tl, err := fake.Load(filepath.Join("..", "..", "testdata", "timelines", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestScenarios(t *testing.T) {
	for _, name := range []string{"basic", "jitter", "gapless", "ads", "format-change", "pause-resume", "duplicate", "control", "seek", "quiet-start"} {
		t.Run(name, func(t *testing.T) {
			tl := loadTimeline(t, name)
			variants := tl.Variants
			if len(variants) == 0 {
				variants = []fake.Variant{{Name: "default", Expect: tl.Expect}}
			}
			for _, v := range variants {
				t.Run(v.Name, func(t *testing.T) {
					tl := loadTimeline(t, name)
					s := newScenario(t, tl, v.Settings)
					s.run(t, tl)
					files := -1
					if v.Files != nil {
						files = *v.Files
					}
					if keep := os.Getenv("SPYTIFY_KEEP_OUTPUT"); keep != "" {
						dst := filepath.Join(keep, name, v.Name)
						_ = os.RemoveAll(dst)
						if err := os.CopyFS(dst, os.DirFS(s.out)); err != nil {
							t.Log(err)
						}
					}
					if problems := fake.Verify(tl, v.Expect, files, s.out, s.events.all()); len(problems) > 0 {
						for _, p := range problems {
							t.Error(p)
						}
						t.Logf("log:\n%s", s.logs)
					}
				})
			}
		})
	}
}
