package capture

import (
	"context"
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
