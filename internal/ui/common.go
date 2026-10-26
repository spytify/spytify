package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
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

// setImage changes an image only when its resource changes: a refresh
// decodes the picture again.
func setImage(img *canvas.Image, res fyne.Resource) {
	if img.Resource != res {
		img.Resource = res
		img.Refresh()
	}
}

// setLabel changes a label only when its text changes.
func setLabel(l *widget.Label, text string) {
	if l.Text != text {
		l.SetText(text)
	}
}

// rowText is a list row's title over its details as one block: two stacked
// widgets would each add their padding between the lines.
func rowText() *widget.RichText {
	rt := widget.NewRichText(
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNameForeground, SizeName: theme.SizeNameText, TextStyle: fyne.TextStyle{Bold: true}}},
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, SizeName: theme.SizeNameCaptionText}},
	)
	rt.Truncation = fyne.TextTruncateEllipsis
	return rt
}

func setRowText(rt *widget.RichText, title, details string) {
	t, d := rt.Segments[0].(*widget.TextSegment), rt.Segments[1].(*widget.TextSegment)
	if t.Text != title || d.Text != details {
		t.Text, d.Text = title, details
		rt.Refresh()
	}
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
	// slots bounds the loads running at once.
	slots chan struct{}
}

func newCovers(loaded func()) *covers {
	return &covers{cache: map[string]fyne.Resource{}, pending: map[string]bool{}, loaded: loaded, slots: make(chan struct{}, 2)}
}

// get returns the cover at url, or nil while it loads or when it failed.
// count is how many covers are in the cache, loaded or failed: a screen
// that changed nothing else redraws its rows when it grows.
func (c *covers) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cache)
}

func (c *covers) get(url string) fyne.Resource {
	if url == "" {
		return nil
	}
	return c.load(url, func(ctx context.Context) ([]byte, error) {
		data, _, err := tag.FetchCover(ctx, nil, url)
		return data, err
	})
}

// load returns the cover cached under key, or starts fetch and returns nil
// until it is done; loaded runs then, so the screen can draw it.
func (c *covers) load(key string, fetch func(context.Context) ([]byte, error)) fyne.Resource {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.cache[key]; ok {
		return r
	}
	if !c.pending[key] {
		c.pending[key] = true
		go func() {
			c.slots <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			data, err := fetch(ctx)
			cancel()
			<-c.slots
			c.mu.Lock()
			if err == nil {
				c.cache[key] = fyne.NewStaticResource(key, data)
			} else {
				c.cache[key] = nil
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

// toggle is an on/off switch for a yes-or-no setting; Fyne has only check
// boxes. A tap on it, or the space bar once it has the focus, flips it.
type toggle struct {
	widget.BaseWidget
	on               bool
	changed          func(bool)
	focused, hovered bool
}

func newToggle(changed func(bool)) *toggle {
	t := &toggle{changed: changed}
	t.ExtendBaseWidget(t)
	return t
}

// SetOn shows a value without reporting it as a change.
func (t *toggle) SetOn(on bool) {
	if on == t.on {
		return
	}
	t.on = on
	t.Refresh()
}

func (t *toggle) flip() {
	t.on = !t.on
	t.Refresh()
	if t.changed != nil {
		t.changed(t.on)
	}
}

// Tapped flips the switch when the tap lands on it, not elsewhere in the
// form row it stretches across.
func (t *toggle) Tapped(e *fyne.PointEvent) {
	if e.Position.X > toggleSize().Width {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(t); c != nil && !t.focused && !fyne.CurrentDevice().IsMobile() {
		c.Focus(t)
	}
	t.flip()
}

func (t *toggle) FocusGained() { t.focused = true; t.Refresh() }
func (t *toggle) FocusLost()   { t.focused = false; t.Refresh() }

func (t *toggle) TypedRune(r rune) {
	if r == ' ' {
		t.flip()
	}
}

func (t *toggle) TypedKey(*fyne.KeyEvent) {}

func (t *toggle) MouseIn(*desktop.MouseEvent)    { t.hovered = true; t.Refresh() }
func (t *toggle) MouseMoved(*desktop.MouseEvent) {}
func (t *toggle) MouseOut()                      { t.hovered = false; t.Refresh() }

func (t *toggle) CreateRenderer() fyne.WidgetRenderer {
	r := &toggleRenderer{t: t, track: canvas.NewRectangle(nil), halo: canvas.NewCircle(nil), knob: canvas.NewCircle(nil)}
	r.Refresh()
	return r
}

// toggleSize is the track's: as tall as an inline icon and a little, about
// twice as wide.
func toggleSize() fyne.Size {
	h := theme.Size(theme.SizeNameInlineIcon) + 2
	return fyne.NewSize(h*9/5, h)
}

// toggleInset is the gap between the track's edge and the knob.
const toggleInset = 3

type toggleRenderer struct {
	t          *toggle
	track      *canvas.Rectangle
	halo, knob *canvas.Circle
}

func (r *toggleRenderer) Layout(s fyne.Size) {
	ts := toggleSize()
	top := (s.Height - ts.Height) / 2
	r.track.Move(fyne.NewPos(0, top))
	r.track.Resize(ts)
	d := ts.Height - 2*toggleInset
	x := float32(toggleInset)
	if r.t.on {
		x = ts.Width - toggleInset - d
	}
	r.knob.Move(fyne.NewPos(x, top+toggleInset))
	r.knob.Resize(fyne.NewSquareSize(d))
	// The pointer's halo, around the knob.
	h := d + 2*toggleInset
	r.halo.Move(fyne.NewPos(x-toggleInset, top))
	r.halo.Resize(fyne.NewSquareSize(h))
}

func (r *toggleRenderer) MinSize() fyne.Size {
	return toggleSize().AddWidthHeight(0, 2*theme.Padding())
}

// Refresh draws on as a primary track with a white knob, and off as an
// input-coloured track with a grey knob, so both read in either theme.
func (r *toggleRenderer) Refresh() {
	ts := toggleSize()
	r.track.CornerRadius = ts.Height / 2
	r.track.StrokeWidth = theme.InputBorderSize()
	if r.t.on {
		r.track.FillColor = theme.Color(theme.ColorNamePrimary)
		r.track.StrokeColor = theme.Color(theme.ColorNamePrimary)
		r.knob.FillColor = color.White
	} else {
		r.track.FillColor = theme.Color(theme.ColorNameInputBackground)
		r.track.StrokeColor = theme.Color(theme.ColorNameInputBorder)
		r.knob.FillColor = theme.Color(theme.ColorNamePlaceHolder)
	}
	if r.t.focused {
		r.track.StrokeColor = theme.Color(theme.ColorNameFocus)
		r.track.StrokeWidth = 2
	}
	r.halo.FillColor = color.Transparent
	if r.t.hovered {
		r.halo.FillColor = theme.Color(theme.ColorNameHover)
	}
	r.Layout(r.t.Size())
	r.track.Refresh()
	r.halo.Refresh()
	r.knob.Refresh()
}

func (r *toggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.track, r.halo, r.knob}
}

func (r *toggleRenderer) Destroy() {}

// relayout lays a scroll's content out again after part of it showed or
// hid: Fyne lays a container out only when it is resized or refreshed, so
// what follows would stay where it was and overlap the next section.
func relayout(s *container.Scroll) {
	s.Content.Refresh()
	s.Refresh()
}

// panel draws its content, inset by pad, on a box of a theme colour with
// rounded corners and an optional outline.
type panel struct {
	widget.BaseWidget
	fill, line fyne.ThemeColorName
	radius     float32
	pad        fyne.Size
	content    fyne.CanvasObject
}

func newPanel(fill, line fyne.ThemeColorName, radius float32, pad fyne.Size, content fyne.CanvasObject) *panel {
	p := &panel{fill: fill, line: line, radius: radius, pad: pad, content: content}
	p.ExtendBaseWidget(p)
	return p
}

func (p *panel) CreateRenderer() fyne.WidgetRenderer {
	r := &panelRenderer{p: p, box: canvas.NewRectangle(nil)}
	r.Refresh()
	return r
}

type panelRenderer struct {
	p   *panel
	box *canvas.Rectangle
}

func (r *panelRenderer) Layout(s fyne.Size) {
	r.box.Resize(s)
	r.p.content.Move(fyne.NewPos(r.p.pad.Width, r.p.pad.Height))
	r.p.content.Resize(s.Subtract(r.p.pad).Subtract(r.p.pad))
}

func (r *panelRenderer) MinSize() fyne.Size {
	return r.p.content.MinSize().Add(r.p.pad).Add(r.p.pad)
}

// Refresh takes the colours from the theme, so a theme change shows, and
// lays the content out again, as a container's Refresh does.
func (r *panelRenderer) Refresh() {
	r.box.FillColor = theme.ColorForWidget(r.p.fill, r.p)
	r.box.StrokeColor = color.Transparent
	r.box.StrokeWidth = 0
	if r.p.line != "" {
		r.box.StrokeColor = theme.ColorForWidget(r.p.line, r.p)
		r.box.StrokeWidth = 1
	}
	r.box.CornerRadius = r.p.radius
	r.box.Refresh()
	r.Layout(r.p.Size())
	r.p.content.Refresh()
}

func (r *panelRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.box, r.p.content}
}

func (r *panelRenderer) Destroy() {}
