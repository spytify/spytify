package encode

import (
	"os"
	"path/filepath"
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
