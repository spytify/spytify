package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

// What Tab needs that Fyne 2.8.2 leaves out. Tab and Shift+Tab move the
// focus through the controls in the order of the containers that hold
// them; Space presses a button, ticks a box, flips a switch and opens a
// list of choices. The tabs' headers take no focus: Cmd or Ctrl with a
// digit shows them.

// inReadingOrder is container.NewBorder with its parts in the order they
// are read, top, left, centre, right then bottom, which is the order Tab
// follows. NewBorder puts the centre first, so Tab went through a screen
// before the bar above it.
func inReadingOrder(top, bottom, left, right fyne.CanvasObject, center ...fyne.CanvasObject) *fyne.Container {
	var objs []fyne.CanvasObject
	for _, o := range []fyne.CanvasObject{top, left} {
		if o != nil {
			objs = append(objs, o)
		}
	}
	objs = append(objs, center...)
	for _, o := range []fyne.CanvasObject{right, bottom} {
		if o != nil {
			objs = append(objs, o)
		}
	}
	return container.New(layout.NewBorderLayout(top, bottom, left, right), objs...)
}

// focusFirst gives the focus to the first control of the dialog just
// shown over w, the one Tab would reach first: Fyne leaves it on nothing,
// so the first key went nowhere. A dialog opened with the space bar then
// gets its space as a character: a button or a list ignores it, a check
// box first would tick.
func focusFirst(w fyne.Window) {
	c := w.Canvas()
	if c.Overlays().Top() != nil && c.Focused() == nil {
		c.FocusNext()
	}
}

// focusCheck is how often followFocus looks.
const focusCheck = 100 * time.Millisecond

// watchFocus keeps the control Tab focused in sight. Fyne scrolls a list
// to its focused row, but not a scrolled screen to its focused control, and
// tells nothing when the focus moves, so a timer looks; the toolkit's own
// loop wakes 60 times a second.
func (a *App) watchFocus() {
	go func() {
		for range time.Tick(focusCheck) {
			fyne.Do(a.followFocus)
		}
	}()
}

// followFocus scrolls Settings, or the offline screen, to the control that
// just took the focus, when it is out of sight.
func (a *App) followFocus() {
	c := a.win.Canvas()
	f := c.Focused()
	if f == a.focusSeen {
		return
	}
	a.focusSeen = f
	o, ok := f.(fyne.CanvasObject)
	// A dialog has the keys, and the app bar does not scroll.
	if !ok || c.Overlays().Top() != nil || inContainers(a.bar, o) {
		return
	}
	switch {
	case a.center.Objects[0] == a.offline:
		scrollTo(a.offline, o)
	case a.tabs.SelectedIndex() == tabSettings:
		// The search stays above the settings.
		if s := a.screens[tabSettings].(*settingsScreen); o != s.search {
			scrollTo(s.scroll, o)
		}
	}
}

// inContainers says whether o is root or in the containers under it.
func inContainers(root, o fyne.CanvasObject) bool {
	if root == o {
		return true
	}
	if c, ok := root.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if inContainers(child, o) {
				return true
			}
		}
	}
	return false
}

// scrollTo scrolls s as little as it takes to show o whole, with a margin
// for its focus ring; o taller than s shows its top.
func scrollTo(s *container.Scroll, o fyne.CanvasObject) {
	d := fyne.CurrentApp().Driver()
	// Where o is in the part of s shown.
	y := d.AbsolutePositionForObject(o).Y - d.AbsolutePositionForObject(s).Y
	h, view, margin := o.Size().Height, s.Size().Height, theme.Padding()*2
	off := s.Offset
	switch {
	case y < margin:
		off.Y += y - margin
	case y+h+margin > view:
		off.Y += min(y+h+margin-view, y-margin)
	default:
		return
	}
	// The scroll keeps the offset within its content.
	s.ScrollToOffset(off)
}
