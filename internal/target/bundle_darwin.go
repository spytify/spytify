//go:build darwin

package target

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spytify/spytify/internal/proc"
)

// appDirs are where installed apps live; tests replace them.
var appDirs = func() []string {
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// bundleIDOf reads the bundle id of the installed app called name (with or
// without ".app", any case), or "" when there is none.
func bundleIDOf(name string) string {
	want := normalizeName(name) + ".app"
	for _, dir := range appDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.ToLower(e.Name()) != want {
				continue
			}
			plist := filepath.Join(dir, e.Name(), "Contents", "Info.plist")
			out, err := proc.Command("plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", plist).Output()
			if err == nil {
				return strings.TrimSpace(string(out))
			}
		}
	}
	return ""
}
