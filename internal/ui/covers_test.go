package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// A cover is fetched, decoded and shrunk once, to twice the size it is
// shown at; a row showing it again decodes nothing.
func TestCoversAreShrunkOnce(t *testing.T) {
	var big bytes.Buffer
	if err := jpeg.Encode(&big, image.NewRGBA(image.Rect(0, 0, 1200, 800)), nil); err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{}, 4)
	c := newCovers(func() { loaded <- struct{}{} })
	var fetches atomic.Int32
	fetch := func(context.Context) ([]byte, error) {
		fetches.Add(1)
		return big.Bytes(), nil
	}
	if got := c.load("cover", fetch); got.row != nil || got.live != nil {
		t.Fatal("a cover before it loaded")
	}
	<-loaded
	got := c.load("cover", fetch)
	c.load("cover", fetch)
	if n := fetches.Load(); n != 1 {
		t.Errorf("the cover was fetched %d times", n)
	}
	if b := got.live.Bounds(); b.Dx() != 2*liveCover || b.Dy() != 2*liveCover*2/3 {
		t.Errorf("the live card's cover is %v", b)
	}
	if b := got.row.Bounds(); b.Dx() != 2*rowCover || b.Dy() != 2*rowCover*2/3 {
		t.Errorf("a row's cover is %v", b)
	}
	img := canvas.NewImageFromResource(theme.MediaMusicIcon())
	setCover(img, got.row)
	img.Refresh()
	if img.Resource != nil || img.Image != got.row {
		t.Error("the row's picture is not the thumbnail")
	}
}

// The cache keeps the covers used last, up to maxCovers: a long session or
// a scroll through a big library does not grow the app's memory for good.
func TestTheCoverCacheKeepsAtMostMaxCovers(t *testing.T) {
	var small bytes.Buffer
	if err := jpeg.Encode(&small, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{}, 16)
	c := newCovers(func() { loaded <- struct{}{} })
	var fetches sync.Map
	fetch := func(key string) func(context.Context) ([]byte, error) {
		return func(context.Context) ([]byte, error) {
			n, _ := fetches.LoadOrStore(key, new(atomic.Int32))
			n.(*atomic.Int32).Add(1)
			return small.Bytes(), nil
		}
	}
	load := func(key string) thumbs {
		t.Helper()
		if got := c.load(key, fetch(key)); got.row != nil {
			return got
		}
		select {
		case <-loaded:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not load", key)
		}
		return c.load(key, fetch(key))
	}
	load("kept")
	for i := 0; i < 5*maxCovers; i++ {
		load(fmt.Sprint("cover ", i))
		if i%(maxCovers/2) == 0 {
			load("kept")
		}
	}
	c.mu.Lock()
	size, pending := len(c.cache), len(c.pending)
	c.mu.Unlock()
	if size > maxCovers || pending != 0 {
		t.Fatalf("%d covers cached, %d pending, at most %d wanted", size, pending, maxCovers)
	}
	if n, _ := fetches.Load("kept"); n.(*atomic.Int32).Load() != 1 {
		t.Error("a cover in use was dropped")
	}
	if got := load("cover 0"); got.row == nil {
		t.Error("a dropped cover does not load again")
	}
	if n, _ := fetches.Load("cover 0"); n.(*atomic.Int32).Load() != 2 {
		t.Error("a dropped cover was not fetched again")
	}
}
