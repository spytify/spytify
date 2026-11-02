package ui

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
)

// The keys go where Fyne 2.8.2's window sends them
// (internal/driver/glfw/window.go): Tab and Shift+Tab move the focus, unless
// the field focused types tabs; every other key goes to the control
// focused.

func pressTab(c fyne.Canvas) {
	if !typesTab(c) {
		c.FocusNext()
	}
}

func pressShiftTab(c fyne.Canvas) {
	if !typesTab(c) {
		c.FocusPrevious()
	}
}

// typesTab hands Tab to the field focused if it types tabs.
func typesTab(c fyne.Canvas) bool {
	f, ok := c.Focused().(fyne.Tabbable)
	if ok && f.AcceptsTab() {
		c.Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	}
	return ok && f.AcceptsTab()
}

func pressKey(c fyne.Canvas, key fyne.KeyName) {
	if f := c.Focused(); f != nil {
		f.TypedKey(&fyne.KeyEvent{Name: key})
	}
}

// pressSpace is the space bar: the key, then the character it types, which
// goes to whatever has the focus by then.
func pressSpace(c fyne.Canvas) {
	pressKey(c, fyne.KeySpace)
	if f := c.Focused(); f != nil {
		f.TypedRune(' ')
	}
}

// pressEscape is Escape as Fyne 2.8.2's window sends it: to the control
// focused, or to the window when nothing is.
func pressEscape(c fyne.Canvas) {
	e := &fyne.KeyEvent{Name: fyne.KeyEscape}
	if f := c.Focused(); f != nil {
		f.TypedKey(e)
	} else if typed := c.OnTypedKey(); typed != nil {
		typed(e)
	}
}

// typeShortcut sends s as the window does: to the focused widget when it
// takes shortcuts, else to the window's own, which the test window keeps
// in the canvas it wraps.
func typeShortcut(t *testing.T, a *App, s fyne.Shortcut) {
	t.Helper()
	c := a.win.Canvas()
	if f, ok := c.Focused().(fyne.Shortcutable); ok {
		f.TypedShortcut(s)
		return
	}
	window, ok := reflect.ValueOf(c).Elem().FieldByName("WindowlessCanvas").Interface().(fyne.Shortcutable)
	if !ok {
		t.Fatal("the test window keeps no shortcuts")
	}
	window.TypedShortcut(s)
}

// press is key with the system's shortcut key: Cmd on macOS, Ctrl
// elsewhere.
func press(t *testing.T, a *App, key fyne.KeyName) {
	t.Helper()
	typeShortcut(t, a, shortcutFor(key))
}
