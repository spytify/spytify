package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

// A fatal error the runtime raised on its own stack, without its message,
// reads as that error, with that stack: the goroutine listed after it was
// only drawing the window.
func TestCrashOfTheRuntimeOnItsOwnStack(t *testing.T) {
	trace, err := os.ReadFile(filepath.Join("..", "crash", "testdata", "preemptm-windows.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ex := crashEvent(Crash{Program: "spytify-ui", Trace: string(trace)}).Exception[0]
	if ex.Type != "fatal error" || ex.Value != "thrown in runtime.preemptM (os_windows.go:1177)" {
		t.Errorf("exception %s %q", ex.Type, ex.Value)
	}
	frames := ex.Stacktrace.Frames
	if len(frames) != 8 {
		t.Fatalf("%d frames: %+v", len(frames), frames)
	}
	if first, last := frames[0], frames[7]; first.Function != "mstart" || last.Module != "runtime" || last.Function != "throw" {
		t.Errorf("frames from %+v to %+v", first, last)
	}
	if f := frames[3]; f.Function != "sysmon" || f.Filename != "runtime/proc.go" || f.Lineno != 6597 {
		t.Errorf("sysmon frame %+v", f)
	}
}

// A fatal error a goroutine raised, without its message, names the
// function that gave up, with the goroutine's stack.
func TestCrashOfAGoroutineWithoutItsMessage(t *testing.T) {
	trace := `goroutine 7 [running]:
internal/sync.fatal({0x1001e94ce?, 0xd?})
	runtime/panic.go:1205 +0x20
internal/sync.(*Mutex).unlockSlow(0x65abc481c100, 0xffffffff)
	internal/sync/mutex.go:204 +0x38
github.com/spytify/spytify/internal/core.(*Session).Stop(0x1400019e000)
	github.com/spytify/spytify/internal/core/session.go:300 +0x6a8
`
	ex := crashEvent(Crash{Program: "spytifyd", Trace: trace}).Exception[0]
	if ex.Type != "fatal error" || ex.Value != "thrown in internal/sync.(*Mutex).unlockSlow (mutex.go:204)" {
		t.Errorf("exception %s %q", ex.Type, ex.Value)
	}
	frames := ex.Stacktrace.Frames
	if len(frames) != 3 || frames[0].Function != "(*Session).Stop" || !frames[0].InApp {
		t.Errorf("frames %+v", frames)
	}
}
