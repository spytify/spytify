package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

const shortcutKey = fyne.KeyModifierShortcutDefault

// shownKeys are the keys the hints show.
func shownKeys(a *App) map[string]*hint {
	out := map[string]*hint{}
	if !a.hints.Visible() {
		return out
	}
	for _, o := range a.hints.Objects {
		if h := o.(*hint); h.Visible() {
			out[h.key] = h
		}
	}
	return out
}

// holdUntilShown holds the shortcut key from start for hintHold.
func holdUntilShown(t *testing.T, a *App, start time.Time) {
	t.Helper()
	a.watchKeys(shortcutKey, start)
	a.watchKeys(shortcutKey, start.Add(hintHold))
	if !a.hints.Visible() {
		t.Fatal("no key shows with the shortcut key held")
	}
}

// underItsTarget fails unless h lies under target, centred on it.
func underItsTarget(t *testing.T, a *App, h *hint, target fyne.CanvasObject) {
	t.Helper()
	d := a.fa.Driver()
	hp, tp := d.AbsolutePositionForObject(h), d.AbsolutePositionForObject(target)
	centre := hp.X + h.Size().Width/2 - (tp.X + target.Size().Width/2)
	if hp.Y < tp.Y+target.Size().Height || hp.Y > tp.Y+target.Size().Height+2*theme.Padding() || centre < -0.5 || centre > 0.5 {
		t.Errorf("%s at %v %v, its target at %v %v", h.key, hp, h.Size(), tp, target.Size())
	}
}

// Holding the shortcut key alone shows each key under what it acts on, R
// under Record and 1 to 5 under the tabs, once held for hintHold; letting
// it go hides them.
func TestHoldingTheShortcutKeyShowsEachKey(t *testing.T) {
	a, _ := connectedTestApp(t)
	start := time.Now()
	for _, at := range []time.Duration{0, hintPoll, hintHold - time.Millisecond} {
		a.watchKeys(shortcutKey, start.Add(at))
		if keys := shownKeys(a); len(keys) != 0 {
			t.Fatalf("%d keys show %v after the shortcut key went down", len(keys), at)
		}
	}
	a.watchKeys(shortcutKey, start.Add(hintHold))
	targets := map[string]fyne.CanvasObject{"R": a.action}
	for i, item := range a.tabs.Items {
		targets[string(tabDigits[i])] = tabButton(item)
	}
	keys := shownKeys(a)
	if len(keys) != len(targets) {
		t.Fatalf("%d keys show, want %d", len(keys), len(targets))
	}
	for key, target := range targets {
		if h := keys[key]; h == nil {
			t.Errorf("no %s shows", key)
		} else {
			underItsTarget(t, a, h, target)
		}
	}
	shown := texts(a.hints)
	slices.Sort(shown)
	if want := []string{"1", "2", "3", "4", "5", "R"}; !slices.Equal(shown, want) {
		t.Errorf("the hints read %v, want %v", shown, want)
	}
	a.watchKeys(0, start.Add(hintHold+hintPoll))
	if a.hints.Visible() {
		t.Fatal("the keys stay once the shortcut key is up")
	}
}

// With another modifier, or let go before hintHold, the shortcut key shows
// nothing: Cmd+Shift+Z, or Cmd+C typed in one go.
func TestOnlyTheShortcutKeyHeldAloneShowsTheKeys(t *testing.T) {
	a, _ := connectedTestApp(t)
	start := time.Now()
	other := fyne.KeyModifierControl
	if shortcutKey == fyne.KeyModifierControl {
		other = fyne.KeyModifierSuper
	}
	for _, mods := range []fyne.KeyModifier{shortcutKey | fyne.KeyModifierShift, shortcutKey | fyne.KeyModifierAlt, other, fyne.KeyModifierShift} {
		a.watchKeys(mods, start)
		a.watchKeys(mods, start.Add(2*hintHold))
		if a.hints.Visible() {
			t.Fatalf("the keys show with modifiers %v held", mods)
		}
	}
	// Let go halfway, then down again: the wait starts over.
	a.watchKeys(shortcutKey, start)
	a.watchKeys(0, start.Add(hintHold/2))
	again := start.Add(hintHold/2 + hintPoll)
	a.watchKeys(shortcutKey, again)
	a.watchKeys(shortcutKey, start.Add(hintHold))
	if a.hints.Visible() {
		t.Fatal("the keys show before the shortcut key was held again for hintHold")
	}
	a.watchKeys(shortcutKey, again.Add(hintHold))
	if !a.hints.Visible() {
		t.Fatal("no key shows once held again for hintHold")
	}
}

// A key that would do nothing shows nothing: no key under a dialog, which
// takes the keys, no R while Record is off, no tab key offline, where the
// tabs are not shown. A key shows as soon as it would act, the shortcut
// key still held.
func TestNoKeyShowsWhereItDoesNothing(t *testing.T) {
	a, m, _ := connectedApp(t)
	start := time.Now()
	d := dialog.NewInformation("Spytify", "A question", a.win)
	d.Show()
	a.watchKeys(shortcutKey, start)
	a.watchKeys(shortcutKey, start.Add(hintHold))
	if a.hints.Visible() {
		t.Fatal("the keys show over a dialog")
	}
	d.Hide()
	a.watchKeys(shortcutKey, start.Add(hintHold+hintPoll))
	if len(shownKeys(a)) != 6 {
		t.Fatalf("%d keys show once the dialog is gone", len(shownKeys(a)))
	}

	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	a.render()
	if !a.action.Disabled() {
		t.Fatal("Record is on with the player closed")
	}
	a.watchKeys(shortcutKey, start.Add(hintHold+2*hintPoll))
	if keys := shownKeys(a); keys["R"] != nil || len(keys) != 5 {
		t.Errorf("with Record off, %d keys show, R %v", len(keys), keys["R"] != nil)
	}

	off, om, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	off.render()
	holdUntilShown(t, off, start)
	for i := range off.tabs.Items {
		if shownKeys(off)[string(tabDigits[i])] != nil {
			t.Fatalf("offline, %s shows", tabDigits[i])
		}
	}
	om.SetConn(model.Connected, "")
	om.SetInfo(info())
	om.SetSettings(defaults())
	off.render()
	off.watchKeys(shortcutKey, start.Add(hintHold+hintPoll))
	keys := shownKeys(off)
	for i, item := range off.tabs.Items {
		if h := keys[string(tabDigits[i])]; h == nil {
			t.Errorf("connected, %s does not show", tabDigits[i])
		} else {
			underItsTarget(t, off, h, tabButton(item))
		}
	}
}

// R stays under the app bar's button as it goes from Record to the wider
// Stop now.
func TestRFollowsTheButton(t *testing.T) {
	a, _ := connectedTestApp(t)
	start := time.Now()
	holdUntilShown(t, a, start)
	before := a.action.Size().Width
	a.setAction(lang.StopNow, theme.MediaStopIcon(), widget.DangerImportance)
	a.bar.Refresh()
	if a.action.Size().Width <= before {
		t.Fatalf("Stop now is %v wide, Record %v", a.action.Size().Width, before)
	}
	a.watchKeys(shortcutKey, start.Add(hintHold+hintPoll))
	underItsTarget(t, a, shownKeys(a)["R"], a.action)
}

// S shows under the search field of the Library and of the Settings, and
// on no other screen, following the tab shown while the key is held.
func TestSShowsUnderTheSearch(t *testing.T) {
	a, _ := connectedTestApp(t)
	start := time.Now()
	holdUntilShown(t, a, start)
	if shownKeys(a)["S"] != nil {
		t.Fatal("S shows on the Record screen")
	}
	for i, step := range []struct {
		tab    int
		search fyne.CanvasObject
	}{
		{tabLibrary, a.screens[tabLibrary].(*libraryScreen).search},
		{tabSettings, a.screens[tabSettings].(*settingsScreen).search},
		{tabLogs, nil},
		{tabAbout, nil},
	} {
		a.tabs.SelectIndex(step.tab)
		a.watchKeys(shortcutKey, start.Add(hintHold+time.Duration(i+1)*hintPoll))
		h := shownKeys(a)["S"]
		switch {
		case step.search == nil && h != nil:
			t.Errorf("S shows on tab %d", step.tab)
		case step.search != nil && h == nil:
			t.Errorf("no S on tab %d", step.tab)
		case h != nil:
			underItsTarget(t, a, h, step.search)
		}
	}
}

// The hints take no click, no hover and no focus: Fyne gives them to what
// is under the mouse that takes them, so they go to what a hint covers.
func TestHintsTakeNoClick(t *testing.T) {
	a, _ := connectedTestApp(t)
	holdUntilShown(t, a, time.Now())
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch o.(type) {
		case fyne.Tappable, fyne.SecondaryTappable, fyne.DoubleTappable, fyne.Draggable, fyne.Focusable, fyne.Scrollable, desktop.Hoverable, desktop.Mouseable:
			t.Errorf("%T takes the mouse or the keys", o)
		}
		switch w := o.(type) {
		case *fyne.Container:
			for _, c := range w.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(w).Objects() {
				walk(c)
			}
		}
	}
	walk(a.hints)
	h := shownKeys(a)["1"]
	test.MoveMouse(a.win.Canvas(), a.fa.Driver().AbsolutePositionForObject(h).AddXY(h.Size().Width/2, h.Size().Height/2))
	if hovered(t, a.win.Canvas()) == reflect.ValueOf(h).Pointer() {
		t.Error("the hover goes to the hint")
	}
}

// The hints as pictures, in both themes, on the Record screen with a
// session going, and on the Settings, S under their search.
func TestShortcutHintSnapshots(t *testing.T) {
	if os.Getenv("SPYTIFY_UI_SNAPSHOTS") == "" {
		t.Skip("only for the pictures")
	}
	a, m := connectedTestApp(t)
	feedSession(t, m)
	for _, look := range []string{"light", "dark"} {
		values := defaults()
		values["app.theme"] = look
		m.SetSettings(values)
		a.render()
		holdUntilShown(t, a, time.Now())
		snapshot(t, a, "shortcut-hints-"+look)
		a.watchKeys(0, time.Now())
		a.tabs.SelectIndex(tabSettings)
		holdUntilShown(t, a, time.Now())
		snapshot(t, a, "shortcut-hints-settings-"+look)
		a.watchKeys(0, time.Now())
		a.tabs.SelectIndex(tabRecord)
	}
}
