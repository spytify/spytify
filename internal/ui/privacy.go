package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// The Sentry bug tracker, all or nothing (package telemetry). On, the app
// sends its crashes, the problems it runs into with the steps that led
// there, its launches and timings, counts of the tracks saved and skipped
// and how they were cut, and, on a click, the checks that fail. Off, it
// sends nothing and asks nothing.

var trackerKey = settings.PrivacyBugTracker.String()

const (
	// pendingKey holds privacy choices made before the recorder answered,
	// sent to it once it does.
	pendingKey = "privacy-pending"
	// privacySeenKey is set once the bug tracker choice was shown.
	privacySeenKey = "bug-tracker-choice-seen"
	trackerLabel   = "Sentry bug tracker: send crashes, errors, traces and recording counts"
	optedOut       = "You opted out of the Sentry bug tracker"
)

// launchTrace times the way from launch to a recorder that answers.
type launchTrace struct {
	start, started, connected time.Time
	done                      bool
}

// privacy is a privacy setting as the app knows it: a choice not sent to
// the recorder yet, the recorder's value, or the config file's at launch.
func (a *App) privacy(key string) any {
	if v, ok := a.pendingPrivacy()[key]; ok {
		return v
	}
	if v, ok := a.last.Settings[key]; ok {
		return v
	}
	return a.opts.Privacy[key]
}

func (a *App) pendingPrivacy() map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(a.fa.Preferences().String(pendingKey)), &m)
	return m
}

// setPrivacy applies a privacy choice now and sends it to the recorder,
// at once or when it answers.
func (a *App) setPrivacy(key string, v any) {
	p := a.pendingPrivacy()
	p[key] = v
	b, _ := json.Marshal(p)
	a.fa.Preferences().SetString(pendingKey, string(b))
	if key == trackerKey {
		on, _ := v.(bool)
		a.tel.SetEnabled(on && a.shown)
		a.tel.Breadcrumb("settings", trackerKey, map[string]any{"value": on})
	}
	if a.last.Conn == model.Connected {
		a.flushPrivacy()
	}
}

// flushPrivacy sends the choices made while the recorder was away.
func (a *App) flushPrivacy() {
	p := a.pendingPrivacy()
	if len(p) == 0 {
		return
	}
	a.calls.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if a.link.Do(ctx, proto.MethodSettingsSet, proto.SettingsSetParams{Values: p}, nil) == nil {
			fyne.Do(func() {
				// Only what was sent: a choice made meanwhile stays.
				now := a.pendingPrivacy()
				for k, v := range p {
					if fmt.Sprint(now[k]) == fmt.Sprint(v) {
						delete(now, k)
					}
				}
				b, _ := json.Marshal(now)
				a.fa.Preferences().SetString(pendingKey, string(b))
			})
		}
	})
}

// renderPrivacy follows the connection and the settings: the bug tracker
// starts or stops, a crash left behind leaves, the launch is timed and the
// checks noted. Nothing starts before the first-run notice had its say.
func (a *App) renderPrivacy(s model.State) {
	connected := s.Conn == model.Connected
	if connected != a.wasConnected {
		a.wasConnected = connected
		if connected {
			a.launch.connected = time.Now()
			a.flushPrivacy()
		}
		a.tel.Breadcrumb("app", map[bool]string{true: "recorder connected", false: "recorder gone"}[connected], nil)
		// A recorder that went away may have crashed; one that came back
		// moved its trace aside.
		if a.shown {
			a.checkCrashes()
		}
	}
	on, _ := a.privacy(trackerKey).(bool)
	a.tel.SetEnabled(on && a.shown)
	if !a.shown || !s.HaveInfo {
		return
	}
	if !a.launch.done {
		a.launch.done = true
		a.launchReady(s)
	}
	if checks := fmt.Sprint(s.Info.Checks); checks != a.checksNoted {
		a.checksNoted = checks
		a.tel.Breadcrumb("checks", fmt.Sprintf("%d of %d pass", len(s.Info.Checks)-len(failingNames(s.Info.Checks)), len(s.Info.Checks)),
			map[string]any{"failing": strings.Join(failingNames(s.Info.Checks), ", ")})
	}
}

// launchReady reports a launch that reached a recorder that answers.
func (a *App) launchReady(s model.State) {
	now := time.Now()
	a.tel.Step("ready")
	a.tel.Breadcrumb("app", "recorder ready", nil)
	a.tel.Log("launch", map[string]any{
		"ready":           true,
		"ms":              now.Sub(a.launch.start).Milliseconds(),
		"auto_started":    !a.launch.started.IsZero(),
		"failing":         failingNames(s.Info.Checks),
		"software_opengl": a.opts.SoftwareOpenGL,
	})
	var spans []telemetry.Span
	if !a.launch.started.IsZero() && a.launch.connected.After(a.launch.started) {
		spans = append(spans, telemetry.Span{Op: "recorder.start", Start: a.launch.started, End: a.launch.connected})
	}
	if !a.launch.connected.IsZero() {
		spans = append(spans, telemetry.Span{Op: "recorder.info", Start: a.launch.connected, End: now})
	}
	a.tel.Measure("launch", a.launch.start, now, nil, spans)
}

// launchFailed reports a recorder that did not start.
func (a *App) launchFailed(err error) {
	kind := startErrorKind(err)
	a.tel.Log("launch", map[string]any{"ready": false, "start_error": kind, "software_opengl": a.opts.SoftwareOpenGL})
	a.tel.Problem("recorder_start", kind)
}

func startErrorKind(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, link.ErrInArchive):
		return "in archive"
	case strings.Contains(msg, "does not answer"):
		return "no answer"
	case errors.Is(err, link.ErrNoDaemon) || errors.Is(err, exec.ErrNotFound) || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file"):
		return "not found"
	}
	return "failed"
}

// failingNames are the checks that fail, but for the player not running,
// which the app bar shows and is no problem.
func failingNames(checks []proto.Check) []string {
	var names []string
	for _, c := range checks {
		if !c.OK && c.Name != "player running" {
			names = append(names, c.Name)
		}
	}
	return names
}

// problemCodes are the recorder's errors worth a problem report: not a
// full disk, which is the user's to fix.
var problemCodes = map[string]bool{
	"capture_stalled": true, "ring_overflow": true, "write": true, "temp_file": true,
	"output_dir": true, "read": true, "encode": true, "rename": true,
}

// onEvent turns the recorder's events into breadcrumbs, steps and problem
// reports. It runs on the link's goroutine.
func (a *App) onEvent(ev proto.Event) {
	if !a.tel.Enabled() {
		return
	}
	switch ev.Method {
	case proto.EventLevel:
		var e proto.LevelEvent
		if !a.heard && ev.Decode(&e) == nil && e.PeakDBFS > -60 {
			a.heard = true
			a.tel.Step("first_audio")
			a.tel.Breadcrumb("audio", "first sound", nil)
		}
	case proto.EventSessionState:
		var e proto.SessionStateEvent
		if ev.Decode(&e) == nil {
			a.tel.Breadcrumb("session", string(e.State), nil)
		}
	case proto.EventTargetState:
		var e proto.TargetStateEvent
		if ev.Decode(&e) == nil {
			a.tel.Breadcrumb("player", map[bool]string{true: "running", false: "not running"}[e.Running], nil)
		}
	case proto.EventTrackStarted:
		var e proto.TrackStartedEvent
		if ev.Decode(&e) != nil {
			return
		}
		a.tel.Breadcrumb("track", "started", map[string]any{"length_s": e.LengthMs / 1000})
		if e.Cut != nil {
			a.tel.Distribution("cut.offset", float64(e.Cut.OffsetMs), "millisecond", map[string]string{"kind": e.Cut.Kind, "by": e.Cut.By})
		}
	case proto.EventTrackFinished:
		var e proto.TrackFinishedEvent
		if ev.Decode(&e) != nil {
			return
		}
		data := map[string]any{"tagged": e.Tagged}
		counted := map[string]string{}
		if e.Timings != nil {
			data["format"] = e.Timings.Format
			counted["format"] = e.Timings.Format
		}
		// A take cut short and kept by hand is counted apart: it says
		// nothing of how well tracks record.
		if e.Partial {
			a.tel.Breadcrumb("track", "kept", data)
			a.tel.Count("track.kept", 1, counted)
			return
		}
		if e.Timings != nil {
			a.measureFinish(*e.Timings)
		}
		a.tel.Breadcrumb("track", "saved", data)
		a.tel.Count("track.saved", 1, counted)
		a.tel.Step("first_saved")
	case proto.EventTrackSkipped:
		var e proto.TrackSkippedEvent
		if ev.Decode(&e) != nil {
			return
		}
		a.tel.Breadcrumb("track", "skipped", map[string]any{"reason": string(e.Reason)})
		a.tel.Count("track.skipped", 1, map[string]string{"reason": string(e.Reason)})
	case proto.EventError:
		var e proto.ErrorEvent
		if ev.Decode(&e) != nil {
			return
		}
		a.tel.Breadcrumb("error", e.Code, nil)
		if problemCodes[e.Code] {
			a.tel.Problem(e.Code, e.Msg)
		}
	case proto.EventSettingsChanged:
		var e proto.SettingsValues
		if ev.Decode(&e) != nil {
			return
		}
		for k, v := range e.Values {
			a.tel.Breadcrumb("settings", k, map[string]any{"value": safeValue(v)})
		}
	}
}

// safeValue keeps a setting's value when it cannot hold a name or a path.
func safeValue(v any) any {
	switch v := v.(type) {
	case bool, float64, int, int64:
		return v
	case string:
		if len(v) <= 12 && !strings.ContainsAny(v, `/\~{ `) {
			return v
		}
	}
	return "changed"
}

// measureFinish sends how long saving a track took, step by step, ending
// now.
func (a *App) measureFinish(t proto.FinishTimings) {
	end := time.Now()
	at := end.Add(-time.Duration(t.TotalMs) * time.Millisecond)
	start := at
	var spans []telemetry.Span
	for _, step := range []struct {
		op string
		ms int64
	}{{"trim", t.TrimMs}, {"cover", t.CoverMs}, {"encode", t.EncodeMs}, {"tags", t.TagMs}} {
		if step.ms <= 0 {
			continue
		}
		next := at.Add(time.Duration(step.ms) * time.Millisecond)
		spans = append(spans, telemetry.Span{Op: step.op, Start: at, End: next})
		at = next
	}
	a.tel.Measure("track.finish", start, end, map[string]string{"format": t.Format}, spans)
}

// noteAction leaves a breadcrumb of what the user did.
func (a *App) noteAction(what string) {
	a.tel.Breadcrumb("ui", what, nil)
	if what == "record" {
		a.tel.Step("first_record")
	}
}

// noteTab notes the tab shown; opening Logs, where the diagnostics are,
// is counted once a launch with the checks that fail then.
func (a *App) noteTab(i int) {
	if i < 0 || i >= len(a.tabs.Items) {
		return
	}
	a.tel.Breadcrumb("ui", "tab "+a.tabs.Items[i].Text, nil)
	if i == tabLogs && !a.diagNoted && a.tel.Enabled() {
		a.diagNoted = true
		a.tel.Log("diagnostics", map[string]any{"failing": failingNames(a.last.Info.Checks)})
	}
}

// askPrivacy shows, once, the bug tracker choice to someone who saw the
// first-run notice before it had one.
func (a *App) askPrivacy(then func()) {
	prefs := a.fa.Preferences()
	if !a.tel.Available() || prefs.Bool(privacySeenKey) {
		then()
		return
	}
	tracker := a.trackerCheck()
	body := widget.NewLabel("Spytify can send its crashes, errors, traces and recording counts to its developers through Sentry, to help them fix issues.")
	body.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom("Help fix issues", "OK", container.NewVBox(body, tracker), a.win)
	d.SetOnClosed(func() {
		prefs.SetBool(privacySeenKey, true)
		a.setPrivacy(trackerKey, tracker.Checked)
		then()
	})
	d.Resize(fyne.NewSize(460, 220))
	d.Show()
}

// trackerCheck is the bug tracker box, as the setting is now.
func (a *App) trackerCheck() *widget.Check {
	c := widget.NewCheck(trackerLabel, nil)
	on, _ := a.privacy(trackerKey).(bool)
	c.SetChecked(on)
	return c
}

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

// sendChecks shows what the diagnostics' report holds, and sends it on
// Send.
func (a *App) sendChecks(s model.State, sent func(ok bool)) {
	var failing []proto.Check
	for _, c := range s.Info.Checks {
		if !c.OK && c.Name != "player running" {
			failing = append(failing, c)
		}
	}
	var log []string
	lines := s.Logs
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
	}
	for _, l := range lines {
		log = append(log, l.TS+" "+l.Level+" "+l.Msg)
	}
	report := telemetry.Checks{Failing: failing, Log: log}
	body := widget.NewLabel("Send the checks that fail and the end of the recorder's log to the developers? The log can name tracks you recorded.")
	body.Wrapping = fyne.TextWrapWord
	preview := widget.NewLabelWithStyle(a.tel.ChecksPreview(report), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	scroll := container.NewScroll(preview)
	scroll.SetMinSize(fyne.NewSize(0, 160))
	what := widget.NewAccordion(widget.NewAccordionItem("What will be sent", scroll))
	message := widget.NewMultiLineEntry()
	message.SetPlaceHolder("Comment (optional)")
	message.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm("Send to the developers", "Send", "Cancel", container.NewVBox(body, what, message), func(send bool) {
		if !send {
			return
		}
		text := message.Text
		// A report that does not leave is no error to show: the button
		// says so and the app goes on.
		a.reports.Go(func() {
			err := a.tel.SendChecks(report, text)
			fyne.Do(func() { sent(err == nil) })
		})
	}, a.win)
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
}
