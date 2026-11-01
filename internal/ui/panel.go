package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// panel draws its content, inset by pad, on a box with rounded corners, an
// optional fill and an optional outline, both theme colours.
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
	r.box.FillColor = color.Transparent
	if r.p.fill != "" {
		r.box.FillColor = theme.ColorForWidget(r.p.fill, r.p)
	}
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
