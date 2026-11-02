package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A dropdown is outlined as the search field is, square, in the green
// while it has the focus, and as high as a field beside it: on a card, and
// in the advanced block's smaller text, where Fyne's was 2 pixels lower.
func TestADropdownLooksAsAField(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	for _, th := range []cardTheme{{input: colorCard}, {input: colorAdvancedInput, small: true}} {
		field := widget.NewEntry()
		sel := newSelectField([]string{"System", "Light", "Dark"}, nil)
		sel.SetSelected("System")
		c := test.NewTempWindow(t, container.NewThemeOverride(container.NewVBox(field, sel), th)).Canvas()
		if f, s := field.MinSize().Height, sel.MinSize().Height; s != f {
			t.Errorf("small %v: the dropdown is %v high, a field %v", th.small, s, f)
		}
		objs := test.TempWidgetRenderer(t, sel).Objects()
		outline := objs[len(objs)-1].(*canvas.Rectangle)
		want := func(n fyne.ThemeColorName) {
			t.Helper()
			if got := theme.ColorForWidget(n, sel); sel.borderName() != n || outline.StrokeColor != got {
				t.Errorf("small %v: outlined %v in %s, want %s, %v", th.small, outline.StrokeColor, sel.borderName(), n, got)
			}
		}
		if outline.StrokeWidth != theme.InputBorderSize() || outline.CornerRadius != 0 {
			t.Errorf("small %v: outline %v wide, corners %v round", th.small, outline.StrokeWidth, outline.CornerRadius)
		}
		want(theme.ColorNameInputBorder)
		c.Focus(sel)
		want(theme.ColorNamePrimary)
		c.Unfocus()
		want(theme.ColorNameInputBorder)
		sel.Disable()
		want(theme.ColorNameDisabled)
	}
}
