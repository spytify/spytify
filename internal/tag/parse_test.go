package tag

import (
	"reflect"
	"testing"

	"github.com/spytify/spytify/internal/meta"
)

// Ported from v1's window-title and track tests. The advert and idle
// window titles of v1 have no equivalent: v2 reads the playback status and
// the item id instead (gap G9).

func TestSplitArtistTitle(t *testing.T) {
	cases := []struct {
		in            string
		artist, title string
		ok            bool
	}{
		{"Artist Name - Song Title", "Artist Name", "Song Title", true},
		{"Artist Name - Song Title - Live", "Artist Name", "Song Title - Live", true},
		{"Sponsored Session", "", "", false},
		{"#1337: DAILY NEWS", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		a, ti, ok := SplitArtistTitle(c.in)
		if a != c.artist || ti != c.title || ok != c.ok {
			t.Errorf("SplitArtistTitle(%q) = %q, %q, %v", c.in, a, ti, ok)
		}
	}
}

func TestSplitTitle(t *testing.T) {
	cases := []struct {
		in, base, ext string
		sep           Separator
	}{
		{"Song Title", "Song Title", "", SepNone},
		{"Song Title - Live", "Song Title", "Live", SepDash},
		{"Title (Featuring Other)", "Title", "Featuring Other", SepParenthesis},
		{"Title (Featuring Other) - Remastered", "Title (Featuring Other)", "Remastered", SepDash},
		{"Song (feat. Lumen) (Live)", "Song", "feat. Lumen · Live", SepParenthesis},
		{"Song (Live (2011))", "Song", "Live (2011)", SepParenthesis},
		{"Song (Part 1) Reprise", "Song (Part 1) Reprise", "", SepNone},
		{"(Not) Alone", "(Not) Alone", "", SepNone},
		{"", "", "", SepNone},
	}
	for _, c := range cases {
		b, e, s := SplitTitle(c.in)
		if b != c.base || e != c.ext || s != c.sep {
			t.Errorf("SplitTitle(%q) = %q, %q, %v", c.in, b, e, s)
		}
	}
}

func TestJoinTitle(t *testing.T) {
	cases := []struct {
		title, ext string
		sep        Separator
		want       string
	}{
		{"", "", SepNone, ""},
		{"Title", "Must Not Return", SepNone, "Title"},
		{"Title", "Remastered", SepDash, "Title - Remastered"},
		{"Title", "Featuring Other", SepParenthesis, "Title (Featuring Other)"},
		{"Song", "feat. Lumen · Live", SepParenthesis, "Song (feat. Lumen) (Live)"},
		{"Title (Featuring Other)", "Remastered", SepDash, "Title (Featuring Other) - Remastered"},
	}
	for _, c := range cases {
		if got := JoinTitle(c.title, c.ext, c.sep); got != c.want {
			t.Errorf("JoinTitle(%q, %q) = %q, want %q", c.title, c.ext, got, c.want)
		}
	}
}

func TestPerformers(t *testing.T) {
	cases := map[string][]string{
		"Song (feat. A & B)":   {"A", "B"},
		"Song (with A, B & C)": {"A", "B", "C"},
		"Artist - Song (Live)": nil,
		"Song":                 nil,
	}
	for in, want := range cases {
		if got := Performers(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Performers(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSimplifyTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Song (Remastered 2011)": "Song",
		"Song - Live at Home":    "Song",
		"Song":                   "Song",
	} {
		if got := SimplifyTitle(in); got != want {
			t.Errorf("SimplifyTitle(%q) = %q", in, got)
		}
	}
}

func TestFromLocal(t *testing.T) {
	info := meta.TrackInfo{Title: "Song (feat. Guest) - Live", Artists: []string{"Artist"}, Album: "Album", TrackNumber: 4}
	got := FromLocal(info, Options{})
	if got.Title != info.Title || !reflect.DeepEqual(got.Performers, []string{"Artist", "Guest"}) ||
		!reflect.DeepEqual(got.AlbumArtists, []string{"Artist"}) || got.Track != 4 || got.Source != "local" {
		t.Fatalf("FromLocal = %+v", got)
	}
	sub := FromLocal(info, Options{ExtraToSubtitle: true})
	if sub.Title != "Song (feat. Guest)" || sub.Subtitle != "Live" {
		t.Fatalf("subtitle: %+v", sub)
	}
	parsed := FromLocal(meta.TrackInfo{Title: "Artist - Song"}, Options{})
	if parsed.Title != "Song" || !reflect.DeepEqual(parsed.Performers, []string{"Artist"}) {
		t.Fatalf("parsed: %+v", parsed)
	}
}
