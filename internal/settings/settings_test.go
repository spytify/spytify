package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spytify", "config.toml")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestEveryDefaultIsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Table {
		if seen[d.Key] {
			t.Errorf("%s: duplicate key", d.Key)
		}
		seen[d.Key] = true
		got, err := d.Coerce(d.Default)
		if err != nil {
			t.Errorf("%s: default %v rejected: %v", d.Key, d.Default, err)
			continue
		}
		if got != d.Default {
			t.Errorf("%s: default %v coerces to %v", d.Key, d.Default, got)
		}
	}
}

// A choice list names every value of its setting, in order.
func TestChoicesNameEveryValue(t *testing.T) {
	for _, d := range Table {
		if d.Choices != nil && len(d.Choices) != len(d.Enum) {
			t.Errorf("%s: %d choices for %d values", d.Key, len(d.Choices), len(d.Enum))
		}
	}
}

func TestSetPersistsAndReloads(t *testing.T) {
	s, path := openTemp(t)
	applied, rejected, changed, err := s.Set(map[string]any{
		"encode.format":        "flac",
		"recorder.min_seconds": 45.0, // JSON numbers arrive as float64
		"recorder.silence_db":  "-55",
		"output.dir":           "~/Music/Rec",
		"recorder.duplicates":  "suffix",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 0 {
		t.Fatalf("rejected = %v", rejected)
	}
	if len(applied) != 5 || changed["encode.format"] != "flac" {
		t.Fatalf("applied = %v changed = %v", applied, changed)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no mode bits; the folder's ACL protects the file.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v", info.Mode().Perm())
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v := again.Values()
	if v.String("encode.format") != "flac" || v.Int("recorder.min_seconds") != 45 || v.Float("recorder.silence_db") != -55 {
		t.Fatalf("reloaded values wrong: %v %v %v", v.String("encode.format"), v.Int("recorder.min_seconds"), v.Float("recorder.silence_db"))
	}
	if !strings.HasSuffix(v.Path("output.dir"), filepath.Join("Music", "Rec")) || strings.HasPrefix(v.Path("output.dir"), "~") {
		t.Fatalf("Path(output.dir) = %q", v.Path("output.dir"))
	}
	if len(again.Warnings()) != 0 {
		t.Fatalf("warnings = %v", again.Warnings())
	}
}

func TestSetRejectsInvalidAndUnknown(t *testing.T) {
	s, _ := openTemp(t)
	cases := map[string]any{
		"encode.format":          "ogg",
		"encode.bitrate_kbps":    192,
		"recorder.min_seconds":   1.5,
		"output.dir":             "relative/dir",
		"output.template":        "{artist}/{title}",
		"recorder.session_timer": "forever",
		"no.such.key":            true,
		"app.log_level":          "debug",
	}
	applied, rejected, _, err := s.Set(cases)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %v", applied)
	}
	for k := range cases {
		if _, ok := rejected[k]; !ok {
			t.Errorf("%s was not rejected", k)
		}
	}
}

func TestTemplatePlaceholders(t *testing.T) {
	d, _ := Lookup("output.template")
	if _, err := d.Coerce("{track} {artist} - {title} ({year})"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Coerce("{artist} - {genre}"); err == nil {
		t.Fatal("unknown placeholder accepted")
	}
	if _, err := d.Coerce("no placeholder"); err == nil {
		t.Fatal("template without placeholder accepted")
	}
}

func TestResetPrefix(t *testing.T) {
	s, _ := openTemp(t)
	if _, _, _, err := s.Set(map[string]any{"output.underscores": true, "encode.format": "wav"}); err != nil {
		t.Fatal(err)
	}
	changed, err := s.Reset("output")
	if err != nil {
		t.Fatal(err)
	}
	if changed["output.underscores"] != false || len(changed) != 1 {
		t.Fatalf("changed = %v", changed)
	}
	v := s.Values()
	if v.Bool("output.underscores") || v.String("encode.format") != "wav" {
		t.Fatal("reset touched the wrong keys")
	}
}

func TestOverrideIsNotPersisted(t *testing.T) {
	s, path := openTemp(t)
	if rej := s.Override(map[string]any{"encode.format": "wav", "recorder.min_seconds": "1"}); len(rej) != 0 {
		t.Fatal(rej)
	}
	if s.Values().String("encode.format") != "wav" {
		t.Fatal("override not applied")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("override wrote config.toml")
	}
}

func TestLoadWarnsAndKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	music := filepath.Join(dir, "music")
	// A TOML literal string: Windows paths keep their backslashes.
	content := "[encode]\nformat = \"ogg\"\n[output]\ndir = '" + music + "'\nnope = 1\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Values().String("encode.format") != "flac" || s.Values().Path("output.dir") != music {
		t.Fatal("bad load")
	}
	if len(s.Warnings()) != 2 {
		t.Fatalf("warnings = %v", s.Warnings())
	}
}

func TestDuration(t *testing.T) {
	v := Defaults().With("recorder.session_timer", "90m")
	if v.Duration("recorder.session_timer") != 90*time.Minute {
		t.Fatal(v.Duration("recorder.session_timer"))
	}
	if Defaults().Duration("recorder.session_timer") != 0 {
		t.Fatal("default timer not off")
	}
}

func TestHomeRelativePaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	for in, want := range map[string]string{"~": "~", "~/Music//Rec/": "~/Music/Rec", "~/a/../b": "~/b"} {
		if got, err := cleanPath(in); err != nil || got != want {
			t.Errorf("cleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if got := ExpandHome("~/Music/Rec"); got != filepath.Join(home, "Music", "Rec") {
		t.Errorf("ExpandHome = %q", got)
	}
	if runtime.GOOS == "windows" {
		if got, err := cleanPath(`~\Music\Rec`); err != nil || got != "~/Music/Rec" {
			t.Errorf(`cleanPath(~\Music\Rec) = %q, %v`, got, err)
		}
	}
}
