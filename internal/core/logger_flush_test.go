package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// An info or debug line waits in memory, one write for many lines, and
// reaches the file within logFlushEvery.
func TestALogLineReachesTheFileWithinASecond(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spytifyd.log")
	l, err := NewLogger(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for i := 0; i < 50; i++ {
		l.Debugf("capture: line %d", i)
	}
	if got := readLog(t, path); got != "" {
		t.Fatalf("each line was written at once:\n%s", got)
	}
	deadline := time.Now().Add(logFlushEvery + 2*time.Second)
	for !strings.Contains(readLog(t, path), "capture: line 49") {
		if time.Now().After(deadline) {
			t.Fatalf("the lines did not reach the file within %v", logFlushEvery)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := strings.Count(readLog(t, path), "capture: line"); n != 50 {
		t.Fatalf("%d lines of 50 in the file", n)
	}
}

// An error line is written at once, with the lines before it.
func TestAnErrorLineReachesTheFileAtOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spytifyd.log")
	l, err := NewLogger(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Debugf("what led to it")
	l.Errorf("it failed")
	got := readLog(t, path)
	if !strings.Contains(got, "what led to it") || !strings.Contains(got, "it failed") {
		t.Fatalf("the file has:\n%s", got)
	}
}

// Close writes the lines still in memory, as spytifyd exits.
func TestClosingTheLogWritesTheWaitingLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spytifyd.log")
	l, err := NewLogger(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	l.Infof("SIGTERM received, stopping")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path); !strings.Contains(got, "SIGTERM received, stopping") {
		t.Fatalf("the last line was lost at Close:\n%s", got)
	}
	l.Infof("a line after Close is dropped")
}

// crashEnv makes the test binary a spytifyd that logs, then dies of a
// panic in a goroutine, which no deferred Close survives.
const crashEnv = "SPYTIFY_TEST_LOG_CRASH"

func TestMain(m *testing.M) {
	if path := os.Getenv(crashEnv); path != "" {
		l, err := NewLogger(path, nil)
		if err != nil {
			os.Exit(3)
		}
		defer l.Close()
		l.Infof("a line from before the last second")
		time.Sleep(logFlushEvery + 300*time.Millisecond)
		l.Errorf("the error before the crash")
		l.Debugf("the debug line just before the crash")
		go func() { panic("boom") }()
		select {}
	}
	os.Exit(m.Run())
}

// A fatal panic skips Close. What was logged more than logFlushEvery
// before, and every error line, is in the file; the info and debug lines
// of the last second are not.
func TestALogSurvivesAFatalPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a process that crashes")
	}
	path := filepath.Join(t.TempDir(), "spytifyd.log")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), crashEnv+"="+path)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "boom") {
		t.Fatalf("the helper did not crash: %v\n%s", err, out)
	}
	got := readLog(t, path)
	for _, want := range []string{"a line from before the last second", "the error before the crash"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q was lost in the crash; the file has:\n%s", want, got)
		}
	}
	if strings.Contains(got, "the debug line just before the crash") {
		t.Errorf("the debug line after the error reached the file: the test no longer shows what a crash loses")
	}
}

// Every log line is encoded once, by the core: the broadcaster gets the
// JSON, which the bus sends as it is (see the bus's
// TestEncodedParamsAreSentAsTheyAre).
func TestEveryLogLineIsEncodedOnce(t *testing.T) {
	c, _ := newTestCore(t, false)
	var mu sync.Mutex
	var got []any
	c.SetBroadcaster(func(method string, payload any) {
		mu.Lock()
		defer mu.Unlock()
		if method == proto.EventLog {
			got = append(got, payload)
		}
	})
	const n = 20
	for i := 0; i < n; i++ {
		c.log.Debugf("capture: line %d", i)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != n {
		t.Fatalf("%d log events for %d lines", len(got), n)
	}
	for i, p := range got {
		raw, ok := p.(json.RawMessage)
		if !ok {
			t.Fatalf("line %d reached the broadcaster as %T, to be encoded there", i, p)
		}
		var ev proto.LogEvent
		if err := json.Unmarshal(raw, &ev); err != nil || ev.Level != proto.LogDebug || !strings.HasPrefix(ev.Msg, "capture: line") {
			t.Fatalf("line %d: %s (%v)", i, raw, err)
		}
	}
}
