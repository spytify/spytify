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

// A session that stops reading for a while, as one writing to a disk that
// stalls does, loses nothing of the first 15 s the helper sends meanwhile.
func TestAStalledSessionLosesNoAudioForAWhile(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "helper")
	// 1000 chunks of 10 ms at 48 kHz stereo float: 10 s of audio, at once.
	body := `#!/bin/sh
printf 'SPYTIFY-TAP f32le 48000 2\n'
head -c 3840000 /dev/zero
exec sleep 30
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	h := newHelper(script, func(target.Target) ([]string, bool) { return nil, true }, t.Logf)
	if _, err := h.Open(context.Background(), target.Target{}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	time.Sleep(time.Second)
	if n := h.Overruns(); n != 0 {
		t.Fatalf("%d chunks dropped while the session did not read for 1 s", n)
	}
	for i := range 1000 {
		select {
		case <-h.Frames():
		case <-time.After(5 * time.Second):
			t.Fatalf("%d chunks of 1000", i)
		}
	}
}

// A helper that keeps exiting without its header is started again less
// and less often; once it gives its header, the wait starts over.
func TestAFailingHelperIsRetriedLessOften(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	works := filepath.Join(dir, "works")
	script := filepath.Join(dir, "helper")
	body := `#!/bin/sh
echo run >> "` + runs + `"
if [ ! -e "` + works + `" ]; then echo 'spytify-tap: not permitted' >&2; exit 1; fi
printf 'SPYTIFY-TAP f32le 48000 2\n'
head -c 3840 /dev/zero
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "run")
	}
	h := newHelper(script, func(target.Target) ([]string, bool) { return nil, true }, t.Logf)
	h.retryFirst, h.retryLast = 20*time.Millisecond, 320*time.Millisecond
	if _, err := h.Open(context.Background(), target.Target{}); err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	go func() {
		for range h.Frames() {
		}
	}()
	// 20, 40, 80, 160 then 320 ms between runs: 7 or 8 in 1.5 s, where a
	// fixed 20 ms made about 50.
	time.Sleep(1500 * time.Millisecond)
	if n := count(); n > 10 {
		t.Fatalf("the helper ran %d times in 1.5 s", n)
	}
	if err := os.WriteFile(works, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A run that gave its header brings the wait back to 20 ms: the runs
	// that follow come about 25 a second, not 3.
	deadline := time.Now().Add(2 * time.Second)
	for h.Health()["capture.header"] != "yes" {
		if time.Now().After(deadline) {
			t.Fatal("no run gave its header")
		}
		time.Sleep(time.Millisecond)
	}
	before := count()
	time.Sleep(time.Second)
	if n := count() - before; n < 8 {
		t.Fatalf("%d runs in 1 s after one gave its header", n)
	}
}

// The volume the helper says it read comes out of Volumes, the latest one
// first, and stays out of the lines kept for its health.
func TestTheHelpersVolumeComesOutOfVolumes(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "helper")
	body := `#!/bin/sh
echo 'spytify-tap: recording 1 at 48000 Hz, the mix rate of My Speakers' >&2
echo 'spytify-tap: volume 0.850' >&2
echo 'spytify-tap: volume 0.400' >&2
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
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-h.Frames():
		case v := <-h.Volumes():
			if v != 0.4 {
				// 0.85 may be taken before 0.4 comes.
				if v == 0.85 {
					continue
				}
				t.Fatalf("volume %v, want 0.4", v)
			}
			if e := h.Health()["capture.stderr"]; e != "recording 1 at 48000 Hz, the mix rate of a device" {
				t.Fatalf("lines kept %q, want the recording line only", e)
			}
			return
		case <-deadline:
			t.Fatalf("no volume from the helper; health %v", h.Health())
		}
	}
}
