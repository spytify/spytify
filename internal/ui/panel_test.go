package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A panel's box fills it and its content sits inset by the padding, which
// the panel adds to the content's size on each side. A refresh lays the
// content out again, as a container's does.
func TestAPanelInsetsItsContent(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	content := canvas.NewRectangle(nil)
	content.SetMinSize(fyne.NewSize(50, 20))
	p := newPanel(colorCard, "", 8, fyne.NewSize(6, 4), content)
	test.NewTempWindow(t, p)
	if got, want := p.MinSize(), fyne.NewSize(62, 28); got != want {
		t.Errorf("min size %v, want %v", got, want)
	}
	p.Resize(fyne.NewSize(200, 100))
	box := test.TempWidgetRenderer(t, p).(*panelRenderer).box
	if box.Position() != (fyne.Position{}) || box.Size() != fyne.NewSize(200, 100) {
		t.Errorf("the box is %v at %v", box.Size(), box.Position())
	}
	if content.Position() != fyne.NewPos(6, 4) || content.Size() != fyne.NewSize(188, 92) {
		t.Errorf("the content is %v at %v", content.Size(), content.Position())
	}
	content.Resize(fyne.NewSize(1, 1))
	p.Refresh()
	if content.Size() != fyne.NewSize(188, 92) {
		t.Errorf("after a refresh, the content is %v", content.Size())
	}
}

// A panel takes its colours from the theme on each refresh, so a change of
// theme shows, and from a theme override around it; with no line colour it
// has no outline, and with no fill colour, as the session alert, only its
// outline.
func TestAPanelFollowsTheTheme(t *testing.T) {
	fa := test.NewTempApp(t)
	fa.Settings().SetTheme(spytifyTheme{mode: "light"})
	outlined := newPanel(colorCard, colorCardLine, 10, fyne.Size{}, widget.NewLabel("outlined"))
	plain := newPanel(colorFooter, "", 4, fyne.Size{}, widget.NewLabel("plain"))
	line := newPanel("", theme.ColorNamePrimary, 8, fyne.Size{}, widget.NewLabel("line"))
	input := newPanel(theme.ColorNameInputBackground, "", 0, fyne.Size{}, widget.NewLabel("input"))
	test.NewTempWindow(t, container.NewVBox(outlined, plain, line, container.NewThemeOverride(input, cardTheme{input: colorCardInput})))
	box := func(p *panel) *canvas.Rectangle { return test.TempWidgetRenderer(t, p).(*panelRenderer).box }
	for i, mode := range []string{"light", "dark"} {
		fa.Settings().SetTheme(spytifyTheme{mode: mode})
		if b := box(outlined); b.FillColor != cardColors[colorCard][i] || b.StrokeColor != cardColors[colorCardLine][i] || b.StrokeWidth != 1 || b.CornerRadius != 10 {
			t.Errorf("%s, outlined: %v, outline %v %v wide, rounded %v", mode, b.FillColor, b.StrokeColor, b.StrokeWidth, b.CornerRadius)
		}
		if b := box(plain); b.FillColor != cardColors[colorFooter][i] || b.StrokeWidth != 0 || b.CornerRadius != 4 {
			t.Errorf("%s, plain: %v, outline %v wide, rounded %v", mode, b.FillColor, b.StrokeWidth, b.CornerRadius)
		}
		primary := fa.Settings().Theme().Color(theme.ColorNamePrimary, fa.Settings().ThemeVariant())
		if b := box(line); b.FillColor != color.Transparent || b.StrokeColor != primary || b.StrokeWidth != 1 || b.CornerRadius != 8 {
			t.Errorf("%s, line only: %v, outline %v %v wide, rounded %v", mode, b.FillColor, b.StrokeColor, b.StrokeWidth, b.CornerRadius)
		}
		if b := box(input); b.FillColor != cardColors[colorCardInput][i] {
			t.Errorf("%s, in an override: %v, want the override's %v", mode, b.FillColor, cardColors[colorCardInput][i])
		}
	}
}
