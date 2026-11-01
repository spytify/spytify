package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// fold is a title that shows and hides what it holds, as Fyne's accordion
// with one item does, and is laid out the same. Its header is a button:
// the accordion's own, opened with Space, lost the focus, and Tab started
// again from the first control.
type fold struct {
	widget.BaseWidget
	header *button
	detail fyne.CanvasObject
	open   bool
}

func newFold(title string, detail fyne.CanvasObject) *fold {
	f := &fold{detail: detail}
	f.header = newButton(title, func() { f.setOpen(!f.open) })
	f.header.Alignment = widget.ButtonAlignLeading
	f.header.IconPlacement = widget.ButtonIconLeadingText
	f.header.Importance = widget.LowImportance
	f.ExtendBaseWidget(f)
	f.setOpen(false)
	return f
}

func (f *fold) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.New(foldLayout{f}, f.header, f.detail))
}

func (f *fold) setOpen(open bool) {
	f.open = open
	f.header.Icon = theme.MenuDropDownIcon()
	if open {
		f.header.Icon = theme.MenuDropUpIcon()
	}
	f.header.Refresh()
	show(f.detail, open)
	f.Refresh()
}

// foldLayout is the accordion's: the header at the top; open, the detail a
// padding below it, with the height left; as wide as the detail even
// closed.
type foldLayout struct{ f *fold }

func (l foldLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	h := l.f.header.MinSize().Height
	l.f.header.Move(fyne.Position{})
	l.f.header.Resize(fyne.NewSize(size.Width, h))
	if l.f.open {
		y := h + theme.Padding()
		l.f.detail.Move(fyne.NewPos(0, y))
		l.f.detail.Resize(fyne.NewSize(size.Width, max(size.Height-y, l.f.detail.MinSize().Height)))
	}
}

func (l foldLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	header, detail := l.f.header.MinSize(), l.f.detail.MinSize()
	size := fyne.NewSize(max(header.Width, detail.Width), header.Height)
	if l.f.open {
		size.Height += theme.Padding() + detail.Height
	}
	return size
}
