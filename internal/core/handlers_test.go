package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

func newTestCore(t *testing.T, running bool) (*Core, *recorder) {
	t.Helper()
	dir := t.TempDir()
	store, err := settings.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if rej := store.Override(map[string]any{"output.dir": filepath.Join(dir, "out")}); len(rej) > 0 {
		t.Fatal(rej)
	}
	tl := loadTimeline(t, "basic")
	player, err := fake.NewPlayer(tl, false)
	if err != nil {
		t.Fatal(err)
	}
	log, _ := NewLogger("", nil)
	c := New(Options{
		Env:      target.Env{Target: target.Target{ProcessNames: []string{"fakeplayer"}}, FFmpeg: "ffmpeg", StateDir: filepath.Join(dir, "state")},
		Store:    store,
		Log:      log,
		Backends: fakeBackends{fake.NewBackend(player)},
		FindProcess: func(target.Target) (target.Process, bool) {
			return target.Process{PID: 7, Name: "fakeplayer"}, running
		},
	})
	rec := &recorder{}
	c.SetBroadcaster(rec.add)
	return c, rec
}

func call(t *testing.T, c *Core, method string, params any, out any) error {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	res, err := c.Handle(context.Background(), method, raw)
	if err != nil {
		return err
	}
	if out != nil {
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

func code(err error) int {
	var perr *proto.Error
	if errors.As(err, &perr) {
		return perr.Code
	}
	return 0
}

func TestSettingsActions(t *testing.T) {
	c, rec := newTestCore(t, true)
	var res proto.SettingsSetResult
	if err := call(t, c, proto.MethodSettingsSet, proto.SettingsSetParams{Values: map[string]any{
		"encode.format":        "flac",
		"recorder.min_seconds": 10,
		"bogus":                1,
	}}, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 || res.Rejected["bogus"] == "" {
		t.Fatalf("set result %+v", res)
	}
	var changed proto.SettingsValues
	for _, ev := range rec.all() {
		if ev.Method == proto.EventSettingsChanged {
			_ = ev.Decode(&changed)
		}
	}
	if changed.Values["encode.format"] != "flac" || changed.Values["recorder.min_seconds"] != float64(10) {
		t.Fatalf("settings.changed = %+v", changed.Values)
	}

	var got proto.SettingsValues
	if err := call(t, c, proto.MethodSettingsGet, proto.SettingsGetParams{Keys: []string{"encode.format", "recorder.min_seconds"}}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Values["encode.format"] != "flac" || got.Values["recorder.min_seconds"] != float64(10) {
		t.Fatalf("get = %+v", got.Values)
	}
	if err := call(t, c, proto.MethodSettingsGet, proto.SettingsGetParams{Keys: []string{"nope"}}, nil); code(err) != proto.CodeInvalidParams {
		t.Fatalf("unknown key: %v", err)
	}
	if err := call(t, c, proto.MethodSettingsReset, proto.SettingsResetParams{Prefix: "encode"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := call(t, c, proto.MethodSettingsGet, proto.SettingsGetParams{Keys: []string{"encode.format"}}, &got); err != nil || got.Values["encode.format"] != "mp3" {
		t.Fatalf("after reset: %+v %v", got.Values, err)
	}
}

func TestSessionErrors(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed: session.start refuses before it looks for the player")
	}
	c, _ := newTestCore(t, false)
	if err := call(t, c, proto.MethodSessionStart, nil, nil); code(err) != proto.CodeTargetNotRunning {
		t.Fatalf("start without player: %v", err)
	}
	if err := call(t, c, proto.MethodSessionStop, nil, nil); code(err) != proto.CodeNoSession {
		t.Fatalf("stop without session: %v", err)
	}
	var st proto.SessionStatusResult
	if err := call(t, c, proto.MethodSessionStatus, nil, &st); err != nil || st.State != proto.StateIdle {
		t.Fatalf("status: %+v %v", st, err)
	}
	if err := call(t, c, proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: "rewind"}, nil); code(err) != proto.CodeInvalidParams {
		t.Fatalf("bad control: %v", err)
	}
	if err := call(t, c, "no.such", nil, nil); code(err) != proto.CodeMethodNotFound {
		t.Fatalf("unknown method: %v", err)
	}
	if err := call(t, c, proto.MethodSessionStart, json.RawMessage(`{"single_track":"yes"}`), nil); code(err) != proto.CodeInvalidParams {
		t.Fatalf("bad params: %v", err)
	}
}

func TestSecondStartIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	c, _ := newTestCore(t, true)
	if c.ffErr != nil {
		t.Skip(c.ffErr)
	}
	rec := &recorder{}
	c.SetBroadcaster(rec.add)
	if err := call(t, c, proto.MethodSessionStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := call(t, c, proto.MethodSessionStart, nil, nil); code(err) != proto.CodeSessionActive {
		t.Fatalf("second start: %v", err)
	}
	// A client learns that a session runs from its first state event.
	deadline := time.Now().Add(5 * time.Second)
	for !announced(rec.all(), proto.StateWaiting) {
		if time.Now().After(deadline) {
			t.Fatalf("no session.state waiting announced: %+v", rec.all())
		}
		time.Sleep(10 * time.Millisecond)
	}
	var stop proto.SessionStopResult
	if err := call(t, c, proto.MethodSessionStop, proto.SessionStopParams{Now: true}, &stop); err != nil {
		t.Fatal(err)
	}
	c.Wait()
	var st proto.SessionStatusResult
	if err := call(t, c, proto.MethodSessionStatus, nil, &st); err != nil || st.State != proto.StateIdle {
		t.Fatalf("after stop: %+v %v", st, err)
	}
}

func TestCoreInfoAndLogs(t *testing.T) {
	c, _ := newTestCore(t, true)
	c.Startup()
	var info proto.CoreInfoResult
	if err := call(t, c, proto.MethodCoreInfo, nil, &info); err != nil {
		t.Fatal(err)
	}
	if info.Protocol != proto.Protocol || info.Target.Process != "fakeplayer" {
		t.Fatalf("info = %+v", info)
	}
	if wantMPRIS := runtime.GOOS == "linux"; (info.Target.MPRIS == "org.mpris.MediaPlayer2.fakeplayer") != wantMPRIS {
		t.Fatalf("MPRIS name %q on %s", info.Target.MPRIS, runtime.GOOS)
	}
	checks := map[string]bool{}
	for _, ch := range info.Checks {
		checks[ch.Name] = ch.OK
	}
	if !checks["target configured"] || !checks["player running"] || !checks["output folder writable"] {
		t.Fatalf("checks = %+v", info.Checks)
	}
	var tail proto.LogTailResult
	if err := call(t, c, proto.MethodLogTail, proto.LogTailParams{Lines: 50}, &tail); err != nil {
		t.Fatal(err)
	}
	if len(tail.Lines) == 0 || tail.Lines[0].Msg != "target process=fakeplayer mpris=org.mpris.MediaPlayer2.fakeplayer bundle=" {
		t.Fatalf("first log line = %+v", tail.Lines)
	}
	var recent proto.EventsRecentResult
	if err := call(t, c, proto.MethodEventsRecent, nil, &recent); err != nil || len(recent.Events) == 0 {
		t.Fatalf("recent events: %d %v", len(recent.Events), err)
	}
}

func announced(events []proto.Event, st proto.SessionState) bool {
	for _, ev := range events {
		var e proto.SessionStateEvent
		if ev.Method == proto.EventSessionState && ev.Decode(&e) == nil && e.State == st {
			return true
		}
	}
	return false
}

func TestPlayerLaunch(t *testing.T) {
	c, _ := newTestCore(t, true)
	if err := call(t, c, proto.MethodPlayerLaunch, nil, nil); err != nil {
		t.Fatalf("launch while running: %v", err)
	}
	c, _ = newTestCore(t, false)
	marker := filepath.Join(t.TempDir(), "launched")
	c.env.Target.Launch = "touch " + marker
	if runtime.GOOS == "windows" {
		c.env.Target.Launch = "type nul > " + marker
	}
	if err := call(t, c, proto.MethodPlayerLaunch, nil, nil); err != nil {
		t.Fatalf("launch: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("SPYTIFY_TARGET_LAUNCH did not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
