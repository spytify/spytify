package ui

import (
	"errors"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/lang"
)

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

// closesOnEscapeEverywhere opens a modal, and Escape closes it from each of
// its controls in turn, then from nothing focused, as dismissed tells its
// Close, Cancel or No does.
func closesOnEscapeEverywhere(t *testing.T, a *App, name string, open func(), dismissed func() bool) {
	t.Helper()
	c := a.win.Canvas()
	open()
	first := c.Focused()
	if first == nil {
		t.Fatalf("%s opens with nothing focused", name)
	}
	stops := 1
	for ; stops < 50; stops++ {
		pressTab(c)
		if c.Focused() == first {
			break
		}
	}
	pressEscape(c)
	for stop := 0; stop <= stops; stop++ {
		open()
		if c.Overlays().Top() == nil {
			t.Fatalf("%s did not open", name)
		}
		at := "nothing focused"
		if stop < stops {
			for range stop {
				pressTab(c)
			}
			at = focusName(a, c.Focused())
		} else {
			c.Unfocus()
		}
		pressEscape(c)
		if c.Overlays().Top() != nil {
			t.Fatalf("Escape on %s left %s open", at, name)
		}
		if dismissed != nil && !dismissed() {
			t.Fatalf("Escape on %s closed %s otherwise than its Close, Cancel or No", at, name)
		}
	}
}

// Every modal closes on Escape, wherever the focus is in it: the dialogs,
// Fyne's included, the diagnostics and the crash reports.
func TestEscapeClosesEveryModal(t *testing.T) {
	t.Run("confirm and error", func(t *testing.T) {
		a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
		feedSession(t, m)
		a.render()
		quit := false
		a.quitApp = func() { quit = true }
		closesOnEscapeEverywhere(t, a, lang.Quit, a.quit, func() bool { return !quit })
		closesOnEscapeEverywhere(t, a, lang.RestartRecorderAsk, a.player.restart, nil)
		closesOnEscapeEverywhere(t, a, "an error", func() { a.showError(errors.New("boom")) }, nil)
	})

	t.Run("diagnostics and sending them", func(t *testing.T) {
		srv := fakesentry.New(t)
		a, m := privacyApp(t, srv, true)
		connect(t, a, m, true)
		failCheck(a, m)
		l := a.screens[tabLogs].(*logsScreen)
		closesOnEscapeEverywhere(t, a, lang.Diagnostics, a.showDiagnostics, func() bool { return !l.open })
		answer := "none"
		closesOnEscapeEverywhere(t, a, lang.SendToDevelopers, func() {
			answer = "none"
			a.sendChecks(a.last, func(ok bool) { answer = "sent" })
		}, func() bool { return answer == "none" })
	})

	t.Run("first run and privacy", func(t *testing.T) {
		srv := fakesentry.New(t)
		a, _ := privacyApp(t, srv, true)
		prefs := a.fa.Preferences()
		closed := false
		closesOnEscapeEverywhere(t, a, lang.BeforeYouRecord, func() {
			closed = false
			prefs.SetBool(firstRunKey, false)
			a.maybeShowFirstRun(func() { closed = true })
		}, func() bool { return closed && prefs.Bool(firstRunKey) })
		closesOnEscapeEverywhere(t, a, lang.HelpFixIssues, func() {
			closed = false
			prefs.SetBool(privacySeenKey, false)
			a.askPrivacy(func() { closed = true })
		}, func() bool { return closed && prefs.Bool(privacySeenKey) })
	})

	t.Run("crash", func(t *testing.T) {
		srv := fakesentry.New(t)
		a, m := privacyApp(t, srv, false)
		connect(t, a, m, false)
		a.tel = telemetry.New(telemetry.Options{Prefs: a.fa.Preferences()})
		then := false
		closesOnEscapeEverywhere(t, a, "the crash report", func() {
			then = false
			a.showCrash(telemetry.Crash{Program: "spytifyd", Trace: "panic: boom"}, func() { then = true })
		}, func() bool { return then })
	})
}

// The folder picker is Fyne's own, controls and all: Escape closes it with
// nothing focused, which is all of it the app reaches.
func TestEscapeClosesTheFolderPickerWithNothingFocused(t *testing.T) {
	a, _, _ := connectedApp(t)
	a.showTab(tabSettings)
	a.render()
	c := a.win.Canvas()
	tabTo(t, a, "Choose folder", 1)
	pressSpace(c)
	if c.Overlays().Top() == nil {
		t.Fatal("the folder picker did not open")
	}
	c.Unfocus()
	pressEscape(c)
	if c.Overlays().Top() != nil {
		t.Fatal("Escape left the folder picker open")
	}
}
