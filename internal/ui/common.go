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
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
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

// heading is a section title.
func heading(text string) *widget.Label {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.SizeName = theme.SizeNameSubHeadingText
	return l
}

// caption is secondary text: smaller and grey, but readable, unlike a
// low-importance label, which uses the disabled colour.
func caption(text string) *widget.RichText {
	rt := widget.NewRichText(&widget.TextSegment{Text: text, Style: widget.RichTextStyle{
		ColorName: theme.ColorNamePlaceHolder,
		SizeName:  theme.SizeNameCaptionText,
		Inline:    true,
	}})
	return rt
}

// clipped is a caption that ends in an ellipsis when its space runs out,
// for list rows; a plain caption keeps its full width.
func clipped(text string) *widget.RichText {
	rt := caption(text)
	rt.Truncation = fyne.TextTruncateEllipsis
	return rt
}

// wrapped is a caption that breaks into lines.
func wrapped(text string) *widget.RichText {
	rt := caption(text)
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// emptyState is the centered message of a screen with nothing to show.
func emptyState(title string, body *widget.Label) fyne.CanvasObject {
	body.Alignment = fyne.TextAlignCenter
	body.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(420, 120), container.NewVBox(
		widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}), body)))
}

// setCaption changes a caption's text.
func setCaption(rt *widget.RichText, text string) {
	seg := rt.Segments[0].(*widget.TextSegment)
	if seg.Text != text {
		seg.Text = text
		rt.Refresh()
	}
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

// bar is a thin meter; widget.ProgressBar is as tall as a button.
type bar struct {
	widget.BaseWidget
	value float64
	color fyne.ThemeColorName
}

func newBar(color fyne.ThemeColorName) *bar {
	b := &bar{color: color}
	b.ExtendBaseWidget(b)
	return b
}

// SetValue sets the filled share, from 0 to 1.
func (b *bar) SetValue(v float64) {
	v = max(0, min(1, v))
	if v == b.value {
		return
	}
	b.value = v
	b.Refresh()
}

func (b *bar) CreateRenderer() fyne.WidgetRenderer {
	r := &barRenderer{b: b, track: canvas.NewRectangle(nil), fill: canvas.NewRectangle(nil)}
	r.Refresh()
	return r
}

const barHeight = 4

type barRenderer struct {
	b           *bar
	track, fill *canvas.Rectangle
}

func (r *barRenderer) Layout(s fyne.Size) {
	y := (s.Height - barHeight) / 2
	r.track.Move(fyne.NewPos(0, y))
	r.track.Resize(fyne.NewSize(s.Width, barHeight))
	r.fill.Move(fyne.NewPos(0, y))
	r.fill.Resize(fyne.NewSize(s.Width*float32(r.b.value), barHeight))
}

func (r *barRenderer) MinSize() fyne.Size { return fyne.NewSize(40, barHeight) }

func (r *barRenderer) Refresh() {
	r.track.FillColor = theme.Color(theme.ColorNameInputBackground)
	r.fill.FillColor = theme.Color(r.b.color)
	r.track.CornerRadius, r.fill.CornerRadius = barHeight/2, barHeight/2
	r.Layout(r.b.Size())
	r.track.Refresh()
	r.fill.Refresh()
}

func (r *barRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.track, r.fill} }

func (r *barRenderer) Destroy() {}
