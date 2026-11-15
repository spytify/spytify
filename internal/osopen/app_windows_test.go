package osopen

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The two ways the player is installed: the Store puts an alias on PATH,
// reached by its name with no extension through PATHEXT; the installer
// registers it in App Paths. App finds both, as cmd /C start did. Each
// program is a .cmd that leaves a marker, so the tests open a console
// window: CI only, or with SPYTIFY_REAL_OPEN set.
func TestAppFindsAProgramOnPathByItsName(t *testing.T) {
	realOpen(t)
	dir, marker := markerProgram(t, "spytifypath829.cmd")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := App("spytifypath829"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, marker)
}

func TestAppFindsAProgramInAppPaths(t *testing.T) {
	realOpen(t)
	dir, marker := markerProgram(t, "target.cmd")
	const sub = `Software\Microsoft\Windows\CurrentVersion\App Paths\spytifyapppaths829.exe`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, sub, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, sub) })
	err = k.SetStringValue("", filepath.Join(dir, "target.cmd"))
	k.Close()
	if err != nil {
		t.Fatal(err)
	}

	if err := App("spytifyapppaths829"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, marker)
}

// A name that resolves to nothing fails at once, and never waits on the
// shell's "cannot find" dialog, which a recorder with no window cannot
// dismiss. Nothing starts, so it runs everywhere.
func TestAppFailsAtOnceOnAnUnknownName(t *testing.T) {
	done := make(chan error, 1)
	go func() { done <- App("spytify-no-such-program-829") }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an unknown name started something")
		}
		// Any other error, such as a wrong SHELLEXECUTEINFOW size, means
		// the shell never looked the name up.
		if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			t.Fatalf("refused with %v, want file not found", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("App is still waiting after 10 s: the shell showed a dialog")
	}
}

func realOpen(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "" && os.Getenv("SPYTIFY_REAL_OPEN") == "" {
		t.Skip("starts a program in a console window: set SPYTIFY_REAL_OPEN=1 to run it")
	}
}

// markerProgram writes name in a folder of its own, a .cmd that creates the
// marker it returns.
func markerProgram(t *testing.T, name string) (dir, marker string) {
	t.Helper()
	dir = t.TempDir()
	if strings.ContainsAny(dir, " \t") {
		t.Skipf("%s holds a space", dir)
	}
	marker = filepath.Join(dir, "marker")
	write(t, filepath.Join(dir, name), "@type nul > \""+marker+"\"\r\n")
	return dir, marker
}

func waitFor(t *testing.T, marker string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
	}
	t.Fatal("the program did not run")
}
