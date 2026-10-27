package platform

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/target"
)

// Every check has a name, and one that fails says what to fix: the Logs
// tab shows them as they are.
func TestProbeSaysWhatToFix(t *testing.T) {
	checks := Backends{TapHelper: filepath.Join(t.TempDir(), "no-such-helper")}.Probe()
	if len(checks) == 0 {
		t.Fatal("no checks")
	}
	helper := false
	for _, c := range checks {
		if c.Name == "" || (!c.OK && c.Detail == "") {
			t.Errorf("check %+v", c)
		}
		if c.Name == "capture helper" {
			helper = true
			if c.OK {
				t.Errorf("a missing helper passed: %+v", c)
			}
		}
	}
	if (runtime.GOOS == "darwin" || runtime.GOOS == "windows") && !helper {
		t.Errorf("no capture helper check in %+v", checks)
	}
}

func TestBackendsWithoutHooks(t *testing.T) {
	var b Backends
	if b.mute() {
		t.Error("muted with no setting")
	}
	b.logf("no Logf: %d", 1)
	b.MuteOutput = func() bool { return true }
	var got string
	b.Logf = func(f string, a ...any) { got = f }
	if !b.mute() {
		t.Error("not muted")
	}
	if b.logf("hello"); got != "hello" {
		t.Errorf("logf passed %q", got)
	}
}

func TestFakeBackends(t *testing.T) {
	tl, err := fake.Load(filepath.Join("..", "..", "testdata", "timelines", "basic.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := Fake{Timeline: tl}
	c, np, err := f.New(target.Target{})
	if err != nil || c == nil || np == nil {
		t.Fatalf("New: %v %v %v", c, np, err)
	}
	c.Close()
	if checks := f.Probe(); len(checks) != 1 || !checks[0].OK || checks[0].Detail != "basic" {
		t.Errorf("checks %+v", checks)
	}
}
