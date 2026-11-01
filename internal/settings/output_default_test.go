package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain gives the tests a home of their own: this computer's
// ~/Music/Spytify would keep the former default output folder.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "spytify-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// The output folder is ~/Spytify by default, out of the Music folder that
// Windows often syncs or protects. An install that recorded in
// ~/Music/Spytify keeps it: the new default would hide its tracks from
// the Library.
func TestTheFormerOutputFolderIsKeptWhereItHoldsRecordings(t *testing.T) {
	// Up to 2.0.0-alpha.4, config.toml held every setting, defaults too.
	const full = "[app]\ntheme = 'system'\n[output]\ndir = '~/Music/Spytify'\n"
	for _, tc := range []struct {
		name   string
		made   []string
		config string
		want   string
	}{
		{"a new install", nil, "", "~/Spytify"},
		{"an install that recorded in Music", []string{"Music/Spytify"}, "", "~/Music/Spytify"},
		{"an install whose folder was never made", []string{"Music"}, "", "~/Spytify"},
		{"an install that made the new folder", []string{"Music/Spytify", "Spytify"}, "", "~/Spytify"},
		{"a folder chosen", []string{"Music/Spytify"}, "[output]\ndir = '~/Mixes'\n", "~/Mixes"},
		{"a file holding every default", nil, full, "~/Spytify"},
		{"a file holding every default, its folder made", []string{"Music/Spytify"}, full, "~/Music/Spytify"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			for _, d := range tc.made {
				if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(d)), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			if tc.config != "" {
				if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Values().String(OutputDir); got != tc.want {
				t.Errorf("output.dir %q, want %q", got, tc.want)
			}
		})
	}
}

// Once a setting is saved, the former folder is written down: it stays,
// even after the new default folder is made.
func TestTheFormerOutputFolderIsSavedWithTheSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, "Music", "Spytify"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Set(map[string]any{"app.theme": "dark"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "~/Music/Spytify") {
		t.Errorf("config.toml:\n%s", b)
	}
	if err := os.MkdirAll(filepath.Join(home, "Spytify"), 0o755); err != nil {
		t.Fatal(err)
	}
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	if got := s.Values().String(OutputDir); got != "~/Music/Spytify" {
		t.Errorf("output.dir %q once saved", got)
	}
}
