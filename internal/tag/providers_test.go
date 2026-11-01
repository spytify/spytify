package tag

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

func TestFetchCoverCapsSize(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			w.Write(bytes.Repeat([]byte{0xFF}, MaxCoverBytes+10))
			return
		}
		w.Write(buf.Bytes())
	}))
	defer srv.Close()
	data, mime, err := FetchCover(context.Background(), nil, srv.URL+"/cover")
	if err != nil || mime != "image/png" || len(data) != buf.Len() {
		t.Fatalf("cover: %v %s %d", err, mime, len(data))
	}
	if _, _, err := FetchCover(context.Background(), nil, srv.URL+"/big"); err == nil {
		t.Fatal("oversized cover accepted")
	}
}

// Every format gets its tags back from ffprobe, the track and disc numbers
// the player gave included.
func TestWriterTagsEveryFormat(t *testing.T) {
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

	var cover bytes.Buffer
	png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	track := Track{Title: "Song", Subtitle: "Live", SubtitleSep: SepParenthesis, Performers: []string{"Artist", "Guest"}, AlbumArtists: []string{"Artist"},
		Album: "Album", Track: 2, Disc: 1, Year: 2001, Cover: cover.Bytes(), CoverMIME: "image/png"}

	for _, format := range []string{"flac", "mp3", "aac"} {
		codec := map[string]string{"flac": "flac", "mp3": "libmp3lame", "aac": "aac"}[format]
		out := filepath.Join(dir, "out."+map[string]string{"flac": "flac", "mp3": "mp3", "aac": "m4a"}[format])
		if b, err := exec.Command(ffmpeg, "-loglevel", "error", "-i", in, "-c:a", codec, out).CombinedOutput(); err != nil {
			t.Skipf("cannot encode %s: %v %s", format, err, b)
		}
		if err := (Writer{FFmpeg: ffmpeg}).Write(context.Background(), out, format, track, Options{Cover: true}); err != nil {
			t.Fatal(err)
		}
		probe, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format_tags:stream=codec_type", "-of", "json", out).Output()
		if err != nil {
			t.Fatal(err)
		}
		s := strings.ToLower(string(probe))
		title := map[string]string{"flac": `"title": "song"`, "mp3": `"title": "song"`, "aac": `"title": "song (live)"`}[format]
		sub := map[string]string{"flac": `"subtitle": "live"`, "mp3": `"tit3": "live"`, "aac": `"artist"`}[format]
		for _, want := range []string{title, sub, `"artist": "artist, guest"`, `"album": "album"`, `"track": "2`, `"disc": "1`, `"video"`, "2001"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: %s missing from %s", format, want, s)
			}
		}
	}

	wav := filepath.Join(dir, "x.wav")
	os.WriteFile(wav, []byte("RIFF"), 0o600)
	if err := (Writer{FFmpeg: ffmpeg}).Write(context.Background(), wav, "wav", track, Options{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wav + ".json")
	if err != nil || !strings.Contains(string(b), `"album": "Album"`) {
		t.Fatalf("sidecar: %v %s", err, b)
	}
}

// Windows covers come as file:///C:/... from the media session poller.
func TestFilePathOfAFileURL(t *testing.T) {
	for _, c := range []struct{ path, goos, want string }{
		{"/C:/Users/u/AppData/Local/Temp/spytify-cover-1", "windows", `C:\Users\u\AppData\Local\Temp\spytify-cover-1`},
		{"/tmp/cover.png", "darwin", "/tmp/cover.png"},
		{"/home/u/cover.jpg", "linux", "/home/u/cover.jpg"},
	} {
		if got := filePath(c.path, c.goos); got != c.want {
			t.Errorf("filePath(%q, %s) = %q, want %q", c.path, c.goos, got, c.want)
		}
	}
}
