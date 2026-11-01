package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
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

// footerBar is a screen's bottom bar, on a grey of its own under a line:
// what the screen holds on the left, its actions on the right.
func footerBar(left, actions fyne.CanvasObject) fyne.CanvasObject {
	pad := theme.Padding()
	row := container.New(layout.NewCustomPaddedLayout(pad/2, pad/2, pad*2, pad),
		container.NewBorder(nil, nil, nil, actions, container.NewVBox(layout.NewSpacer(), left, layout.NewSpacer())))
	return container.NewBorder(widget.NewSeparator(), nil, nil, nil, newPanel(colorFooter, "", 0, fyne.Size{}, row))
}

// outlined draws a line around a button without a fill of its own, for a
// secondary action on a list row.
func outlined(b *widget.Button) *panel {
	b.Importance = widget.LowImportance
	return newPanel("", colorOutline, 8, fyne.Size{}, b)
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
