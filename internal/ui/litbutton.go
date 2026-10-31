package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// litButton is a button filled with a colour, the green, the red of Stop
// or a warning's orange, that looks focused as a plain button does in the
// light theme, in either theme: the light grey under a tint of its colour,
// light enough for dark text. Fyne blends its focus colour, a translucent
// green, over the fill instead, which turned the red brown and hardly
// changed the green; the dark theme's green is already a light one. It
// takes a tooltip, as tipButton does.
type litButton struct {
	tipButton
	focused bool
	// lit is the button in the theme that lights it up: the layouts hold
	// it, and it shows and hides with the button.
	lit *container.ThemeOverride
}

func newLitButton(text string, icon fyne.Resource, imp widget.Importance, tapped func()) *litButton {
	b := &litButton{}
	b.Text, b.Icon, b.Importance, b.OnTapped = text, icon, imp, tapped
	b.ExtendBaseWidget(b)
	b.lit = container.NewThemeOverride(b, litTheme{b})
	return b
}

// TypedKey is button's, for the lit button: Fyne finds the window of the
// widget it holds, not of the button inside.
func (b *litButton) TypedKey(e *fyne.KeyEvent) {
	pressOnSpace(&b.Button.Button, e)
	escapeFrom(b, e)
}

func (b *litButton) FocusGained() {
	b.focused = true
	b.Button.FocusGained()
}

func (b *litButton) FocusLost() {
	b.focused = false
	b.Button.FocusLost()
}

func (b *litButton) Show() {
	b.Button.Show()
	b.lit.Show()
}

func (b *litButton) Hide() {
	b.Button.Hide()
	b.lit.Hide()
}

// litTheme is the app's theme with, while its button has the focus, the
// light theme's plain button grey for the fill, a tint of the fill's
// colour for the focus, which Fyne blends over it, and the light theme's
// text colour.
type litTheme struct{ b *litButton }

func (t litTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if t.b.focused {
		if c := litColor(n, t.b.Importance); c != nil {
			return c
		}
	}
	return fyne.CurrentApp().Settings().Theme().Color(n, v)
}

// litColor is the colour n has on a focused button of importance imp, or
// nil for the theme's own.
func litColor(n fyne.ThemeColorName, imp widget.Importance) color.Color {
	light := spytifyTheme{mode: "light"}
	switch n {
	case theme.ColorNamePrimary, theme.ColorNameError, theme.ColorNameWarning:
		return light.Color(theme.ColorNameButton, theme.VariantLight)
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnError, theme.ColorNameForegroundOnWarning:
		return light.Color(theme.ColorNameForeground, theme.VariantLight)
	case theme.ColorNameFocus:
		switch imp {
		case widget.DangerImportance:
			return withAlpha(nrgba(light.Color(theme.ColorNameError, theme.VariantLight)), 0x7f)
		case widget.WarningImportance:
			return withAlpha(nrgba(light.Color(theme.ColorNameWarning, theme.VariantLight)), 0x7f)
		}
		return light.Color(theme.ColorNameFocus, theme.VariantLight)
	}
	return nil
}

func (litTheme) Font(s fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(s)
}

func (litTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(n)
}

func (litTheme) Size(n fyne.ThemeSizeName) float32 {
	return fyne.CurrentApp().Settings().Theme().Size(n)
}

func nrgba(c color.Color) color.NRGBA {
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}
