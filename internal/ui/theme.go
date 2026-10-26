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

// Settings: cards on a darker page, each card's advanced settings in a
// greyer block, and text in three steps, a section's title strongest.
const (
	colorPage          fyne.ThemeColorName = "spytifyPage"
	colorCard          fyne.ThemeColorName = "spytifyCard"
	colorCardLine      fyne.ThemeColorName = "spytifyCardLine"
	colorCardInput     fyne.ThemeColorName = "spytifyCardInput"
	colorAdvanced      fyne.ThemeColorName = "spytifyAdvanced"
	colorAdvancedInput fyne.ThemeColorName = "spytifyAdvancedInput"
	colorTitle         fyne.ThemeColorName = "spytifyTitle"
	colorLabel         fyne.ThemeColorName = "spytifyLabel"
	colorSubtle        fyne.ThemeColorName = "spytifySubtle"
)

// settingsColors are the light and dark values of the settings colours.
var settingsColors = map[fyne.ThemeColorName][2]color.NRGBA{
	colorPage:          {{R: 0xf2, G: 0xf2, B: 0xf5, A: 0xff}, {R: 0x0f, G: 0x0f, B: 0x10, A: 0xff}},
	colorCard:          {{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, {R: 0x1c, G: 0x1c, B: 0x1e, A: 0xff}},
	colorCardLine:      {{R: 0xe8, G: 0xe8, B: 0xed, A: 0xff}, {R: 0x2c, G: 0x2c, B: 0x2e, A: 0xff}},
	colorCardInput:     {{R: 0xf2, G: 0xf2, B: 0xf5, A: 0xff}, {R: 0x2a, G: 0x2a, B: 0x2d, A: 0xff}},
	colorAdvanced:      {{R: 0xf5, G: 0xf5, B: 0xf7, A: 0xff}, {R: 0x15, G: 0x15, B: 0x17, A: 0xff}},
	colorAdvancedInput: {{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, {R: 0x24, G: 0x24, B: 0x27, A: 0xff}},
	colorTitle:         {{R: 0x1d, G: 0x1d, B: 0x1f, A: 0xff}, {R: 0xf5, G: 0xf5, B: 0xf7, A: 0xff}},
	colorLabel:         {{R: 0x3a, G: 0x3a, B: 0x3e, A: 0xff}, {R: 0xd6, G: 0xd6, B: 0xdb, A: 0xff}},
	colorSubtle:        {{R: 0x6e, G: 0x6e, B: 0x73, A: 0xff}, {R: 0xa1, G: 0xa1, B: 0xa6, A: 0xff}},
}

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
	if c, ok := settingsColors[name]; ok {
		if light {
			return c[0]
		}
		return c[1]
	}
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
	// Hover and selection are drawn over what they mark, buttons included,
	// so they stay translucent: on white they read #f4f4f4 and #ececec.
	case theme.ColorNameHover:
		if light {
			return color.NRGBA{A: 0x0b}
		}
	case theme.ColorNameSelection:
		// A row picked in a list: a little greyer than one under the pointer.
		if light {
			return color.NRGBA{A: 0x13}
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

// cardTheme is the app's theme inside a settings card: its inputs and lines
// in the card's colours, and in the advanced block, smaller text.
type cardTheme struct {
	input fyne.ThemeColorName
	small bool
}

func (cardTheme) base() fyne.Theme { return fyne.CurrentApp().Settings().Theme() }

func (c cardTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground:
		name = c.input
	case theme.ColorNameSeparator:
		name = colorCardLine
	}
	return c.base().Color(name, v)
}

func (c cardTheme) Font(s fyne.TextStyle) fyne.Resource     { return c.base().Font(s) }
func (c cardTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return c.base().Icon(n) }

func (c cardTheme) Size(n fyne.ThemeSizeName) float32 {
	s := c.base().Size(n)
	if c.small {
		switch n {
		case theme.SizeNameText:
			return s - 1.5
		case theme.SizeNameInnerPadding:
			return s - 2
		}
	}
	return s
}
