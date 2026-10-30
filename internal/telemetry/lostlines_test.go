package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/crash"
)

// A crash found with the stderr its run kept reads as the runtime's
// message, with the lines printed along with it.
func TestACrashKeepsTheLinesItsStderrHad(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "crash", "testdata", "preemptm-windows.txt"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	prefs := &memPrefs{m: map[string]string{}}
	FindCrashes(dir, prefs, time.Now().Add(-time.Minute))
	path := filepath.Join(dir, "spytify-ui.crash.log.1")
	lost := "runtime.preemptM: duplicatehandle failed; errno=6\nfatal error: runtime.preemptM: duplicatehandle failed\n\n"
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crash.StderrOf(path), []byte(lost+string(b)), 0o600); err != nil {
		t.Fatal(err)
	}
	crashes := FindCrashes(dir, prefs, time.Now())
	if len(crashes) != 1 || !strings.HasPrefix(crashes[0].Trace, lost) {
		t.Fatalf("crashes %+v", crashes)
	}
	ex := crashEvent(crashes[0]).Exception[0]
	if ex.Type != "fatal error" || ex.Value != "runtime.preemptM: duplicatehandle failed\nruntime.preemptM: duplicatehandle failed; errno=6" {
		t.Errorf("exception %s %q", ex.Type, ex.Value)
	}
	if len(ex.Stacktrace.Frames) != 8 || ex.Stacktrace.Frames[7].Function != "throw" {
		t.Errorf("frames %+v", ex.Stacktrace.Frames)
	}
	MarkHandled(prefs, crashes[0])
	if again := FindCrashes(dir, prefs, time.Now()); len(again) != 0 {
		t.Errorf("a handled crash came back: %v", again)
	}
}

// A signal's other lines follow its name.
func TestASignalKeepsItsLines(t *testing.T) {
	trace := `SIGSEGV: segmentation violation
PC=0x10462012c m=0 sigcode=2 addr=0x0
signal arrived during cgo execution

goroutine 1 gp=0x46cdc2c221e0 m=0 mp=0x1046e1660 [syscall]:
runtime.cgocall(0x104620124, 0x46cdc2c9fe78)
	runtime/cgocall.go:167 +0x44
`
	ex := crashEvent(Crash{Program: "spytifyd", Trace: trace}).Exception[0]
	if ex.Type != "panic" || ex.Value != "SIGSEGV: segmentation violation\nPC=0x10462012c m=0 sigcode=2 addr=0x0\nsignal arrived during cgo execution" {
		t.Errorf("exception %s %q", ex.Type, ex.Value)
	}
}
