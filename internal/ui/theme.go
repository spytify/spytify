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

// Muted status colours for the session list, softer than Success and
// Warning, which stay for the status dot and the level bar.
const (
	colorSaved   fyne.ThemeColorName = "spytifySaved"
	colorSkipped fyne.ThemeColorName = "spytifySkipped"
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
	light := v == theme.VariantLight
	switch name {
	case colorSaved:
		if light {
			return color.NRGBA{R: 0x4f, G: 0x8f, B: 0x66, A: 0xff}
		}
		return color.NRGBA{R: 0x7c, G: 0xb8, B: 0x93, A: 0xff}
	case colorSkipped:
		if light {
			return color.NRGBA{R: 0xa8, G: 0x84, B: 0x4c, A: 0xff}
		}
		return color.NRGBA{R: 0xc4, G: 0xa4, B: 0x70, A: 0xff}
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return green
	case theme.ColorNameFocus:
		return withAlpha(green, 0x7f)
	case theme.ColorNameHover:
		if v == theme.VariantLight {
			return color.NRGBA{R: 0xf4, G: 0xf4, B: 0xf4, A: 0xff}
		}
	case theme.ColorNameSelection:
		// A row picked in a list: a little greyer than one under the pointer.
		if v == theme.VariantLight {
			return color.NRGBA{R: 0xec, G: 0xec, B: 0xec, A: 0xff}
		}
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c}
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
