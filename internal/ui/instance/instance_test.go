package instance

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// socketPath is in a short folder: Unix socket paths have a length limit.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "spyinst")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "ui.sock")
}

// running claims path and serves it, counting the windows shown.
func running(t *testing.T, path string) *atomic.Int32 {
	t.Helper()
	inst, err := Claim(path)
	if err != nil {
		t.Fatalf("first launch: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
	shown := &atomic.Int32{}
	go inst.Serve(func() { shown.Add(1) })
	return shown
}

// The Windows bug: double-clicking Spytify.exe with the window hidden to
// the tray started a second app instead of bringing the window back.
func TestASecondLaunchShowsTheRunningWindow(t *testing.T) {
	path := socketPath(t)
	shown := running(t, path)
	if inst, err := Claim(path); !errors.Is(err, ErrRunning) {
		if inst != nil {
			inst.Close()
		}
		t.Fatalf("second launch: %v, want ErrRunning", err)
	}
	if n := shown.Load(); n != 1 {
		t.Fatalf("the window was shown %d times, want once", n)
	}
}

// A crash leaves the socket file with no one listening: the next launch
// takes it over, and answers the one after.
func TestAStaleSocketDoesNotStopTheFirstLaunch(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no stale socket file: %v", err)
	}
	shown := running(t, path)
	if _, err := Claim(path); !errors.Is(err, ErrRunning) {
		t.Fatalf("launch after the stale socket was replaced: %v, want ErrRunning", err)
	}
	if n := shown.Load(); n != 1 {
		t.Fatalf("the window was shown %d times, want once", n)
	}
}

// Two launches at the same moment, a double-click registered twice: one
// runs, the other shows its window.
func TestTwoLaunchesAtOnceMakeOneFirst(t *testing.T) {
	for range 10 {
		path := socketPath(t)
		var shown atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		insts := make([]*Instance, 2)
		errs := make([]error, 2)
		for i := range insts {
			wg.Go(func() {
				<-start
				insts[i], errs[i] = Claim(path)
				if insts[i] != nil {
					go insts[i].Serve(func() { shown.Add(1) })
				}
			})
		}
		close(start)
		wg.Wait()
		first := 0
		for i, inst := range insts {
			switch {
			case inst != nil:
				first++
				inst.Close()
			case !errors.Is(errs[i], ErrRunning):
				t.Errorf("launch %d: %v, want ErrRunning", i, errs[i])
			}
		}
		if first != 1 || shown.Load() != 1 {
			t.Fatalf("%d launches ran and %d windows were shown, want one of each", first, shown.Load())
		}
	}
}

// Once the app quits, its socket goes with it and the next launch runs.
func TestALaunchAfterQuittingRuns(t *testing.T) {
	path := socketPath(t)
	inst, err := Claim(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the socket file is still there after quitting: %v", err)
	}
	next, err := Claim(path)
	if err != nil {
		t.Fatalf("launch after quitting: %v", err)
	}
	next.Close()
}
