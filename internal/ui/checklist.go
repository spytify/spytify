package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// checkList is the diagnostics' checks, which scroll in box past
// checksMaxHeight. The dialog opens with the focus on it, as Tab first
// stops on it, for the arrows to move a highlight from check to check, as
// on the Library's list; Tab then goes on to the fixes in the checks.
type checkList struct {
	widget.BaseWidget
	box       *fyne.Container
	scroll    *container.Scroll
	page      *fyne.Container
	highlight *canvas.Rectangle
	rows      []fyne.CanvasObject
	// cursor is the check the arrows highlight while the list has the
	// focus.
	focused bool
	cursor  int
}

var _ fyne.Focusable = (*checkList)(nil)

func newCheckList(box *fyne.Container) *checkList {
	l := &checkList{box: box, highlight: canvas.NewRectangle(color.Transparent)}
	l.highlight.Hide()
	l.page = container.New(checkPage{l}, l.highlight, box)
	l.scroll = container.NewVScroll(l.page)
	l.ExtendBaseWidget(l)
	return l
}

// setRows shows rows, a line between each, the highlight on the last: what
// needs attention, where the list is scrolled to.
func (l *checkList) setRows(rows []fyne.CanvasObject) {
	l.rows = rows
	l.cursor = max(0, len(rows)-1)
	l.box.Objects = lines(rows)
	l.box.Refresh()
	l.page.Refresh()
}

func (l *checkList) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(l.scroll)
}

func (l *checkList) FocusGained() {
	l.focused = true
	l.page.Refresh()
}

func (l *checkList) FocusLost() {
	l.focused = false
	l.page.Refresh()
}

// TypedKey moves the highlight a check down or up, scrolling it into view.
// Escape closes the dialog.
func (l *checkList) TypedKey(e *fyne.KeyEvent) {
	switch {
	case e.Name == fyne.KeyDown && l.cursor < len(l.rows)-1:
		l.cursor++
	case e.Name == fyne.KeyUp && l.cursor > 0:
		l.cursor--
	default:
		escapeFrom(l, e)
		return
	}
	row := l.rows[l.cursor]
	top, off := row.Position().Y, l.scroll.Offset
	if view := l.scroll.Size().Height; top+row.Size().Height > off.Y+view {
		off.Y = top + row.Size().Height - view
	}
	off.Y = min(off.Y, top)
	l.scroll.ScrollToOffset(off)
	l.page.Refresh()
}

func (l *checkList) TypedRune(rune) {}

// checkPage lays the checks out, the highlight under the one the arrows
// are on, from one side of the list to the other.
type checkPage struct{ l *checkList }

func (p checkPage) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l := p.l
	l.box.Resize(size)
	l.highlight.Hidden = !l.focused || len(l.rows) == 0
	if l.highlight.Hidden {
		return
	}
	row := l.rows[l.cursor]
	l.highlight.FillColor = theme.ColorForWidget(theme.ColorNameHover, l)
	l.highlight.Move(fyne.NewPos(0, row.Position().Y))
	l.highlight.Resize(fyne.NewSize(size.Width, row.Size().Height))
	l.highlight.Refresh()
}

func (p checkPage) MinSize([]fyne.CanvasObject) fyne.Size { return p.l.box.MinSize() }
