//go:build darwin

package target

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundleIDFromTheInstalledApp(t *testing.T) {
	dir := t.TempDir()
	contents := filepath.Join(dir, "Player.app", "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>org.example.player</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func(f func() []string) { appDirs = f }(appDirs)
	appDirs = func() []string { return []string{dir} }
	for _, name := range []string{"player", "Player.app", "PLAYER"} {
		if got := bundleIDOf(name); got != "org.example.player" {
			t.Errorf("bundleIDOf(%q) = %q", name, got)
		}
	}
	if got := bundleIDOf("other"); got != "" {
		t.Errorf("bundleIDOf(other) = %q", got)
	}
	t.Setenv("SPYTIFY_TARGET_PROCESS", "player")
	t.Setenv("SPYTIFY_TARGET_BUNDLE_ID", "")
	if got := Load().Target.BundleID; got != "org.example.player" {
		t.Errorf("Load found bundle id %q", got)
	}
}
