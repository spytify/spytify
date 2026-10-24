//go:build windows

package meta

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// The poller loads the media session API in Windows PowerShell and reports a
// player that is not running: no session, no error.
func TestSMTCPollerRunsOnWindows(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	m := NewSMTC()
	m.Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	events, err := m.Watch(ctx, target.Target{ProcessNames: []string{"spytify-no-such-player"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	for len(got) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("the poller stopped after %+v", got)
			}
			got = append(got, ev)
		case <-ctx.Done():
			t.Fatalf("no poll in 60 s; got %+v", got)
		}
	}
	if got[0].Kind != TrackChanged || !got[0].Track.Empty() || got[1].Kind != StatusChanged || got[1].Status != Stopped {
		t.Fatalf("first events = %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) > 0 {
		t.Fatalf("poller errors: %v", logs)
	}
}

// A thumbnail stream, as a player hands one over, is saved whole: here
// the stream of a file, which Windows opens the same way.
func TestSMTCSavesAThumbnail(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "cover.jpg"), filepath.Join(dir, "saved.jpg")
	want := make([]byte, 70000)
	for i := range want {
		want[i] = byte(i * 7)
	}
	if err := os.WriteFile(src, want, 0o600); err != nil {
		t.Fatal(err)
	}
	body := `
$null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
$null = [Windows.Storage.Streams.RandomAccessStreamReference, Windows.Storage.Streams, ContentType = WindowsRuntime]
$file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($src)) ([Windows.Storage.StorageFile])
Save-Thumbnail ([Windows.Storage.Streams.RandomAccessStreamReference]::CreateFromFile($file)) $dst
`
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := powershell(ctx, smtcScript(nil, map[string]string{"src": psQuote(src), "dst": psQuote(dst)}, body)).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("%v (output: %s)", err, out)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("saved %d bytes, want %d", len(got), len(want))
	}
}
