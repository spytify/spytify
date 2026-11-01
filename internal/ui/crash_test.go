package ui

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
)

// leaveACrash writes a trace where spytifyd keeps the previous run's,
// after the app first looked.
func leaveACrash(t *testing.T, a *App, msg string) {
	t.Helper()
	telemetry.FindCrashes(a.opts.StateDir, a.fa.Preferences(), time.Now().Add(-time.Hour))
	trace := "panic: " + msg + "\n\ngoroutine 9 [running]:\ngithub.com/spytify/spytify/internal/core.(*Session).run(0x1)\n\tgithub.com/spytify/spytify/internal/core/session.go:300 +0x10\n"
	if err := os.WriteFile(filepath.Join(a.opts.StateDir, "spytifyd.crash.log.1"), []byte(trace), 0o600); err != nil {
		t.Fatal(err)
	}
}

// With the bug tracker off, a crash shows what happened, the error and its
// trace folded and scrubbed, to report on GitHub; so in a build without a
// bug tracker.
func TestACrashNotSentShowsItsTrace(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false)
	connect(t, a, m, false)
	telemetry.FindCrashes(a.opts.StateDir, a.fa.Preferences(), time.Now().Add(-time.Hour))
	home, _ := os.UserHomeDir()
	trace := "panic: open " + filepath.Join(home, "Music", "x.flac") + ": boom\n\ngoroutine 9 [running]:\n" +
		"github.com/spytify/spytify/internal/core.(*Session).run(0x1)\n\tgithub.com/spytify/spytify/internal/core/session.go:300 +0x10\n"
	if err := os.WriteFile(filepath.Join(a.opts.StateDir, "spytify-ui.crash.log.1"), []byte(trace), 0o600); err != nil {
		t.Fatal(err)
	}
	a.checkCrashes()
	d := overlay(t, a)
	snapshot(t, a, "privacy-crash-not-sent")
	all := strings.Join(texts(d), "\n")
	if !strings.Contains(all, "Spytify stopped unexpectedly") || !strings.Contains(all, "No report was sent to the developers: the Sentry bug tracker is off") {
		t.Errorf("dialog texts %v", texts(d))
	}
	// The dialog's height, from its title to its buttons.
	height := func() float32 {
		top := a.fa.Driver().AbsolutePositionForObject(labels(d, "Spytify stopped unexpectedly")[0])
		return a.fa.Driver().AbsolutePositionForObject(buttonObjects(d, "Close")[0]).Y - top.Y
	}
	folded := height()
	tapButton(t, d, "Error and stack trace")
	snapshot(t, a, "privacy-crash-not-sent-open")
	if open := height(); folded > 200 || open < folded+150 {
		t.Errorf("the dialog is %v tall folded and %v open", folded, open)
	}
	all = strings.Join(texts(d), "\n")
	if !strings.Contains(all, "panic: open ~") || strings.Contains(all, home) {
		t.Errorf("the trace shown is not scrubbed: %v", texts(d))
	}
	if b := buttons(d, "Report on GitHub"); len(b) != 1 {
		t.Fatal("no Report on GitHub button")
	}
	tapButton(t, d, "Error and stack trace")
	if h := height(); h != folded {
		t.Errorf("the dialog is %v tall folded again, %v at first", h, folded)
	}
	tapButton(t, d, "Close")
	if quiet(a, srv) != 0 {
		t.Error("a crash left with the bug tracker off")
	}

	a.tel = telemetry.New(telemetry.Options{Prefs: a.fa.Preferences()})
	leaveACrash(t, a, "boom")
	a.checkCrashes()
	if all := strings.Join(texts(overlay(t, a)), "\n"); !strings.Contains(all, "this build has no bug tracker") {
		t.Errorf("dialog texts in a build without a bug tracker: %v", all)
	}
}

// The GitHub issue holds the crash, cut to what a browser's address takes.
func TestIssueURLHoldsTheCrash(t *testing.T) {
	details := "Program: spytifyd\n\npanic: boom\n" + strings.Repeat("github.com/spytify/spytify/internal/core.(*Session).run(0x1)\n", 500)
	raw := issueURL("panic: boom", details)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Path != "/spytify/spytify/issues/new" || q.Get("title") != "Crash: panic: boom" || !strings.Contains(q.Get("body"), "panic: boom") {
		t.Errorf("issue %s", raw)
	}
	if len(raw) > 8000 || !strings.Contains(q.Get("body"), "Copy in Spytify gives the rest") {
		t.Errorf("issue address of %d characters", len(raw))
	}
}

// With the bug tracker on, a crash leaves on its own, with no dialog.
func TestACrashLeavesOnItsOwn(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	leaveACrash(t, a, "boom")
	a.checkCrashes()
	if a.win.Canvas().Overlays().Top() != nil {
		t.Error("a dialog asks about the crash")
	}
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ex := ev["exception"].([]any)[0].(map[string]any); ex["value"] != "boom" {
		t.Errorf("sent %v", ex)
	}
	if left := telemetry.FindCrashes(a.opts.StateDir, a.fa.Preferences(), time.Now()); len(left) != 0 {
		t.Error("the crash would leave again")
	}
}

// labels are the visible labels under o with this text.
func labels(o fyne.CanvasObject, text string) []*widget.Label {
	var out []*widget.Label
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *widget.Label:
			if w.Text == text {
				out = append(out, w)
			}
			return
		case *fyne.Container:
			for _, child := range w.Objects {
				walk(child)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(o)
	return out
}
