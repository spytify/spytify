package ui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

func TestAFailedCheckReadsAsWhatIsWrong(t *testing.T) {
	for name, want := range map[string]string{
		"ffmpeg found": "ffmpeg not found", "player running": "player not running",
		"output folder writable": "output folder not writable", "capture helper": "capture helper",
	} {
		if got := checkLabel(proto.Check{Name: name}); got != want {
			t.Errorf("checkLabel(%q) = %q, want %q", name, got, want)
		}
		if got := checkLabel(proto.Check{Name: name, OK: true}); got != name {
			t.Errorf("a passing %q reads %q", name, got)
		}
	}
}

// The diagnostics are a dialog behind a button of the Logs screen, which
// turns to a warning while a check fails. Opened from another tab, the
// dialog follows the checks there, and closing it stays on that tab.
func TestTheDiagnosticsAreADialog(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.showTab(tabLogs)
	a.render()
	b := a.screens[tabLogs].(*logsScreen).diagnose
	if b.Importance != widget.LowImportance {
		t.Errorf("every check passes: the button's importance is %v", b.Importance)
	}
	failCheck(a, m)
	if b.Importance != widget.WarningImportance {
		t.Errorf("a check fails: the button's importance is %v", b.Importance)
	}
	snapshot(t, a, "logs-check-fails")

	a.showTab(tabRecord)
	m.SetInfo(info())
	a.render()
	d := diagnostics(t, a)
	if all := texts(d); !contains(all, fmt.Sprintf(lang.ChecksPass, 5)) {
		t.Fatalf("opened from Record: %v", all)
	}
	failCheck(a, m)
	if all := texts(d); !contains(all, fmt.Sprintf(lang.ChecksNeedAttention, 1, 6)) || !contains(all, "ffmpeg not found") {
		t.Errorf("a check fails under the dialog: %v", all)
	}
	tapButton(t, d, lang.Close)
	if a.win.Canvas().Overlays().Top() != nil || a.tabs.SelectedIndex() != tabRecord {
		t.Errorf("closed: a dialog %v, tab %d", a.win.Canvas().Overlays().Top(), a.tabs.SelectedIndex())
	}
}

// A failed check's reason shows whole, on its own lines under the name:
// beside it, it was cut short with "…".
func TestAFailedChecksReasonShowsWholeUnderItsName(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	reason := `mkdir \\nas\shared\Music\Recordings from the radio, every Saturday night since the spring of 2019\Spytify: The system cannot find the path specified.`
	in := info()
	in.Checks = append(in.Checks, proto.Check{Name: proto.CheckOutputWritable, Detail: reason})
	// Few enough rows to fit without scrolling, spytifyd's own check
	// included.
	in.Checks, in.Warnings = in.Checks[1:], nil
	m.SetInfo(in)
	m.SetSettings(defaults())
	a.render()
	d := diagnostics(t, a)
	l := a.screens[tabLogs].(*logsScreen)
	if w := l.diag.Size().Width; w != diagWidth {
		t.Errorf("the dialog is %v wide, want %v", w, diagWidth)
	}

	name := labels(d, "output folder not writable")
	var detail *widget.RichText
	for _, rt := range richTextsOf(d) {
		if rt.String() == reason {
			detail = rt
		}
	}
	if len(name) != 1 || detail == nil {
		t.Fatalf("%d names, the reason found %v: %v", len(name), detail != nil, texts(d))
	}
	at := a.fa.Driver().AbsolutePositionForObject
	if n, r := at(name[0]), at(detail); r.Y < n.Y+name[0].Size().Height || r.X != n.X {
		t.Errorf("the reason at %v, the name at %v and %v tall: not under it", r, n, name[0].Size().Height)
	}
	var drawn []string
	for _, o := range test.WidgetRenderer(detail).Objects() {
		if tx, ok := o.(*canvas.Text); ok && tx.Text != "" {
			drawn = append(drawn, tx.Text)
		}
	}
	if shown := strings.Join(drawn, " "); len(drawn) < 2 || strings.Contains(shown, "…") || !slices.Equal(strings.Fields(shown), strings.Fields(reason)) {
		t.Errorf("the reason shows as %q", drawn)
	}
	// The dialog is as tall as the reason's lines: the checks do not scroll.
	if got, want := l.checksScroll.Size().Height, l.checks.MinSize().Height; got < want {
		t.Errorf("the checks show %v of their %v", got, want)
	}
	snapshot(t, a, "diagnostics-reason-under-the-name")

	// A narrower window narrows the dialog.
	a.win.Resize(fyne.NewSize(700, 640))
	l.fitDiagnostics()
	if w := l.diag.Size().Width; w >= 700 {
		t.Errorf("in a window 700 wide, the dialog is %v", w)
	}
}

// A player that is off is in the app bar and offered to open; the
// diagnostics are not, even while its check still says so.
func TestNoDiagnosticsForAPlayerThatIsOff(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	stale := info()
	stale.Checks[1].OK = false
	m.SetInfo(stale)
	m.SetSettings(defaults())
	a.render()
	all := texts(a.win.Content())
	if !contains(all, fmt.Sprintf(lang.Open, "Player")) || contains(all, lang.OpenDiagnostics) {
		t.Errorf("player off: %v", all)
	}
	// The check can lag behind the player's state, which the app bar
	// shows: it sends nobody to the diagnostics either.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 7}))
	m.SetInfo(stale)
	a.render()
	if all := texts(a.win.Content()); contains(all, lang.OpenDiagnostics) {
		t.Errorf("a stale player check sends to the diagnostics: %v", all)
	}
}

// A start the recorder refuses for want of an output folder runs the
// checks again and shows them; the folder lost during a session or
// another one chosen runs them again too.
func TestTheOutputFolderIsCheckedAgain(t *testing.T) {
	a, m, core := connectedApp(t)
	failing := func() bool { return checkFails(m.Snapshot().Info.Checks, proto.CheckOutputWritable) }
	// The drive unplugged since the checks ran.
	core.setNoOutput(true)
	test.Tap(a.action)
	// The app shows the diagnostics, then runs the checks: once the model
	// has them, the dialog is open and shows the one that fails.
	eventually(t, "the checks did not run again after the refused start", failing)
	a.render()
	if len(buttons(overlay(t, a), lang.ChooseAnotherFolder)) != 1 || !a.action.Disabled() {
		t.Fatalf("after the refused start: diagnostics %v, Record disabled %v", texts(overlay(t, a)), a.action.Disabled())
	}
	tapButton(t, overlay(t, a), lang.Close)
	// Another folder chosen.
	core.setNoOutput(false)
	m.Apply(event(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{"output.dir": "/Volumes/Other"}}))
	eventually(t, "the checks did not run again for another folder", func() bool { return !failing() })
	// The folder lost during a recording.
	core.setNoOutput(true)
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "output_dir", Msg: "the output folder cannot be reached, so the recording stopped"}))
	eventually(t, "the checks did not run again for a folder lost while recording", failing)
}

// Only silence from the player is said in one line on the Record tab, with
// Open diagnostics, where the failing player heard check says why and what
// to do on this system, in the lexicon's words. The checks run again when
// the silence is reported and when sound comes back.
func TestOnlySilenceLeadsToThePlayerHeardCheck(t *testing.T) {
	a, m, core := connectedApp(t)
	feedSession(t, m)
	failing := func() bool { return checkFails(m.Snapshot().Info.Checks, proto.CheckPlayerHeard) }
	why := model.WarningText(proto.ErrorEvent{Code: "capture_silent"}, runtime.GOOS)
	core.setSilent(true)
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_silent", Msg: "only silence from the capture for 10s while the player plays"}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -120, RMSDBFS: -120}))
	eventually(t, "the checks did not run again for the silence", failing)
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	want := fmt.Sprintf(lang.OnlySilenceHeard, "Player")
	if !r.alertRow.Visible() || r.alert.String() != want || !r.alertDiagnose.Visible() {
		t.Fatalf("alert shown %v, text %q, Open diagnostics %v", r.alertRow.Visible(), r.alert.String(), r.alertDiagnose.Visible())
	}
	if contains(texts(a.win.Content()), why) {
		t.Error("the Record tab shows the whole warning, not the line")
	}
	snapshot(t, a, "record-silent")
	light := defaults()
	light["app.theme"] = "light"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "record-silent-light")
	m.SetSettings(defaults())
	a.render()

	test.Tap(r.alertDiagnose)
	all := texts(overlay(t, a))
	if !contains(all, "player not heard") || !contains(all, why) {
		t.Fatalf("diagnostics without the failing player heard check and its reason: %q", all)
	}
	snapshot(t, a, "diagnostics-player-heard")
	tapButton(t, overlay(t, a), lang.Close)

	core.setSilent(false)
	before := core.count(proto.MethodCoreInfo)
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_silent"}))
	eventually(t, "the checks did not run again for the sound", func() bool { return core.count(proto.MethodCoreInfo) > before && !failing() })
	a.render()
	if r.alertRow.Visible() {
		t.Fatalf("alert still shows %q with sound again", r.alert.String())
	}
}
