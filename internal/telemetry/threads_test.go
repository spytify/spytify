package telemetry

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
)

// Every goroutine of a trace shows in the report as a thread, the one that
// crashed first, picked, and named in the exception.
func TestEveryGoroutineIsAThread(t *testing.T) {
	ev := crashEvent(Crash{Program: "spytifyd", Trace: trace})
	if len(ev.Threads) != 2 {
		t.Fatalf("%d threads: %+v", len(ev.Threads), ev.Threads)
	}
	crashed, other := ev.Threads[0], ev.Threads[1]
	if crashed.ID != "21" || crashed.Name != "goroutine 21 [running]" || !crashed.Crashed || !crashed.Current || len(crashed.Stacktrace.Frames) != 3 {
		t.Errorf("crashed thread %+v", crashed)
	}
	if other.ID != "1" || other.Name != "goroutine 1 [select]" || other.Crashed || other.Current || other.Stacktrace.Frames[0].Function != "main" {
		t.Errorf("other thread %+v", other)
	}
	if ex := ev.Exception[0]; ex.ThreadID != 21 || len(ex.Stacktrace.Frames) != 3 {
		t.Errorf("exception on thread %d with %d frames", ex.ThreadID, len(ex.Stacktrace.Frames))
	}
}

// The runtime's own stack is a thread too, the crashed one when the
// runtime gave up there, on a thread of its own.
func TestTheRuntimeStackIsAThread(t *testing.T) {
	trace, err := os.ReadFile(filepath.Join("..", "crash", "testdata", "preemptm-windows.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ev := crashEvent(Crash{Program: "spytify-ui", Trace: string(trace)})
	if len(ev.Threads) != 2 {
		t.Fatalf("%d threads: %+v", len(ev.Threads), ev.Threads)
	}
	if th := ev.Threads[0]; th.ID != "runtime" || th.Name != "runtime stack" || !th.Crashed || len(th.Stacktrace.Frames) != 8 {
		t.Errorf("runtime thread %+v", th)
	}
	if th := ev.Threads[1]; th.ID != "1" || th.Name != "goroutine 1 [syscall, locked to thread]" || th.Crashed || len(th.Stacktrace.Frames) != 5 {
		t.Errorf("goroutine thread %+v", th)
	}
	if ex := ev.Exception[0]; ex.ThreadID != 0 || ex.Stacktrace.Frames[0].Function != "mstart" {
		t.Errorf("exception on thread %d from %+v", ex.ThreadID, ex.Stacktrace.Frames[0])
	}
}

// A runtime stack that a goroutine entered, as when its stack grew too
// big, leaves that goroutine the crashed thread.
func TestTheGoroutineThatEnteredTheRuntimeCrashed(t *testing.T) {
	trace := `runtime stack:
runtime.throw({0x1028d3c5e?, 0x0?})
	runtime/panic.go:1229 +0x4d
runtime.newstack()
	runtime/stack.go:1107 +0x5a5
runtime.morestack()
	runtime/asm_amd64.s:621 +0x22

goroutine 1 gp=0x14000002380 m=0 mp=0x1029a8f40 [running]:
main.recurse(0x0?)
	github.com/spytify/spytify/cmd/spytifyd/main.go:10 +0x1c
`
	ev := crashEvent(Crash{Program: "spytifyd", Trace: trace})
	if len(ev.Threads) != 2 || ev.Threads[0].Name != "goroutine 1 [running]" || !ev.Threads[0].Crashed || ev.Threads[1].ID != "runtime" || ev.Threads[1].Crashed {
		t.Fatalf("threads %+v", ev.Threads)
	}
	if ex := ev.Exception[0]; ex.Type != "fatal error" || ex.Value != "thrown in runtime.newstack (stack.go:1107)" || ex.ThreadID != 1 {
		t.Errorf("exception %s %q on thread %d", ex.Type, ex.Value, ex.ThreadID)
	}
}

// A report keeps at most maxStacks threads; the attachment has them all.
func TestThreadsAreCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString("panic: boom\n")
	for i := range maxStacks + 10 {
		b.WriteString("\ngoroutine " + strconv.Itoa(i+1) + " [select]:\nmain.main()\n\tgithub.com/spytify/spytify/cmd/spytifyd/main.go:40 +0x10\n")
	}
	if ev := crashEvent(Crash{Program: "spytifyd", Trace: b.String()}); len(ev.Threads) != maxStacks {
		t.Errorf("%d threads, want %d", len(ev.Threads), maxStacks)
	}
}

// The threads of a report name no home folder, as its exception does not.
func TestThreadsAreScrubbed(t *testing.T) {
	srv := fakesentry.New(t)
	home, _ := os.UserHomeDir()
	text := trace + "\ngoroutine 9 [chan receive]:\ngithub.com/spytify/spytify/internal/core.(*Core).Wait()\n\t" +
		filepath.ToSlash(filepath.Join(home, "dev", "spytify", "internal", "core", "core.go")) + ":120 +0x10\n"
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	if err := c.SendCrash(Crash{Program: "spytifyd", Trace: text}); err != nil {
		t.Fatal(err)
	}
	ev := srv.Wait(t, "event", 1)[0].JSON()
	threads, _ := ev["threads"].([]any)
	if len(threads) != 3 {
		t.Fatalf("threads %v", ev["threads"])
	}
	if raw := srv.Raw(); strings.Contains(raw, home) || strings.Contains(raw, filepath.ToSlash(home)) || !strings.Contains(raw, "core.go") {
		t.Error("a thread names the home folder, or lost its file")
	}
}
