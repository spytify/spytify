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
// registers it in App Paths. App finds both, as cmd /C start did. The
// tests open a console window: CI only, or with SPYTIFY_REAL_OPEN set.
func TestAppFindsAProgramOnPathByItsName(t *testing.T) {
	realOpen(t)
	dir, marker := markerProgram(t, "spytifypath829.cmd")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := App("spytifypath829"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, marker)
}

// App Paths can name a program only, with no arguments, so the program is a
// copy of whoami.exe: App succeeding is the proof, since the name is found
// nowhere else and an unknown name fails (TestAppFailsAtOnceOnAnUnknownName).
// Installers register the name with .exe; the player's name has none.
// The entry goes in HKLM: on the CI runner the shell reads no App Paths
// from HKCU, and cmd /C start found none there either (it waited on its
// "cannot find" dialog).
func TestAppFindsAProgramInAppPaths(t *testing.T) {
	realOpen(t)
	exe := filepath.Join(t.TempDir(), "player829.exe")
	whoami, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "whoami.exe"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, exe, string(whoami))
	const sub = `Software\Microsoft\Windows\CurrentVersion\App Paths\spytifyapppaths829.exe`
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, sub, registry.SET_VALUE)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Skip("writing App Paths in HKLM needs an administrator")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registry.DeleteKey(registry.LOCAL_MACHINE, sub) })
	err = k.SetStringValue("", exe)
	k.Close()
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"spytifyapppaths829.exe", "spytifyapppaths829"} {
		t.Run(name, func(t *testing.T) {
			if err := App(name); err != nil {
				t.Error(err)
			}
		})
	}
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
