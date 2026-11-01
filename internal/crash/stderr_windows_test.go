//go:build windows

package crash

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// captureTo has the child of the tests below keep its crash log there,
// capture its stderr, and stop on a fatal error.
const captureTo = "CRASH_TEST_CAPTURE_TO"

func init() {
	if path := os.Getenv(captureTo); path != "" {
		Keep(path)
		CaptureStderr(path)
		var mu sync.Mutex
		mu.Unlock()
	}
}

// Started with no stderr, as Explorer starts Spytify.exe (here, the null
// device), the program keeps the fatal error's message for the next run.
func TestCaptureStderrKeepsTheFatalError(t *testing.T) {
	path := filepath.Join(crashDir(t), "spytify-ui.crash.log")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), captureTo+"="+path)
	if err := cmd.Run(); err == nil {
		t.Fatal("the child did not stop")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trace := WithLost(path, string(b))
	if got := Headline(trace); got != "fatal error: sync: unlock of unlocked mutex" {
		t.Errorf("headline %q from:\n%s", got, trace)
	}
}

// Given a stderr, the program keeps it, and no file is written.
func TestCaptureStderrLeavesAGivenStderr(t *testing.T) {
	path := filepath.Join(crashDir(t), "spytify-ui.crash.log")
	var stderr bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), captureTo+"="+path)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("the child did not stop")
	}
	if !strings.Contains(stderr.String(), "fatal error: sync: unlock of unlocked mutex") {
		t.Errorf("stderr holds %q", stderr.String())
	}
	if _, err := os.Stat(StderrOf(path)); err == nil {
		t.Error("a stderr file was written")
	}
}
