package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/ui/lang"
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

// S puts the focus in the search field of the Library or the Settings, from
// anywhere on the screen, a field included, its text selected for what is
// typed to replace it. Elsewhere, under a dialog or offline, it moves
// nothing.
func TestTheShortcutGoesToTheSearch(t *testing.T) {
	a, _ := connectedTestApp(t)
	c := a.win.Canvas()
	library := a.screens[tabLibrary].(*libraryScreen).search
	settings := a.screens[tabSettings].(*settingsScreen).search
	press(t, a, fyne.KeyS)
	if f := c.Focused(); f != nil {
		t.Fatalf("on the Record screen, S focused %T", f)
	}
	press(t, a, fyne.Key2)
	press(t, a, fyne.KeyS)
	if c.Focused() != library {
		t.Fatalf("on the Library, S focused %T", c.Focused())
	}
	test.Type(library, "m83")
	press(t, a, fyne.Key3)
	sw := a.screens[tabSettings].(*settingsScreen).fields["recorder.mute_ads"].input.(*toggle)
	c.Focus(sw)
	press(t, a, fyne.KeyS)
	if c.Focused() != settings {
		t.Fatalf("from a Settings switch, S focused %T", c.Focused())
	}
	var folder *applyEntry
	for _, o := range a.screens[tabSettings].(*settingsScreen).fields["output.dir"].input.(*fyne.Container).Objects {
		if e, ok := o.(*applyEntry); ok {
			folder = e
		}
	}
	c.Focus(folder)
	press(t, a, fyne.KeyS)
	if c.Focused() != settings {
		t.Fatalf("from the output folder field, S focused %T", c.Focused())
	}
	press(t, a, fyne.Key2)
	press(t, a, fyne.KeyS)
	if c.Focused() != library || library.SelectedText() != "m83" {
		t.Fatalf("back on the Library: %T focused, %q selected", c.Focused(), library.SelectedText())
	}
	test.Type(library, "air")
	if library.Text != "air" {
		t.Fatalf("typed after S, the search reads %q", library.Text)
	}
	for _, key := range []fyne.KeyName{fyne.Key4, fyne.Key5} {
		press(t, a, key)
		press(t, a, fyne.KeyS)
		if f := c.Focused(); f != nil {
			t.Errorf("on tab %d, S focused %T", a.tabs.SelectedIndex(), f)
		}
	}

	press(t, a, fyne.Key3)
	d := dialog.NewInformation("Spytify", "A question", a.win)
	d.Show()
	press(t, a, fyne.KeyS)
	if c.Focused() == settings {
		t.Fatal("under a dialog, S focused the search")
	}
	d.Hide()

	off, _, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	off.tabs.SelectIndex(tabLibrary)
	off.render()
	press(t, off, fyne.KeyS)
	if f := off.win.Canvas().Focused(); f != nil {
		t.Fatalf("offline, S focused %T", f)
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

// The tooltips write the keys as each system does: the Mac's symbols, or
// the modifiers' names elsewhere.
func TestKeyLabels(t *testing.T) {
	for _, c := range []struct {
		mac  bool
		mod  fyne.KeyModifier
		key  fyne.KeyName
		want string
	}{
		{true, fyne.KeyModifierSuper, fyne.KeyR, "⌘R"},
		{true, fyne.KeyModifierSuper, fyne.Key2, "⌘2"},
		{true, fyne.KeyModifierControl | fyne.KeyModifierShift | fyne.KeyModifierSuper, fyne.KeyR, "⌃⇧⌘R"},
		{false, fyne.KeyModifierControl, fyne.KeyR, "Ctrl+R"},
		{false, fyne.KeyModifierControl, fyne.Key2, "Ctrl+2"},
		{false, fyne.KeyModifierControl | fyne.KeyModifierAlt, fyne.KeyR, "Ctrl+Alt+R"},
	} {
		if got := keyLabel(c.mac, &desktop.CustomShortcut{KeyName: c.key, Modifier: c.mod}); got != c.want {
			t.Errorf("mac %v, modifiers %d with %s: %q, want %q", c.mac, c.mod, c.key, got, c.want)
		}
	}
	// The tooltip writes the key the shortcut is bound with.
	want := lang.Library + " (Ctrl+2)"
	if runtime.GOOS == "darwin" {
		want = lang.Library + " (⌘2)"
	}
	if got := shortcutTip(lang.Library, fyne.Key2); got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
}

// Record/Stop's tooltip follows its name.
func TestTheActionTipNamesItsKey(t *testing.T) {
	a, m := connectedTestApp(t)
	if got, want := a.action.ToolTip(), shortcutTip(lang.Record, actionKey); got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
	for _, step := range []struct {
		state proto.SessionState
		name  string
	}{{proto.StateRecording, lang.Stop}, {proto.StateStopping, lang.StopNow}, {proto.StateIdle, lang.Record}} {
		m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: step.state}))
		a.render()
		if got, want := a.action.ToolTip(), shortcutTip(step.name, actionKey); a.action.Text != step.name || got != want {
			t.Errorf("%s: button %q, tooltip %q, want %q", step.state, a.action.Text, got, want)
		}
	}
}

// hovered is what the test window's mouse is over, as the pointer the
// window keeps.
func hovered(t *testing.T, c fyne.Canvas) uintptr {
	t.Helper()
	h := reflect.ValueOf(c).Elem().FieldByName("hovered")
	if !h.IsValid() {
		t.Fatal("the test window keeps no hover")
	}
	if h.IsNil() {
		return 0
	}
	return h.Elem().Pointer()
}

// lit is whether a tab's button draws its hover.
func lit(b fyne.CanvasObject) bool {
	return reflect.ValueOf(b).Elem().FieldByName("hovered").Bool()
}

// Each tab's tooltip lies over the tab and names its key. The mouse's hover
// goes to the tooltip, and the tab still lights up; a click goes through
// to the tab.
func TestTabTipsLieOverTheirTabs(t *testing.T) {
	a, _ := connectedTestApp(t)
	c := a.win.Canvas()
	tips := a.tabsView.Objects[3:]
	if len(tips) != len(a.tabs.Items) {
		t.Fatalf("%d tooltips for %d tabs", len(tips), len(a.tabs.Items))
	}
	away := a.fa.Driver().AbsolutePositionForObject(a.tabsView).AddXY(a.tabsView.Size().Width-10, 10)
	for _, i := range []int{tabLibrary, tabSettings, tabLogs, tabAbout, tabRecord} {
		item, tip := a.tabs.Items[i], tips[i].(*tabTip)
		b := tabButton(item)
		if b == nil {
			t.Fatalf("%s: no button found on the tab", item.Text)
		}
		if !tip.Visible() || tip.Position() != b.Position() || tip.Size() != b.Size() {
			t.Fatalf("%s: tooltip at %v %v (shown %v), tab at %v %v", item.Text, tip.Position(), tip.Size(), tip.Visible(), b.Position(), b.Size())
		}
		if got, want := tip.ToolTip(), shortcutTip(item.Text, tabDigits[i]); got != want {
			t.Errorf("%s: tooltip %q, want %q", item.Text, got, want)
		}
		at := a.fa.Driver().AbsolutePositionForObject(tip).AddXY(tip.Size().Width/2, tip.Size().Height/2)
		// Away at once: the tooltip shows from a timer of its own, which
		// races the test.
		test.MoveMouse(c, at)
		over, on := hovered(t, c) == reflect.ValueOf(tip).Pointer(), lit(b)
		test.MoveMouse(c, away)
		if !over || !on {
			t.Errorf("%s: hover on the tooltip %v, tab lit %v", item.Text, over, on)
		}
		if lit(b) {
			t.Errorf("%s: the tab stays lit once the mouse is away", item.Text)
		}
		test.TapCanvas(c, at)
		if a.tabs.SelectedIndex() != i {
			t.Errorf("a click on %s showed tab %d", item.Text, a.tabs.SelectedIndex())
		}
	}
	// A window too narrow for the tabs puts some in a menu: no tooltip
	// then lies where they were.
	a.win.Resize(fyne.NewSize(300, 620))
	for _, tip := range tips {
		if tip.Visible() {
			t.Fatal("a tooltip shows while the tabs do not fit")
		}
	}
	a.win.Resize(fyne.NewSize(920, 620))
	for _, tip := range tips {
		if !tip.Visible() {
			t.Fatal("a tooltip stays hidden once the tabs fit")
		}
	}
}

// The tooltips as pictures, with the Mac's keys and the others': made only
// for them, as a tooltip shows from its own timer.
func TestShortcutTipSnapshots(t *testing.T) {
	if os.Getenv("SPYTIFY_UI_SNAPSHOTS") == "" {
		t.Skip("only for the pictures")
	}
	a, _ := connectedTestApp(t)
	c := a.win.Canvas()
	away := a.fa.Driver().AbsolutePositionForObject(a.tabsView).AddXY(a.tabsView.Size().Width/2, 300)
	tip := a.tabsView.Objects[3+tabLibrary].(*tabTip)
	for _, sys := range []struct {
		name string
		mac  bool
		mod  fyne.KeyModifier
	}{{"mac", true, fyne.KeyModifierSuper}, {"ctrl", false, fyne.KeyModifierControl}} {
		label := func(name string, key fyne.KeyName) string {
			return name + " (" + keyLabel(sys.mac, &desktop.CustomShortcut{KeyName: key, Modifier: sys.mod}) + ")"
		}
		a.action.SetToolTip(label(a.action.Text, actionKey))
		tip.SetToolTip(label(lang.Library, tabDigits[tabLibrary]))
		for name, o := range map[string]fyne.CanvasObject{"record": a.action, "library": tip} {
			test.MoveMouse(c, a.fa.Driver().AbsolutePositionForObject(o).AddXY(o.Size().Width/2, o.Size().Height/2))
			time.Sleep(time.Second)
			snapshot(t, a, "shortcut-tip-"+name+"-"+sys.name)
			test.MoveMouse(c, away)
		}
	}
}
