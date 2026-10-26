package telemetry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/denisbrodbeck/machineid"
	"github.com/getsentry/sentry-go"

	"github.com/spytify/spytify/internal/crash"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/pkg/proto"
)

func newClient(t *testing.T, srv *fakesentry.Server, opts Options) *Client {
	t.Helper()
	opts.DSN = srv.DSN()
	c := New(opts)
	t.Cleanup(c.Close)
	return c
}

// With the bug tracker off, nothing at all reaches Sentry, and a report
// asked for says so.
func TestNothingLeavesWhenOff(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{TracesSampleRate: 1, ProblemSampleRate: 1})
	c.Breadcrumb("ui", "Record", nil)
	c.Log("launch", map[string]any{"ready": true})
	c.Count("track.saved", 1, map[string]string{"format": "flac"})
	c.Distribution("cut.offset", -480, sentry.UnitMillisecond, nil)
	c.Step("launched")
	c.Problem("capture_stalled", "")
	now := time.Now()
	c.Measure("launch", now.Add(-time.Second), now, nil, nil)
	if err := c.SendCrash(Crash{Program: "spytifyd", Trace: trace}); err != errOff {
		t.Errorf("SendCrash with the bug tracker off: %v", err)
	}
	if err := c.SendChecks(Checks{Failing: []proto.Check{{Name: "ffmpeg found"}}}, "hello"); err != errOff {
		t.Errorf("SendChecks with the bug tracker off: %v", err)
	}
	c.Flush(time.Second)
	time.Sleep(100 * time.Millisecond)
	if n := srv.Requests(); n != 0 {
		t.Fatalf("%d requests with the bug tracker off", n)
	}
}

// A Sentry out of reach changes nothing for the app: every call returns,
// a comment that did not leave says so, and closing gives up after 2 s.
func TestASentryOutOfReachIsIgnored(t *testing.T) {
	c := New(Options{DSN: "http://public@127.0.0.1:1/1", TracesSampleRate: 1, ProblemSampleRate: 1})
	c.SetEnabled(true)
	c.Breadcrumb("ui", "record", nil)
	c.Log("launch", map[string]any{"ready": true})
	c.Count("track.saved", 1, nil)
	c.Distribution("cut.offset", -480, sentry.UnitMillisecond, nil)
	c.Step("launched")
	c.Problem("capture_stalled", "")
	now := time.Now()
	c.Measure("launch", now.Add(-time.Second), now, nil, nil)
	_ = c.SendCrash(Crash{Program: "spytifyd", Trace: trace})
	if err := c.SendChecks(Checks{Failing: []proto.Check{{Name: "ffmpeg found"}}}, "a comment"); err == nil {
		t.Error("a comment that did not leave reads as sent")
	}
	start := time.Now()
	c.Close()
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("closing took %v", d)
	}
}

// A scrubbing that fails drops what it was scrubbing, never the app:
// nothing leaves unscrubbed.
func TestAFailingScrubDropsTheReport(t *testing.T) {
	c := New(Options{DSN: "http://public@127.0.0.1:1/1"})
	if ev := c.beforeSend(&sentry.Event{Message: "x", Spans: []*sentry.Span{nil}}, nil); ev != nil {
		t.Error("an event the scrubbing failed on would leave")
	}
	if b := c.beforeBreadcrumb(nil, nil); b != nil {
		t.Error("a breadcrumb the scrubbing failed on would leave")
	}
	if l := c.beforeSendLog(nil); l != nil {
		t.Error("a log the scrubbing failed on would leave")
	}
	if m := c.beforeSendMetric(nil); m != nil {
		t.Error("a metric the scrubbing failed on would leave")
	}
}

// Every launch leaves, whatever the share of timings, naming the computer
// only by its hash, with its system; no host name. Each step leaves once.
func TestEveryLaunchCountsTheComputer(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{TracesSampleRate: 1e-9})
	c.SetEnabled(true)
	now := time.Now()
	c.Measure("launch", now.Add(-time.Second), now, nil, nil)
	c.Measure("track.finish", now.Add(-time.Second), now, nil, nil)
	c.Step("launched")
	c.Step("launched")
	c.Flush(5 * time.Second)

	txs := srv.Wait(t, "transaction", 1)
	time.Sleep(100 * time.Millisecond)
	if txs = srv.Items("transaction"); len(txs) != 1 || txs[0].JSON()["transaction"] != "launch" {
		t.Fatalf("%d transactions, want the launch alone", len(txs))
	}
	tx := txs[0].JSON()
	user, _ := tx["user"].(map[string]any)
	if id := computerID(); id != "" {
		raw, _ := machineid.ID()
		if user["id"] != id || len(id) != 64 || strings.Contains(srv.Raw(), raw) {
			t.Errorf("user %v, want the hash %s and never the machine ID", user, id)
		}
	}
	if user["ip_address"] != "{{auto}}" {
		t.Errorf("user %v, want the country lookup", user)
	}
	if system, _ := tx["contexts"].(map[string]any)["os"].(map[string]any); system["name"] != runtime.GOOS {
		t.Errorf("os context %v", system)
	}
	if _, ok := tx["server_name"]; ok {
		t.Error("the launch has a server name")
	}
	if host, _ := os.Hostname(); host != "" && strings.Contains(srv.Raw(), host) {
		t.Errorf("the host name %q left", host)
	}
	steps := 0
	for _, l := range srv.Logs() {
		if l.Body == "step" {
			steps++
		}
	}
	if steps != 1 {
		t.Errorf("%d step lines, want 1", steps)
	}
}

// The recording counts leave as metrics with the system and the
// computer's hash, and no host name.
func TestRecordingCountsLeave(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	c.Count("track.skipped", 1, map[string]string{"reason": "ad"})
	c.Distribution("cut.offset", -480, sentry.UnitMillisecond, map[string]string{"kind": "boundary", "by": "silence"})
	c.Flush(5 * time.Second)
	srv.Wait(t, "trace_metric", 1)
	got := map[string]fakesentry.Metric{}
	for _, m := range srv.Metrics() {
		got[m.Name] = m
	}
	skipped, cut := got["track.skipped"], got["cut.offset"]
	if skipped.Type != "counter" || skipped.Value != 1 || skipped.Attrs["reason"] != "ad" {
		t.Errorf("track.skipped %+v", skipped)
	}
	if cut.Type != "distribution" || cut.Value != -480 || cut.Unit != "millisecond" || cut.Attrs["kind"] != "boundary" || cut.Attrs["by"] != "silence" {
		t.Errorf("cut.offset %+v", cut)
	}
	for _, m := range []fakesentry.Metric{skipped, cut} {
		if m.Attrs["os"] != runtime.GOOS {
			t.Errorf("%s without the system: %v", m.Name, m.Attrs)
		}
		if id := computerID(); id != "" && m.Attrs["user.id"] != id {
			t.Errorf("%s names the computer %q, want its hash", m.Name, m.Attrs["user.id"])
		}
		if _, ok := m.Attrs["sentry.server.address"]; ok {
			t.Errorf("%s has a server address", m.Name)
		}
	}
	if host, _ := os.Hostname(); host != "" && strings.Contains(srv.Raw(), host) {
		t.Errorf("the host name %q left", host)
	}
}

// A problem is counted each time; its details go once per version, and
// only from the share of installs the dice pick.
func TestProblemsAreCountedAndSampled(t *testing.T) {
	srv := fakesentry.New(t)
	roll := 0.05
	c := newClient(t, srv, Options{Rand: func() float64 { return roll }})
	c.SetEnabled(true)
	c.Breadcrumb("session", "recording", map[string]any{"format": "flac"})
	c.Problem("capture_stalled", "No audio from the player for 10 s")
	c.Problem("capture_stalled", "again")
	roll = 0.5
	c.Problem("incomplete_tracks", "")
	c.Flush(5 * time.Second)

	events := srv.Wait(t, "event", 1)
	time.Sleep(100 * time.Millisecond)
	if events = srv.Items("event"); len(events) != 1 {
		t.Fatalf("%d problem events, want 1", len(events))
	}
	ev := events[0].JSON()
	if ev["message"] != "blocked: capture_stalled" || ev["level"] != "warning" {
		t.Fatalf("event %v", ev)
	}
	if crumbs, _ := ev["breadcrumbs"].([]any); len(crumbs) != 1 || crumbs[0].(map[string]any)["message"] != "recording" {
		t.Errorf("breadcrumbs %v, want the recording one", ev["breadcrumbs"])
	}
	if user, _ := ev["user"].(map[string]any); user["ip_address"] != "{{auto}}" || user["id"] != computerUser().ID {
		t.Errorf("user %v, want the computer's hash and the country lookup", ev["user"])
	}
	if _, ok := ev["server_name"]; ok {
		t.Error("the event has a server name")
	}
	counted := 0
	for _, l := range srv.Logs() {
		if l.Body == "problem" {
			counted++
		}
	}
	if counted != 3 {
		t.Errorf("%d problems counted, want 3", counted)
	}
}

// A timing leaves as a transaction with its steps.
func TestMeasureSendsATransaction(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{TracesSampleRate: 1})
	c.SetEnabled(true)
	start := time.Now().Add(-3 * time.Second)
	c.Measure("track.finish", start, start.Add(2*time.Second), map[string]string{"format": "mp3"}, []Span{
		{Op: "encode", Start: start, End: start.Add(1500 * time.Millisecond)},
	})
	c.Flush(5 * time.Second)
	tx := srv.Wait(t, "transaction", 1)[0].JSON()
	spans, _ := tx["spans"].([]any)
	if tx["transaction"] != "track.finish" || len(spans) != 1 {
		t.Fatalf("transaction %v", tx)
	}
	if op := spans[0].(map[string]any)["op"]; op != "encode" {
		t.Errorf("span op %v", op)
	}
}

const trace = `panic: runtime error: index out of range [3] with length 3

goroutine 21 [running]:
github.com/spytify/spytify/internal/core.(*Session).cutAt(0x1400019e000, 0x2ee0)
	github.com/spytify/spytify/internal/core/session.go:742 +0x6a8
github.com/spytify/spytify/internal/core.(*Session).run(0x1400019e000)
	github.com/spytify/spytify/internal/core/session.go:300 +0x10
created by github.com/spytify/spytify/internal/core.(*Core).StartSession in goroutine 7
	github.com/spytify/spytify/internal/core/core.go:200 +0x88

goroutine 1 [select]:
main.main()
	github.com/spytify/spytify/cmd/spytifyd/main.go:40 +0x10
`

func TestParseTrace(t *testing.T) {
	summary, frames := parseTrace(trace)
	if summary != "panic: runtime error: index out of range [3] with length 3" {
		t.Fatalf("summary %q", summary)
	}
	if len(frames) != 3 {
		t.Fatalf("%d frames: %+v", len(frames), frames)
	}
	last := frames[2]
	if last.Module != "github.com/spytify/spytify/internal/core" || last.Function != "(*Session).cutAt" || last.Lineno != 742 || !last.InApp {
		t.Errorf("crashing frame %+v", last)
	}
	if frames[0].Function != "(*Core).StartSession" || frames[0].Filename != "github.com/spytify/spytify/internal/core/core.go" {
		t.Errorf("oldest frame %+v", frames[0])
	}
}

// crashTo has the child of TestParseARealCrash crash into this file.
const crashTo = "TELEMETRY_TEST_CRASH_TO"

// A trace the Go runtime really wrote, where spytifyd keeps it, reads as
// an exception whose last frame is where the panic happened.
func TestParseARealCrash(t *testing.T) {
	if path := os.Getenv(crashTo); path != "" {
		crash.Keep(path)
		go crashHere()
		select {}
	}
	path := filepath.Join(t.TempDir(), "spytifyd.crash.log")
	cmd := exec.Command(os.Args[0], "-test.run=^TestParseARealCrash$")
	cmd.Env = append(os.Environ(), crashTo+"="+path)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the child did not crash:\n%s", out)
	}
	trace, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ex := crashEvent(Crash{Program: "spytifyd", Trace: string(trace)}).Exception[0]
	if ex.Type != "panic" || ex.Value != "boom in a real crash" {
		t.Errorf("exception %s %q from:\n%s", ex.Type, ex.Value, trace)
	}
	frames := ex.Stacktrace.Frames
	if len(frames) == 0 {
		t.Fatalf("no frames in:\n%s", trace)
	}
	last := frames[len(frames)-1]
	if last.Function != "crashHere" || !last.InApp || filepath.Base(last.Filename) != "telemetry_test.go" || last.Lineno == 0 {
		t.Errorf("crashing frame %+v from:\n%s", last, trace)
	}
}

func crashHere() { panic("boom in a real crash") }

// A crash left after the first look leaves once, with its trace, the
// home folder as ~ and the track's name as it is.
func TestCrashReport(t *testing.T) {
	srv := fakesentry.New(t)
	dir := t.TempDir()
	prefs := &memPrefs{m: map[string]string{}}
	if got := FindCrashes(dir, prefs, time.Now().Add(-time.Minute)); got != nil {
		t.Fatalf("first look found %v", got)
	}
	home, _ := os.UserHomeDir()
	text := strings.Replace(trace, "index out of range", "open "+filepath.Join(home, "Music", "Neon Harbor - Midnight Ferry.flac")+": index out of range", 1)
	if err := os.WriteFile(filepath.Join(dir, "spytifyd.crash.log.1"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	crashes := FindCrashes(dir, prefs, time.Now())
	if len(crashes) != 1 || crashes[0].Program != "spytifyd" {
		t.Fatalf("crashes %+v", crashes)
	}
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	if err := c.SendCrash(crashes[0]); err != nil {
		t.Fatal(err)
	}
	ev := srv.Wait(t, "event", 1)[0].JSON()
	exc := ev["exception"].([]any)[0].(map[string]any)
	if exc["type"] != "panic" || ev["level"] != "fatal" {
		t.Errorf("exception %v, level %v", exc, ev["level"])
	}
	if att := srv.Items("attachment"); len(att) != 1 || strings.Contains(string(att[0].Payload), home) {
		t.Errorf("trace attachment %v", att)
	}
	if raw := srv.Raw(); strings.Contains(raw, home) || !strings.Contains(raw, "Neon Harbor") {
		t.Error("the report names the home folder, or not the track")
	}
	MarkHandled(prefs, crashes[0])
	if again := FindCrashes(dir, prefs, time.Now()); len(again) != 0 {
		t.Errorf("a handled crash came back: %v", again)
	}
}

// The diagnostics' button sends the failing checks, the log's end and the
// message.
func TestSendChecks(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	home, _ := os.UserHomeDir()
	r := Checks{
		Failing: []proto.Check{{Name: "ffmpeg found", Detail: "not in " + filepath.Join(home, "bin")}},
		Log:     []string{"saved " + filepath.Join(home, "Music", "Neon Harbor - Midnight Ferry.flac")},
	}
	if err := c.SendChecks(r, "nothing records"); err != nil {
		t.Fatal(err)
	}
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ev["message"] != "stuck: ffmpeg found" {
		t.Errorf("message %v", ev["message"])
	}
	srv.Wait(t, "feedback", 1)
	att := srv.Items("attachment")
	if len(att) != 1 || !strings.Contains(string(att[0].Payload), "saved ~/Music/Neon Harbor - Midnight Ferry.flac") && !strings.Contains(string(att[0].Payload), `saved ~\Music\Neon Harbor - Midnight Ferry.flac`) {
		t.Errorf("log attachment %v", att)
	}
	if strings.Contains(srv.Raw(), home) {
		t.Error("the home folder left")
	}
}

// A message is cut to the 4096 characters Sentry takes, not bytes, and a
// blank one is not sent; the fake server holds the rest of the feedback
// to Sentry's spec.
func TestFeedbackFollowsTheSpec(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	r := Checks{Failing: []proto.Check{{Name: "ffmpeg found", Detail: "not on PATH"}}}
	if err := c.SendChecks(r, " \n "); err != nil {
		t.Fatal(err)
	}
	if err := c.SendChecks(r, strings.Repeat("é", maxFeedback+10)); err != nil {
		t.Fatal(err)
	}
	feedback := srv.Items("feedback")
	if len(feedback) != 1 {
		t.Fatalf("%d feedbacks for one message", len(feedback))
	}
	msg := feedback[0].JSON()["contexts"].(map[string]any)["feedback"].(map[string]any)["message"].(string)
	if n := utf8.RuneCountInString(msg); n != maxFeedback || !utf8.ValidString(msg) {
		t.Errorf("message of %d characters, valid UTF-8 %v", n, utf8.ValidString(msg))
	}
}

func TestScrubber(t *testing.T) {
	s := &Scrubber{home: []string{`C:\Users\User`, "C:/Users/User"}, output: []string{`C:\Users\User\Music\Mixtapes`, `L:\Mixtapes`}}
	s.setUser("User")
	for in, want := range map[string]string{
		`c:\users\user\Music\x.flac`:          `~\Music\x.flac`,
		`c:\users\user\Music\Mixtapes\x.flac`: `<output>\x.flac`,
		`open L:\Mixtapes\.spytify\a.wav: x`:  `open <output>\.spytify\a.wav: x`,
		"C:/Users/User/Music":                 "~/Music",
		"saved Paper Lanterns (Live).flac":    "saved Paper Lanterns (Live).flac",
		"user's computer, AB side":            "<user>'s computer, AB side",
		"username":                            "username",
	} {
		if got := s.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrubberOutput(t *testing.T) {
	s := &Scrubber{}
	dir := filepath.Join(t.TempDir(), "Mixtapes")
	s.SetOutput(dir + string(filepath.Separator))
	if got, want := s.String(filepath.Join(dir, "a.flac")), filepath.Join("<output>", "a.flac"); got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
	// A drive's root would take every path with it.
	s.SetOutput(string(filepath.Separator))
	if got := s.String(dir); got != dir {
		t.Errorf("String = %q with the root as the output folder", got)
	}
}

func TestParseLocale(t *testing.T) {
	for in, want := range map[string]string{
		"fr_CA.UTF-8":     "CA",
		"en-US":           "US",
		"en_US@rg=cazzzz": "CA",
		"C":               "",
		"C.UTF-8":         "",
		"POSIX":           "",
		"CA\n":            "CA",
		"de_DE@euro":      "DE",
	} {
		if got := parseLocale(in); got != want {
			t.Errorf("parseLocale(%q) = %q, want %q", in, got, want)
		}
	}
	env := map[string]string{"LANG": "fr_CA.UTF-8", "LC_ALL": "C"}
	if got := envRegion(func(k string) string { return env[k] }); got != "CA" {
		t.Errorf("envRegion = %q", got)
	}
}

// The system's own region setting reads as a country; Windows always has
// one.
func TestOSRegion(t *testing.T) {
	raw := osLocale()
	got := parseLocale(raw)
	t.Logf("system region %q, country %q", raw, got)
	if runtime.GOOS == "windows" && got == "" {
		t.Errorf("no country in Windows's region setting %q", raw)
	}
}

func TestEnvironment(t *testing.T) {
	if environment("v2.0.0-alpha.dev") != "development" || environment("v2.0.0-alpha+3f2c1d") != "development" || environment("v2.0.0-alpha.3") != "production" {
		t.Error("environments")
	}
}
