package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// badge is a few words on a small rounded box: a row's status, a take's
// kind, the state and format of the track being recorded. Its colours are
// theme colours, so a theme change shows; lead goes before the text and
// tail after it, each while visible.
type badge struct {
	widget.BaseWidget
	text            string
	fill, line, ink fyne.ThemeColorName
	lead, tail      fyne.CanvasObject
}

func newBadge(text string, fill, line, ink fyne.ThemeColorName) *badge {
	b := &badge{text: text, fill: fill, line: line, ink: ink}
	b.ExtendBaseWidget(b)
	return b
}

// set changes the words and the colours, and draws the badge again only
// when one of them changed. A new width needs its row laid out again.
func (b *badge) set(text string, fill, ink fyne.ThemeColorName) {
	if b.text == text && b.fill == fill && b.ink == ink {
		return
	}
	b.text, b.fill, b.ink = text, fill, ink
	b.Refresh()
}

func (b *badge) CreateRenderer() fyne.WidgetRenderer {
	r := &badgeRenderer{b: b, box: canvas.NewRectangle(nil), label: canvas.NewText("", nil)}
	r.label.TextStyle.Bold = true
	r.Refresh()
	return r
}

const badgeHeight = 24

type badgeRenderer struct {
	b     *badge
	box   *canvas.Rectangle
	label *canvas.Text
}

// parts is what the badge shows, left to right.
func (r *badgeRenderer) parts() []fyne.CanvasObject {
	var out []fyne.CanvasObject
	if r.b.lead != nil && r.b.lead.Visible() {
		out = append(out, r.b.lead)
	}
	out = append(out, r.label)
	if r.b.tail != nil && r.b.tail.Visible() {
		out = append(out, r.b.tail)
	}
	return out
}

func (r *badgeRenderer) Layout(s fyne.Size) {
	r.box.Resize(s)
	x := theme.InnerPadding()
	for _, o := range r.parts() {
		m := o.MinSize()
		o.Resize(m)
		o.Move(fyne.NewPos(x, (s.Height-m.Height)/2))
		x += m.Width + theme.Padding()
	}
}

func (r *badgeRenderer) MinSize() fyne.Size {
	w := 2*theme.InnerPadding() - theme.Padding()
	for _, o := range r.parts() {
		w += o.MinSize().Width + theme.Padding()
	}
	return fyne.NewSize(w, badgeHeight)
}

func (r *badgeRenderer) Refresh() {
	r.box.FillColor = color.Transparent
	if r.b.fill != "" {
		r.box.FillColor = theme.ColorForWidget(r.b.fill, r.b)
	}
	r.box.StrokeColor, r.box.StrokeWidth = color.Transparent, 0
	if r.b.line != "" {
		r.box.StrokeColor, r.box.StrokeWidth = theme.ColorForWidget(r.b.line, r.b), 1
	}
	r.box.CornerRadius = 6
	r.label.Text = r.b.text
	r.label.Color = theme.ColorForWidget(r.b.ink, r.b)
	r.label.TextSize = theme.SizeForWidget(theme.SizeNameCaptionText, r.b)
	r.box.Refresh()
	r.label.Refresh()
	r.Layout(r.b.Size())
}

func (r *badgeRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.box}
	if r.b.lead != nil {
		objs = append(objs, r.b.lead)
	}
	objs = append(objs, r.label)
	if r.b.tail != nil {
		objs = append(objs, r.b.tail)
	}
	return objs
}

func (r *badgeRenderer) Destroy() {}

// levelMeter is the input level as bars of rising height, lit up to the
// level, as a phone shows its signal: the Recording badge's.
type levelMeter struct {
	widget.BaseWidget
	lit   int
	color fyne.ThemeColorName
}

// levelSteps are the RMS levels, in dBFS over the tenth of a second the
// recorder measures, from which each bar after the first lights: the first
// alone is silence or close to it, two classical music, three to five a
// song's vocals and guitars, all six loud rock. Recorded songs, which the
// player brings to a like loudness, spend most of their time from -21 to
// -14.
var levelSteps = []float64{-40, -27, -21, -18, -15}

// levelHeights are the meter's bars, shortest first: a stub for level 0,
// then one per step.
var levelHeights = []float32{2, 4, 6, 8, 10, 12}

func newLevelMeter(color fyne.ThemeColorName) *levelMeter {
	m := &levelMeter{color: color}
	m.ExtendBaseWidget(m)
	return m
}

// SetLevel lights the bars for an RMS level in dBFS: the stub, then one
// more for each step reached. The level comes ten times a second: the meter
// is drawn again only when another bar lights or goes out.
func (m *levelMeter) SetLevel(rmsDBFS float64) {
	lit := 1
	for _, step := range levelSteps {
		if rmsDBFS >= step {
			lit++
		}
	}
	if lit == m.lit {
		return
	}
	m.lit = lit
	m.Refresh()
}

func (m *levelMeter) CreateRenderer() fyne.WidgetRenderer {
	r := &levelRenderer{m: m}
	for range levelHeights {
		r.bars = append(r.bars, canvas.NewRectangle(nil))
	}
	r.Refresh()
	return r
}

const levelBarWidth, levelGap = 3, 2

type levelRenderer struct {
	m    *levelMeter
	bars []*canvas.Rectangle
}

func (r *levelRenderer) Layout(s fyne.Size) {
	top := levelHeights[len(levelHeights)-1]
	for i, b := range r.bars {
		h := levelHeights[i]
		b.Move(fyne.NewPos(float32(i)*(levelBarWidth+levelGap), (s.Height-top)/2+top-h))
		b.Resize(fyne.NewSize(levelBarWidth, h))
	}
}

func (r *levelRenderer) MinSize() fyne.Size {
	n := float32(len(levelHeights))
	return fyne.NewSize(n*levelBarWidth+(n-1)*levelGap, levelHeights[len(levelHeights)-1])
}

func (r *levelRenderer) Refresh() {
	on := color.NRGBAModel.Convert(theme.ColorForWidget(r.m.color, r.m)).(color.NRGBA)
	off := withAlpha(on, uint8(int(on.A)*3/10))
	for i, b := range r.bars {
		b.FillColor = off
		if i < r.m.lit {
			b.FillColor = on
		}
		b.CornerRadius = 1
		b.Refresh()
	}
	r.Layout(r.m.Size())
}

func (r *levelRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(r.bars))
	for i, b := range r.bars {
		objs[i] = b
	}
	return objs
}

func (r *levelRenderer) Destroy() {}
