package telemetry

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/denisbrodbeck/machineid"
	"github.com/getsentry/sentry-go"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/pkg/proto"
)

// countReads replaces the machine ID read with one that counts, and
// forgets the ID read before.
func countReads(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	saved := machineID
	forget := func() {
		computer.mu.Lock()
		computer.read, computer.id = false, ""
		computer.mu.Unlock()
	}
	machineID = func() (string, error) {
		n.Add(1)
		return "machine", nil
	}
	forget()
	t.Cleanup(func() {
		machineID = saved
		forget()
	})
	return &n
}

// The computer's ID is read only once the bug tracker is on, never at
// launch nor for what the app notes while it is off; then once.
func TestTheComputerIDWaitsForTheBugTracker(t *testing.T) {
	reads := countReads(t)
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{TracesSampleRate: 1, ProblemSampleRate: 1})
	c.SetEnabled(false)
	c.Breadcrumb("ui", "Record", nil)
	c.Log("launch", map[string]any{"ready": true})
	c.Count("track.saved", 1, nil)
	c.Distribution("cut.offset", -480, sentry.UnitMillisecond, nil)
	c.Step("launched")
	c.Problem("capture_stalled", "")
	now := time.Now()
	c.Measure("launch", now.Add(-time.Second), now, nil, nil)
	_ = c.SendCrash(Crash{Program: "spytifyd", Trace: trace})
	_ = c.SendChecks(Checks{Failing: []proto.Check{{Name: "ffmpeg found"}}}, "")
	time.Sleep(300 * time.Millisecond)
	if n := reads.Load(); n != 0 {
		t.Fatalf("the machine ID was read %d times with the bug tracker off", n)
	}

	c.SetEnabled(true)
	c.Measure("launch", now.Add(-time.Second), now, nil, nil)
	c.Flush(5 * time.Second)
	tx := srv.Wait(t, "transaction", 1)[0].JSON()
	if user, _ := tx["user"].(map[string]any); user["id"] != protect("machine") {
		t.Errorf("user %v, want the hash of the machine ID", user)
	}
	if n := reads.Load(); n != 1 {
		t.Errorf("the machine ID was read %d times, want once", n)
	}
}

// protect hashes as denisbrodbeck/machineid did, so that each computer
// keeps its ID across the update.
func TestTheHashIsMachineIDs(t *testing.T) {
	raw, err := machineid.ID()
	if err != nil {
		t.Skipf("no machine ID here: %v", err)
	}
	want, _ := machineid.ProtectedID("spytify")
	if got := protect(raw); got != want {
		t.Errorf("protect gives %s, machineid %s", got, want)
	}
}
