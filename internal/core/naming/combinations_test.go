package naming

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// awkward are tracks whose names break naive file naming.
var awkward = []Track{
	// Œ is a ligature, not a letter with a diacritic: it stays.
	{Artists: []string{"Zoë Ångström", "Élodie Brûlé"}, Title: "Ça plane pour moi (Remasterisé)", Album: "Œuvres complètes", TrackNumber: 7, Year: 1977},
	{Artists: []string{"AC/DC"}, Title: `What? "Now": <Live> | Take *2*`, Album: "Live: At/Donington", TrackNumber: 1},
	{Artists: []string{"Band 🎸"}, Title: "Song 🎶", Album: "Emoji 😀"},
	{Artists: []string{"Artist"}, Title: strings.Repeat("Très long titre ", 30), Album: strings.Repeat("Album ", 60)},
	{Artists: []string{"CON"}, Title: "Aux", Album: "nul"},
	{Artists: []string{"Trailing dots..."}, Title: " spaces  inside\tand\nlines ", Album: "..."},
	{Artists: nil, Title: "No artist"},
	{Unknown: true, FallbackStamp: "Untitled 20261027101500"},
}

// windowsReserved are names Windows refuses for a file or folder, with or
// without an extension.
var windowsReserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

func init() {
	for i := 1; i <= 9; i++ {
		windowsReserved[fmt.Sprint("COM", i)] = true
		windowsReserved[fmt.Sprint("LPT", i)] = true
	}
}

func checkComponent(t *testing.T, what, c string, o Options) {
	t.Helper()
	switch {
	case c == "" || !utf8.ValidString(c):
		t.Errorf("%s: empty or broken UTF-8 %q", what, c)
	case len(c) > 255:
		t.Errorf("%s: %d bytes", what, len(c))
	case strings.ContainsAny(c, `/\:*?"<>|`) || strings.ContainsFunc(c, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		t.Errorf("%s: forbidden character in %q", what, c)
	case strings.TrimSpace(c) != c || strings.HasSuffix(c, "."):
		t.Errorf("%s: starts or ends with a space, or ends with a dot: %q", what, c)
	case windowsReserved[strings.ToUpper(strings.SplitN(c, ".", 2)[0])]:
		t.Errorf("%s: %q is a name Windows refuses", what, c)
	case o.Underscores && strings.Contains(c, " "):
		t.Errorf("%s: a space with underscores on: %q", what, c)
	case o.ASCII && strings.ContainsAny(c, "ëÅÉéûÇç"):
		t.Errorf("%s: an accent with diacritics removed: %q", what, c)
	case !norm.NFC.IsNormalString(c):
		t.Errorf("%s: not in NFC: %q", what, c)
	}
}

// Every combination of the output settings gives names every OS accepts,
// and the duplicate check finds a file saved under each of them.
func TestEveryNamingCombinationGivesAValidName(t *testing.T) {
	for _, group := range []string{"none", "artist", "artist/album"} {
		for _, prefix := range []string{"none", "counter", "album_track"} {
			for _, underscores := range []bool{false, true} {
				for _, ascii := range []bool{false, true} {
					o := Options{GroupBy: group, Prefix: prefix, Underscores: underscores, ASCII: ascii, Template: "{artist} - {title}", CounterWidth: 3, Extension: "flac"}
					root := t.TempDir()
					for i, tr := range awkward {
						what := fmt.Sprintf("%s/%s/underscores=%v/ascii=%v/track %d", group, prefix, underscores, ascii, i)
						p := Build(tr, o)
						name := FileName(p, o, int64(i+1), tr.TrackNumber)
						for _, c := range strings.Split(filepath.ToSlash(p.Dir), "/") {
							if c != "" {
								checkComponent(t, what+" folder", c, o)
							}
						}
						checkComponent(t, what+" file", strings.TrimSuffix(name, ".flac"), o)
						if tr.Unknown && p.Dir != "" {
							t.Errorf("%s: an untitled item went to %q", what, p.Dir)
						}
						path := filepath.Join(root, p.Dir, name)
						if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
							t.Fatalf("%s: %v", what, err)
						}
						if err := os.WriteFile(path, nil, 0o644); err != nil {
							t.Fatalf("%s: %v", what, err)
						}
						if !Recorded(root, Build(tr, o), o) {
							t.Errorf("%s: %s saved but not found as recorded", what, filepath.Join(p.Dir, name))
						}
					}
				}
			}
		}
	}
}

// A name whose accents are decomposed (NFD), as macOS and some network
// drives return them, is still the same name for the duplicate check.
func TestRecordedFindsADecomposedName(t *testing.T) {
	tr := Track{Artists: []string{"Zoë"}, Title: "Café"}
	for _, prefix := range []string{"none", "counter"} {
		o := Options{GroupBy: "none", Prefix: prefix, Template: "{artist} - {title}", CounterWidth: 3, Extension: "mp3"}
		root := t.TempDir()
		name := norm.NFD.String(FileName(Build(tr, o), o, 12, 0))
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if !Recorded(root, Build(tr, o), o) {
			t.Errorf("prefix %s: %q not found as recorded", prefix, name)
		}
	}
}

// The duplicate check reads the whole folder when the file is not there:
// a flat library of 10,000 files costs a few milliseconds per track.
func BenchmarkRecordedFlat(b *testing.B) {
	for _, files := range []int{1000, 10000} {
		b.Run(fmt.Sprint(files), func(b *testing.B) {
			root := b.TempDir()
			o := Options{GroupBy: "none", Prefix: "counter", CounterWidth: 6, Template: "{artist} - {title}", Extension: "flac"}
			for i := range files {
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%06d Artist %d - Title %d.flac", i, i, i)), nil, 0o644); err != nil {
					b.Fatal(err)
				}
			}
			p := Build(Track{Artists: []string{"Someone"}, Title: "Never recorded"}, o)
			for b.Loop() {
				Recorded(root, p, o)
			}
		})
	}
}
