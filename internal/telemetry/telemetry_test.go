package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/pkg/proto"
)

func newClient(t *testing.T, srv *fakesentry.Server, opts Options) *Client {
	t.Helper()
	opts.DSN = srv.DSN()
	if opts.Names == nil {
		opts.Names = func() []string { return []string{"Neon Harbor", "Midnight Ferry"} }
	}
	c := New(opts)
	t.Cleanup(c.Close)
	return c
}

// With anonymous stats off, nothing at all reaches Sentry.
func TestNothingLeavesWithStatsOff(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{TracesSampleRate: 1, ProblemSampleRate: 1})
	c.Breadcrumb("ui", "Record", nil)
	c.Log("launch", map[string]any{"ready": true})
	c.Daily()
	c.Step("launched")
	c.Problem("capture_stalled", "")
	now := time.Now()
	c.Measure("launch", now.Add(-time.Second), now, nil, nil)
	c.Flush(time.Second)
	time.Sleep(100 * time.Millisecond)
	if n := srv.Requests(); n != 0 {
		t.Fatalf("%d requests with stats off", n)
	}
}

// The counts carry no ID and no host name: a daily line, the first one
// ever and of the month, and each step once.
func TestStatsCountWithoutAnID(t *testing.T) {
	srv := fakesentry.New(t)
	day := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	c := newClient(t, srv, Options{Now: func() time.Time { return day }})
	c.SetStats(true)
	c.Daily()
	c.Daily()
	c.Step("launched")
	c.Step("launched")
	c.Flush(5 * time.Second)

	var daily, steps []fakesentry.Log
	for _, l := range srv.Logs() {
		switch l.Body {
		case "daily":
			daily = append(daily, l)
		case "step":
			steps = append(steps, l)
		}
	}
	if len(daily) != 1 || daily[0].Attrs["first_install"] != "true" || daily[0].Attrs["first_month"] != "true" {
		t.Fatalf("daily lines %+v", daily)
	}
	if len(steps) != 1 || steps[0].Attrs["step"] != "launched" {
		t.Fatalf("step lines %+v", steps)
	}
	for k := range daily[0].Attrs {
		if strings.Contains(k, "user") || k == "sentry.server.address" {
			t.Errorf("a count carries %s", k)
		}
	}
	if host, _ := os.Hostname(); host != "" && strings.Contains(srv.Raw(), host) {
		t.Errorf("the host name %q left", host)
	}
}

// The next day counts again, not as a new install; a new month says so.
func TestDailyCountsOncePerDay(t *testing.T) {
	srv := fakesentry.New(t)
	now := time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC)
	prefs := &memPrefs{m: map[string]string{keyLastDay: "2026-10-30"}}
	c := newClient(t, srv, Options{Prefs: prefs, Now: func() time.Time { return now }})
	c.SetStats(true)
	c.Daily()
	now = now.Add(2 * time.Hour)
	c.Daily()
	c.Flush(5 * time.Second)
	var got []string
	for _, l := range srv.Logs() {
		if l.Body == "daily" {
			got = append(got, l.Attrs["first_month"]+"/"+l.Attrs["first_install"])
		}
	}
	if strings.Join(got, " ") != "false/false true/false" {
		t.Fatalf("daily first_month/first_install: %v", got)
	}
}

// A problem is counted each time; its details go once per version, and
// only from the share of installs the dice pick.
func TestProblemsAreCountedAndSampled(t *testing.T) {
	srv := fakesentry.New(t)
	roll := 0.05
	c := newClient(t, srv, Options{Rand: func() float64 { return roll }})
	c.SetStats(true)
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
	if user, _ := ev["user"].(map[string]any); user["ip_address"] != "{{auto}}" || len(user) != 1 {
		t.Errorf("user %v, want only the country lookup", ev["user"])
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
	c.SetStats(true)
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

// A crash left after the first look is offered once, with its trace, the
// user's message as feedback, and nothing that names the user.
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
	preview := c.CrashPreview(crashes[0])
	if strings.Contains(preview, home) || strings.Contains(preview, "Neon Harbor") || !strings.Contains(preview, "~") {
		t.Errorf("preview not scrubbed:\n%s", preview)
	}
	if err := c.SendCrash(crashes[0], "It stopped when "+home+" filled up"); err != nil {
		t.Fatal(err)
	}
	ev := srv.Wait(t, "event", 1)[0].JSON()
	exc := ev["exception"].([]any)[0].(map[string]any)
	if exc["type"] != "panic" || ev["level"] != "fatal" {
		t.Errorf("exception %v, level %v", exc, ev["level"])
	}
	feedback := srv.Wait(t, "feedback", 1)[0].JSON()
	fb := feedback["contexts"].(map[string]any)["feedback"].(map[string]any)
	if fb["associated_event_id"] != ev["event_id"] || !strings.Contains(fb["message"].(string), "~ filled up") {
		t.Errorf("feedback %v for event %v", fb, ev["event_id"])
	}
	if att := srv.Items("attachment"); len(att) != 1 || strings.Contains(string(att[0].Payload), home) {
		t.Errorf("trace attachment %v", att)
	}
	if raw := srv.Raw(); strings.Contains(raw, home) || strings.Contains(raw, "Neon Harbor") {
		t.Error("the report names the user or the track")
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
	if len(att) != 1 || !strings.Contains(string(att[0].Payload), "saved ~") || strings.Contains(string(att[0].Payload), "Neon Harbor") {
		t.Errorf("log attachment %v", att)
	}
	if strings.Contains(srv.Raw(), home) {
		t.Error("the home folder left")
	}
}

func TestScrubber(t *testing.T) {
	s := &Scrubber{home: []string{`C:\Users\Jose`, "C:/Users/Jose"}, names: func() []string { return []string{"Paper Lanterns", "Paper Lanterns (Live)", "AB"} }}
	s.setUser("Jose")
	for in, want := range map[string]string{
		`c:\users\jose\Music\x.flac`:       `~\Music\x.flac`,
		"C:/Users/Jose/Music":              "~/Music",
		"saved Paper Lanterns (Live).flac": "saved <name>.flac",
		"jose's computer, AB side":         "<user>'s computer, AB side",
		"joseph":                           "joseph",
	} {
		if got := s.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
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

func TestEnvironment(t *testing.T) {
	if environment("v2.0.0-alpha.dev") != "development" || environment("v2.0.0-alpha+3f2c1d") != "development" || environment("v2.0.0-alpha.3") != "production" {
		t.Error("environments")
	}
}
