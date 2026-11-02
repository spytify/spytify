package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// The log goes to the window's sides, its scroll bar at the edge as the
// Library's is, while its tools and its lines' text stay two paddings off
// them, in line with each other.
func TestTheLogGoesToTheWindowsEdge(t *testing.T) {
	a, m, _ := connectedApp(t)
	for i := range 40 {
		m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: fmt.Sprintf("line %d", i), TS: "2026-10-31T12:00:00Z"}))
	}
	a.showTab(tabLogs)
	a.render()
	l := a.screens[tabLogs].(*logsScreen)
	d := fyne.CurrentApp().Driver()
	width := a.win.Canvas().Size().Width
	if x := d.AbsolutePositionForObject(l.list).X; x != 0 || l.list.Size().Width != width {
		t.Errorf("the log is at %v, %v wide, in a window %v wide", x, l.list.Size().Width, width)
	}
	side := 2 * theme.Padding()
	show := labelsIn(l.root, func(s string) bool { return s == lang.Show })
	prefixes := labelsIn(l.list, func(s string) bool { return strings.HasSuffix(s, " info ") })
	if len(show) != 1 || len(prefixes) == 0 {
		t.Fatalf("%d Show labels, %d lines", len(show), len(prefixes))
	}
	for name, o := range map[string]fyne.CanvasObject{"tools": show[0], "lines": prefixes[0]} {
		if x := d.AbsolutePositionForObject(o).X; x != side {
			t.Errorf("the %s are at %v, want %v", name, x, side)
		}
	}
	snapshot(t, a, "logs-to-the-edge")
}

// The diagnostics' checks go from one side of the dialog to the other,
// their scroll bar at its edge, while they, the summary and the buttons
// stay two paddings off its sides. In the dark theme the dialog is
// charcoal, the cards' colour, rather than Fyne's navy blue.
func TestTheDiagnosticsChecksGoToTheDialogsEdge(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	in := info()
	for i := range 12 {
		in.Checks = append(in.Checks, proto.Check{Name: fmt.Sprintf("check %d", i), OK: true})
	}
	m.SetInfo(in)
	values := defaults()
	values["app.theme"] = "dark"
	m.SetSettings(values)
	a.render()
	diagnostics(t, a)
	l := a.screens[tabLogs].(*logsScreen)
	d := fyne.CurrentApp().Driver()
	dialog, size := d.AbsolutePositionForObject(l.diag.Content), l.diag.Content.Size()
	if x := d.AbsolutePositionForObject(l.checksScroll).X; x != dialog.X || l.checksScroll.Size().Width != size.Width {
		t.Errorf("the checks' scroll is at %v, %v wide, in a dialog at %v, %v wide", x, l.checksScroll.Size().Width, dialog.X, size.Width)
	}
	side := 2 * theme.Padding()
	for name, o := range map[string]fyne.CanvasObject{"checks": l.checks, "summary": l.summary, "buttons": l.actions} {
		if x := d.AbsolutePositionForObject(o).X - dialog.X; x != side {
			t.Errorf("the %s are %v off the dialog's side, want %v", name, x, side)
		}
	}
	th := a.fa.Settings().Theme()
	for i, v := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		if got := (spytifyTheme{mode: "system"}).Color(theme.ColorNameOverlayBackground, v); got != cardColors[colorCard][i] {
			t.Errorf("variant %d: dialogs on %v, want the cards' %v", v, got, cardColors[colorCard][i])
		}
	}
	if got := th.Color(theme.ColorNameOverlayBackground, theme.VariantLight); got != cardColors[colorCard][1] {
		t.Errorf("dark theme: dialogs on %v, want charcoal %v", got, cardColors[colorCard][1])
	}
	snapshot(t, a, "diagnostics-dark-to-the-edge")
}
