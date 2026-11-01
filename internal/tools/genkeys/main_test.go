package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/settings"
)

// keys_gen.go is what go generate writes from the table: a setting added,
// renamed or removed without regenerating it fails here.
func TestTheKeysAreUpToDate(t *testing.T) {
	want, err := render(settings.Table)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "settings", "keys_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatal("internal/settings/keys_gen.go is out of date: run go generate ./internal/settings")
	}
}

func TestKeysThatMakeOneNameAreRefused(t *testing.T) {
	if _, err := render([]settings.Def{{Key: "output.dir_name"}, {Key: "output_dir.name"}}); err == nil {
		t.Error("output.dir_name and output_dir.name are both OutputDirName, and render took them")
	}
	if _, err := render([]settings.Def{{Key: "output.dir-name"}}); err == nil {
		t.Error("render took output.dir-name, which makes no Go name")
	}
}

func TestTheNameComesFromTheKey(t *testing.T) {
	for key, want := range map[string]string{
		"recorder.trim_silence": "RecorderTrimSilence",
		"app.theme":             "AppTheme",
		"encode.bitrate_kbps":   "EncodeBitrateKbps",
	} {
		if got := identifier(key); got != want {
			t.Errorf("%s is named %s, want %s", key, got, want)
		}
	}
}
