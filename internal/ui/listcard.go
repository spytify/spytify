package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// listCard holds a list on the card's colour, as tall as its rows up to the
// room it is given, square and from one side of the window to the other: a
// line between rows starts under their covers. Refresh it when the number
// of rows changes.
func listCard(list *widget.List) *fyne.Container {
	box := newPanel(colorCard, "", 0, fyne.NewSize(3*theme.Padding(), 0), list)
	return container.New(fitRows{list}, container.NewThemeOverride(box, cardTheme{input: colorCardInput, surface: colorCard}))
}

// fitRows lays a list's card out no taller than its rows: under software
// GL, as on Linux without a graphics card, each pixel a card covers costs
// CPU on every frame, and the level meter draws ten a second.
type fitRows struct{ list *widget.List }

func (f fitRows) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	h := size.Height
	if n := f.list.Length(); n > 0 {
		// The list puts a padding between its rows; the card one around them.
		pad := theme.Padding()
		row := f.list.CreateItem().MinSize().Height
		h = min(h, float32(n)*(row+pad)+pad)
	}
	objs[0].Resize(fyne.NewSize(size.Width, h))
}

func (fitRows) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }
