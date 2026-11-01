package naming

import (
	"os"
	"path/filepath"
	"testing"
)

// The cases below are ported from v1's FileManagerTests. v1 built
// "Artist - Title - Live" from the window title when files went to the root,
// and "Title - Live" inside artist/album folders; v2 says the same through
// its template, so the grouped cases use "{title}".
var base = Track{Artists: []string{"Artist"}, Title: "Title - Live", Album: "Single"}

func opts() Options {
	return Options{GroupBy: "none", Prefix: "none", Template: "{artist} - {title}", CounterWidth: 3, Extension: "mp3"}
}

func full(p Plan, o Options, counter int64, track int) string {
	return filepath.ToSlash(filepath.Join(p.Dir, FileName(p, o, counter, track)))
}

func TestBuildFileName(t *testing.T) {
	o := opts()
	if got := full(Build(base, o), o, 0, 0); got != "Artist - Title - Live.mp3" {
		t.Fatalf("got %q", got)
	}
	o.Extension = "wav"
	if got := full(Build(base, o), o, 0, 0); got != "Artist - Title - Live.wav" {
		t.Fatalf("wav: got %q", got)
	}
}

func TestBuildFileNameGroupByFolders(t *testing.T) {
	o := opts()
	o.GroupBy, o.Template = "artist/album", "{title}"
	if got := full(Build(base, o), o, 0, 0); got != "Artist/Single/Title - Live.mp3" {
		t.Fatalf("got %q", got)
	}
	untitled := base
	untitled.Album = ""
	if got := full(Build(untitled, o), o, 0, 0); got != "Artist/Untitled/Title - Live.mp3" {
		t.Fatalf("untitled album: got %q", got)
	}
}

func TestCounterPrefix(t *testing.T) {
	o := opts()
	o.Prefix, o.Underscores = "counter", true
	cases := map[int64]string{0: "000_Artist_-_Title_-_Live.mp3", 100: "100_Artist_-_Title_-_Live.mp3", 12345: "999_Artist_-_Title_-_Live.mp3"}
	for counter, want := range cases {
		if got := full(Build(base, o), o, counter, 0); got != want {
			t.Errorf("counter %d: got %q, want %q", counter, got, want)
		}
	}
	o.CounterWidth = 5
	if got := full(Build(base, o), o, 12345, 0); got != "12345_Artist_-_Title_-_Live.mp3" {
		t.Errorf("width 5: got %q", got)
	}
}

// The settings' example follows every option, folders included.
func TestExample(t *testing.T) {
	o := Options{Template: "{artist} - {title}", Prefix: "none", GroupBy: "none", Extension: "mp3"}
	if got := Example(o, 1); got != "Neon Harbor - Café Lumière.mp3" {
		t.Errorf("plain %q", got)
	}
	o.ASCII = true
	if got := Example(o, 1); got != "Neon Harbor - Cafe Lumiere.mp3" {
		t.Errorf("without diacritics %q", got)
	}
	o.ASCII, o.Underscores, o.Prefix, o.CounterWidth = false, true, "counter", 3
	if got := Example(o, 7); got != "007_Neon_Harbor_-_Café_Lumière.mp3" {
		t.Errorf("underscores and counter %q", got)
	}
	o.GroupBy, o.Prefix = "artist/album", "album_track"
	if got, want := Example(o, 1), filepath.Join("Neon_Harbor", "Harbor_Lights_(2024)", "04_Neon_Harbor_-_Café_Lumière.mp3"); got != want {
		t.Errorf("grouped %q, want %q", got, want)
	}
	// The sample has a guest, so the example shows what all artists do.
	o.GroupBy = "artists/album"
	if got, want := Example(o, 1), filepath.Join("Neon_Harbor,_Lumen", "Harbor_Lights_(2024)", "04_Neon_Harbor_-_Café_Lumière.mp3"); got != want {
		t.Errorf("grouped by all artists %q, want %q", got, want)
	}
	o = Options{Template: "{track} {artists} - {title}", Prefix: "none", GroupBy: "none", Extension: "mp3"}
	if got := Example(o, 1); got != "04 Neon Harbor, Lumen - Café Lumière.mp3" {
		t.Errorf("all artists %q", got)
	}
}

func TestAlbumTrackPrefix(t *testing.T) {
	o := opts()
	o.Prefix, o.Underscores = "album_track", true
	if got := full(Build(base, o), o, 0, 0); got != "Artist_-_Title_-_Live.mp3" {
		t.Errorf("no position: got %q", got)
	}
	if got := full(Build(base, o), o, 0, 1); got != "01_Artist_-_Title_-_Live.mp3" {
		t.Errorf("position 1: got %q", got)
	}
	o.GroupBy, o.Template = "artist/album", "{title}"
	if got := full(Build(base, o), o, 0, 1); got != "Artist/Single/01_Title_-_Live.mp3" {
		t.Errorf("grouped: got %q", got)
	}
}

func TestUnknownTrackGoesToRoot(t *testing.T) {
	o := opts()
	o.GroupBy = "artist/album"
	podcast := Track{Artists: []string{"123 Podcast"}, Unknown: true}
	if got := full(Build(podcast, o), o, 0, 0); got != "123 Podcast.mp3" {
		t.Fatalf("got %q", got)
	}
}

func TestBadlyFormattedNames(t *testing.T) {
	o := opts()
	o.GroupBy = "artist/album"
	if got := full(Build(Track{Artists: []string{`\`}, Album: "/?", Unknown: true}, o), o, 0, 0); got != "INVALID.mp3" {
		t.Errorf("invalid unknown: got %q", got)
	}
	o.Template = "{title}"
	if got := full(Build(Track{Artists: []string{"?"}, Title: "? - Live", Album: "Single"}, o), o, 0, 0); got != "INVALID/Single/- Live.mp3" {
		t.Errorf("invalid artist and title: got %q", got)
	}
}

func TestArtistAndAlbumFolders(t *testing.T) {
	o := opts()
	o.GroupBy, o.Underscores, o.Template = "artist/album", true, "{title}"
	tr := Track{Artists: []string{"Artist DJ"}, Title: "Title", Album: "Single", Year: 2020}
	if got := Build(tr, o).Dir; filepath.ToSlash(got) != "Artist_DJ/Single_(2020)" {
		t.Fatalf("got %q", got)
	}
	// The main artist: the first album artist, else the first performer.
	tr.AlbumArtists = []string{"Band", "Other"}
	if got := Build(tr, o).Dir; filepath.ToSlash(got) != "Band/Single_(2020)" {
		t.Fatalf("album artists: got %q", got)
	}
	tr.AlbumArtists = nil
	tr.Artists = []string{"Artist", "Guest", "Artist"}
	if got := Build(tr, o).Dir; filepath.ToSlash(got) != "Artist/Single_(2020)" {
		t.Fatalf("performers: got %q", got)
	}
	o.GroupBy = "artists/album"
	if got := Build(tr, o).Dir; filepath.ToSlash(got) != "Artist,_Guest/Single_(2020)" {
		t.Fatalf("all performers: got %q", got)
	}
}

func TestTemplateFields(t *testing.T) {
	o := opts()
	o.Template = "{track} {artist} - {title} ({year})"
	tr := Track{Artists: []string{"A"}, Title: "T", TrackNumber: 3, Year: 1999}
	if got := Build(tr, o).Base; got != "03 A - T (1999)" {
		t.Fatalf("got %q", got)
	}
	tr.Year, tr.TrackNumber = 0, 0
	if got := Build(tr, o).Base; got != "A - T" {
		t.Fatalf("empty fields: got %q", got)
	}
}

// {album_artist} is the album's artists, as {artist} was; {artists} is every
// artist on the track, guests included.
func TestArtistFields(t *testing.T) {
	o := opts()
	o.ArtistSeparator = "; "
	tr := Track{Artists: []string{"A", "Guest", "A"}, AlbumArtists: []string{"Band", "Other"}, Title: "T"}
	for tmpl, want := range map[string]string{
		"{album_artist} - {title}": "Band; Other - T",
		"{artist} - {title}":       "Band; Other - T",
		"{artists} - {title}":      "A; Guest - T",
	} {
		o.Template = tmpl
		if got := Build(tr, o).Base; got != want {
			t.Errorf("%s: got %q, want %q", tmpl, got, want)
		}
	}
	tr.AlbumArtists = nil
	o.Template = "{album_artist} - {title}"
	if got := Build(tr, o).Base; got != "A; Guest - T" {
		t.Errorf("no album artist: got %q", got)
	}
}

func TestASCIIAndLength(t *testing.T) {
	o := opts()
	tr := Track{Artists: []string{"Beyoncé"}, Title: "Café del Mar"}
	if got := Build(tr, o).Base; got != "Beyoncé - Café del Mar" {
		t.Fatalf("unicode kept: got %q", got)
	}
	o.ASCII = true
	if got := Build(tr, o).Base; got != "Beyonce - Cafe del Mar" {
		t.Fatalf("ascii: got %q", got)
	}
	long := Track{Artists: []string{"A"}, Title: string(make([]rune, 0)) + repeat("é", 300)}
	if got := Build(long, opts()).Base; len(got) > maxComponent {
		t.Fatalf("length %d over %d", len(got), maxComponent)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestDuplicateSuffix(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Artist - Title - Live.mp3", "Artist - Title - Live 2.mp3", "Artist - Title - Live 3.mp3", "Artist - Title - Live 5.mp3"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte{1}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if got := FreeSuffix(dir, "Artist - Title - Live.mp3", false, exists); got != "Artist - Title - Live 4.mp3" {
		t.Fatalf("got %q", got)
	}
	if got := FreeSuffix(dir, "Other.mp3", false, exists); got != "Other.mp3" {
		t.Fatalf("free name: got %q", got)
	}
}

func TestRecorded(t *testing.T) {
	root := t.TempDir()
	o := opts()
	p := Build(base, o)
	if Recorded(root, p, o) {
		t.Fatal("found in an empty folder")
	}
	if err := os.WriteFile(filepath.Join(root, "007 Artist - Title - Live.mp3"), []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if !Recorded(root, p, o) {
		t.Fatal("counter-prefixed copy not found")
	}
}
