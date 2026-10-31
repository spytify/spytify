package ui

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"sync"
	"time"

	"golang.org/x/image/draw"

	"github.com/spytify/spytify/internal/tag"
)

// The sides of a cover in a list row and on the live card.
const (
	rowCover  = 40
	liveCover = 96
)

// thumbs is a cover at the sizes the app shows it, twice those in points
// for HiDPI screens: a cover can be thousands of pixels wide, and Fyne
// decodes and scales a picture from a resource again on every refresh, as
// a list row's is each time the list redraws. Both are nil while there is
// no cover.
type thumbs struct{ row, live image.Image }

func newThumbs(data []byte) (thumbs, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return thumbs{}, err
	}
	live := shrink(img, 2*liveCover)
	return thumbs{row: shrink(live, 2*rowCover), live: live}, nil
}

// shrink scales img down to fit in a square of side px, keeping its shape;
// a smaller picture stays as it is.
func shrink(img image.Image, px int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= px && h <= px {
		return img
	}
	if w >= h {
		w, h = px, max(1, h*px/w)
	} else {
		w, h = max(1, w*px/h), px
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// maxCovers is how many covers the cache keeps, the ones used last: about
// 35 MB of thumbnails.
const maxCovers = 200

// covers loads album art once per URL, off the toolkit's goroutine, and
// keeps the thumbnails of the covers used last.
type covers struct {
	mu      sync.Mutex
	cache   map[string]thumbs
	used    map[string]uint64
	tick    uint64
	done    int
	pending map[string]bool
	loaded  func()
	// slots bounds the loads running at once.
	slots chan struct{}
}

func newCovers(loaded func()) *covers {
	return &covers{cache: map[string]thumbs{}, used: map[string]uint64{}, pending: map[string]bool{}, loaded: loaded, slots: make(chan struct{}, 2)}
}

// loads is how many covers finished loading, failed ones included: a
// screen that changed nothing else redraws its rows when it grows.
func (c *covers) loads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

// get returns the cover at url, none while it loads or when it failed.
func (c *covers) get(url string) thumbs {
	if url == "" {
		return thumbs{}
	}
	return c.load(url, func(ctx context.Context) ([]byte, error) {
		data, _, err := tag.FetchCover(ctx, nil, url)
		return data, err
	})
}

// load returns the cover cached under key, or starts fetch and returns none
// until it is done; loaded runs then, so the screen can draw it. The
// picture is decoded and shrunk there, once.
func (c *covers) load(key string, fetch func(context.Context) ([]byte, error)) thumbs {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.cache[key]; ok {
		c.tick++
		c.used[key] = c.tick
		return t
	}
	if !c.pending[key] {
		c.pending[key] = true
		go func() {
			c.slots <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			data, err := fetch(ctx)
			cancel()
			var t thumbs
			if err == nil {
				t, _ = newThumbs(data)
			}
			<-c.slots
			c.mu.Lock()
			delete(c.pending, key)
			c.put(key, t)
			c.done++
			c.mu.Unlock()
			if c.loaded != nil {
				c.loaded()
			}
		}()
	}
	return thumbs{}
}

// put caches t under key, in place of the cover used longest ago when the
// cache is full. c.mu is held.
func (c *covers) put(key string, t thumbs) {
	if _, ok := c.cache[key]; !ok && len(c.cache) >= maxCovers {
		oldest := ""
		for k, u := range c.used {
			if oldest == "" || u < c.used[oldest] {
				oldest = k
			}
		}
		delete(c.cache, oldest)
		delete(c.used, oldest)
	}
	c.tick++
	c.cache[key], c.used[key] = t, c.tick
}
