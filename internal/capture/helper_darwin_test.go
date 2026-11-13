package capture

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// A helper that says nothing, then one that stops sending frames, are each
// started again; the third run streams.
func TestStuckHelperIsStartedAgain(t *testing.T) {
	defer func(h, f time.Duration) { headerTimeout, frameTimeout = h, f }(headerTimeout, frameTimeout)
	headerTimeout, frameTimeout = 300*time.Millisecond, 300*time.Millisecond
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "helper")
	body := `#!/bin/sh
echo run >> "` + runs + `"
n=$(wc -l < "` + runs + `")
if [ "$n" -eq 1 ]; then exec sleep 30; fi
printf 'SPYTIFY-TAP f32le 48000 2\n'
if [ "$n" -eq 2 ]; then head -c 3840 /dev/zero; exec sleep 30; fi
exec cat /dev/zero
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	h := newHelper(script, func(target.Target) ([]string, bool) { return nil, true }, t.Logf)
	if _, err := h.Open(context.Background(), target.Target{}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	deadline := time.After(15 * time.Second)
	chunks := 0
	for {
		select {
		case <-h.Frames():
			chunks++
			b, _ := os.ReadFile(runs)
			if n := strings.Count(string(b), "run"); n >= 3 && chunks > 10 {
				return
			}
		case <-deadline:
			b, _ := os.ReadFile(runs)
			t.Fatalf("after 15 s: %d runs, %d chunks", strings.Count(string(b), "run"), chunks)
		}
	}
}

// The helper's health counts its runs after the first, keeps its exit
// statuses and its last stderr lines without the device's name, and says
// it got its header.
func TestTheHelperSaysWhatItDid(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "helper")
	body := `#!/bin/sh
echo run >> "` + runs + `"
n=$(wc -l < "` + runs + `")
if [ "$n" -eq 1 ]; then echo 'spytify-tap: first try' >&2; exit 1; fi
if [ "$n" -eq 2 ]; then echo 'spytify-tap: target processes or output format changed' >&2; exit 3; fi
echo 'spytify-tap: tapping player[1] on My Speakers at 48000 Hz (tap reports 48000 Hz)' >&2
printf 'SPYTIFY-TAP f32le 48000 2\n'
exec cat /dev/zero
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	h := newHelper(script, func(target.Target) ([]string, bool) { return nil, true }, t.Logf)
	if _, err := h.Open(context.Background(), target.Target{}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	want := map[string]string{
		"capture.backend":            "coreaudio",
		"capture.restarts":           "2",
		"capture.last_exits":         "1,3",
		"capture.header":             "yes",
		"capture.waiting_for_player": "no",
		"capture.stderr":             "first try\ntarget processes or output format changed\ntapping player[1] on a device at 48000 Hz (tap reports 48000 Hz)",
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-h.Frames():
			if maps.Equal(h.Health(), want) {
				return
			}
		case <-deadline:
			t.Fatalf("health %v, want %v", h.Health(), want)
		}
	}
}

// A helper waiting for the player says so, and that it never ran.
func TestAWaitingHelperSaysSo(t *testing.T) {
	h := newHelper("spytify-tap", func(target.Target) ([]string, bool) { return nil, false }, t.Logf)
	if _, err := h.Open(context.Background(), target.Target{}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	want := map[string]string{"capture.backend": "coreaudio", "capture.restarts": "0", "capture.last_exits": "", "capture.header": "no", "capture.waiting_for_player": "yes"}
	deadline := time.Now().Add(5 * time.Second)
	for !maps.Equal(h.Health(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("health %v, want %v", h.Health(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The lines a helper writes as it exits, as the Windows tap's C runtime
// flushes its stderr then, are read before its exit is counted: Wait
// closes the pipe they wait in.
func TestTheHelpersLastLinesAreKept(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "helper")
	body := `#!/bin/sh
echo run >> "` + runs + `"
n=$(wc -l < "` + runs + `" | tr -d ' ')
seq 300 | sed "s/^/spytify-tap: run $n line /" >&2
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	h := newHelper(script, func(target.Target) ([]string, bool) { return nil, true }, t.Logf)
	if _, err := h.Open(context.Background(), target.Target{}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	for run := 1; run <= 3; run++ {
		exits := strings.TrimSuffix(strings.Repeat("1,", run), ",")
		deadline := time.Now().Add(10 * time.Second)
		for h.Health()["capture.last_exits"] != exits {
			if time.Now().After(deadline) {
				t.Fatalf("run %d: health %v", run, h.Health())
			}
			time.Sleep(time.Millisecond)
		}
		lines := strings.Split(h.Health()["capture.stderr"], "\n")
		if last, want := lines[len(lines)-1], fmt.Sprintf("run %d line 300", run); last != want {
			t.Fatalf("run %d ended: its last line kept is %q, want %q", run, last, want)
		}
	}
}
