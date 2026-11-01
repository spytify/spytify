package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

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
