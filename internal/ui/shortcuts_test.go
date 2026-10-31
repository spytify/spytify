package ui

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

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

func connectedTestApp(t *testing.T) (*App, *model.Model) {
	t.Helper()
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.render()
	return a, m
}

func (c *recordingCore) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.calls {
		if m == method {
			n++
		}
	}
	return n
}

// R is the app bar's action, as a click: Record, then Stop, then Stop now.
func TestTheShortcutPressesRecordThenStop(t *testing.T) {
	a, m, core := connectedApp(t)
	press(t, a, fyne.KeyR)
	waitCall(t, core, proto.MethodSessionStart)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	a.render()
	// A new press, not the key held.
	a.actionKeyAt = time.Time{}
	press(t, a, fyne.KeyR)
	if stops := waitStops(t, core, 1); stops[0].Now {
		t.Fatal("the shortcut's Stop ended the session at once")
	}
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateStopping}))
	a.render()
	a.actionKeyAt = time.Time{}
	press(t, a, fyne.KeyR)
	if stops := waitStops(t, core, 2); !stops[1].Now {
		t.Fatal("the shortcut's Stop now did not end the session at once")
	}
}

// A held key repeats; the session starts once, and does not go on to Stop
// and Stop now.
func TestAHeldShortcutPressesOnce(t *testing.T) {
	a, m, core := connectedApp(t)
	for range 5 {
		press(t, a, fyne.KeyR)
	}
	a.calls.Wait()
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	a.render()
	press(t, a, fyne.KeyR)
	a.calls.Wait()
	if starts, stops := core.count(proto.MethodSessionStart), core.count(proto.MethodSessionStop); starts != 1 || stops != 0 {
		t.Fatalf("a held key sent %d session.start and %d session.stop, want 1 and 0", starts, stops)
	}
}

// Record is off while the player is closed: from the keyboard too.
func TestTheShortcutLeavesADisabledRecord(t *testing.T) {
	a, m, core := connectedApp(t)
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	a.render()
	if !a.action.Disabled() {
		t.Fatal("Record is on with the player closed")
	}
	press(t, a, fyne.KeyR)
	a.calls.Wait()
	if core.called(proto.MethodSessionStart) {
		t.Fatal("the shortcut started a session Record had off")
	}
}

// 1 to 5 show the tabs in their order, as a click does: what had the focus
// loses it, so a key goes nowhere hidden.
func TestShortcutsShowTheTabs(t *testing.T) {
	a, _ := connectedTestApp(t)
	for _, step := range []struct {
		key  fyne.KeyName
		want int
	}{{fyne.Key2, tabLibrary}, {fyne.Key3, tabSettings}, {fyne.Key4, tabLogs}, {fyne.Key5, tabAbout}, {fyne.Key1, tabRecord}, {fyne.Key6, tabRecord}} {
		press(t, a, step.key)
		if got := a.tabs.SelectedIndex(); got != step.want {
			t.Errorf("%s showed tab %d, want %d", step.key, got, step.want)
		}
	}
	press(t, a, fyne.Key3)
	sw := a.screens[tabSettings].(*settingsScreen).fields["recorder.mute_ads"].input.(*toggle)
	if a.win.Canvas().Focus(sw); a.win.Canvas().Focused() != sw {
		t.Fatal("the switch took no focus")
	}
	press(t, a, fyne.Key1)
	if f := a.win.Canvas().Focused(); f != nil {
		t.Fatalf("a hidden Settings switch kept the focus: %T", f)
	}
}

// In a text field the shortcuts still work and type nothing, and the
// field's own keys stay its own.
func TestShortcutsGoThroughTextFields(t *testing.T) {
	a, _ := connectedTestApp(t)
	press(t, a, fyne.Key2)
	search := a.screens[tabLibrary].(*libraryScreen).search
	if a.win.Canvas().Focus(search); a.win.Canvas().Focused() != search {
		t.Fatal("the search took no focus")
	}
	test.Type(search, "m83")
	typeShortcut(t, a, &fyne.ShortcutSelectAll{})
	if got := search.SelectedText(); got != "m83" {
		t.Fatalf("select all in the search selected %q", got)
	}
	if a.runShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyLeft, Modifier: fyne.KeyModifierShortcutDefault}) {
		t.Fatal("the app took an entry's word move")
	}
	press(t, a, fyne.Key3)
	if a.tabs.SelectedIndex() != tabSettings || search.Text != "m83" {
		t.Fatalf("from the Library search: tab %d, search %q", a.tabs.SelectedIndex(), search.Text)
	}
	var folder *applyEntry
	for _, o := range a.screens[tabSettings].(*settingsScreen).fields["output.dir"].input.(*fyne.Container).Objects {
		if e, ok := o.(*applyEntry); ok {
			folder = e
		}
	}
	if a.win.Canvas().Focus(folder); a.win.Canvas().Focused() != folder {
		t.Fatal("the output folder field took no focus")
	}
	press(t, a, fyne.Key2)
	if a.tabs.SelectedIndex() != tabLibrary {
		t.Fatalf("from a Settings field: tab %d", a.tabs.SelectedIndex())
	}
}

// A dialog takes the keys, as it takes the clicks.
func TestNoShortcutsUnderADialog(t *testing.T) {
	a, _, core := connectedApp(t)
	d := dialog.NewInformation("Spytify", "A question", a.win)
	d.Show()
	press(t, a, fyne.Key2)
	press(t, a, fyne.KeyR)
	a.calls.Wait()
	if a.tabs.SelectedIndex() != tabRecord || core.called(proto.MethodSessionStart) {
		t.Fatalf("under a dialog: tab %d, session started %v", a.tabs.SelectedIndex(), core.called(proto.MethodSessionStart))
	}
	d.Hide()
	press(t, a, fyne.Key2)
	if a.tabs.SelectedIndex() != tabLibrary {
		t.Fatal("the shortcuts stay off once the dialog is gone")
	}
}

// The offline screen shows no tabs, so their keys change nothing there.
func TestNoTabShortcutsOffline(t *testing.T) {
	a, _, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	a.render()
	press(t, a, fyne.Key2)
	if a.tabs.SelectedIndex() != tabRecord {
		t.Fatalf("offline, the shortcut chose tab %d", a.tabs.SelectedIndex())
	}
}
