package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

func targetStates(rec *recorder) []proto.TargetStateEvent {
	var out []proto.TargetStateEvent
	for _, ev := range rec.all() {
		if ev.Method == proto.EventTargetState {
			var s proto.TargetStateEvent
			_ = ev.Decode(&s)
			out = append(out, s)
		}
	}
	return out
}

// Clients hear about the player when it appears, restarts or exits, not on
// every poll.
func TestTargetStateIsSentOnlyWhenItChanges(t *testing.T) {
	c, rec := newTestCore(t, false)
	running, pid := false, 0
	c.find = func(target.Target) (target.Process, bool) {
		return target.Process{PID: pid, Name: "fakeplayer"}, running
	}
	c.pollTarget()
	running, pid = true, 7
	c.pollTarget()
	c.pollTarget()
	pid = 8
	c.pollTarget()
	running, pid = false, 0
	c.pollTarget()
	c.pollTarget()
	want := []proto.TargetStateEvent{{Running: true, PID: 7}, {Running: true, PID: 8}, {Running: false}}
	if got := targetStates(rec); !slices.Equal(got, want) {
		t.Fatalf("target states %+v, want %+v", got, want)
	}
}

func TestWatchTargetStopsWithItsContext(t *testing.T) {
	c, rec := newTestCore(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.WatchTarget(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(targetStates(rec)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the running player was not reported")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchTarget did not stop")
	}
}

// core.shutdown stops the recording before the daemon goes.
func TestShutdownStopsTheSessionThenCallsTheHook(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, _ := newTestCoreAt(t, true, true)
	if _, err := c.StartSession(SessionOptions{}); err != nil {
		t.Fatal(err)
	}
	state := make(chan proto.SessionState, 1)
	c.SetShutdown(func() { state <- c.Status().State })
	c.Shutdown()
	select {
	case st := <-state:
		if st != proto.StateIdle {
			t.Fatalf("the hook ran with the session %s", st)
		}
	default:
		t.Fatal("the shutdown hook was not called")
	}
}

// A session raises the player's volume to 100% and notes the old one; a
// daemon that died before putting it back does so at its next start.
func TestThePlayerVolumeIsRestoredAfterAnUncleanStop(t *testing.T) {
	c, _ := newTestCore(t, true)
	player := c.backends.(fakeBackends).b.Player()
	_ = player.SetVolume(1)
	if err := os.MkdirAll(c.env.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.markerPath(), []byte(`{"volume":0.4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Startup()
	if v, _ := player.Volume(); v != 0.4 {
		t.Fatalf("volume %.2f, want 0.40", v)
	}
	if _, err := os.Stat(c.markerPath()); err == nil {
		t.Fatal("the marker stayed after the volume came back")
	}
}

// The marker waits for the player: its volume can only be set while it
// runs.
func TestTheVolumeMarkerWaitsForThePlayer(t *testing.T) {
	c, _ := newTestCore(t, false)
	if err := os.MkdirAll(c.env.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.markerPath(), []byte(`{"volume":0.4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Startup()
	if _, err := os.Stat(c.markerPath()); err != nil {
		t.Fatal("the marker went while the player was not running")
	}

	if err := os.WriteFile(c.markerPath(), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Startup()
	if _, err := os.Stat(c.markerPath()); err == nil {
		t.Fatal("a broken marker stayed")
	}
}

func TestControlChecksTheCommand(t *testing.T) {
	c, _ := newTestCore(t, true)
	var perr *proto.Error
	if err := c.Control("rewind"); !errors.As(err, &perr) || perr.Code != proto.CodeInvalidParams {
		t.Fatalf("rewind: %v", err)
	}
	// With no session, through a connection of its own.
	if err := c.Control(proto.CmdPause); err != nil {
		t.Fatalf("pause: %v", err)
	}
}
