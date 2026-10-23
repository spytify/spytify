package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Spytify's green: the logo's on the dark theme, a deeper one on the light
// theme so white text on a button stays readable.
var (
	greenDark  = color.NRGBA{R: 0x1e, G: 0xd6, B: 0x5d, A: 0xff}
	greenLight = color.NRGBA{R: 0x13, G: 0x8a, B: 0x3f, A: 0xff}
)

// spytifyTheme is Fyne's theme with the green in place of the blue accent.
// mode is app.theme: "system" follows the OS, "light" and "dark" force it.
type spytifyTheme struct {
	mode string
}

func (t spytifyTheme) variant(v fyne.ThemeVariant) fyne.ThemeVariant {
	switch t.mode {
	case "light":
		return theme.VariantLight
	case "dark":
		return theme.VariantDark
	}
	return v
}

func (t spytifyTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	v = t.variant(v)
	green := greenDark
	if v == theme.VariantLight {
		green = greenLight
	}
	switch name {
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return green
	case theme.ColorNameFocus:
		return withAlpha(green, 0x7f)
	case theme.ColorNameSelection:
		return withAlpha(green, 0x3f)
	}
	return theme.DefaultTheme().Color(name, v)
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

func (spytifyTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (spytifyTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (spytifyTheme) Size(n fyne.ThemeSizeName) float32       { return theme.DefaultTheme().Size(n) }
