package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// cardList shows rows of one height on the card's colour, square and from
// one side of the window to the other, on a page that scrolls as Settings'
// does: its scroll bar is at the window's edge, where a widget.List puts
// its own inside the card. Like widget.List, it builds only the rows in
// view, as a library holds thousands of files, and Tab stops on it for the
// arrows to move a highlight from row to row.
type cardList struct {
	widget.BaseWidget
	length func() int
	create func() fyne.CanvasObject
	update func(id widget.ListItemID, o fyne.CanvasObject)

	scroll  *container.Scroll
	page    *fyne.Container
	card    *canvas.Rectangle
	itemMin fyne.Size
	shown   []shownRow
	spare   []*cardRow
	// cursor is the row the arrows highlight while the list has the focus.
	focused bool
	cursor  widget.ListItemID
}

var _ fyne.Focusable = (*cardList)(nil)

type shownRow struct {
	id  widget.ListItemID
	row *cardRow
}

func newCardList(length func() int, create func() fyne.CanvasObject, update func(widget.ListItemID, fyne.CanvasObject)) *cardList {
	l := &cardList{length: length, create: create, update: update}
	l.ExtendBaseWidget(l)
	return l
}

// rowSide keeps a row's content off the window's sides: its cover and the
// line above it start there, while the highlight under the pointer runs
// from one side to the other.
func rowSide() float32 { return theme.Padding() * 3 }

// cardHeight is the card's for n rows: a padding between them and two
// under the last.
func (l *cardList) cardHeight(n int) float32 {
	if n == 0 {
		return 0
	}
	pad := theme.Padding()
	return float32(n)*(l.itemMin.Height+pad) + pad
}

func (l *cardList) CreateRenderer() fyne.WidgetRenderer {
	l.card = canvas.NewRectangle(color.Transparent)
	l.page = container.New(cardPage{l}, l.card)
	l.scroll = container.NewVScroll(l.page)
	l.scroll.OnScrolled = func(fyne.Position) { l.place(false) }
	// Lighter shadows at the scrolled edges, as in Settings.
	r := &cardListRenderer{l: l, objs: []fyne.CanvasObject{container.NewThemeOverride(l.scroll, cardTheme{})}}
	r.measure()
	return r
}

// place lays out the rows in view, building them or reusing those that
// left the view. all updates every row, for new data; otherwise only the
// rows coming into view are, as on a scroll.
func (l *cardList) place(all bool) {
	if l.scroll == nil {
		return
	}
	n := l.length()
	step := l.itemMin.Height + theme.Padding()
	first, last := 0, -1
	if view := l.scroll.Size().Height; n > 0 && step > 0 && view > 0 {
		first = max(0, int(l.scroll.Offset.Y/step))
		last = min(n-1, int((l.scroll.Offset.Y+view)/step))
	}
	old := make(map[widget.ListItemID]*cardRow, len(l.shown))
	for _, s := range l.shown {
		old[s.id] = s.row
	}
	l.shown = l.shown[:0]
	objs := []fyne.CanvasObject{l.card}
	for id := first; id <= last; id++ {
		row, ok := old[id]
		if ok {
			delete(old, id)
		} else {
			row = l.spareRow()
		}
		row.setLook(id == 0, l.focused && id == l.cursor)
		row.box.Move(fyne.NewPos(0, float32(id)*step))
		row.box.Resize(fyne.NewSize(l.page.Size().Width, l.itemMin.Height))
		if all || !ok {
			l.update(id, row.content)
		}
		l.shown = append(l.shown, shownRow{id, row})
		objs = append(objs, row.box)
	}
	for _, row := range old {
		if row.hovered {
			row.MouseOut()
		}
		l.spare = append(l.spare, row)
	}
	l.page.Objects = objs
	l.card.Resize(fyne.NewSize(l.page.Size().Width, l.cardHeight(n)))
}

func (l *cardList) spareRow() *cardRow {
	if n := len(l.spare); n > 0 {
		row := l.spare[n-1]
		l.spare = l.spare[:n-1]
		return row
	}
	row := &cardRow{content: l.create(), line: widget.NewSeparator(), hover: canvas.NewRectangle(color.Transparent)}
	row.ExtendBaseWidget(row)
	row.box = container.NewThemeOverride(row, cardTheme{input: colorCardInput, surface: colorCard})
	return row
}

// FocusGained highlights the row the arrows last left, as widget.List does.
func (l *cardList) FocusGained() {
	l.focused = true
	l.place(false)
}

func (l *cardList) FocusLost() {
	l.focused = false
	l.place(false)
}

// TypedKey moves the highlight a row down or up, scrolling it into view.
func (l *cardList) TypedKey(e *fyne.KeyEvent) {
	switch {
	case e.Name == fyne.KeyDown && l.cursor < l.length()-1:
		l.cursor++
	case e.Name == fyne.KeyUp && l.cursor > 0:
		l.cursor--
	default:
		return
	}
	l.ScrollTo(l.cursor)
}

func (l *cardList) TypedRune(rune) {}

// ScrollTo scrolls as little as it takes to show the row whole, as
// widget.List's does.
func (l *cardList) ScrollTo(id widget.ListItemID) {
	if l.scroll == nil {
		return
	}
	top := float32(id) * (l.itemMin.Height + theme.Padding())
	off := l.scroll.Offset
	if view := l.scroll.Size().Height; top+l.itemMin.Height > off.Y+view {
		off.Y = top + l.itemMin.Height - view
	}
	off.Y = min(off.Y, top)
	// The offset moved by hand tells OnScrolled nothing.
	l.scroll.ScrollToOffset(off)
	l.place(false)
}

func (l *cardList) ScrollToTop() {
	if l.scroll != nil {
		l.scroll.ScrollToTop()
		l.place(false)
	}
}

func (l *cardList) ScrollToBottom() {
	if l.scroll != nil {
		l.scroll.ScrollToBottom()
		l.place(false)
	}
}

type cardListRenderer struct {
	l    *cardList
	objs []fyne.CanvasObject
}

// measure takes a row's height from a new one, as the theme sets it.
func (r *cardListRenderer) measure() {
	r.l.itemMin = r.l.create().MinSize()
}

func (r *cardListRenderer) Layout(s fyne.Size) {
	r.objs[0].Resize(s)
	// A taller window shows more rows, with the page the same size.
	r.l.place(false)
}

func (r *cardListRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.l.itemMin.Width+2*rowSide(), r.l.itemMin.Height)
}

// Refresh sizes the page to the rows, keeps the scroll and the highlight
// in range and updates every row in view.
func (r *cardListRenderer) Refresh() {
	l := r.l
	r.measure()
	l.card.FillColor = theme.ColorForWidget(colorCard, l)
	l.card.Refresh()
	l.cursor = max(0, min(l.cursor, l.length()-1))
	l.scroll.Refresh()
	l.place(true)
	canvas.Refresh(l)
}

func (r *cardListRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *cardListRenderer) Destroy()                     {}

// cardPage lays the card out as tall as its rows: under software GL, as on
// Linux without a graphics card, each pixel a card covers costs CPU on
// every frame, and the level meter draws ten a second. The page shows
// below.
type cardPage struct{ l *cardList }

func (p cardPage) Layout([]fyne.CanvasObject, fyne.Size) { p.l.place(false) }

func (p cardPage) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(p.l.itemMin.Width+2*rowSide(), p.l.cardHeight(p.l.length()))
}

// cardRow is a row of a cardList: its content, a line above it but on the
// first row, and a highlight under the pointer or the arrows, as
// widget.List draws them.
type cardRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	line    *widget.Separator
	hover   *canvas.Rectangle
	// box is the row in the card's theme, as the list lays it out.
	box                   fyne.CanvasObject
	first, focus, hovered bool
}

var _ desktop.Hoverable = (*cardRow)(nil)

func (r *cardRow) setLook(first, focus bool) {
	if r.first != first || r.focus != focus {
		r.first, r.focus = first, focus
		r.Refresh()
	}
}

func (r *cardRow) MouseIn(*desktop.MouseEvent) {
	r.hovered = true
	r.Refresh()
}

func (r *cardRow) MouseMoved(*desktop.MouseEvent) {}

func (r *cardRow) MouseOut() {
	r.hovered = false
	r.Refresh()
}

func (r *cardRow) CreateRenderer() fyne.WidgetRenderer {
	rr := &cardRowRenderer{r: r}
	rr.Refresh()
	return rr
}

type cardRowRenderer struct{ r *cardRow }

func (rr *cardRowRenderer) Layout(s fyne.Size) {
	r := rr.r
	r.hover.Resize(s)
	side := rowSide()
	// The line sits in the middle of the padding above the row.
	thick := theme.SizeForWidget(theme.SizeNameSeparatorThickness, r)
	r.line.Move(fyne.NewPos(side, -(theme.Padding()+thick)/2))
	r.line.Resize(fyne.NewSize(s.Width-2*side, thick))
	r.content.Move(fyne.NewPos(side, 0))
	r.content.Resize(fyne.NewSize(s.Width-2*side, s.Height))
}

func (rr *cardRowRenderer) MinSize() fyne.Size {
	return rr.r.content.MinSize().AddWidthHeight(2*rowSide(), 0)
}

func (rr *cardRowRenderer) Refresh() {
	r := rr.r
	// Square, as it meets the window's sides.
	r.hover.FillColor = theme.ColorForWidget(theme.ColorNameHover, r)
	r.hover.Hidden = !r.hovered && !r.focus
	r.hover.Refresh()
	r.line.Hidden = r.first
	r.line.Refresh()
	rr.Layout(r.Size())
}

func (rr *cardRowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{rr.r.hover, rr.r.line, rr.r.content}
}

func (rr *cardRowRenderer) Destroy() {}
