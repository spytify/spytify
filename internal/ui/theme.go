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

// A list row's status badge and the text on it: a soft green for a saved
// file and a sand for a skipped one on the light theme, deep ones under
// light text on the dark theme.
const (
	colorSavedBadge     fyne.ThemeColorName = "spytifySavedBadge"
	colorOnSavedBadge   fyne.ThemeColorName = "spytifyOnSavedBadge"
	colorSkippedBadge   fyne.ThemeColorName = "spytifySkippedBadge"
	colorOnSkippedBadge fyne.ThemeColorName = "spytifyOnSkippedBadge"
)

// A screen's footer, on a grey of its own under the list, and the line
// around a button drawn as an outline.
const (
	colorFooter  fyne.ThemeColorName = "spytifyFooter"
	colorOutline fyne.ThemeColorName = "spytifyOutline"
)

// An info alert's text, in the green of its outline and icon, darker on the
// light theme and lighter on the dark one, as MUI's outlined Alert does.
const colorInfoText fyne.ThemeColorName = "spytifyInfoText"

// Cards on a darker page, a settings card's advanced settings in a greyer
// block, and text in three steps, a card's title strongest. The bars at
// the top, the log and About stay on Fyne's plain window colour.
const (
	colorPage          fyne.ThemeColorName = "spytifyPage"
	colorPlain         fyne.ThemeColorName = "spytifyPlain"
	colorCard          fyne.ThemeColorName = "spytifyCard"
	colorCardLine      fyne.ThemeColorName = "spytifyCardLine"
	colorCardInput     fyne.ThemeColorName = "spytifyCardInput"
	colorAdvanced      fyne.ThemeColorName = "spytifyAdvanced"
	colorAdvancedInput fyne.ThemeColorName = "spytifyAdvancedInput"
	colorScrollShadow  fyne.ThemeColorName = "spytifyScrollShadow"
	colorTitle         fyne.ThemeColorName = "spytifyTitle"
	colorLabel         fyne.ThemeColorName = "spytifyLabel"
	colorSubtle        fyne.ThemeColorName = "spytifySubtle"
)

// cardColors are the light and dark values of the colours above.
var cardColors = map[fyne.ThemeColorName][2]color.NRGBA{
	colorSavedBadge:     {{R: 0xdc, G: 0xef, B: 0xe2, A: 0xff}, {R: 0x17, G: 0x35, B: 0x23, A: 0xff}},
	colorOnSavedBadge:   {{R: 0x1e, G: 0x4d, B: 0x30, A: 0xff}, {R: 0xb5, G: 0xe3, B: 0xc5, A: 0xff}},
	colorSkippedBadge:   {{R: 0xf8, G: 0xec, B: 0xd4, A: 0xff}, {R: 0x3d, G: 0x2f, B: 0x12, A: 0xff}},
	colorOnSkippedBadge: {{R: 0x5a, G: 0x42, B: 0x12, A: 0xff}, {R: 0xf2, G: 0xd9, B: 0xa6, A: 0xff}},
	colorFooter:         {{R: 0xeb, G: 0xeb, B: 0xef, A: 0xff}, {R: 0x14, G: 0x14, B: 0x16, A: 0xff}},
	colorOutline:        {{R: 0xd2, G: 0xd2, B: 0xd8, A: 0xff}, {R: 0x3a, G: 0x3a, B: 0x3e, A: 0xff}},
	colorPage:           {{R: 0xf2, G: 0xf2, B: 0xf5, A: 0xff}, {R: 0x0f, G: 0x0f, B: 0x10, A: 0xff}},
	colorPlain:          {{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, {R: 0x17, G: 0x17, B: 0x18, A: 0xff}},
	colorCard:           {{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, {R: 0x1c, G: 0x1c, B: 0x1e, A: 0xff}},
	colorCardLine:       {{R: 0xe8, G: 0xe8, B: 0xed, A: 0xff}, {R: 0x2c, G: 0x2c, B: 0x2e, A: 0xff}},
	colorCardInput:      {{R: 0xf2, G: 0xf2, B: 0xf5, A: 0xff}, {R: 0x2a, G: 0x2a, B: 0x2d, A: 0xff}},
	colorAdvanced:       {{R: 0xf5, G: 0xf5, B: 0xf7, A: 0xff}, {R: 0x15, G: 0x15, B: 0x17, A: 0xff}},
	colorAdvancedInput:  {{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, {R: 0x24, G: 0x24, B: 0x27, A: 0xff}},
	colorScrollShadow:   {{A: 0x14}, {A: 0x40}},
	colorTitle:          {{R: 0x1d, G: 0x1d, B: 0x1f, A: 0xff}, {R: 0xf5, G: 0xf5, B: 0xf7, A: 0xff}},
	colorLabel:          {{R: 0x3a, G: 0x3a, B: 0x3e, A: 0xff}, {R: 0xd6, G: 0xd6, B: 0xdb, A: 0xff}},
	colorSubtle:         {{R: 0x6e, G: 0x6e, B: 0x73, A: 0xff}, {R: 0xa1, G: 0xa1, B: 0xa6, A: 0xff}},
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
	// The window is cleared to the background colour before each frame, at
	// no cost: the page goes there. A rectangle the size of the window would
	// be filled again on every frame the level meter moves, which software
	// GL does on the CPU.
	if name == theme.ColorNameBackground {
		name = colorPage
	}
	if c, ok := cardColors[name]; ok {
		if light {
			return c[0]
		}
		return c[1]
	}
	switch name {
	case colorInfoText:
		if light {
			return color.NRGBA{R: 0x08, G: 0x37, B: 0x19, A: 0xff}
		}
		return color.NRGBA{R: 0xa5, G: 0xef, B: 0xbe, A: 0xff}
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

// cardTheme is the app's theme on a card or another surface: its inputs and
// lines in the card's colours, a ticked check box showing the surface it is
// on, a light shadow at a scrolled list's edges, and in the advanced block,
// smaller text.
type cardTheme struct {
	input, surface fyne.ThemeColorName
	small          bool
}

func (cardTheme) base() fyne.Theme { return fyne.CurrentApp().Settings().Theme() }

func (c cardTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch {
	case name == theme.ColorNameInputBackground && c.input != "":
		name = c.input
	case name == theme.ColorNameBackground && c.surface != "":
		name = c.surface
	case name == theme.ColorNameSeparator:
		name = colorCardLine
	case name == theme.ColorNameShadow:
		name = colorScrollShadow
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

// The recording card's own colours: a badge of translucent white and the
// outline of one, and the red Recording badge with its dot.
const (
	colorMediaBadge fyne.ThemeColorName = "spytifyMediaBadge"
	colorMediaLine  fyne.ThemeColorName = "spytifyMediaLine"
	colorRecBadge   fyne.ThemeColorName = "spytifyRecBadge"
	colorOnRecBadge fyne.ThemeColorName = "spytifyOnRecBadge"
)

// mediaTheme is the app's theme on the recording card, over the cover's
// darkened colours whichever the app's theme: white text and icons,
// buttons and the bar's track in translucent white, the bright green.
type mediaTheme struct{}

func (mediaTheme) base() fyne.Theme { return fyne.CurrentApp().Settings().Theme() }

func (m mediaTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	switch name {
	case theme.ColorNameForeground:
		return white
	case theme.ColorNamePlaceHolder:
		return withAlpha(white, 0xd9)
	case theme.ColorNameButton, colorMediaBadge:
		return withAlpha(white, 0x2e)
	case theme.ColorNameHover:
		return withAlpha(white, 0x1f)
	case theme.ColorNamePressed:
		return withAlpha(white, 0x40)
	case theme.ColorNameInputBackground:
		return withAlpha(white, 0x4d)
	case colorMediaLine:
		return withAlpha(white, 0x66)
	case theme.ColorNamePrimary:
		return greenDark
	case colorRecBadge:
		return color.NRGBA{R: 0xff, G: 0xda, B: 0xd6, A: 0xff}
	case colorOnRecBadge:
		return color.NRGBA{R: 0x41, G: 0x00, B: 0x02, A: 0xff}
	}
	return m.base().Color(name, v)
}

func (m mediaTheme) Font(s fyne.TextStyle) fyne.Resource     { return m.base().Font(s) }
func (m mediaTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return m.base().Icon(n) }
func (m mediaTheme) Size(n fyne.ThemeSizeName) float32       { return m.base().Size(n) }
