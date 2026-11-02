package ui

import (
	"strconv"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// testCardList shows n numbered rows in a window of 400 by 300.
func testCardList(t *testing.T, n *int) (*cardList, fyne.Window) {
	t.Helper()
	test.NewTempApp(t).Settings().SetTheme(spytifyTheme{mode: "light"})
	l := newCardList(func() int { return *n },
		func() fyne.CanvasObject { return widget.NewLabel("row") },
		func(id widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(strconv.Itoa(id)) })
	w := test.NewTempWindow(t, l)
	w.Resize(fyne.NewSize(400, 300))
	l.Refresh()
	return l, w
}

// drawnIDs is the rows the list built, top to bottom.
func drawnIDs(l *cardList) []widget.ListItemID {
	var ids []widget.ListItemID
	for _, s := range l.shown {
		ids = append(ids, s.id)
	}
	return ids
}

func rowAt(l *cardList, id widget.ListItemID) *cardRow {
	for _, s := range l.shown {
		if s.id == id {
			return s.row
		}
	}
	return nil
}

// The card is as tall as its rows, so the page shows below a few; with more
// rows than fit, it runs past the window and the page scrolls.
func TestAListCardIsAsTallAsItsRows(t *testing.T) {
	n := 3
	l, _ := testCardList(t, &n)
	row, pad := l.itemMin.Height, theme.Padding()
	if want := fyne.NewSize(l.Size().Width, 3*(row+pad)+pad); l.card.Size() != want {
		t.Errorf("3 rows: the card is %v, want %v", l.card.Size(), want)
	}
	l.ScrollToBottom()
	if off := l.scroll.Offset.Y; off != 0 {
		t.Errorf("3 rows: the page scrolls by %v", off)
	}

	n = 100
	l.Refresh()
	if h := l.card.Size().Height; h != 100*(row+pad)+pad {
		t.Errorf("100 rows: the card is %v high", h)
	}
	l.ScrollToBottom()
	if l.scroll.Offset.Y == 0 {
		t.Error("100 rows: the page does not scroll")
	}
	if ids := drawnIDs(l); len(ids) == 0 || ids[len(ids)-1] != 99 {
		t.Errorf("at the bottom, rows %v of 100 are drawn", ids)
	}
}

// The card scrolls with the page, as Settings' cards do, and the scroll bar
// is at the window's edge: the list scrolled inside its card.
func TestTheListCardScrollsWithThePage(t *testing.T) {
	n := 100
	l, _ := testCardList(t, &n)
	if l.scroll.Size() != l.Size() {
		t.Errorf("the scroll is %v in a list of %v", l.scroll.Size(), l.Size())
	}
	// A scroll as the wheel makes it, without Scrolled: on macOS it starts a
	// timer that refreshes the list half a second later, from another
	// goroutine, while the next test changes the theme.
	l.scroll.ScrollToOffset(fyne.NewPos(0, 500))
	l.scroll.OnScrolled(l.scroll.Offset)
	if top := l.page.Position().Y + l.card.Position().Y; top >= 0 {
		t.Errorf("the card's top stayed in view at %v after a scroll down", top)
	}
	// Like widget.List, only the rows in view are built.
	if ids := drawnIDs(l); len(ids) == 0 || len(ids) > 12 || ids[0] == 0 {
		t.Errorf("after a scroll down, rows %v of 100 are drawn", ids)
	}
}

// The card is square and white with no outline, from one side of the list
// to the other; its rows' content is off the sides, their highlight not, and
// they see the card's colours:
// its surface as the background, inputs a shade darker.
func TestAListCardIsDrawnInTheCardsColours(t *testing.T) {
	n := 2
	l, _ := testCardList(t, &n)
	if l.card.FillColor != cardColors[colorCard][0] || l.card.StrokeWidth != 0 || l.card.CornerRadius != 0 || l.card.Position() != (fyne.Position{}) {
		t.Errorf("the card is %v, outlined %v wide, rounded %v, at %v", l.card.FillColor, l.card.StrokeWidth, l.card.CornerRadius, l.card.Position())
	}
	first, pad := rowAt(l, 0), theme.Padding()
	if first.content.Position() != fyne.NewPos(3*pad, 0) || first.content.Size().Width != l.Size().Width-6*pad {
		t.Errorf("the first row's content is at %v, %v wide, in a list %v wide", first.content.Position(), first.content.Size().Width, l.Size().Width)
	}
	// The highlight runs from one side of the card to the other, as the
	// card does; it stopped at the content's sides.
	if first.box.Position().X != 0 || first.hover.Position().X != 0 || first.hover.Size().Width != l.Size().Width {
		t.Errorf("the highlight is at %v, %v wide, in a list %v wide", first.hover.Position().X+first.box.Position().X, first.hover.Size().Width, l.Size().Width)
	}
	if !first.line.Hidden || rowAt(l, 1).line.Hidden {
		t.Error("the line is above the first row, or not above the second")
	}
	label := first.content.(*widget.Label)
	if got := theme.ColorForWidget(theme.ColorNameBackground, label); got != cardColors[colorCard][0] {
		t.Errorf("a row's background is %v", got)
	}
	if got := theme.ColorForWidget(theme.ColorNameInputBackground, label); got != cardColors[colorCardInput][0] {
		t.Errorf("an input on the card is %v", got)
	}
	if _, ok := first.box.(*container.ThemeOverride); !ok {
		t.Errorf("the row is a %T", first.box)
	}
}

// Tab stops on the list, as on widget.List: the arrows move a highlight
// from row to row and scroll it into view.
func TestTheArrowsMoveAHighlightThroughTheList(t *testing.T) {
	n := 100
	l, w := testCardList(t, &n)
	c := w.Canvas()
	c.FocusNext()
	if c.Focused() != l {
		t.Fatalf("Tab focused %T", c.Focused())
	}
	if row := rowAt(l, 0); row.hover.Hidden {
		t.Error("the focused list highlights no row")
	}
	for range 20 {
		l.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	}
	row := rowAt(l, 20)
	if row == nil || row.hover.Hidden || !rowAt(l, 19).hover.Hidden {
		t.Fatalf("20 rows down, rows %v drawn, the 20th highlighted %v", drawnIDs(l), row != nil && !row.hover.Hidden)
	}
	if bottom := row.box.Position().Y + row.box.Size().Height - l.scroll.Offset.Y; bottom > l.scroll.Size().Height {
		t.Errorf("the highlighted row ends at %v, out of a page %v high", bottom, l.scroll.Size().Height)
	}
	for range 25 {
		l.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	}
	if l.cursor != 0 || l.scroll.Offset.Y != 0 {
		t.Errorf("back up, row %d is highlighted, the page scrolled by %v", l.cursor, l.scroll.Offset.Y)
	}
	c.Unfocus()
	if !rowAt(l, 0).hover.Hidden {
		t.Error("the row stays highlighted once the list loses the focus")
	}
}
