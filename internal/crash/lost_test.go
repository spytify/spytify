package crash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lost is what Spytify.exe printed to stderr before the trace of the
// alpha.1 crash, as Go 1.26.0 prints it; the errno is made up.
const lost = "runtime.preemptM: duplicatehandle failed; errno=6\nfatal error: runtime.preemptM: duplicatehandle failed\n\n"

func TestStderrOf(t *testing.T) {
	for in, want := range map[string]string{
		"/s/spytify-ui.crash.log":   "/s/spytify-ui.crash.stderr",
		"/s/spytify-ui.crash.log.1": "/s/spytify-ui.crash.stderr.1",
		"/s/trace":                  "/s/trace.stderr",
	} {
		if got := StderrOf(in); got != want {
			t.Errorf("StderrOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// The lines the runtime printed to stderr before the trace come back in
// front of it, and the fatal error's message heads it.
func TestWithLost(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "preemptm-windows.txt"))
	if err != nil {
		t.Fatal(err)
	}
	trace := string(b)
	path := filepath.Join(t.TempDir(), "spytify-ui.crash.log")
	if got := WithLost(path, trace); got != trace {
		t.Errorf("without a stderr file, the trace changed")
	}

	if err := os.WriteFile(StderrOf(path), []byte("runtime: an earlier warning\n\n"+lost+trace), 0o600); err != nil {
		t.Fatal(err)
	}
	got := WithLost(path, trace)
	if want := lost + strings.TrimSpace(trace); got != want {
		t.Fatalf("with its stderr, the trace starts with:\n%s", got[:200])
	}
	if h := Headline(got); h != "fatal error: runtime.preemptM: duplicatehandle failed" {
		t.Errorf("headline %q", h)
	}
	if h := Header(got); len(h) != 2 || h[0] != "runtime.preemptM: duplicatehandle failed; errno=6" {
		t.Errorf("header %q", h)
	}

	if err := os.WriteFile(StderrOf(path), []byte(lost+"another trace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := WithLost(path, trace); got != trace {
		t.Errorf("a stderr without this trace changed it")
	}
}

// Keep moves a crash's stderr along with its trace, and never leaves an
// older crash's beside it.
func TestKeepMovesTheStderrWithItsTrace(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "preemptm-windows.txt"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(crashDir(t), "spytify-ui.crash.log")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, string(b))
	write(StderrOf(path), lost+string(b))
	write(StderrOf(path+".1"), "an older crash's stderr\n")
	if got := Keep(path); got != "fatal error: runtime.preemptM: duplicatehandle failed" {
		t.Errorf("summary %q", got)
	}
	if moved, _ := os.ReadFile(StderrOf(path + ".1")); !strings.HasPrefix(string(moved), lost) {
		t.Errorf("the stderr beside the trace holds %.60q", moved)
	}
	if _, err := os.Stat(StderrOf(path)); err == nil {
		t.Error("the stderr stayed beside the new run's log")
	}

	// A run that did not crash leaves the previous crash's files alone.
	write(StderrOf(path), "runtime: a warning\n")
	Keep(path)
	if moved, _ := os.ReadFile(StderrOf(path + ".1")); !strings.HasPrefix(string(moved), lost) {
		t.Errorf("a clean run moved its stderr over the crash's: %.60q", moved)
	}
}
