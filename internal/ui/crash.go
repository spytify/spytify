package ui

import (
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry"
)

// checkCrashes sends the crashes left behind with the bug tracker on.
// With it off, or in a build without one, it shows them, for the user to
// report on GitHub if they want.
func (a *App) checkCrashes() {
	prefs := a.fa.Preferences()
	on := a.tel.Enabled()
	var unsent []telemetry.Crash
	for _, cr := range telemetry.FindCrashes(a.opts.StateDir, prefs, time.Now()) {
		telemetry.MarkHandled(prefs, cr)
		if on {
			go func(cr telemetry.Crash) { _ = a.tel.SendCrash(cr) }(cr)
		} else {
			unsent = append(unsent, cr)
		}
	}
	a.showCrashes(unsent)
}

// showCrashes shows the crashes one after the other.
func (a *App) showCrashes(crashes []telemetry.Crash) {
	if len(crashes) > 0 {
		a.showCrash(crashes[0], func() { a.showCrashes(crashes[1:]) })
	}
}

// showCrash says a program crashed and that no report left, with its
// error and trace, folded, to report on GitHub by hand.
func (a *App) showCrash(cr telemetry.Crash, then func()) {
	who := "Spytify"
	if cr.Program == "spytifyd" {
		who = "The recorder"
	}
	why := "the Sentry bug tracker is off"
	if !a.tel.Available() {
		why = "this build has no bug tracker"
	}
	title, details := a.tel.CrashReport(cr)
	body := widget.NewLabel("No report was sent to the developers: " + why + ". " +
		"To report it yourself, open an issue on GitHub with the error below.")
	body.Wrapping = fyne.TextWrapWord
	errTitle := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	errTitle.Wrapping = fyne.TextWrapWord
	trace := container.NewScroll(widget.NewLabelWithStyle(details, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}))
	trace.SetMinSize(fyne.NewSize(0, 180))
	fault := container.NewBorder(errTitle, nil, nil, nil, trace)
	fault.Hide()
	// A fold of its own rather than an accordion, which says nothing when it
	// opens: the dialog fits what it shows, compact folded, taller open.
	var d *dialog.CustomDialog
	fit := func() {
		d.Resize(fyne.NewSize(crashDialogWidth, 0))
		// The overlay centres the dialog again as it lays it out.
		if top := a.win.Canvas().Overlays().Top(); top != nil {
			top.Refresh()
		}
	}
	fold := widget.NewButtonWithIcon("Error and stack trace", theme.MenuExpandIcon(), nil)
	fold.Importance = widget.LowImportance
	fold.Alignment = widget.ButtonAlignLeading
	fold.OnTapped = func() {
		if fault.Visible() {
			fault.Hide()
			fold.SetIcon(theme.MenuExpandIcon())
		} else {
			fault.Show()
			fold.SetIcon(theme.MenuDropDownIcon())
		}
		fit()
	}
	d = dialog.NewCustomWithoutButtons(who+" stopped unexpectedly", container.NewVBox(body, fold, fault), a.win)
	closeButton := widget.NewButton("Close", d.Hide)
	copyButton := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		a.fa.Clipboard().SetContent(title + "\n\n" + details)
	})
	report := widget.NewButtonWithIcon("Report on GitHub", openInNewIcon, func() { a.openURL(issueURL(title, details)) })
	report.Importance = widget.HighImportance
	d.SetButtons([]fyne.CanvasObject{closeButton, copyButton, report})
	d.SetOnClosed(then)
	d.Resize(fyne.NewSize(crashDialogWidth, 0))
	d.Show()
	// Its text wraps at the dialog's width, known once shown.
	fit()
	focusFirst(a.win)
}

// crashDialogWidth is the crash dialog's width; its height follows what
// it shows.
const crashDialogWidth = 600

// issueURL opens a new GitHub issue holding a crash. GitHub takes its text
// from the address, which browsers keep under some 8,000 characters, so
// the details are cut after their first lines, where the crash happened.
func issueURL(title, details string) string {
	const most = 2500
	if len(details) > most {
		cut := strings.LastIndex(details[:most], "\n")
		if cut < 0 {
			cut = most
		}
		details = details[:cut] + "\n… (cut: Copy in Spytify gives the rest)"
	}
	body := "What I was doing:\n\n\n```\n" + details + "\n```\n"
	q := url.Values{"title": {"Crash: " + title}, "body": {body}}
	return sourceURL + "/issues/new?" + q.Encode()
}
