package ui

import (
	"errors"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// launchTrace times the way from launch to a recorder that answers.
type launchTrace struct {
	start, started, connected time.Time
	done                      bool
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

// feedTracker turns the recorder's events into breadcrumbs, steps and problem
// reports. It runs on the link's goroutine.
func (a *App) feedTracker(ev proto.Event) {
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

// noteTab notes the tab shown.
func (a *App) noteTab(i int) {
	if i < 0 || i >= len(a.tabs.Items) {
		return
	}
	a.tel.Breadcrumb("ui", "tab "+a.tabs.Items[i].Text, nil)
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
	what := newFold("What will be sent", scroll)
	message := newCommentEntry()
	message.SetPlaceHolder("Comment (optional)")
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
	focusFirst(a.win)
}
