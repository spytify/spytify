package encode

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// On Windows, ffmpeg is looked for beside spytifyd first (the copy the
// zip ships), then where winget, Chocolatey and Scoop put it.
func TestWellKnownLooksBesideSpytifydFirst(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`, "ProgramData": `C:\ProgramData`,
		"USERPROFILE": `C:\Users\u`, "ProgramFiles": `C:\Program Files`, "SystemDrive": `C:`}
	got := wellKnown("windows", func(k string) string { return env[k] })
	self, _ := os.Executable()
	if len(got) < 6 || got[0] != filepath.Join(filepath.Dir(self), "ffmpeg.exe") {
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
}

// The encoder list is read once, as core.info asks at every connection;
// a run that fails is not kept.
func TestTheEncoderListIsReadOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake ffmpeg is a shell script")
	}
	dir := t.TempDir()
	runs, fail := filepath.Join(dir, "runs"), filepath.Join(dir, "fail")
	script := filepath.Join(dir, "ffmpeg")
	body := `#!/bin/sh
echo run >> "` + runs + `"
[ -e "` + fail + `" ] && exit 1
printf 'Encoders:\n ------\n A....D libmp3lame           MP3\n A....D aac                  AAC\n'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "run")
	}
	if err := os.WriteFile(fail, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f := &FFmpeg{Path: script}
	if f.HasEncoder("libmp3lame") {
		t.Fatal("an encoder from a run that failed")
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if !f.HasEncoder("libmp3lame") || !f.HasEncoder("aac") || f.HasEncoder("libopus") {
			t.Fatal("the list is not what ffmpeg printed")
		}
	}
	if n := count(); n != 2 {
		t.Fatalf("ffmpeg ran %d times, want 2: the failed run, then one", n)
	}
}
