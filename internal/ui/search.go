package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/spytify/spytify/internal/ui/lang"
)

// newSearch is the Library's and Settings' search field: the magnifier
// before the text, for looks, and once something is typed a button at the
// end that clears it, which Tab reaches. changed runs on every edit.
func newSearch(a *App, placeholder string, changed func(string)) *keyEntry {
	e := newKeyEntry(a)
	clear := &clearButton{}
	clear.Icon, clear.Importance = theme.ContentClearIcon(), widget.LowImportance
	clear.OnTapped = func() {
		e.SetText("")
		// The button hides with the text: the focus goes back to the field
		// rather than to nothing.
		if c := fyne.CurrentApp().Driver().CanvasForObject(e); c != nil {
			c.Focus(e)
		}
	}
	clear.ExtendBaseWidget(clear)
	clear.SetToolTip(lang.ClearSearch)
	clear.Hide()
	// Before SetIcon: its refresh draws the field, with the action item it
	// has by then.
	e.ActionItem = container.NewThemeOverride(clear, clearTheme{cardTheme{input: colorCard}})
	e.SetIcon(theme.SearchIcon())
	e.SetPlaceHolder(placeholder)
	e.OnChanged = func(s string) {
		show(clear, s != "")
		changed(s)
	}
	return e
}

// clearOf is the clear button of a field newSearch made.
func clearOf(e *keyEntry) *clearButton {
	return e.ActionItem.(*container.ThemeOverride).Content.(*clearButton)
}

// clearButton is pressed by the space Space types rather than by the key,
// unlike button: it hands the focus to the field, which would then get
// that space.
type clearButton struct {
	tipButton
}

// MinSize is the button's shown, hidden too: Fyne's button counts no icon
// while hidden, and the field lays its text out once, before the button
// first shows.
func (b *clearButton) MinSize() fyne.Size {
	th := b.Theme()
	return fyne.NewSquareSize(th.Size(theme.SizeNameInlineIcon) + 2*th.Size(theme.SizeNameInnerPadding))
}

func (b *clearButton) TypedKey(e *fyne.KeyEvent) {
	if e.Name != fyne.KeySpace {
		b.tipButton.TypedKey(e)
	}
}

func (b *clearButton) TypedRune(r rune) {
	if r == ' ' && b.OnTapped != nil {
		b.OnTapped()
	}
}

// clearTheme draws the clear button with half the padding around its icon
// and no background under the pointer: only its focus shows.
type clearTheme struct {
	cardTheme
}

func (t clearTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameHover {
		return color.Transparent
	}
	return t.cardTheme.Color(n, v)
}

func (t clearTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInnerPadding {
		return t.cardTheme.Size(n) / 2
	}
	return t.cardTheme.Size(n)
}

// squareFields draws the fields in o white on the page, square, as the
// cards draw theirs.
func squareFields(o fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(o, cardTheme{input: colorCard})
}
