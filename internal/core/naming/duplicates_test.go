package naming

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/unicode/norm"
)

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
