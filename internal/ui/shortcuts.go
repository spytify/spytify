package ui

import (
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// The shortcuts take the system's shortcut key, Cmd on macOS and Ctrl
// elsewhere: with R, the app bar's Record or Stop; with 1, 2 and on, the
// tabs in their order. Fyne's own (copy, cut, paste, select all, undo,
// redo, and an entry's word moves) and the system's (quit, hide, minimize)
// use other keys, and none of these types a character in a field.
const actionKey = fyne.KeyR

var tabKeys = []fyne.KeyName{fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4, fyne.Key5, fyne.Key6, fyne.Key7, fyne.Key8, fyne.Key9}

// keyRepeat is how long after a press the action's key does nothing: a
// held key repeats, and Record would go on to Stop, then Stop now.
const keyRepeat = time.Second

func shortcutFor(key fyne.KeyName) *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierShortcutDefault}
}

func (a *App) addShortcuts() {
	run := func(s fyne.Shortcut) { a.runShortcut(s) }
	c := a.win.Canvas()
	c.AddShortcut(shortcutFor(actionKey), run)
	for _, key := range tabKeys[:min(len(a.tabs.Items), len(tabKeys))] {
		c.AddShortcut(shortcutFor(key), run)
	}
}

// runShortcut runs s and says whether it is one of the app's.
func (a *App) runShortcut(s fyne.Shortcut) bool {
	k, ok := s.(*desktop.CustomShortcut)
	if !ok || k.Modifier != fyne.KeyModifierShortcutDefault {
		return false
	}
	tab := slices.Index(tabKeys, k.KeyName)
	if k.KeyName != actionKey && (tab < 0 || tab >= len(a.tabs.Items)) {
		return false
	}
	// A dialog or a menu takes the keys, as it takes the clicks.
	if a.win.Canvas().Overlays().Top() != nil {
		return true
	}
	if k.KeyName == actionKey {
		a.pressAction()
	} else {
		a.keyTab(tab)
	}
	return true
}

// pressAction is a click on the app bar's Record, Stop or Stop now, unless
// it is disabled or its key is held.
func (a *App) pressAction() {
	now := time.Now()
	held := now.Sub(a.actionKeyAt) < keyRepeat
	a.actionKeyAt = now
	if held || a.action.Disabled() {
		return
	}
	a.action.Tapped(&fyne.PointEvent{})
}

// keyTab is a click on tab i: what had the focus loses it, or typing would
// go on in a screen no longer shown.
func (a *App) keyTab(i int) {
	// The offline screen shows no tabs.
	if a.center.Objects[0] != a.tabsView {
		return
	}
	a.win.Canvas().Unfocus()
	a.tabs.SelectIndex(i)
}

// keyEntry is an entry the app's shortcuts go through: Fyne hands every
// shortcut to the focused entry, which has no use for these.
type keyEntry struct {
	widget.Entry
	app *App
}

func newKeyEntry(a *App) *keyEntry {
	e := &keyEntry{app: a}
	e.ExtendBaseWidget(e)
	return e
}

func (e *keyEntry) TypedShortcut(s fyne.Shortcut) {
	if !e.app.runShortcut(s) {
		e.Entry.TypedShortcut(s)
	}
}
