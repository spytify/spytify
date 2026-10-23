package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// library writes 10,000 tracks in artist/album folders, the size the
// folder scan has to stay quick for.
func library(b *testing.B) string {
	root := b.TempDir()
	for a := 0; a < 500; a++ {
		dir := filepath.Join(root, fmt.Sprintf("Artist %03d", a), "Album")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for t := 0; t < 20; t++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d Title %d.flac", t, t)), []byte("fLaC"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
	return root
}

func BenchmarkScanAudio(b *testing.B) {
	root := library(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if n := len(scanAudio(root)); n != 10000 {
			b.Fatalf("%d files", n)
		}
	}
}

func BenchmarkSearchAndSort(b *testing.B) {
	files := scanAudio(library(b))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var shown []libraryFile
		for _, f := range files {
			if strings.Contains(strings.ToLower(f.rel), "title 1") {
				shown = append(shown, f)
			}
		}
		sort.Slice(shown, func(i, j int) bool { return strings.ToLower(shown[i].rel) < strings.ToLower(shown[j].rel) })
	}
}
