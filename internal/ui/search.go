package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// newSearch is the Library's and Settings' search field: the magnifier
// before the text, for looks, and once something is typed a button at the
// end that clears it, which Tab reaches. changed runs on every edit.
func newSearch(placeholder string, changed func(string)) *widget.Entry {
	e := widget.NewEntry()
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
	clear.SetToolTip("Clear the search")
	clear.Hide()
	// Before SetIcon: its refresh draws the field, with the action item it
	// has by then.
	e.ActionItem = clear
	e.SetIcon(theme.SearchIcon())
	e.SetPlaceHolder(placeholder)
	e.OnChanged = func(s string) {
		show(clear, s != "")
		changed(s)
	}
	return e
}

// clearButton is pressed by the space Space types rather than by the key,
// unlike button: it hands the focus to the field, which would then get
// that space.
type clearButton struct {
	tipButton
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

// searchBox draws a search field white on the page, as the cards are, with
// square corners.
func searchBox(search *widget.Entry) fyne.CanvasObject {
	return container.NewThemeOverride(search, cardTheme{input: colorCard, square: true})
}
