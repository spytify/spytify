package encode

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// On Windows, ffmpeg is looked for where winget, Chocolatey and Scoop put
// it, after the copy the zip ships beside spytifyd.
func TestWellKnownHasThePackageManagersPlaces(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`, "ProgramData": `C:\ProgramData`,
		"USERPROFILE": `C:\Users\u`, "ProgramFiles": `C:\Program Files`, "SystemDrive": `C:`}
	got := wellKnown("windows", func(k string) string { return env[k] })
	if len(got) < 5 {
		t.Fatalf("candidates %q", got)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{"WinGet", "chocolatey", "scoop", "Program Files", "ffmpeg.exe"} {
		if !strings.Contains(all, want) {
			t.Errorf("no %s candidate in %q", want, got)
		}
	}
	if mac := wellKnown("darwin", func(string) string { return "" }); !strings.Contains(strings.Join(mac, " "), "/opt/homebrew/bin/ffmpeg") {
		t.Errorf("macOS candidates %q", mac)
	}
	self, _ := os.Executable()
	if got := besideProgram("windows"); got != filepath.Join(filepath.Dir(self), "ffmpeg.exe") {
		t.Errorf("beside the program: %q", got)
	}
}

// The ffmpeg beside the program, the one the macOS app and the Windows zip
// ship, runs even when another ffmpeg comes first on PATH. SPYTIFY_FFMPEG
// still wins over both.
func TestTheFFmpegBesideTheProgramComesBeforePATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake ffmpeg is a shell script")
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	fake := func(path, version string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'ffmpeg version "+version+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	beside := filepath.Join(filepath.Dir(self), "ffmpeg")
	if _, err := os.Stat(beside); err == nil {
		t.Skipf("%s already exists", beside)
	}
	fake(beside, "beside")
	t.Cleanup(func() { os.Remove(beside) })
	onPath := t.TempDir()
	fake(filepath.Join(onPath, "ffmpeg"), "path")
	t.Setenv("PATH", onPath+string(os.PathListSeparator)+os.Getenv("PATH"))

	f, err := Find("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != beside || f.Version != "ffmpeg version beside" {
		t.Errorf("found %s (%s), want the one beside the program, %s", f.Path, f.Version, beside)
	}
	chosen := filepath.Join(onPath, "ffmpeg")
	if f, err := Find(chosen); err != nil || f.Path != chosen {
		t.Errorf("SPYTIFY_FFMPEG=%s found %+v, %v", chosen, f, err)
	}
}
