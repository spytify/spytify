package ui

import (
	"image/color"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// cardList shows rows of one height, or of the heights height gives them,
// on the card's colour, square and from one side of the window to the
// other, on a page that scrolls as Settings' does: its scroll bar is at the
// window's edge, where a widget.List puts its own inside the card. Like
// widget.List, it builds only the rows in view, as a library holds
// thousands of files, and Tab stops on it for the arrows to move a
// highlight from row to row, then from a row to its buttons, which Tab
// passes over.
type cardList struct {
	widget.BaseWidget
	length func() int
	create func() fyne.CanvasObject
	update func(id widget.ListItemID, o fyne.CanvasObject)
	// height, when set, gives a row a height of its own, 0 keeping the
	// rows' own; sizeRows then adds up where each row starts. Without it,
	// as for the Library's files, that is a multiplication.
	height        func(id widget.ListItemID) float32
	tops, heights []float32
	// band, when set, names a row's background, "" for the card's: a run of
	// rows on one is a single block, without a line at its ends.
	band func(id widget.ListItemID) fyne.ThemeColorName

	scroll  *container.Scroll
	frame   *themed
	page    *fyne.Container
	card    *canvas.Rectangle
	itemMin fyne.Size
	shown   []shownRow
	spare   []*cardRow
	// look is the theme's text and background the spare rows were drawn in.
	look [2]color.Color
	// cursor is the row the arrows highlight while the list has the focus,
	// col where on it: 0 the row itself, then its buttons left to right.
	// lit is the button col shows focused.
	focused bool
	cursor  widget.ListItemID
	col     int
	lit     *rowButton
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
	return l.rowTop(n) + theme.Padding()
}

// sizeRows adds up the heights of n rows when height gives them, for
// rowTop, rowHeight and rowAt.
func (l *cardList) sizeRows(n int) {
	if l.height == nil {
		return
	}
	pad := theme.Padding()
	l.tops, l.heights = l.tops[:0], l.heights[:0]
	var y float32
	for id := range n {
		h := l.height(id)
		if h <= 0 {
			h = l.itemMin.Height
		}
		l.tops, l.heights = append(l.tops, y), append(l.heights, h)
		y += h + pad
	}
	l.tops = append(l.tops, y)
}

// rowTop is where row id starts on the page; past the last row, where one
// more would.
func (l *cardList) rowTop(id int) float32 {
	if l.height == nil {
		return float32(id) * (l.itemMin.Height + theme.Padding())
	}
	return l.tops[min(id, len(l.tops)-1)]
}

func (l *cardList) rowHeight(id int) float32 {
	if l.height == nil || id >= len(l.heights) {
		return l.itemMin.Height
	}
	return l.heights[id]
}

// rowAt is the row of n at y on the page, n past the last one.
func (l *cardList) rowAt(y float32, n int) int {
	if l.height == nil {
		return int(y / (l.itemMin.Height + theme.Padding()))
	}
	return sort.Search(n, func(i int) bool { return l.tops[i+1] > y })
}

// bandOf is the background band names for row id, "" without one.
func (l *cardList) bandOf(id widget.ListItemID) fyne.ThemeColorName {
	if l.band == nil || id < 0 || id >= l.length() {
		return ""
	}
	return l.band(id)
}

// parts is the list's frame and the rows it keeps for later: a spare row
// that lost its theme would come back in the app's.
func (l *cardList) parts() []fyne.CanvasObject {
	if l.frame == nil {
		return nil
	}
	parts := []fyne.CanvasObject{l.frame}
	for _, row := range l.spare {
		parts = append(parts, row.box)
	}
	return parts
}

func (l *cardList) CreateRenderer() fyne.WidgetRenderer {
	l.card = canvas.NewRectangle(color.Transparent)
	l.page = container.New(cardPage{l}, l.card)
	l.scroll = container.NewVScroll(l.page)
	l.scroll.OnScrolled = func(fyne.Position) { l.place(false) }
	// Lighter shadows at the scrolled edges, as in Settings.
	l.frame = newThemed(l.scroll, cardTheme{})
	r := &cardListRenderer{l: l, objs: []fyne.CanvasObject{l.frame}}
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
	l.sizeRows(n)
	first, last := 0, -1
	if view := l.scroll.Size().Height; n > 0 && l.rowTop(n) > 0 && view > 0 {
		first = max(0, l.rowAt(l.scroll.Offset.Y, n))
		last = min(n-1, l.rowAt(l.scroll.Offset.Y+view, n))
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
		fill, above := l.bandOf(id), l.bandOf(id-1)
		row.setLook(id == 0 || fill != above, l.focused && id == l.cursor, fill, id > 0 && fill != "" && fill == above)
		row.box.Move(fyne.NewPos(0, l.rowTop(id)))
		row.box.Resize(fyne.NewSize(l.page.Size().Width, l.rowHeight(id)))
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
	// The row's buttons change with its file, and its row with a scroll.
	var lit *rowButton
	if buttons, ok := l.cursorButtons(); ok && l.focused {
		l.col = min(l.col, len(buttons))
		if l.col > 0 {
			lit = buttons[l.col-1]
		}
	}
	l.light(lit)
}

// cursorButtons is the buttons the arrows reach on the cursor's row, and
// whether the row is built.
func (l *cardList) cursorButtons() ([]*rowButton, bool) {
	for _, s := range l.shown {
		if s.id == l.cursor {
			return rowButtons(s.row.content), true
		}
	}
	return nil, false
}

// rowButtons is the buttons in o's containers that are shown and on, left
// to right.
func rowButtons(o fyne.CanvasObject) []*rowButton {
	if !o.Visible() {
		return nil
	}
	switch o := o.(type) {
	case *rowButton:
		if !o.Disabled() {
			return []*rowButton{o}
		}
	case *fyne.Container:
		var buttons []*rowButton
		for _, child := range o.Objects {
			buttons = append(buttons, rowButtons(child)...)
		}
		return buttons
	case *themed:
		return rowButtons(o.o.Content)
	}
	return nil
}

// light shows b focused, and the button it showed before no more.
func (l *cardList) light(b *rowButton) {
	if b == l.lit {
		return
	}
	if l.lit != nil {
		l.lit.Button.FocusLost()
	}
	if b != nil {
		b.Button.FocusGained()
	}
	l.lit = b
}

func (l *cardList) spareRow() *cardRow {
	if n := len(l.spare); n > 0 {
		row := l.spare[n-1]
		l.spare = l.spare[:n-1]
		return row
	}
	row := &cardRow{content: l.create(), line: widget.NewSeparator(), back: canvas.NewRectangle(color.Transparent), hover: canvas.NewRectangle(color.Transparent)}
	row.ExtendBaseWidget(row)
	row.box = newThemed(row, cardTheme{input: colorCardInput, surface: colorCard})
	return row
}

// FocusGained highlights the row the arrows last left, as widget.List does.
func (l *cardList) FocusGained() {
	l.focused = true
	l.place(false)
}

// FocusLost leaves the highlight on the row, for Tab back to start there.
func (l *cardList) FocusLost() {
	l.focused = false
	l.col = 0
	l.place(false)
}

// TypedKey moves the highlight a row down or up, scrolling it into view,
// and Left and Right between the row and its buttons. Space presses the
// button highlighted, or on the row its main one: Play.
func (l *cardList) TypedKey(e *fyne.KeyEvent) {
	switch {
	case e.Name == fyne.KeyDown && l.cursor < l.length()-1:
		l.cursor++
		l.col = 0
	case e.Name == fyne.KeyUp && l.cursor > 0:
		l.cursor--
		l.col = 0
	case e.Name == fyne.KeyLeft && l.col > 0:
		l.col--
	case e.Name == fyne.KeyRight:
		// place stops it at the last button.
		l.col++
	case e.Name == fyne.KeySpace:
		// The wheel may have scrolled the row away.
		l.ScrollTo(l.cursor)
		l.press()
		return
	default:
		return
	}
	l.ScrollTo(l.cursor)
}

func (l *cardList) press() {
	b := l.lit
	if l.col == 0 {
		buttons, _ := l.cursorButtons()
		for _, main := range buttons {
			if main.main {
				b = main
			}
		}
	}
	if b != nil && b.OnTapped != nil {
		b.OnTapped()
	}
}

func (l *cardList) TypedRune(rune) {}

// ScrollTo scrolls as little as it takes to show the row whole, as
// widget.List's does.
func (l *cardList) ScrollTo(id widget.ListItemID) {
	if l.scroll == nil {
		return
	}
	l.sizeRows(l.length())
	top, height := l.rowTop(id), l.rowHeight(id)
	off := l.scroll.Offset
	if view := l.scroll.Size().Height; top+height > off.Y+view {
		off.Y = top + height - view
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
	// The rows put aside missed the refresh a new theme gives the rows in
	// view: drawn again, they kept the former theme's icons and badges.
	if look := [2]color.Color{theme.ColorForWidget(theme.ColorNameForeground, l), theme.ColorForWidget(theme.ColorNameBackground, l)}; look != l.look {
		l.look, l.spare = look, nil
		r.measure()
	}
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
	n := p.l.length()
	p.l.sizeRows(n)
	return fyne.NewSize(p.l.itemMin.Width+2*rowSide(), p.l.cardHeight(n))
}

// cardRow is a row of a cardList: its content, a line above it but on the
// first row, and a highlight under the pointer or the arrows, as
// widget.List draws them; on a band, its background, over the gap above
// when the row above is on the band too, and no line at the band's ends.
type cardRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	line    *widget.Separator
	back    *canvas.Rectangle
	hover   *canvas.Rectangle
	// box is the row in the card's theme, as the list lays it out.
	box                    fyne.CanvasObject
	noLine, focus, hovered bool
	fill                   fyne.ThemeColorName
	joined                 bool
}

var _ desktop.Hoverable = (*cardRow)(nil)

func (r *cardRow) parts() []fyne.CanvasObject { return []fyne.CanvasObject{r.line, r.content} }

func (r *cardRow) setLook(noLine, focus bool, fill fyne.ThemeColorName, joined bool) {
	if r.noLine != noLine || r.focus != focus || r.fill != fill || r.joined != joined {
		r.noLine, r.focus, r.fill, r.joined = noLine, focus, fill, joined
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
	r.back.Move(fyne.NewPos(0, 0))
	r.back.Resize(s)
	if r.joined {
		r.back.Move(fyne.NewPos(0, -theme.Padding()))
		r.back.Resize(s.AddWidthHeight(0, theme.Padding()))
	}
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
	r.back.Hidden = r.fill == ""
	if r.fill != "" {
		r.back.FillColor = theme.ColorForWidget(r.fill, r)
	}
	r.back.Refresh()
	r.line.Hidden = r.noLine
	r.line.Refresh()
	rr.Layout(r.Size())
}

func (rr *cardRowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{rr.r.back, rr.r.hover, rr.r.line, rr.r.content}
}

func (rr *cardRowRenderer) Destroy() {}
