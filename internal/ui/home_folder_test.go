package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
)

// The Record screen and the Library write a folder in the home folder from
// ~, as Settings writes the default one: the rest of the path names no one.
// A folder the user chose is still blurred in a report's picture, the
// default one is not.
func TestTheOutputFolderIsWrittenFromTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	rec := a.screens[tabRecord].(*recordScreen)
	lib := a.screens[tabLibrary].(*libraryScreen)
	for _, c := range []struct {
		dir, shown string
		blurred    bool
	}{
		{"~/Spytify", filepath.Join("~", "Spytify"), false},
		{filepath.Join(home, "Mixtapes"), filepath.Join("~", "Mixtapes"), true},
	} {
		values := defaults()
		values[trackerKey] = true
		values["output.dir"] = c.dir
		m.SetSettings(values)

		a.showTab(tabRecord)
		a.render()
		if text := rec.idleText.Text; !strings.Contains(text, c.shown+".") || strings.Contains(text, home) {
			t.Errorf("%s: the Record screen says %q", c.dir, text)
		}
		if img := a.picture(); img == nil || blurs(a, img, rec.idleText) != c.blurred {
			t.Errorf("%s: the Record screen's text blurred %v, want %v", c.dir, !c.blurred, c.blurred)
		}
		snapshot(t, a, "home-record-"+filepath.Base(c.dir))

		a.showTab(tabLibrary)
		a.render()
		lib.scan()
		for _, text := range []string{lib.hint.Text, lib.count.String()} {
			if !strings.Contains(text, c.shown) || strings.Contains(text, home) {
				t.Errorf("%s: the empty Library says %q", c.dir, text)
			}
		}
		if img := a.picture(); img == nil || blurs(a, img, lib.hint) != c.blurred {
			t.Errorf("%s: the Library's text blurred %v, want %v", c.dir, !c.blurred, c.blurred)
		}
		snapshot(t, a, "home-library-"+filepath.Base(c.dir))
	}
}
