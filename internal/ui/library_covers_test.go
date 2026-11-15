package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Scrolling the Library down well past the Record screen's 60 covers and
// back reads each file's cover once: ffmpeg starts once per file, not
// again for a row that comes back. The Library keeps a row's thumbnail
// alone, not the live card's two.
func TestScrollingTheLibraryBackAndForthReadsEachCoverOnce(t *testing.T) {
	// 150 files and a picture twice the row thumbnail keep the test under
	// a few seconds: internal/ui is close to CI's 20 minutes under -race.
	const files = 150
	dir := t.TempDir()
	for i := range files {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("Artist - Song %03d.mp3", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 4*rowCover, 4*rowCover)), nil); err != nil {
		t.Fatal(err)
	}
	// The test reads the covers: no ffmpeg before it does.
	t.Setenv("SPYTIFY_FFMPEG", filepath.Join(dir, "none"))
	values := defaults()
	values["output.dir"] = dir
	a, _ := connectedTestApp(t, values)
	lib := a.screens[tabLibrary].(*libraryScreen)
	// Set before the first draw: the rows it shows load their covers too.
	var mu sync.Mutex
	runs := map[string]int{}
	lib.readCover = func(_ context.Context, path string) ([]byte, error) {
		mu.Lock()
		runs[filepath.Base(path)]++
		mu.Unlock()
		return picture.Bytes(), nil
	}
	a.showTab(tabLibrary)
	a.render()
	if len(lib.shown) != files {
		t.Fatalf("%d files listed", len(lib.shown))
	}
	settle := func() {
		t.Helper()
		eventually(t, "the covers never loaded", func() bool {
			lib.covers.mu.Lock()
			defer lib.covers.mu.Unlock()
			return len(lib.covers.pending) == 0
		})
	}
	scroll := func(ids ...int) {
		t.Helper()
		for _, id := range ids {
			lib.list.ScrollTo(id)
			settle()
		}
	}
	var down, up []int
	for id := 0; id < files; id += 4 {
		down = append(down, id)
		up = append([]int{id}, up...)
	}
	scroll(down...)
	scroll(up...)
	scroll(down...)
	mu.Lock()
	defer mu.Unlock()
	if len(runs) < files/2 {
		t.Fatalf("the scroll read %d covers of %d files", len(runs), files)
	}
	for name, n := range runs {
		if n != 1 {
			t.Errorf("%s: its cover was read %d times", name, n)
		}
	}
	got := lib.cover(lib.shown[0])
	if got.row == nil || got.row.Bounds().Dx() != 2*rowCover {
		t.Fatalf("the first row's cover is %v", got.row)
	}
	if got.live != nil || got.glow != nil {
		t.Error("the Library keeps the live card's pictures too")
	}
}

// A Library row shows the same picture as before, made without the live
// card's sizes.
func TestARowThumbnailIsTheRowOfTheFullSet(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 900, 600))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 7)
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, src, nil); err != nil {
		t.Fatal(err)
	}
	full, err := newThumbs(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	row, err := newRowThumbs(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	a, b := full.row.(*image.NRGBA), row.row.(*image.NRGBA)
	if a.Rect != b.Rect || !bytes.Equal(a.Pix, b.Pix) {
		t.Fatalf("the row's thumbnail changed: %v vs %v", a.Rect, b.Rect)
	}
	if row.live != nil || row.glow != nil {
		t.Error("the row thumbnail made the live card's pictures")
	}
}
