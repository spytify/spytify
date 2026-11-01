package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/model"
)

// closeWindow answers the window's close button: with Minimize to tray on
// and a tray to go to, the window hides there and the recording goes on;
// otherwise the app quits, which stops the recorder too.
func (a *App) closeWindow() {
	if a.hasTray() && a.last.Settings[settings.AppMinimizeToTray.String()] == true {
		a.hideToTray()
		return
	}
	a.quit()
}

// hasTray says whether the tray icon shows, or would: the platform has a
// tray and, on Linux, the desktop draws it and the toolkit started it.
func (a *App) hasTray() bool { return a.tray && !a.noTrayHost && !a.trayLate }

// trayNote is what Minimize to tray says under its label: why closing
// quits even with it on.
func (a *App) trayNote(s model.State) string {
	switch {
	case a.noTrayHost:
		return noTrayNote
	case a.trayLate && s.Settings[settings.AppMinimizeToTray.String()] == true:
		return lateTrayNote
	}
	return ""
}

// setTrayHost takes watchTrayHost's word: with a tray host, closing hides
// to the tray, whose icon registers with it by itself; with none, closing
// quits and Settings says why.
func (a *App) setTrayHost(present bool) {
	if a.noTrayHost == !present {
		return
	}
	a.noTrayHost = !present
	a.render()
}

func (a *App) setupTray() {
	desk, ok := a.fa.(desktop.App)
	if !ok {
		return
	}
	// The toolkit starts Linux's tray with its loop: one set up later
	// never shows, and closing quits until Spytify restarts.
	a.trayLate = a.running && !lateTrayShows
	desk.SetSystemTrayIcon(Logo)
	// The tray's action is the app bar's, which render copies there:
	// Record, Stop, or Stop now while the last track is being saved.
	a.trayAction = fyne.NewMenuItem("Record", a.toggleRecording)
	a.trayAction.Disabled = true
	a.trayMenu = fyne.NewMenu("Spytify",
		a.trayAction,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open window", a.showWindow),
	)
	desk.SetSystemTrayMenu(a.trayMenu)
}

// syncTray gives the tray the app bar's action, rebuilding the tray menu
// only when it changes.
func (a *App) syncTray() {
	if a.trayMenu == nil {
		return
	}
	label, off := a.action.Text, a.action.Disabled()
	if a.trayAction.Label == label && a.trayAction.Disabled == off {
		return
	}
	a.trayAction.Label, a.trayAction.Disabled = label, off
	a.trayMenu.Refresh()
}

const trayNoticeKey = "tray-notice-seen"

// hideToTray hides the window, and on macOS the Dock icon with it; the
// session goes on. The first time, a notification says where the app is.
func (a *App) hideToTray() {
	a.win.Hide()
	a.notes.hidden = true
	if a.opts.ManualRender {
		return
	}
	showInDock(false)
	prefs := a.fa.Preferences()
	if !prefs.Bool(trayNoticeKey) {
		prefs.SetBool(trayNoticeKey, true)
		a.fa.SendNotification(fyne.NewNotification("Spytify is still running",
			"It records from the menu bar. Quit it from there, or turn off Minimize to tray in Settings."))
	}
}

func (a *App) showWindow() {
	a.notes.hidden = false
	if !a.opts.ManualRender {
		showInDock(true)
	}
	a.win.Show()
	a.win.RequestFocus()
}

// Reopen brings the window back, hidden to the tray or not: Spytify was
// opened again while it runs. It may be called from any goroutine.
func (a *App) Reopen() {
	fyne.Do(a.showWindow)
}
