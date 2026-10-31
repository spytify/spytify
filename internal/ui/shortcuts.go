package ui

import (
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"
)

// The shortcuts take the system's shortcut key, Cmd on macOS and Ctrl
// elsewhere: with R, the app bar's Record or Stop; with 1, 2 and on, the
// tabs in their order. Fyne's own (copy, cut, paste, select all, undo,
// redo, and an entry's word moves) and the system's (quit, hide, minimize)
// use other keys, and none of these types a character in a field.
const actionKey = fyne.KeyR

var tabDigits = []fyne.KeyName{fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4, fyne.Key5, fyne.Key6, fyne.Key7, fyne.Key8, fyne.Key9}

// keyRepeat is how long after a press the action's key does nothing: a
// held key repeats, and Record would go on to Stop, then Stop now.
const keyRepeat = time.Second

func shortcutFor(key fyne.KeyName) *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierShortcutDefault}
}

// shortcutTip is the tooltip of what key acts on, called name: "Record
// (⌘R)" on macOS, "Record (Ctrl+R)" elsewhere. It writes the shortcut the
// key is bound with, so the two cannot differ.
func shortcutTip(name string, key fyne.KeyName) string {
	return name + " (" + keyLabel(runtime.GOOS == "darwin", shortcutFor(key)) + ")"
}

// keyLabel writes s as Fyne's menus do: on a Mac, the modifiers' symbols
// before the key; elsewhere, their names joined to the key with +.
func keyLabel(mac bool, s *desktop.CustomShortcut) string {
	var b strings.Builder
	for _, m := range []struct {
		mod        fyne.KeyModifier
		mac, other string
	}{
		{fyne.KeyModifierControl, "⌃", "Ctrl+"},
		{fyne.KeyModifierAlt, "⌥", "Alt+"},
		{fyne.KeyModifierShift, "⇧", "Shift+"},
		{fyne.KeyModifierSuper, "⌘", "Super+"},
	} {
		switch {
		case s.Modifier&m.mod == 0:
		case mac:
			b.WriteString(m.mac)
		default:
			b.WriteString(m.other)
		}
	}
	b.WriteString(string(s.KeyName))
	return b.String()
}

func (a *App) addShortcuts() {
	run := func(s fyne.Shortcut) { a.runShortcut(s) }
	c := a.win.Canvas()
	c.AddShortcut(shortcutFor(actionKey), run)
	for _, key := range tabDigits[:min(len(a.tabs.Items), len(tabDigits))] {
		c.AddShortcut(shortcutFor(key), run)
	}
}

// runShortcut runs s and says whether it is one of the app's.
func (a *App) runShortcut(s fyne.Shortcut) bool {
	k, ok := s.(*desktop.CustomShortcut)
	if !ok || k.Modifier != fyne.KeyModifierShortcutDefault {
		return false
	}
	tab := slices.Index(tabDigits, k.KeyName)
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

// tabTip lies over a tab to show its key: Fyne's tabs take no tooltip. It
// takes no click, so a click goes to the tab under it, and it hands the
// hover on, so the tab still lights up.
type tabTip struct {
	ttwidget.ToolTipWidget
	item *container.TabItem
}

// tabTips are the tooltips of the tabs that have a key, for tabsLayout to
// lay over them.
func tabTips(tabs *container.AppTabs) []fyne.CanvasObject {
	var tips []fyne.CanvasObject
	for i, item := range tabs.Items[:min(len(tabs.Items), len(tabDigits))] {
		t := &tabTip{item: item}
		t.ExtendBaseWidget(t)
		t.SetToolTip(shortcutTip(item.Text, tabDigits[i]))
		tips = append(tips, t)
	}
	return tips
}

func (t *tabTip) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewWithoutLayout())
}

func (t *tabTip) MouseIn(e *desktop.MouseEvent) {
	t.ToolTipWidget.MouseIn(e)
	if b, ok := tabButton(t.item).(desktop.Hoverable); ok {
		b.MouseIn(e)
	}
}

func (t *tabTip) MouseMoved(e *desktop.MouseEvent) {
	t.ToolTipWidget.MouseMoved(e)
	if b, ok := tabButton(t.item).(desktop.Hoverable); ok {
		b.MouseMoved(e)
	}
}

func (t *tabTip) MouseOut() {
	t.ToolTipWidget.MouseOut()
	if b, ok := tabButton(t.item).(desktop.Hoverable); ok {
		b.MouseOut()
	}
}

// coverTabs lays each tip over its tab: the tabs' bar is at their top left,
// so a tab's place in the bar is its place here. When the tabs do not all
// fit, some go to a menu and keep where they last were; then no tip shows.
// The window is wide enough for them all.
func coverTabs(tabs *container.AppTabs, tips []fyne.CanvasObject) {
	fit := tabsFit(tabs)
	for _, o := range tips {
		t := o.(*tabTip)
		b := tabButton(t.item)
		if !fit || b == nil {
			t.Hide()
			continue
		}
		t.Move(b.Position())
		t.Resize(b.Size())
		t.Show()
	}
}

// tabsFit says whether the tabs all show in their bar.
func tabsFit(tabs *container.AppTabs) bool {
	n := len(tabs.Items)
	if n == 0 {
		return false
	}
	last := tabButton(tabs.Items[n-1])
	return last != nil && last.Position().X+last.Size().Width <= tabs.Size().Width
}

// tabButton is the button the tabs draw for item, or nil before they draw
// it. Fyne keeps it unexported, on the item; a Fyne that moves it leaves
// the tabs without tooltips, and TestTabTipsLieOverTheirTabs fails.
func tabButton(item *container.TabItem) fyne.CanvasObject {
	f := reflect.ValueOf(item).Elem().FieldByName("button")
	if f.Kind() != reflect.Pointer || f.IsNil() {
		return nil
	}
	b, _ := reflect.NewAt(f.Type().Elem(), f.UnsafePointer()).Interface().(fyne.CanvasObject)
	return b
}
