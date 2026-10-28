package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/draw"

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

// setCover shows a cover's thumbnail, or the music icon while there is none.
func setCover(img *canvas.Image, cover image.Image) {
	if cover == nil {
		setImage(img, theme.MediaMusicIcon())
		return
	}
	if img.Image != cover || img.Resource != nil {
		img.Resource, img.Image = nil, cover
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

// setRowText shows the lines one under another, each in the style of its
// segment, except that the text after an inline segment stays on its line; a
// line past the text's segments takes the style of the one above.
func setRowText(rt *widget.RichText, lines ...string) {
	changed := len(rt.Segments) != len(lines)
	for i, text := range lines {
		if i == len(rt.Segments) {
			rt.Segments = append(rt.Segments, &widget.TextSegment{Style: rt.Segments[i-1].(*widget.TextSegment).Style})
		}
		if seg := rt.Segments[i].(*widget.TextSegment); seg.Text != text {
			seg.Text, changed = text, true
		}
	}
	if changed {
		rt.Segments = rt.Segments[:len(lines)]
		rt.Refresh()
	}
}

// tight takes a caption's padding off above and below it, so that rows of
// captions sit as close as the lines of a paragraph.
func tight(o fyne.CanvasObject) *fyne.Container {
	p := -theme.InnerPadding()
	return container.New(layout.NewCustomPaddedLayout(p, p, 0, 0), o)
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

// plain sets a part of the window on the plain colour instead of the page.
func plain(content fyne.CanvasObject) fyne.CanvasObject {
	box := newPanel(colorPlain, "", 0, fyne.Size{}, content)
	return container.NewThemeOverride(box, cardTheme{surface: colorPlain})
}

// margin keeps a screen off the window's sides and bottom, as the window's
// own padding would; the window has none, so the bars' plain colour goes to
// its edges.
func margin(o fyne.CanvasObject) *fyne.Container {
	pad := theme.Padding()
	return container.New(layout.NewCustomPaddedLayout(0, pad, pad, pad), o)
}

// tabsLayout puts the plain colour behind the tabs' bar, and behind the
// padding the window's layout leaves above it, up to the app bar: the tabs
// draw no background of their own, and the page's grey is below.
type tabsLayout struct{ tabs *container.AppTabs }

// withPlainTabBar is tabs with the plain colour behind their bar.
func withPlainTabBar(tabs *container.AppTabs) *fyne.Container {
	return container.New(tabsLayout{tabs}, canvas.NewRectangle(theme.Color(colorPlain)), tabs)
}

func (l tabsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	l.tabs.Resize(size)
	pad := theme.Padding()
	// The tabs put their content a padding below their bar.
	bar := float32(0)
	if item := l.tabs.Selected(); item != nil {
		bar = item.Content.Position().Y - pad
	}
	back := objs[0].(*canvas.Rectangle)
	back.FillColor = theme.Color(colorPlain)
	back.Move(fyne.NewPos(0, -pad))
	back.Resize(fyne.NewSize(size.Width, bar+pad))
}

func (l tabsLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.tabs.MinSize() }

// inset keeps a part of a screen off the window's sides, in line with the
// cards.
func inset(o fyne.CanvasObject) *fyne.Container {
	pad := theme.Padding()
	return container.New(layout.NewCustomPaddedLayout(pad, pad, pad*2, pad*2), o)
}

// listCard holds a list in a card as tall as its rows, up to the room it is
// given; refresh it when the number of rows changes.
func listCard(list *widget.List) *fyne.Container {
	pad := theme.Padding()
	box := newPanel(colorCard, colorCardLine, 10, fyne.NewSize(pad, pad), list)
	return container.New(fitRows{list}, container.NewThemeOverride(box, cardTheme{input: colorCardInput, surface: colorCard}))
}

// fitRows lays a list's card out no taller than its rows: under software
// GL, as on Linux without a graphics card, each pixel a card covers costs
// CPU on every frame, and the level meter draws ten a second.
type fitRows struct{ list *widget.List }

func (f fitRows) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	h := size.Height
	if n := f.list.Length(); n > 0 {
		// The list puts a padding between its rows; the card one around them.
		pad := theme.Padding()
		row := f.list.CreateItem().MinSize().Height
		h = min(h, float32(n)*(row+pad)+pad)
	}
	objs[0].Resize(fyne.NewSize(size.Width, h))
}

func (fitRows) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }

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
