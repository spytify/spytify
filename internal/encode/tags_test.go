package encode

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// The tags come back as written, the characters ffmpeg escapes included.
func TestTagsComeOutAsWritten(t *testing.T) {
	f := ffmpegOrSkip(t)
	want := map[string]string{"title": "Midnight City = A; B # C \\ D", "artist": "M83, The Hurry Up Orchestra"}
	for _, ext := range []string{"flac", "mp3", "m4a"} {
		path := filepath.Join(t.TempDir(), "track."+ext)
		args := []string{"-v", "error", "-nostdin", "-f", "lavfi", "-t", "0.5", "-i", "anullsrc"}
		for k, v := range want {
			args = append(args, "-metadata", k+"="+v)
		}
		if b, err := exec.Command(f.Path, append(args, path)...).CombinedOutput(); err != nil {
			t.Fatalf("make the %s: %v: %s", ext, err, b)
		}
		got, err := f.Tags(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s %q, want %q", ext, k, got[k], v)
			}
		}
	}
	if _, err := f.Tags(context.Background(), filepath.Join(t.TempDir(), "absent.flac")); err == nil {
		t.Error("a missing file has tags")
	}
}

// A value goes on past an escaped new line, and only the file's own tags
// count, not a stream's.
func TestParseMetadata(t *testing.T) {
	got := parseMetadata(";FFMETADATA1\nTITLE=Reunion\nartist=M83\\\nand more\n# a comment=no\n\n[STREAM]\ntitle=Stream\n")
	if len(got) != 2 || got["title"] != "Reunion" || got["artist"] != "M83\nand more" {
		t.Errorf("parsed %q", got)
	}
}
