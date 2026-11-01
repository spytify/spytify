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

// over is how Fyne 2.8.2's button draws its focus colour over its fill:
// the over operator, on alpha-premultiplied values.
func over(under, top color.Color) color.Color {
	dr, dg, db, da := under.RGBA()
	sr, sg, sb, sa := top.RGBA()
	blend := func(src, dst, alpha uint32) uint16 {
		return uint16((src + dst - (dst * alpha / 0xFFFF)) & 0xFFFF)
	}
	return color.RGBA64{R: blend(sr, dr, sa), G: blend(sg, dg, sa), B: blend(sb, db, sa), A: blend(sa, da, sa)}
}

// A focused filled button looks as a focused plain one does in the light
// theme, in either theme: Record and the other green buttons take the
// light grey under the green focus tint, Stop a red tint and a warning an
// orange one, all with dark text. Fyne blended its translucent green over
// the fill instead: Stop turned brown and the green hardly changed. Out of
// focus, each has its colour back.
func TestAFocusedFilledButtonLooksAsAPlainOneFocused(t *testing.T) {
	fa := test.NewTempApp(t)
	fa.Settings().SetTheme(spytifyTheme{mode: "light"})
	plain := newButton("Project page", nil)
	record := newLitButton("Record", nil, widget.HighImportance, nil)
	stop := newLitButton("Stop", nil, widget.DangerImportance, nil)
	diagnose := newLitButton("Open diagnostics", nil, widget.WarningImportance, nil)
	c := test.NewTempWindow(t, container.NewVBox(plain, record.lit, stop.lit, diagnose.lit)).Canvas()
	fill := func(b fyne.Widget) color.Color {
		return test.TempWidgetRenderer(t, b).Objects()[0].(*canvas.Rectangle).FillColor
	}
	same := func(a, b color.Color) bool { return color.NRGBAModel.Convert(a) == color.NRGBAModel.Convert(b) }
	light := func(n fyne.ThemeColorName) color.Color {
		return spytifyTheme{mode: "light"}.Color(n, theme.VariantLight)
	}
	tint := func(n fyne.ThemeColorName) color.Color {
		return over(light(theme.ColorNameButton), withAlpha(nrgba(light(n)), 0x7f))
	}
	for _, mode := range []string{"light", "dark"} {
		fa.Settings().SetTheme(spytifyTheme{mode: mode})
		c.Focus(plain)
		if mode == "light" {
			if got, want := fill(plain), over(light(theme.ColorNameButton), light(theme.ColorNameFocus)); !same(got, want) {
				t.Fatalf("a focused plain button is %v, want %v", got, want)
			}
		}
		for _, b := range []struct {
			button     *litButton
			colour, on fyne.ThemeColorName
			lit        color.Color
		}{
			{record, theme.ColorNamePrimary, theme.ColorNameForegroundOnPrimary, over(light(theme.ColorNameButton), light(theme.ColorNameFocus))},
			{stop, theme.ColorNameError, theme.ColorNameForegroundOnError, tint(theme.ColorNameError)},
			{diagnose, theme.ColorNameWarning, theme.ColorNameForegroundOnWarning, tint(theme.ColorNameWarning)},
		} {
			c.Focus(b.button)
			if got := fill(b.button); !same(got, b.lit) {
				t.Errorf("%s, %s focused: filled %v, want %v", mode, b.button.Text, got, b.lit)
			}
			if brown := over(theme.Color(b.colour), theme.Color(theme.ColorNameFocus)); same(fill(b.button), brown) {
				t.Errorf("%s, %s focused: still Fyne's green over its colour", mode, b.button.Text)
			}
			if got := theme.ColorForWidget(b.on, b.button); got != light(theme.ColorNameForeground) {
				t.Errorf("%s, %s focused: text %v, want the light theme's %v", mode, b.button.Text, got, light(theme.ColorNameForeground))
			}
			c.Focus(plain)
			if got := fill(b.button); !same(got, theme.Color(b.colour)) {
				t.Errorf("%s, %s out of focus: filled %v, want its %v", mode, b.button.Text, got, theme.Color(b.colour))
			}
			if got := theme.ColorForWidget(b.on, b.button); got != theme.Color(b.on) {
				t.Errorf("%s, %s out of focus: text %v, want %v", mode, b.button.Text, got, theme.Color(b.on))
			}
		}
	}
}

// A lit button hides and shows with what holds it in its layout, which
// leaves no gap for it in a row while it is hidden.
func TestALitButtonHidesWithItsTheme(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	b := newLitButton("Play", nil, widget.HighImportance, nil)
	show(b, false)
	if b.Visible() || b.lit.Visible() {
		t.Errorf("hidden: the button shown %v, its theme %v", b.Visible(), b.lit.Visible())
	}
	show(b, true)
	if !b.Visible() || !b.lit.Visible() {
		t.Errorf("shown: the button shown %v, its theme %v", b.Visible(), b.lit.Visible())
	}
}
