package tag

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

func jpegCover(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func coverSize(t *testing.T, data []byte) (int, int, string) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height, format
}

func TestCoverIsResizedToItsSetting(t *testing.T) {
	big := jpegCover(t, 1400, 1000)
	for size, want := range map[string][2]int{"small": {300, 214}, "medium": {640, 457}, "large": {1400, 1000}, "": {640, 457}} {
		data, mime := fitCover(big, "image/jpeg", size)
		w, h, format := coverSize(t, data)
		if w != want[0] || h != want[1] || format != "jpeg" || mime != "image/jpeg" {
			t.Errorf("%q: %dx%d %s (%s), want %dx%d jpeg", size, w, h, format, mime, want[0], want[1])
		}
	}
	if data, _ := fitCover(big, "image/jpeg", "large"); !bytes.Equal(data, big) {
		t.Error("large re-encoded the cover")
	}

	small := jpegCover(t, 200, 200)
	for _, size := range []string{"small", "medium", "large"} {
		if data, _ := fitCover(small, "image/jpeg", size); !bytes.Equal(data, small) {
			t.Errorf("%s: a 200 px cover changed", size)
		}
	}

	var p bytes.Buffer
	png.Encode(&p, image.NewRGBA(image.Rect(0, 0, 900, 900)))
	data, mime := fitCover(p.Bytes(), "image/png", "small")
	if w, h, format := coverSize(t, data); w != 300 || h != 300 || format != "png" || mime != "image/png" {
		t.Errorf("png: %dx%d %s (%s)", w, h, format, mime)
	}

	junk := []byte("not an image")
	if data, mime := fitCover(junk, "image/jpeg", "small"); !bytes.Equal(data, junk) || mime != "image/jpeg" {
		t.Error("a cover that does not decode is not left as it came")
	}
}

// The cover in the file has the size the setting asks for.
func TestTheEmbeddedCoverHasItsSize(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	f := pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
	in := filepath.Join(dir, "in.wav")
	w, _ := pcm.CreateWAV(in, f)
	w.Write(pcm.Synth{Seed: 1}.Render(f, 22050))
	w.Close()
	cover := jpegCover(t, 1400, 1400)
	for size, want := range map[string]string{"small": "300x300", "medium": "640x640", "large": "1400x1400"} {
		out := filepath.Join(dir, size+".mp3")
		if b, err := exec.Command(ffmpeg, "-loglevel", "error", "-i", in, "-c:a", "libmp3lame", out).CombinedOutput(); err != nil {
			t.Skipf("cannot encode mp3: %v %s", err, b)
		}
		track := Track{Title: "Song", Performers: []string{"Artist"}, Cover: cover, CoverMIME: "image/jpeg"}
		if err := (Writer{FFmpeg: ffmpeg}).Write(context.Background(), out, "mp3", track, Options{Cover: true, CoverSize: size}); err != nil {
			t.Fatal(err)
		}
		probe, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", out).Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(probe)); got != want {
			t.Errorf("%s: cover %s, want %s", size, got, want)
		}
		os.Remove(out)
	}
}
