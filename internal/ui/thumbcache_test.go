package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/encode"
)

// A cover the Library read once, and a file found with none, are not read
// again at the next launch: the thumbnails on disk show instead.
func TestTheLibraryReadsEachCoverOnceAcrossLaunches(t *testing.T) {
	const files = 12
	dir, cache := t.TempDir(), t.TempDir()
	for i := range files {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("Artist - Song %02d.mp3", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 4*rowCover, 4*rowCover)), nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPYTIFY_FFMPEG", filepath.Join(dir, "none"))
	var mu sync.Mutex
	runs := map[string]int{}
	launch := func() *libraryScreen {
		values := defaults()
		values["output.dir"] = dir
		a, _ := connectedTestApp(t, values)
		lib := a.screens[tabLibrary].(*libraryScreen)
		lib.disk = thumbCache{dir: cache}
		lib.readCover = func(_ context.Context, path string) ([]byte, error) {
			mu.Lock()
			runs[filepath.Base(path)]++
			mu.Unlock()
			if strings.HasSuffix(path, "00.mp3") {
				return nil, encode.ErrNoCover
			}
			return picture.Bytes(), nil
		}
		a.showTab(tabLibrary)
		a.render()
		eventually(t, "the covers never loaded", func() bool {
			lib.covers.mu.Lock()
			defer lib.covers.mu.Unlock()
			return len(lib.covers.pending) == 0 && len(lib.covers.cache) > 0
		})
		return lib
	}
	launch()
	mu.Lock()
	first := len(runs)
	mu.Unlock()
	if first == 0 {
		t.Fatal("the first launch read no cover")
	}
	lib := launch()
	mu.Lock()
	for name, n := range runs {
		if n != 1 {
			t.Errorf("%s: its cover was read %d times over two launches", name, n)
		}
	}
	mu.Unlock()
	shown := 0
	for _, f := range lib.shown {
		if got := lib.cover(f); got.row != nil {
			shown++
			if got.row.Bounds().Dx() != 2*rowCover {
				t.Errorf("%s: a thumbnail %d px wide", f.rel, got.row.Bounds().Dx())
			}
		}
	}
	if shown == 0 {
		t.Error("the second launch shows no cover from the disk")
	}
	// The rows out of view just started loading: their thumbnails are
	// written before the test's folder goes.
	eventually(t, "the covers never loaded", func() bool {
		lib.covers.mu.Lock()
		defer lib.covers.mu.Unlock()
		return len(lib.covers.pending) == 0
	})
}

// A thumbnail that no longer decodes is deleted and read again; a folder
// that cannot be written keeps nothing and fails nothing.
func TestABrokenThumbnailIsReadAgain(t *testing.T) {
	c := thumbCache{dir: t.TempDir()}
	if err := os.WriteFile(c.path("song"), []byte("not a picture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.read("song"); ok {
		t.Error("a thumbnail that does not decode was read")
	}
	if _, err := os.Stat(c.path("song")); !os.IsNotExist(err) {
		t.Errorf("the broken thumbnail is still there: %v", err)
	}

	c.write("none", nil)
	if data, ok := c.read("none"); !ok || len(data) != 0 {
		t.Errorf("a file with no cover: %q, %v", data, ok)
	}

	file := filepath.Join(t.TempDir(), "a file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := thumbCache{dir: filepath.Join(file, "covers")}
	blocked.write("song", []byte("x"))
	if _, ok := blocked.read("song"); ok {
		t.Error("a folder that cannot be made kept a thumbnail")
	}
	var off thumbCache
	off.write("song", []byte("x"))
	if _, ok := off.read("song"); ok {
		t.Error("no folder kept a thumbnail")
	}
}

// prune deletes the thumbnails not used in a month, then the oldest while
// the rest weigh more than thumbBytes.
func TestPruneKeepsTheThumbnailsUsedLast(t *testing.T) {
	c := thumbCache{dir: t.TempDir()}
	now := time.Now()
	put := func(name string, size int64, used time.Time) string {
		t.Helper()
		p := filepath.Join(c.dir, name)
		// Sparse: the size is counted, not written.
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(p, size); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, used, used); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := put("stale.png", 1, now.Add(-thumbAge-time.Hour))
	older := put("older.png", 30<<20, now.Add(-2*time.Hour))
	newer := put("newer.png", 30<<20, now.Add(-time.Hour))
	c.prune(now)
	for _, k := range []struct {
		path string
		kept bool
	}{{stale, false}, {older, false}, {newer, true}} {
		_, err := os.Stat(k.path)
		if kept := err == nil; kept != k.kept {
			t.Errorf("%s: kept %v, want %v", filepath.Base(k.path), kept, k.kept)
		}
	}

	if _, ok := c.read("missing"); ok {
		t.Error("a thumbnail never written was read")
	}
	used := put(filepath.Base(c.path("song")), 0, now.Add(-thumbAge+time.Hour))
	if err := os.WriteFile(used, mustPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(used, now.Add(-thumbAge+time.Hour), now.Add(-thumbAge+time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.read("song"); !ok {
		t.Fatal("the thumbnail was not read")
	}
	c.prune(now.Add(2 * time.Hour))
	if _, err := os.Stat(used); err != nil {
		t.Errorf("a thumbnail read lately was pruned: %v", err)
	}
}

// A thumbnail read back from disk is the picture newRowThumbs makes from
// the cover itself.
func TestARowPNGIsTheRowThumbnail(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 900, 600))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 7)
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, src, nil); err != nil {
		t.Fatal(err)
	}
	want, err := newRowThumbs(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	row, err := rowPNG(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, err := newRowThumbs(row)
	if err != nil {
		t.Fatal(err)
	}
	if got.row.Bounds() != want.row.Bounds() {
		t.Fatalf("bounds %v, want %v", got.row.Bounds(), want.row.Bounds())
	}
	b := want.row.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.NRGBAModel.Convert(got.row.At(x, y)) != color.NRGBAModel.Convert(want.row.At(x, y)) {
				t.Fatalf("pixel (%d,%d) differs", x, y)
			}
		}
	}
}

func mustPNG(t *testing.T) []byte {
	t.Helper()
	var src bytes.Buffer
	if err := jpeg.Encode(&src, image.NewRGBA(image.Rect(0, 0, 4*rowCover, 4*rowCover)), nil); err != nil {
		t.Fatal(err)
	}
	row, err := rowPNG(src.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return row
}
