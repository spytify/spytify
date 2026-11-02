package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// keyRows scrolls a column of rows that the keyboard reaches as it does
// the lists: Tab stops on it, and the arrows move a highlight from row to
// row, scrolling it into view. A scroll alone takes no focus, so its rows
// could not be gone through without the pointer. The separators between
// the rows are passed over.
type keyRows struct {
	widget.BaseWidget
	rows    *fyne.Container
	scroll  *container.Scroll
	mark    *canvas.Rectangle
	focused bool
	cursor  int
}

var _ fyne.Focusable = (*keyRows)(nil)

func newKeyRows(rows *fyne.Container) *keyRows {
	k := &keyRows{rows: rows, mark: canvas.NewRectangle(nil)}
	k.mark.Hide()
	k.scroll = container.NewVScroll(container.NewStack(container.NewWithoutLayout(k.mark), rows))
	k.ExtendBaseWidget(k)
	return k
}

// items are the rows the highlight stops on.
func (k *keyRows) items() []fyne.CanvasObject {
	var out []fyne.CanvasObject
	for _, o := range k.rows.Objects {
		if _, ok := o.(*widget.Separator); !ok {
			out = append(out, o)
		}
	}
	return out
}

func (k *keyRows) FocusGained() {
	k.focused = true
	k.place()
}

func (k *keyRows) FocusLost() {
	k.focused = false
	k.place()
}

// TypedKey moves the highlight a row down or up; Escape closes the dialog
// the rows are in.
func (k *keyRows) TypedKey(e *fyne.KeyEvent) {
	switch {
	case e.Name == fyne.KeyDown && k.cursor < len(k.items())-1:
		k.cursor++
	case e.Name == fyne.KeyUp && k.cursor > 0:
		k.cursor--
	default:
		escapeFrom(k, e)
		return
	}
	k.place()
}

func (k *keyRows) TypedRune(rune) {}

// place draws the highlight under the cursor's row while the rows have the
// focus, and scrolls as little as it takes to show that row whole.
func (k *keyRows) place() {
	items := k.items()
	k.cursor = max(0, min(k.cursor, len(items)-1))
	if !k.focused || len(items) == 0 {
		k.mark.Hide()
		return
	}
	row := items[k.cursor]
	top, height := row.Position().Y, row.Size().Height
	k.mark.FillColor = theme.ColorForWidget(theme.ColorNameHover, k)
	k.mark.Move(fyne.NewPos(0, top))
	k.mark.Resize(fyne.NewSize(k.rows.Size().Width, height))
	k.mark.Show()
	k.mark.Refresh()
	off := k.scroll.Offset
	if view := k.scroll.Size().Height; top+height > off.Y+view {
		off.Y = top + height - view
	}
	off.Y = min(off.Y, top)
	k.scroll.ScrollToOffset(off)
}

func (k *keyRows) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(k.scroll)
}
