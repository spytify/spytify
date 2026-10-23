package tag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

func lastFMServer(t *testing.T, handler func(method string, q map[string]string) any) *LastFM {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k, v := range r.URL.Query() {
			q[k] = v[0]
		}
		if q["api_key"] != "test-key" || q["format"] != "json" {
			t.Errorf("bad query %v", q)
		}
		_ = json.NewEncoder(w).Encode(handler(q["method"], q))
	}))
	t.Cleanup(srv.Close)
	l := NewLastFM("test-key", "medium")
	l.BaseURL = srv.URL
	return l
}

func TestLastFMTrackFound(t *testing.T) {
	l := lastFMServer(t, func(method string, q map[string]string) any {
		if method != "track.getInfo" || q["artist"] != "Artist" || q["track"] != "Song" {
			t.Errorf("unexpected call %s %v", method, q)
		}
		return map[string]any{"track": map[string]any{
			"name": "Song", "artist": map[string]any{"name": "Artist"},
			"album": map[string]any{"artist": "Artist", "title": "Album", "@attr": map[string]any{"position": "3"},
				"image": []map[string]string{{"#text": "http://img/64s.jpg", "size": "medium"}, {"#text": "http://img/300x300.jpg", "size": "extralarge"}}},
		}}
	})
	got, err := l.Lookup(context.Background(), "k", Track{Title: "Song", Performers: []string{"Artist"}})
	if err != nil {
		t.Fatal(err)
	}
	want := Track{Album: "Album", AlbumArtists: []string{"Artist"}, Track: 3, CoverURL: "http://img/300x300.jpg", Source: "lastfm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLastFMSimplifiedRetryAndSingleFallback(t *testing.T) {
	var calls []string
	l := lastFMServer(t, func(method string, q map[string]string) any {
		calls = append(calls, method+":"+q["track"]+q["album"])
		switch {
		case method == "track.getInfo" && q["track"] == "Song (Remastered)":
			return map[string]any{"error": 6, "message": "Track not found"}
		case method == "track.getInfo":
			return map[string]any{"track": map[string]any{"name": "Song", "artist": map[string]any{"name": "Artist"},
				"album": map[string]any{"artist": "Various Artists", "title": "Hits", "@attr": map[string]any{"position": "12"}}}}
		default:
			return map[string]any{"album": map[string]any{"artist": "Artist", "name": "Song",
				"image": []map[string]string{{"#text": "http://img/a.jpg", "size": "extralarge"}}}}
		}
	})
	got, err := l.Lookup(context.Background(), "k", Track{Title: "Song (Remastered)", Performers: []string{"Artist"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Album != "Song" || got.Track != 12 || got.CoverURL != "http://img/a.jpg" {
		t.Fatalf("got %+v (calls %v)", got, calls)
	}
	if want := []string{"track.getInfo:Song (Remastered)", "track.getInfo:Song", "album.getInfo:Song"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestLastFMNotFound(t *testing.T) {
	l := lastFMServer(t, func(string, map[string]string) any { return map[string]any{"error": 6, "message": "Track not found"} })
	if _, err := l.Lookup(context.Background(), "k", Track{Title: "Song", Performers: []string{"A"}}); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestBuiltinKeyIsUsedWithoutUserKey(t *testing.T) {
	l := NewLastFM("", "medium")
	found := false
	for _, k := range builtinLastFMKeys {
		found = found || k == l.Key
	}
	if !found {
		t.Fatalf("key %q is not a built-in key", l.Key)
	}
}

type memTokens map[string]string

func (m memTokens) Get(k string) (string, error) { return m[k], nil }
func (m memTokens) Set(k, v string) error        { m[k] = v; return nil }

func TestSpotifyIdentifyAndLookup(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if user != "id" || pass != "secret" || r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "rt" {
			http.Error(w, "bad", 400)
			return
		}
		fmt.Fprint(w, `{"access_token":"at","expires_in":3600}`)
	})
	mux.HandleFunc("/v1/me/player/currently-playing", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			http.Error(w, "unauthorised", 401)
			return
		}
		fmt.Fprint(w, `{"currently_playing_type":"track","item":{"name":"Song - Live","track_number":2,"disc_number":1,
			"artists":[{"id":"a1","name":"Artist"},{"id":"a2","name":"Guest"}],
			"album":{"name":"Album","release_date":"2019-05-01","artists":[{"name":"Artist"}],
			"images":[{"url":"http://i/640","width":640},{"url":"http://i/300","width":300},{"url":"http://i/64","width":64}]}}}`)
	})
	mux.HandleFunc("/v1/artists/a1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"genres":["synthpop","electro"]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := NewSpotify("id", "secret", "http://127.0.0.1:7812/callback", "medium", memTokens{spotifyTokenKey: "rt"})
	s.AuthBase, s.APIBase = srv.URL, srv.URL+"/v1"
	s.Identify(context.Background(), "track-1")
	got, err := s.Lookup(context.Background(), "track-1", Track{Title: "Song - Live"})
	if err != nil {
		t.Fatal(err)
	}
	want := Track{Title: "Song - Live", Performers: []string{"Artist", "Guest"}, AlbumArtists: []string{"Artist"}, Album: "Album",
		Genres: []string{"synthpop", "electro"}, Disc: 1, Track: 2, Year: 2019, CoverURL: "http://i/300", Source: "spotify_api"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if _, err := s.Lookup(context.Background(), "track-1", Track{Title: "Song - Live"}); err == nil {
		t.Fatal("cache entry reused")
	}
}

func TestSpotifyNotConnected(t *testing.T) {
	s := NewSpotify("id", "secret", "http://127.0.0.1:7812/callback", "medium", memTokens{})
	s.Identify(context.Background(), "k")
	if _, err := s.Lookup(context.Background(), "k", Track{}); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

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

func TestWriterTagsFLACAndMP3(t *testing.T) {
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
	track := Track{Title: "Song", Subtitle: "Live", Performers: []string{"Artist", "Guest"}, AlbumArtists: []string{"Artist"},
		Album: "Album", Genres: []string{"pop"}, Track: 2, Disc: 1, Year: 2001, Cover: cover.Bytes(), CoverMIME: "image/png"}

	for _, format := range []string{"flac", "mp3"} {
		codec := map[string]string{"flac": "flac", "mp3": "libmp3lame"}[format]
		out := filepath.Join(dir, "out."+format)
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
		for _, want := range []string{`"title": "song"`, `"artist": "artist, guest"`, `"album": "album"`, `"video"`, "2001"} {
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
