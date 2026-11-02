package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// selectField is a select drawn as the text fields are: outlined as one,
// in the primary colour while it has the focus, and as high as one. Fyne
// draws a select with no outline, and a little lower than a field in the
// smaller text of the advanced block.
type selectField struct {
	widget.Select
	focused bool
}

func newSelectField(options []string, changed func(string)) *selectField {
	s := &selectField{}
	s.Options, s.OnChanged = options, changed
	s.ExtendBaseWidget(s)
	return s
}

func (s *selectField) FocusGained() {
	s.focused = true
	s.Select.FocusGained()
}

func (s *selectField) FocusLost() {
	s.focused = false
	s.Select.FocusLost()
}

// borderName is the outline's colour: a field's, the green while it has
// the focus, dimmed while disabled.
func (s *selectField) borderName() fyne.ThemeColorName {
	switch {
	case s.Disabled():
		return theme.ColorNameDisabled
	case s.focused:
		return theme.ColorNamePrimary
	}
	return theme.ColorNameInputBorder
}

func (s *selectField) CreateRenderer() fyne.WidgetRenderer {
	r := &selectFieldRenderer{WidgetRenderer: s.Select.CreateRenderer(), s: s, border: canvas.NewRectangle(nil)}
	r.objects = append(append([]fyne.CanvasObject{}, r.WidgetRenderer.Objects()...), r.border)
	r.Refresh()
	return r
}

type selectFieldRenderer struct {
	fyne.WidgetRenderer
	s       *selectField
	border  *canvas.Rectangle
	objects []fyne.CanvasObject
}

// Layout draws the outline inside the select's edge, as a field does.
func (r *selectFieldRenderer) Layout(size fyne.Size) {
	r.WidgetRenderer.Layout(size)
	b := r.s.Theme().Size(theme.SizeNameInputBorder)
	r.border.StrokeWidth = b
	r.border.Move(fyne.NewSquareOffsetPos(b / 2))
	r.border.Resize(fyne.NewSize(size.Width-b-.5, size.Height-b-.5))
}

// MinSize is at least a one-line field's height: the height of an M and
// the inner padding above and below it.
func (r *selectFieldRenderer) MinSize() fyne.Size {
	th := r.s.Theme()
	m := r.WidgetRenderer.MinSize()
	field := fyne.MeasureText("M", th.Size(theme.SizeNameText), fyne.TextStyle{}).Height + 2*th.Size(theme.SizeNameInnerPadding)
	return fyne.NewSize(m.Width, max(m.Height, field))
}

func (r *selectFieldRenderer) Refresh() {
	r.WidgetRenderer.Refresh()
	th := r.s.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	r.border.FillColor = color.Transparent
	r.border.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.border.StrokeColor = th.Color(r.s.borderName(), v)
	r.border.Refresh()
	r.Layout(r.s.Size())
}

func (r *selectFieldRenderer) Objects() []fyne.CanvasObject { return r.objects }
