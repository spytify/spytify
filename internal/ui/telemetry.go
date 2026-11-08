package ui

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/ui/lang"
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
	attrs := map[string]any{
		"ready":           true,
		"ms":              now.Sub(a.launch.start).Milliseconds(),
		"auto_started":    !a.launch.started.IsZero(),
		"failing":         failingNames(s.Info.Checks),
		"advice":          adviceNames(s.Info.Checks),
		"software_opengl": a.opts.SoftwareOpenGL,
	}
	// Where the output folder is, say, written to or not, for every
	// launch: what share of them a fix would reach.
	for _, c := range s.Info.Checks {
		for k, v := range c.Facts {
			attrs[k] = v
		}
	}
	a.tel.Log("launch", attrs)
	var spans []telemetry.Span
	if !a.launch.started.IsZero() && a.launch.connected.After(a.launch.started) {
		spans = append(spans, telemetry.Span{Op: "recorder.start", Start: a.launch.started, End: a.launch.connected})
	}
	if !a.launch.connected.IsZero() {
		spans = append(spans, telemetry.Span{Op: "recorder.info", Start: a.launch.connected, End: now})
	}
	a.tel.Measure("launch", a.launch.start, now, nil, spans)
}

// launchFailed reports a recorder that did not start, with what its start
// found and saw.
func (a *App) launchFailed(err error) {
	kind := startErrorKind(err)
	a.tel.Log("launch", map[string]any{"ready": false, "start_error": kind, "software_opengl": a.opts.SoftwareOpenGL})
	start := a.link.EndStart()
	waited := time.Since(start.At)
	// Not on the toolkit's goroutine, which this is: the report waits for
	// the window's picture, taken there.
	a.reports.Go(func() { a.tel.ProblemWith("recorder_start", kind, a.startFacts(start, waited)) })
}

// startFacts are what a recorder that did not start leaves beside its
// kind: where spytifyd was, whether it ran, how it stopped and what it
// said, with no path. They are nil when no start was made.
func (a *App) startFacts(s link.Start, waited time.Duration) map[string]string {
	if s.At.IsZero() {
		return nil
	}
	facts := map[string]string{
		"start.found":         s.Found,
		"start.in_temp":       strconv.FormatBool(s.InTemp),
		"start.started":       strconv.FormatBool(s.Started),
		"start.exited":        strconv.FormatBool(s.Exited),
		"start.waited_ms":     strconv.FormatInt(waited.Milliseconds(), 10),
		"start.socket_exists": strconv.FormatBool(s.SocketExists),
	}
	if s.Found != "none" {
		facts["start.exe_bytes"] = strconv.FormatInt(s.ExeBytes, 10)
	}
	if s.Err != "" {
		facts["start.error"] = s.Err
	}
	if s.Errno != 0 {
		facts["start.errno"] = strconv.FormatUint(uint64(s.Errno), 10)
	}
	if s.Exited {
		facts["start.exit_code"] = strconv.Itoa(s.ExitCode)
	}
	if s.Started {
		facts["start.stderr"] = lastLines(s.Stderr, 5, 1000)
	}
	if s.DialErr != "" {
		facts["start.dial_error"] = s.DialErr
	}
	if tail, ok := logSince(filepath.Join(a.opts.StateDir, "spytifyd.log"), s.At); ok {
		facts["start.log_tail"] = lastLines(tail, 20, 2000)
	}
	return facts
}

// logSince is what spytifyd's log holds from the line written at since on,
// read from its last 64 KiB: the lines of an earlier run, which can name
// tracks, stay out. ok is false with no log.
func logSince(path string, since time.Time) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	const most = 64 << 10
	var from int64
	if fi, err := f.Stat(); err == nil && fi.Size() > most {
		from = fi.Size() - most
	}
	b, err := io.ReadAll(io.NewSectionReader(f, from, most))
	if err != nil {
		return "", false
	}
	lines := strings.Split(string(b), "\n")
	if from > 0 {
		// The first line read is cut.
		lines = lines[1:]
	}
	for i, l := range lines {
		ts, _, _ := strings.Cut(l, " ")
		if at, err := time.Parse(time.RFC3339Nano, ts); err == nil && !at.Before(since) {
			return strings.Join(lines[i:], "\n"), true
		}
	}
	return "", true
}

// lastLines are the last n lines of s, their end kept to max bytes.
func lastLines(s string, n, max int) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	s = strings.Join(lines, "\n")
	for cut := len(s) - max; cut > 0 && cut < len(s); cut++ {
		if utf8.RuneStart(s[cut]) {
			return s[cut:]
		}
	}
	return s
}

func startErrorKind(err error) string {
	switch {
	case errors.Is(err, link.ErrInArchive):
		return "in archive"
	case errors.Is(err, errNoAnswer):
		return "no answer"
	case errors.Is(err, link.ErrNoDaemon) || errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
		return "not found"
	}
	return "failed"
}

// failingNames are the checks that fail, but for the player not running,
// which the app bar shows and is no problem.
func failingNames(checks []proto.Check) []string {
	var names []string
	for _, c := range checks {
		if needsAttention(c) {
			names = append(names, c.Name)
		}
	}
	return names
}

// adviceNames names the advice checks that fail: recording works, at a
// lower quality.
func adviceNames(checks []proto.Check) []string {
	var names []string
	for _, c := range checks {
		if !c.OK && c.Advice {
			names = append(names, c.Name)
		}
	}
	return names
}

// problemCodes are the recorder's errors worth a problem report: not a
// full disk, which is the user's to fix.
var problemCodes = map[proto.ErrorCode]bool{
	proto.ErrorCaptureStalled: true, proto.ErrorRingOverflow: true, proto.ErrorWrite: true, proto.ErrorTempFile: true,
	proto.ErrorOutputDir: true, proto.ErrorRead: true, proto.ErrorEncode: true, proto.ErrorRename: true,
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
		a.tel.Breadcrumb("error", string(e.Code), nil)
		if problemCodes[e.Code] {
			a.tel.ProblemWith(string(e.Code), e.Msg, e.Facts)
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

// screenshotWidth is the width of the window's picture in Send to the
// developers, which fits the dialog.
const screenshotWidth = 480

// sendChecks shows what the diagnostics' report holds, and sends it on
// Send.
func (a *App) sendChecks(s model.State, sent func(ok bool)) {
	var failing []proto.Check
	for _, c := range a.diagChecks(s) {
		if needsAttention(c) {
			failing = append(failing, c)
		}
	}
	var log []string
	lines := s.Logs
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
	}
	for _, l := range lines {
		log = append(log, l.TS+" "+string(l.Level)+" "+l.Msg)
	}
	// The window as it shows under this dialog, which is not open yet.
	report := telemetry.Checks{Failing: failing, Log: log, Screenshot: a.picture()}
	without := lang.SendChecksText
	body := widget.NewLabel(without)
	body.Wrapping = fyne.TextWrapWord
	preview := container.NewVBox()
	// keep, ticked, sends the picture; unticked, the dialog and the
	// report leave it out.
	var keep *check
	if shot := report.Screenshot; shot != nil {
		with := lang.SendChecksPicture
		img := canvas.NewImageFromImage(shot)
		img.FillMode = canvas.ImageFillContain
		b := shot.Bounds()
		img.SetMinSize(fyne.NewSize(screenshotWidth, screenshotWidth*float32(b.Dy())/float32(b.Dx())))
		// At the left, as the log's long lines widen what scrolls.
		pic := container.NewHBox(img)
		preview.Add(pic)
		keep = newCheck(lang.SendPicture)
		keep.OnChanged = func(on bool) {
			if on {
				body.SetText(with)
				pic.Show()
			} else {
				body.SetText(without)
				pic.Hide()
			}
		}
		keep.SetChecked(true)
	}
	preview.Add(widget.NewLabelWithStyle(a.tel.ChecksPreview(report), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}))
	scroll := container.NewScroll(preview)
	scroll.SetMinSize(fyne.NewSize(0, 160))
	content := container.NewVBox(body, newFold(lang.WhatWillBeSent, scroll))
	if keep != nil {
		content.Add(keep)
	}
	message := newCommentEntry()
	message.SetPlaceHolder(lang.CommentHint)
	content.Add(message)
	showDialog(a.win, lang.SendToDevelopers, content, lang.Cancel, lang.Send, fyne.NewSize(560, 420), func(send bool) {
		if !send {
			return
		}
		if keep != nil && !keep.Checked {
			report.Screenshot = nil
		}
		text := message.Text
		// A report that does not leave is no error to show: the button
		// says so and the app goes on.
		a.reports.Go(func() {
			err := a.tel.SendChecks(report, text)
			fyne.Do(func() { sent(err == nil) })
		})
	})
}
