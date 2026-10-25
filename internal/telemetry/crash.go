package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/spytify/spytify/pkg/proto"
)

// Crash is the trace a program left when it stopped on a fatal error.
type Crash struct {
	// Program is spytifyd or spytify-ui.
	Program string
	Trace   string
	Time    time.Time
	key     string
}

// CrashLogs are the files crash.Keep writes for each program, the
// current run's then the previous one's.
func CrashLogs(stateDir string) map[string][]string {
	logs := map[string][]string{}
	for _, p := range []string{"spytifyd", "spytify-ui"} {
		path := filepath.Join(stateDir, p+".crash.log")
		logs[p] = []string{path, path + ".1"}
	}
	return logs
}

const (
	keyCrashSince   = "telemetry.crash.since"
	keyCrashHandled = "telemetry.crash.handled"
)

// FindCrashes returns the crashes not sent nor dismissed yet. The first
// call only notes the time: a crash from before it, by a version without
// the bug tracker, is not sent now.
func FindCrashes(stateDir string, prefs Prefs, now time.Time) (found []Crash) {
	defer quietly()
	since, err := time.Parse(time.RFC3339, prefs.String(keyCrashSince))
	if err != nil {
		prefs.SetString(keyCrashSince, now.UTC().Format(time.RFC3339))
		return nil
	}
	handled := strings.Fields(prefs.String(keyCrashHandled))
	var out []Crash
	for _, p := range []string{"spytifyd", "spytify-ui"} {
		for _, path := range CrashLogs(stateDir)[p] {
			st, err := os.Stat(path)
			if err != nil || st.ModTime().Before(since) {
				continue
			}
			b, err := os.ReadFile(path)
			trace := strings.TrimSpace(string(b))
			if err != nil || trace == "" {
				continue
			}
			sum := sha256.Sum256([]byte(trace))
			key := hex.EncodeToString(sum[:8])
			if contains(handled, key) || containsKey(out, key) {
				continue
			}
			out = append(out, Crash{Program: p, Trace: trace, Time: st.ModTime(), key: key})
		}
	}
	return out
}

// MarkHandled keeps cr from being sent again.
func MarkHandled(prefs Prefs, cr Crash) {
	defer quietly()
	handled := append(strings.Fields(prefs.String(keyCrashHandled)), cr.key)
	if len(handled) > 20 {
		handled = handled[len(handled)-20:]
	}
	prefs.SetString(keyCrashHandled, strings.Join(handled, " "))
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsKey(crashes []Crash, key string) bool {
	for _, c := range crashes {
		if c.key == key {
			return true
		}
	}
	return false
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// CrashReport is a crash as the user can report it by hand: its error, and
// the details with the trace. The home folder, the user name and the
// names of the tracks the app knows are replaced, as in what Sentry gets.
func (c *Client) CrashReport(cr Crash) (title, details string) {
	var scrub *Scrubber
	if c != nil {
		scrub = c.scrub
	}
	summary, _ := parseTrace(cr.Trace)
	details = fmt.Sprintf("Program: %s\nVersion: %s\nSystem: %s/%s\n\n%s",
		cr.Program, proto.Version, runtime.GOOS, runtime.GOARCH, strings.TrimSpace(cr.Trace))
	return scrub.String(summary), scrub.String(details)
}

// SendCrash sends a crash with its trace.
func (c *Client) SendCrash(cr Crash) (err error) {
	defer func() {
		if recover() != nil {
			err = errNotSent
		}
	}()
	hub := c.hub()
	if hub == nil {
		return errOff
	}
	ev := crashEvent(cr)
	ev.Attachments = []*sentry.Attachment{{Filename: cr.Program + "-trace.txt", ContentType: "text/plain", Payload: []byte(c.scrub.String(cr.Trace))}}
	if id := hub.CaptureEvent(ev); id == nil || !hub.Flush(10*time.Second) {
		return errNotSent
	}
	return nil
}

// crashEvent turns a Go crash trace into a Sentry exception, so crashes in
// the same place group into one issue.
func crashEvent(cr Crash) *sentry.Event {
	summary, frames := parseTrace(cr.Trace)
	typ, value := "panic", summary
	for _, p := range []string{"panic: ", "fatal error: "} {
		if strings.HasPrefix(summary, p) {
			typ, value = strings.TrimSuffix(p, ": "), strings.TrimPrefix(summary, p)
		}
	}
	ev := sentry.NewEvent()
	ev.Level = sentry.LevelFatal
	ev.Platform = "go"
	ev.Timestamp = cr.Time
	ev.Tags["program"] = cr.Program
	ev.Exception = []sentry.Exception{{
		Type:       typ,
		Value:      value,
		Stacktrace: &sentry.Stacktrace{Frames: frames},
		Mechanism:  &sentry.Mechanism{Type: "go.panic", Handled: new(bool)},
	}}
	return ev
}

// parseTrace reads the first line of a Go crash trace and the frames of
// the goroutine that crashed, oldest first as Sentry wants them.
func parseTrace(trace string) (summary string, frames []sentry.Frame) {
	lines := strings.Split(strings.ReplaceAll(trace, "\r\n", "\n"), "\n")
	summary = strings.TrimSpace(lines[0])
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "goroutine ") && strings.HasSuffix(strings.TrimSpace(l), ":") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return summary, nil
	}
	for i := start; i+1 < len(lines); i += 2 {
		fn, loc := strings.TrimSpace(lines[i]), strings.TrimSpace(lines[i+1])
		if fn == "" || !strings.HasPrefix(lines[i+1], "\t") {
			break
		}
		fn = strings.TrimPrefix(fn, "created by ")
		if j := strings.Index(fn, " in goroutine "); j >= 0 {
			fn = fn[:j]
		} else if j := strings.LastIndex(fn, "("); j > 0 && strings.HasSuffix(fn, ")") {
			fn = fn[:j]
		}
		module, function := splitFunc(fn)
		file, line := loc, 0
		if j := strings.LastIndex(loc, " +0x"); j >= 0 {
			file = loc[:j]
		}
		if j := strings.LastIndex(file, ":"); j >= 0 {
			line, _ = strconv.Atoi(file[j+1:])
			file = file[:j]
		}
		frames = append(frames, sentry.Frame{
			Function: function,
			Module:   module,
			Filename: file,
			AbsPath:  file,
			Lineno:   line,
			InApp:    module == "main" || strings.HasPrefix(module, "github.com/spytify/spytify"),
		})
	}
	for i, j := 0, len(frames)-1; i < j; i, j = i+1, j-1 {
		frames[i], frames[j] = frames[j], frames[i]
	}
	return summary, frames
}

// splitFunc splits github.com/x/y/pkg.(*T).m into its package and
// function.
func splitFunc(fn string) (module, function string) {
	slash := strings.LastIndex(fn, "/")
	dot := strings.Index(fn[slash+1:], ".")
	if dot < 0 {
		return "", fn
	}
	return fn[:slash+1+dot], fn[slash+1+dot+1:]
}
