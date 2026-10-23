package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCrashSummary(t *testing.T) {
	trace := `panic: runtime error: index out of range [3] with length 3

goroutine 21 [running]:
runtime.panicIndex(...)
	/usr/local/go/src/runtime/panic.go:115 +0x1c
github.com/spytify/spytify/internal/core.(*Session).cutAt(0x1400019e000, 0x2ee0)
	/src/internal/core/session.go:742 +0x6a8
created by github.com/spytify/spytify/internal/core.(*Core).StartSession in goroutine 7
`
	want := "panic: runtime error: index out of range [3] with length 3 at /src/internal/core/session.go:742"
	if got := crashSummary(trace); got != want {
		t.Fatalf("summary = %q", got)
	}
}

func TestKeepCrashesReportsThePreviousRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "spytifyd.crash.log")
	if got := keepCrashes(path); got != "" {
		t.Fatalf("first run reported %q", got)
	}
	if err := os.WriteFile(path, []byte("panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n\t/src/cmd/spytifyd/main.go:40 +0x10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := keepCrashes(path); got != "panic: boom at /src/cmd/spytifyd/main.go:40" {
		t.Fatalf("second run reported %q", got)
	}
	if b, _ := os.ReadFile(path + ".1"); len(b) == 0 {
		t.Fatal("the trace was not kept in .1")
	}
	if got := keepCrashes(path); got != "" {
		t.Fatalf("a clean run left %q", got)
	}
}

// A panic on another goroutine, as a session would raise, reaches the file.
func TestCrashOutputIsKept(t *testing.T) {
	if path := os.Getenv("SPYTIFY_CRASH_TEST"); path != "" {
		keepCrashes(path)
		go func() { panic("boom in a session") }()
		time.Sleep(5 * time.Second)
		return
	}
	path := filepath.Join(t.TempDir(), "spytifyd.crash.log")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashOutputIsKept$")
	cmd.Env = append(os.Environ(), "SPYTIFY_CRASH_TEST="+path)
	if err := cmd.Run(); err == nil {
		t.Fatal("the child did not crash")
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "panic: boom in a session") {
		t.Fatalf("crash log holds %q", b)
	}
	if got := keepCrashes(path); !strings.HasPrefix(got, "panic: boom in a session at ") || !strings.Contains(got, "crash_test.go:") {
		t.Fatalf("summary %q", got)
	}
}
