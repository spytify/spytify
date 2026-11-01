package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A list's card is as tall as its rows, so the list shows them all without
// scrolling and the page shows below; with more rows than fit, it takes the
// room it is given and the list scrolls. With no rows, it takes the room.
func TestAListCardIsAsTallAsItsRows(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	n := 3
	list := widget.NewList(func() int { return n }, func() fyne.CanvasObject { return widget.NewLabel("row") }, func(widget.ListItemID, fyne.CanvasObject) {})
	card := listCard(list)
	test.NewTempWindow(t, card)
	card.Resize(fyne.NewSize(400, 600))
	box := card.Objects[0]
	row, pad := list.CreateItem().MinSize().Height, theme.Padding()
	if want := fyne.NewSize(400, 3*(row+pad)+pad); box.Size() != want {
		t.Errorf("3 rows: the card is %v, want %v", box.Size(), want)
	}
	list.ScrollToBottom()
	if off := list.GetScrollOffset(); off != 0 {
		t.Errorf("3 rows: the list scrolls by %v", off)
	}

	n = 100
	card.Refresh()
	if box.Size() != fyne.NewSize(400, 600) {
		t.Errorf("100 rows: the card is %v, want all the room", box.Size())
	}
	list.ScrollToBottom()
	if list.GetScrollOffset() == 0 {
		t.Error("100 rows: the list does not scroll")
	}

	n = 0
	card.Refresh()
	if box.Size() != fyne.NewSize(400, 600) {
		t.Errorf("no rows: the card is %v, want all the room", box.Size())
	}
}

// The card is a square white panel with no outline, its list off the sides
// only, and the list inside sees the card's colours: its surface as the
// background, inputs a shade darker.
func TestAListCardIsDrawnInTheCardsColours(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	list := widget.NewList(func() int { return 1 }, func() fyne.CanvasObject { return widget.NewLabel("row") }, func(widget.ListItemID, fyne.CanvasObject) {})
	card := listCard(list)
	test.NewTempWindow(t, card)
	p := card.Objects[0].(*container.ThemeOverride).Content.(*panel)
	box := test.TempWidgetRenderer(t, p).(*panelRenderer).box
	if box.FillColor != cardColors[colorCard][0] || box.StrokeWidth != 0 || box.CornerRadius != 0 {
		t.Errorf("the card is %v, outlined %v wide, rounded %v", box.FillColor, box.StrokeWidth, box.CornerRadius)
	}
	if pad := theme.Padding(); list.Position() != fyne.NewPos(3*pad, 0) {
		t.Errorf("the list is at %v in its card", list.Position())
	}
	if got := theme.ColorForWidget(theme.ColorNameBackground, list); got != cardColors[colorCard][0] {
		t.Errorf("the list's background is %v", got)
	}
	if got := theme.ColorForWidget(theme.ColorNameInputBackground, list); got != cardColors[colorCardInput][0] {
		t.Errorf("an input on the card is %v", got)
	}
}
