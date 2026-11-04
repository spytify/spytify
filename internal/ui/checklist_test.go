package ui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

// The diagnostics open with the focus on their checks, the last one
// highlighted, where they are scrolled to: the arrows move the highlight
// from check to check, scrolling it into view, Tab goes on to the buttons,
// and Escape closes the dialog. The buttons have no background but their
// focus, Send to the developers too.
func TestTheDiagnosticsOpenOnTheirChecks(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	in := info()
	for i := range 12 {
		in.Checks = append(in.Checks, proto.Check{Name: fmt.Sprintf("check %d", i), OK: true})
	}
	in.Checks = append(in.Checks, proto.Check{ID: proto.CheckFFmpeg, Name: "ffmpeg found", Detail: "not on PATH"})
	m.SetInfo(in)
	a.render()
	d := diagnostics(t, a)
	c := a.win.Canvas()
	l := a.screens[tabLogs].(*logsScreen).checkList
	if c.Focused() != l {
		t.Fatalf("the diagnostics open with %s focused", focusName(a, c.Focused()))
	}
	at := func(i int) bool {
		return !l.highlight.Hidden && l.highlight.Position().Y == l.rows[i].Position().Y && l.highlight.Size().Height == l.rows[i].Size().Height
	}
	if !at(len(l.rows) - 1) {
		t.Error("opened: the highlight is not on the last check")
	}
	if l.scroll.Offset.Y == 0 {
		t.Fatal("the checks do not scroll: nothing to bring into view")
	}
	snapshot(t, a, "diagnostics-checks-focused")
	for range len(l.rows) {
		pressKey(c, fyne.KeyUp)
	}
	if l.cursor != 0 || !at(0) || l.scroll.Offset.Y != 0 {
		t.Errorf("Up to the first check: on %d, scrolled %v", l.cursor, l.scroll.Offset.Y)
	}
	pressKey(c, fyne.KeyDown)
	if l.cursor != 1 || !at(1) {
		t.Errorf("Down: on %d", l.cursor)
	}
	for _, name := range []string{lang.RunChecksAgain, lang.SendToDevelopers, lang.Close} {
		if b := buttons(d, name); len(b) != 1 || b[0].Importance != widget.LowImportance {
			t.Errorf("%s: %d buttons, or one with a background", name, len(b))
		}
	}
	pressTab(c)
	if got := focusName(a, c.Focused()); got != lang.RunChecksAgain || !l.highlight.Hidden {
		t.Errorf("Tab from the checks: %q focused, highlight shown %v", got, !l.highlight.Hidden)
	}
	snapshot(t, a, "diagnostics-run-again-focused")
	c.Focus(l)
	pressEscape(c)
	if c.Overlays().Top() != nil {
		t.Error("Escape from the checks left the dialog shown")
	}
}

// A check's fix is reached as a Library row's buttons are: Tab stops on the
// checks once and goes on to the dialog's buttons, Right lights the fix of
// the check highlighted, Left goes back to the check, and Space presses it.
func TestACheckFixIsReachedByTheArrowsNotTab(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	in := info()
	in.Checks = append(in.Checks, proto.Check{ID: proto.CheckOutputWritable, Name: "output folder writable", Detail: "permission denied"})
	m.SetInfo(in)
	a.render()
	diagnostics(t, a)
	c := a.win.Canvas()
	l := a.screens[tabLogs].(*logsScreen).checkList
	if c.Focused() != l {
		t.Fatalf("the diagnostics open with %s focused", focusName(a, c.Focused()))
	}
	pressTab(c)
	if got := focusName(a, c.Focused()); got != lang.RunChecksAgain {
		t.Errorf("Tab from the checks: %q focused, want %q", got, lang.RunChecksAgain)
	}
	c.Focus(l)
	pressKey(c, fyne.KeyRight)
	if l.lit == nil || l.lit.Text != lang.ChooseAnotherFolder || !l.lit.focused {
		t.Fatalf("Right on the failed check lit %v", l.lit)
	}
	snapshot(t, a, "diagnostics-fix-lit")
	pressKey(c, fyne.KeyRight)
	if l.lit == nil || l.lit.Text != lang.ChooseAnotherFolder {
		t.Error("Right past the last fix left it")
	}
	fix := l.lit
	pressKey(c, fyne.KeyLeft)
	if l.lit != nil || fix.focused {
		t.Error("Left did not go back to the check")
	}
	pressKey(c, fyne.KeyRight)
	pressKey(c, fyne.KeySpace)
	if c.Overlays().Top() != nil || a.tabs.SelectedIndex() != tabSettings {
		t.Errorf("Space on Choose another folder: dialog shown %v, tab %d", c.Overlays().Top() != nil, a.tabs.SelectedIndex())
	}
}
