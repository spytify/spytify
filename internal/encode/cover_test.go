package encode

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// withCover writes a short FLAC with picture attached, as the tag writer does.
func withCover(t *testing.T, f *FFmpeg, picture []byte) string {
	t.Helper()
	dir := t.TempDir()
	cover := filepath.Join(dir, "cover.png")
	if err := os.WriteFile(cover, picture, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "track.flac")
	cmd := exec.Command(f.Path, "-v", "error", "-nostdin", "-f", "lavfi", "-t", "0.5", "-i", "anullsrc=r=44100:cl=stereo", "-i", cover,
		"-map", "0:a", "-map", "1:v", "-c:a", "flac", "-c:v", "copy", "-disposition:v", "attached_pic", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make the FLAC: %v: %s", err, b)
	}
	return out
}

func TestCoverComesOutAsStored(t *testing.T) {
	f := ffmpegOrSkip(t)
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	got, err := f.Cover(context.Background(), withCover(t, f, picture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, picture.Bytes()) {
		t.Fatalf("cover is %d bytes, stored %d", len(got), picture.Len())
	}

	bare := filepath.Join(t.TempDir(), "bare.flac")
	if b, err := exec.Command(f.Path, "-v", "error", "-nostdin", "-f", "lavfi", "-t", "0.5", "-i", "anullsrc", bare).CombinedOutput(); err != nil {
		t.Fatalf("make the bare FLAC: %v: %s", err, b)
	}
	if _, err := f.Cover(context.Background(), bare); err == nil {
		t.Fatal("a file without art gave a cover")
	}
}
