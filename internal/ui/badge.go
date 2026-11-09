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
// theme colours, so a theme change shows; lead goes before the text while
// visible.
type badge struct {
	widget.BaseWidget
	text            string
	fill, line, ink fyne.ThemeColorName
	lead            fyne.CanvasObject
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

func (b *badge) parts() []fyne.CanvasObject {
	if b.lead == nil {
		return nil
	}
	return []fyne.CanvasObject{b.lead}
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
	return append(out, r.label)
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
	return append(objs, r.label)
}

func (r *badgeRenderer) Destroy() {}
