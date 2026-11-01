package ui

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// checkSelect looks like a select, but its dropdown holds boxes to tick,
// and stays open while they are ticked: a select's list closes on the first
// choice. Its text is whatever Selected is set to.
type checkSelect struct {
	widget.Select
	boxes *fyne.Container
	pop   *widget.PopUp
}

func newCheckSelect(boxes *fyne.Container) *checkSelect {
	s := &checkSelect{boxes: boxes}
	s.ExtendBaseWidget(s)
	for _, b := range boxes.Objects {
		b.(*menuBox).menu = s
	}
	return s
}

func (s *checkSelect) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
		c.Focus(s)
	}
	s.open()
}

// TypedKey opens the dropdown on the arrows, as a select's.
func (s *checkSelect) TypedKey(e *fyne.KeyEvent) {
	switch e.Name {
	case fyne.KeyUp, fyne.KeyDown:
		s.open()
	}
}

// TypedRune opens it on Space. The character comes after the key: opened
// on the key, the dropdown got it and its first box ticked.
func (s *checkSelect) TypedRune(r rune) {
	if r == ' ' {
		s.open()
	}
}

// open shows the dropdown with the focus on its first box, where the
// arrows start.
func (s *checkSelect) open() {
	c := fyne.CurrentApp().Driver().CanvasForObject(s)
	if c == nil || s.Disabled() {
		return
	}
	if s.pop == nil {
		s.pop = widget.NewPopUp(s.boxes, c)
	}
	s.pop.ShowAtPosition(fyne.CurrentApp().Driver().AbsolutePositionForObject(s).AddXY(0, s.Size().Height))
	s.pop.Resize(fyne.NewSize(max(s.Size().Width, s.pop.MinSize().Width), s.pop.MinSize().Height))
	c.Focus(s.boxes.Objects[0].(fyne.Focusable))
}

// step moves the focus from box b to the next one, by 1, or the one
// before, by -1, and stays at the ends, as in a select's list.
func (s *checkSelect) step(b *menuBox, by int) {
	i := slices.Index(s.boxes.Objects, fyne.CanvasObject(b)) + by
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil && i >= 0 && i < len(s.boxes.Objects) {
		c.Focus(s.boxes.Objects[i].(fyne.Focusable))
	}
}

// close hides the dropdown and gives the focus back to the select, then
// moves it on as Tab would, by 1, or Shift+Tab, by -1.
func (s *checkSelect) close(by int) {
	s.pop.Hide()
	c := fyne.CurrentApp().Driver().CanvasForObject(s)
	if c == nil {
		return
	}
	c.Focus(s)
	switch by {
	case 1:
		c.FocusNext()
	case -1:
		c.FocusPrevious()
	}
}

// menuBox is a box in a checkSelect's dropdown, which takes the keys a
// select's list does: the arrows go from box to box, Space ticks, Enter
// and Escape close it. Tab closes it and goes on, Shift+Tab back: Fyne's
// Tab went round the boxes and never out.
type menuBox struct {
	widget.Check
	menu *checkSelect
}

func newMenuBox(text string, changed func(bool)) *menuBox {
	b := &menuBox{}
	b.Text, b.OnChanged = text, changed
	b.ExtendBaseWidget(b)
	return b
}

// AcceptsTab keeps Tab from Fyne, which would focus the next box.
func (b *menuBox) AcceptsTab() bool { return true }

func (b *menuBox) TypedKey(e *fyne.KeyEvent) {
	switch e.Name {
	case fyne.KeyUp:
		b.menu.step(b, -1)
	case fyne.KeyDown:
		b.menu.step(b, 1)
	case fyne.KeyReturn, fyne.KeyEnter, fyne.KeyEscape:
		b.menu.close(0)
	case fyne.KeyTab:
		by := 1
		if d, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok && d.CurrentKeyModifiers()&fyne.KeyModifierShift != 0 {
			by = -1
		}
		b.menu.close(by)
	}
}
