package openfiles

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var errHeld = errors.New("being used by another process")

// fakeRename refuses the first refusals calls with err, then renames, and
// records the waits between tries and the lines logged.
type fakeRename struct {
	refusals int
	err      error
	tries    int
	asked    int
	waits    []time.Duration
	logged   []string
}

func (f *fakeRename) install(t *testing.T) {
	t.Helper()
	oldRename, oldSleep, oldHeld, oldHeldBy := rename, sleep, held, heldBy
	t.Cleanup(func() { rename, sleep, held, heldBy = oldRename, oldSleep, oldHeld, oldHeldBy })
	rename = func(string, string) error {
		f.tries++
		if f.tries <= f.refusals {
			return f.err
		}
		return nil
	}
	sleep = func(d time.Duration) { f.waits = append(f.waits, d) }
	held = func(err error) bool { return errors.Is(err, errHeld) }
	heldBy = func(...string) []string {
		f.asked++
		return []string{"Scanner"}
	}
}

func (f *fakeRename) logf(format string, args ...any) {
	f.logged = append(f.logged, fmt.Sprintf(format, args...))
}

// A file held for a moment is renamed once it is let go, each wait twice
// the last, and each refusal is logged.
func TestAHeldFileIsRenamedOnceLetGo(t *testing.T) {
	f := &fakeRename{refusals: 3, err: errHeld}
	f.install(t)
	if err := Rename("Artist - Title.mp3.part", "Artist - Title.mp3", f.logf); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second}
	if fmt.Sprint(f.waits) != fmt.Sprint(want) {
		t.Errorf("waits %v, want %v", f.waits, want)
	}
	if len(f.logged) != 4 || f.logged[3] != "Artist - Title.mp3: renamed at try 4 of 7" {
		t.Errorf("logged %q", f.logged)
	}
}

// A file held all along is given up after 7 tries, about 16 s, with the
// error Windows gave.
func TestAFileHeldAllAlongIsGivenUp(t *testing.T) {
	f := &fakeRename{refusals: 100, err: errHeld}
	f.install(t)
	err := Rename("a.part", "a", f.logf)
	if !errors.Is(err, errHeld) {
		t.Fatalf("got %v, want the refusal", err)
	}
	var total time.Duration
	for _, w := range f.waits {
		total += w
	}
	if f.tries != 7 || total != 15750*time.Millisecond {
		t.Errorf("%d tries over %v, want 7 over 15.75s", f.tries, total)
	}
}

// Any other error is not tried again.
func TestOtherRenameErrorsAreNotTriedAgain(t *testing.T) {
	gone := errors.New("the system cannot find the file specified")
	f := &fakeRename{refusals: 100, err: gone}
	f.install(t)
	if err := Rename("a.part", "a", nil); !errors.Is(err, gone) || f.tries != 1 || len(f.waits) != 0 {
		t.Errorf("err %v after %d tries and waits %v", err, f.tries, f.waits)
	}
}

// Windows is asked which programs hold the file once, when Rename gives up,
// not before each try: a Restart Manager session on every try is a
// ransomware unlock step to a behaviour engine.
func TestHoldersAreAskedOnceWhenRenameGivesUp(t *testing.T) {
	f := &fakeRename{refusals: 100, err: errHeld}
	f.install(t)
	err := Rename("a.part", "a", f.logf)
	if f.tries != 7 || f.asked != 1 {
		t.Errorf("%d refusals asked Windows for the holders %d times, want 7 and 1", f.tries, f.asked)
	}
	if err == nil || err.Error() != "being used by another process (held by Scanner)" {
		t.Errorf("got %v, want the refusal naming the holder", err)
	}
}

// A file let go before the last try is renamed without asking Windows who
// held it.
func TestHoldersAreNotAskedWhenTheFileIsLetGo(t *testing.T) {
	f := &fakeRename{refusals: 6, err: errHeld}
	f.install(t)
	if err := Rename("a.part", "a", f.logf); err != nil {
		t.Fatal(err)
	}
	if f.asked != 0 {
		t.Errorf("asked Windows for the holders %d times, want 0", f.asked)
	}
}
