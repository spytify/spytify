package telemetry

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

// TestLiveSentry sends one of each report to a real Sentry project, to
// check there what the fake server cannot: that Sentry takes them. It
// runs only with SPYTIFY_SENTRY_LIVE_DSN set to a test project's DSN.
func TestLiveSentry(t *testing.T) {
	dsn := os.Getenv("SPYTIFY_SENTRY_LIVE_DSN")
	if dsn == "" {
		t.Skip("SPYTIFY_SENTRY_LIVE_DSN is not set")
	}
	mark := "live check " + time.Now().Format("20060102-150405")
	c := New(Options{DSN: dsn, Names: func() []string { return []string{"Neon Harbor"} }, TracesSampleRate: 1, ProblemSampleRate: 1})
	defer c.Close()
	cr := Crash{Program: "spytifyd", Time: time.Now(), Trace: strings.Replace(trace, "index out of range", mark+": index out of range", 1)}
	if err := c.SendCrash(cr, mark+": the crash's message, about Neon Harbor"); err != nil {
		t.Fatal(err)
	}
	checks := Checks{Failing: []proto.Check{{Name: "ffmpeg found", Detail: mark}}, Log: []string{"saved ~/Music/Neon Harbor - Midnight Ferry.flac"}}
	if err := c.SendChecks(checks, mark+": the checks' message"); err != nil {
		t.Fatal(err)
	}
	c.SetStats(true)
	c.Step("launched")
	c.Problem("live_check", mark)
	now := time.Now()
	c.Measure("launch", now.Add(-time.Second), now, nil, []Span{{Op: "recorder.start", Start: now.Add(-time.Second), End: now}})
	if !c.Flush(10 * time.Second) {
		t.Fatal("the stats did not leave in 10 s")
	}
	t.Logf("In Sentry, look for %q: a crash and a stuck: ffmpeg found issue, each with its user feedback and attachment; "+
		"a blocked: live_check issue; a launch transaction; and the logs daily, step and problem. "+
		"No server name, no IP address, and <name> for Neon Harbor.", mark)
}
