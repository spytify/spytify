package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// What the app sends the developers, and only what the user agreed to: a
// crash report or the failing checks on a click, and, with anonymous stats
// on, counts, timings and the problems it runs into (package telemetry).

const (
	statsKey   = "privacy.anonymous_stats"
	crashesKey = "privacy.crash_reports"
	// pendingKey holds privacy choices made before the recorder answered,
	// sent to it once it does.
	pendingKey = "privacy-pending"
	// privacySeenKey is set once the anonymous stats choice was shown.
	privacySeenKey = "privacy-choice-seen"
	statsLabel     = "Send anonymous stats to the developers to help fix bugs"
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
	if key == statsKey {
		on, _ := v.(bool)
		a.tel.SetStats(on)
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
	go func() {
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
	}()
}

// renderPrivacy follows the connection and the settings: anonymous stats
// start or stop, a crash left behind is offered, the launch is timed.
func (a *App) renderPrivacy(s model.State) {
	connected := s.Conn == model.Connected
	if connected != a.wasConnected {
		a.wasConnected = connected
		if connected {
			a.launch.connected = time.Now()
			a.flushPrivacy()
		}
		// A recorder that went away may have crashed; one that came back
		// moved its trace aside.
		if a.shown {
			a.checkCrashes()
		}
	}
	on, _ := a.privacy(statsKey).(bool)
	a.tel.SetStats(on)
	if s.HaveInfo && !a.launch.done {
		a.launch.done = true
		a.launchReady(s)
	}
}

// launchReady reports a launch that reached a recorder that answers.
func (a *App) launchReady(s model.State) {
	now := time.Now()
	a.tel.Step("ready")
	a.tel.Breadcrumb("app", "recorder ready", nil)
	a.tel.Log("launch", map[string]any{
		"ready":        true,
		"ms":           now.Sub(a.launch.start).Milliseconds(),
		"auto_started": !a.launch.started.IsZero(),
		"failing":      failingNames(s.Info.Checks),
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
	a.tel.Log("launch", map[string]any{"ready": false, "start_error": kind})
	a.tel.Problem("recorder_start", kind)
}

func startErrorKind(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "does not answer"):
		return "no answer"
	case errors.Is(err, exec.ErrNotFound) || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file"):
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
	if !a.tel.Stats() {
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
		if ev.Decode(&e) == nil {
			a.tel.Breadcrumb("track", "started", map[string]any{"length_s": e.LengthMs / 1000})
		}
	case proto.EventTrackFinished:
		var e proto.TrackFinishedEvent
		if ev.Decode(&e) != nil {
			return
		}
		a.incomplete = 0
		data := map[string]any{"tagged": e.Tagged}
		if e.Timings != nil {
			data["format"] = e.Timings.Format
			a.measureFinish(*e.Timings)
		}
		a.tel.Breadcrumb("track", "saved", data)
		a.tel.Step("first_saved")
	case proto.EventTrackSkipped:
		var e proto.TrackSkippedEvent
		if ev.Decode(&e) != nil {
			return
		}
		a.tel.Breadcrumb("track", "skipped", map[string]any{"reason": string(e.Reason)})
		if e.Reason == proto.SkipIncomplete {
			if a.incomplete++; a.incomplete == 3 {
				a.tel.Problem("incomplete_tracks", "three incomplete tracks in a row")
			}
		}
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
	if i == tabLogs && !a.diagNoted && a.tel.Stats() {
		a.diagNoted = true
		a.tel.Log("diagnostics", map[string]any{"failing": failingNames(a.last.Info.Checks)})
	}
}

// askPrivacy shows, once, the anonymous stats choice to someone who saw
// the first-run notice before it had one.
func (a *App) askPrivacy(then func()) {
	prefs := a.fa.Preferences()
	if !a.tel.Available() || prefs.Bool(privacySeenKey) {
		then()
		return
	}
	stats := a.statsCheck()
	body := widget.NewLabel("Spytify can tell its developers how it is doing: how many people use it, how long its steps take and the problems it runs into. " +
		"Nothing in it names you, your computer or your music. You can change this in Settings > Privacy.")
	body.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom("Help fix bugs", "OK", container.NewVBox(body, stats), a.win)
	d.SetOnClosed(func() {
		prefs.SetBool(privacySeenKey, true)
		a.setPrivacy(statsKey, stats.Checked)
		then()
	})
	d.Resize(fyne.NewSize(460, 220))
	d.Show()
}

// statsCheck is the anonymous stats box, as the setting is now.
func (a *App) statsCheck() *widget.Check {
	c := widget.NewCheck(statsLabel, nil)
	on, _ := a.privacy(statsKey).(bool)
	c.SetChecked(on)
	return c
}

// checkCrashes offers, sends or forgets the crashes left behind, as the
// crash reports setting says.
func (a *App) checkCrashes() {
	if !a.tel.Available() || a.crashOpen {
		return
	}
	prefs := a.fa.Preferences()
	crashes := telemetry.FindCrashes(a.opts.StateDir, prefs, time.Now())
	if len(crashes) == 0 {
		return
	}
	switch a.privacy(crashesKey) {
	case "never":
		for _, cr := range crashes {
			telemetry.MarkHandled(prefs, cr)
		}
	case "always":
		for _, cr := range crashes {
			telemetry.MarkHandled(prefs, cr)
			go func(cr telemetry.Crash) { _ = a.tel.SendCrash(cr, "") }(cr)
		}
	default:
		a.askCrash(crashes[0], a.checkCrashes)
	}
}

// askCrash asks before sending a crash, showing what would leave.
func (a *App) askCrash(cr telemetry.Crash, then func()) {
	a.crashOpen = true
	who := "Spytify"
	if cr.Program == "spytifyd" {
		who = "The recorder"
	}
	body := widget.NewLabel(who + " stopped on an error. Send the report to the developers? It holds the error and where it happened in the code; " +
		"your home folder, user name and track names are replaced.")
	body.Wrapping = fyne.TextWrapWord
	preview := widget.NewLabelWithStyle(a.tel.CrashPreview(cr), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	scroll := container.NewScroll(preview)
	scroll.SetMinSize(fyne.NewSize(0, 160))
	what := widget.NewAccordion(widget.NewAccordionItem("What will be sent", scroll))
	message := widget.NewMultiLineEntry()
	message.SetPlaceHolder("What were you doing? (optional)")
	message.Wrapping = fyne.TextWrapWord
	again := widget.NewCheck("Don't ask again", nil)
	d := dialog.NewCustomConfirm(who+" stopped unexpectedly", "Send", "Don't send", container.NewVBox(body, what, message, again), func(send bool) {
		telemetry.MarkHandled(a.fa.Preferences(), cr)
		if again.Checked {
			a.setPrivacy(crashesKey, map[bool]string{true: "always", false: "never"}[send])
		}
		if send {
			text := message.Text
			go func() {
				if err := a.tel.SendCrash(cr, text); err != nil {
					fyne.Do(func() { a.showError(err) })
				}
			}()
		}
		a.crashOpen = false
		then()
	}, a.win)
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
}

// sendChecks shows what the diagnostics' report holds, and sends it on
// Send.
func (a *App) sendChecks(s model.State, sent func()) {
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
	body := widget.NewLabel("Send the checks that fail and the end of the recorder's log to the developers? " +
		"Your home folder, user name and track names are replaced.")
	body.Wrapping = fyne.TextWrapWord
	preview := widget.NewLabelWithStyle(a.tel.ChecksPreview(report), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	scroll := container.NewScroll(preview)
	scroll.SetMinSize(fyne.NewSize(0, 160))
	what := widget.NewAccordion(widget.NewAccordionItem("What will be sent", scroll))
	message := widget.NewMultiLineEntry()
	message.SetPlaceHolder("What were you trying to do? (optional)")
	message.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm("Send to the developers", "Send", "Cancel", container.NewVBox(body, what, message), func(send bool) {
		if !send {
			return
		}
		text := message.Text
		go func() {
			err := a.tel.SendChecks(report, text)
			fyne.Do(func() {
				if err != nil {
					a.showError(err)
					return
				}
				sent()
			})
		}()
	}, a.win)
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
}
