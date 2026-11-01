package crash

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestHeadline(t *testing.T) {
	windows, err := os.ReadFile(filepath.Join("testdata", "preemptm-windows.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, trace, want string }{
		{"a panic", "panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n\t/src/main.go:3 +0x10\n", "panic: boom"},
		{"a signal", "SIGSEGV: segmentation violation\nPC=0x10462012c m=0 sigcode=2 addr=0x0\n", "SIGSEGV: segmentation violation"},
		// The thread that preempts the others could not, on Windows: the
		// runtime threw on its own stack.
		{"the runtime's stack", string(windows), "fatal error: thrown in runtime.preemptM (os_windows.go:1177)"},
		{"a goroutine's stack", "\r\ngoroutine 1 [running]:\r\n" +
			"internal/sync.fatal({0x1001e94ce?, 0xd?})\r\n\tC:/Program Files/Go/src/runtime/panic.go:1205 +0x20\r\n" +
			"internal/sync.(*Mutex).unlockSlow(0x65abc481c100, 0xffffffff)\r\n\tC:/Program Files/Go/src/internal/sync/mutex.go:204 +0x38\r\n",
			"fatal error: thrown in internal/sync.(*Mutex).unlockSlow (mutex.go:204)"},
	} {
		if got := Headline(c.trace); got != c.want {
			t.Errorf("%s: headline %q, want %q", c.name, got, c.want)
		}
	}
	if got, want := Summary(string(windows)), "fatal error: thrown in runtime.preemptM (os_windows.go:1177)"; got != want {
		t.Errorf("summary %q, want %q", got, want)
	}
}

// fatalTo has the child of TestHeadlineOfARealFatalError stop on a fatal
// error, writing its trace to this file.
const fatalTo = "CRASH_TEST_FATAL_TO"

// A fatal error's trace, as the runtime really writes it, names what
// stopped the program, with or without the runtime's message.
func TestHeadlineOfARealFatalError(t *testing.T) {
	if path := os.Getenv(fatalTo); path != "" {
		Keep(path)
		var mu sync.Mutex
		mu.Unlock()
	}
	path := filepath.Join(crashDir(t), "spytifyd.crash.log")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHeadlineOfARealFatalError$")
	cmd.Env = append(os.Environ(), fatalTo+"="+path)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the child did not stop:\n%s", out)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trace := string(b)
	got := Headline(trace)
	if strings.HasPrefix(strings.TrimSpace(trace), "fatal error: ") {
		// A Go that copies the message to the crash output.
		if want := "fatal error: sync: unlock of unlocked mutex"; got != want {
			t.Errorf("headline %q, want %q, from:\n%s", got, want, trace)
		}
		return
	}
	if !strings.HasPrefix(got, "fatal error: thrown in ") || !strings.Contains(got, "(*Mutex).unlockSlow (mutex.go:") {
		t.Errorf("headline %q from:\n%s", got, trace)
	}
}
