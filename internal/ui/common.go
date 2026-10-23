package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/tag"
	"github.com/spytify/spytify/pkg/proto"
)

func showError(w fyne.Window, err error) {
	var perr *proto.Error
	if errors.As(err, &perr) {
		err = errors.New(perr.Message)
	}
	dialog.ShowError(err, w)
}

func fmtDuration(ms int64) string {
	if ms <= 0 {
		return "0:00"
	}
	d := time.Duration(ms) * time.Millisecond
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// expandHome turns a leading ~ into the home folder, as the core does.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func muted(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	return l
}

// covers loads album art once per URL, off the toolkit's goroutine.
type covers struct {
	mu      sync.Mutex
	cache   map[string]fyne.Resource
	pending map[string]bool
	loaded  func()
}

func newCovers(loaded func()) *covers {
	return &covers{cache: map[string]fyne.Resource{}, pending: map[string]bool{}, loaded: loaded}
}

// get returns the cover for url, or nil while it loads or when it failed.
func (c *covers) get(url string) fyne.Resource {
	if url == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.cache[url]; ok {
		return r
	}
	if !c.pending[url] {
		c.pending[url] = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			data, _, err := tag.FetchCover(ctx, nil, url)
			c.mu.Lock()
			if err == nil {
				c.cache[url] = fyne.NewStaticResource(url, data)
			} else {
				c.cache[url] = nil
			}
			c.mu.Unlock()
			if c.loaded != nil {
				c.loaded()
			}
		}()
	}
	return nil
}
