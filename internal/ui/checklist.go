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
// checksMaxHeight. The dialog opens with the focus on it, Tab stopping on
// it once, as on the Library's list: Up and Down move a highlight from
// check to check, Left and Right between the check and its fixes, Space
// presses the fix highlighted, and Tab goes on to the dialog's buttons.
type checkList struct {
	widget.BaseWidget
	box       *fyne.Container
	scroll    *container.Scroll
	page      *fyne.Container
	highlight *canvas.Rectangle
	rows      []fyne.CanvasObject
	// cursor is the check the arrows highlight while the list has the
	// focus, col its fix they are on, 0 for the check itself, and lit that
	// fix, which looks focused.
	focused bool
	cursor  int
	col     int
	lit     *rowLitButton
}

// rowLitButton is a check's fix, a litButton that Tab passes over, as it
// does a Library row's buttons: the list's arrows light it, and Space on
// the list presses it.
type rowLitButton struct {
	litButton
	// FocusGained hides litButton's: without it, rowLitButton is no
	// fyne.Focusable, the one thing Tab looks for.
	FocusGained struct{}
}

func newRowLitButton(text string, icon fyne.Resource, imp widget.Importance, tapped func()) *rowLitButton {
	b := &rowLitButton{}
	b.Text, b.Icon, b.Importance, b.OnTapped = text, icon, imp, tapped
	b.ExtendBaseWidget(b)
	b.lit = newThemed(b, litTheme{&b.litButton})
	return b
}

// checkFixes are the fixes a check's row shows, in their order.
func checkFixes(o fyne.CanvasObject) []*rowLitButton {
	if !o.Visible() {
		return nil
	}
	switch o := o.(type) {
	case *rowLitButton:
		if !o.Disabled() {
			return []*rowLitButton{o}
		}
	case *themed:
		return checkFixes(o.o.Content)
	case *fyne.Container:
		var fixes []*rowLitButton
		for _, child := range o.Objects {
			fixes = append(fixes, checkFixes(child)...)
		}
		return fixes
	}
	return nil
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
	// The fix lit went with its row.
	l.col, l.lit = 0, nil
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
	l.col = 0
	l.relight()
	l.page.Refresh()
}

// TypedKey moves the highlight a check down or up, scrolling it into view,
// or to a fix of the check and back; Space presses the fix. Escape closes
// the dialog.
func (l *checkList) TypedKey(e *fyne.KeyEvent) {
	switch {
	case e.Name == fyne.KeyDown && l.cursor < len(l.rows)-1:
		l.cursor++
		l.col = 0
	case e.Name == fyne.KeyUp && l.cursor > 0:
		l.cursor--
		l.col = 0
	case e.Name == fyne.KeyLeft && l.col > 0:
		l.col--
	case e.Name == fyne.KeyRight:
		// relight stops it at the last fix.
		l.col++
	case e.Name == fyne.KeySpace:
		if l.lit != nil && l.lit.OnTapped != nil {
			l.lit.OnTapped()
		}
		return
	default:
		escapeFrom(l, e)
		return
	}
	l.relight()
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

// relight lights the fix the arrows are on: none on the check itself, nor
// while the list has not the focus.
func (l *checkList) relight() {
	var lit *rowLitButton
	if l.focused && len(l.rows) > 0 {
		fixes := checkFixes(l.rows[l.cursor])
		l.col = min(l.col, len(fixes))
		if l.col > 0 {
			lit = fixes[l.col-1]
		}
	}
	if lit == l.lit {
		return
	}
	if l.lit != nil {
		l.lit.litButton.FocusLost()
	}
	if lit != nil {
		lit.litButton.FocusGained()
	}
	l.lit = lit
}

// checkPage lays the checks out off the dialog's sides, in line with its
// title, and the highlight under the one the arrows are on from one side
// to the other.
type checkPage struct{ l *checkList }

// checkSide is how far the checks are from the dialog's sides.
func checkSide() float32 { return 2 * theme.Padding() }

func (p checkPage) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l := p.l
	l.box.Move(fyne.NewPos(checkSide(), 0))
	l.box.Resize(fyne.NewSize(size.Width-2*checkSide(), size.Height))
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

func (p checkPage) MinSize([]fyne.CanvasObject) fyne.Size {
	return p.l.box.MinSize().AddWidthHeight(2*checkSide(), 0)
}
