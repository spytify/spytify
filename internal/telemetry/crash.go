package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/spytify/spytify/internal/crash"
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
			out = append(out, Crash{Program: p, Trace: crash.WithLost(path, trace), Time: st.ModTime(), key: key})
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

// CrashReport is a crash as the user can report it by hand: its error, and
// the details with the trace. The home folder and the user name are
// replaced, as in what Sentry gets.
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
	summary, stacks := parseTrace(cr.Trace)
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
	// The lines printed along with the message, such as an errno or where
	// a signal hit, follow it.
	for _, l := range crash.Header(cr.Trace) {
		if l != summary {
			value += "\n" + l
		}
	}
	ex := sentry.Exception{
		Type:       typ,
		Value:      value,
		Stacktrace: &sentry.Stacktrace{},
		Mechanism:  &sentry.Mechanism{Type: "go.panic", Handled: new(bool)},
	}
	// Every stack shows in the event, as a thread to pick, the one that
	// crashed first and picked.
	for i, s := range stacks {
		if i == 0 {
			ex.Stacktrace.Frames = s.frames
			ex.ThreadID, _ = strconv.ParseUint(s.id, 10, 64)
		}
		ev.Threads = append(ev.Threads, sentry.Thread{ID: s.id, Name: s.name, Stacktrace: &sentry.Stacktrace{Frames: s.frames}, Crashed: i == 0, Current: i == 0})
	}
	ev.Exception = []sentry.Exception{ex}
	return ev
}

// parseTrace reads the headline of a Go crash trace (crash.Headline) and
// its stacks, the one that crashed first.
func parseTrace(trace string) (summary string, stacks []stack) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(trace), "\r\n", "\n"), "\n")
	summary = crash.Headline(trace)
	for i := 0; i < len(lines) && len(stacks) < maxStacks; i++ {
		head := strings.TrimSpace(lines[i])
		var s stack
		switch {
		case head == "runtime stack:" && len(stacks) == 0:
			s = stack{id: "runtime", name: "runtime stack"}
		case strings.HasPrefix(lines[i], "goroutine ") && strings.HasSuffix(head, ":"):
			s = goroutine(head)
		default:
			continue
		}
		var end int
		s.frames, end = framesFrom(lines, i+1)
		stacks = append(stacks, s)
		i = end - 1
	}
	// The first goroutine crashed, unless a thread that runs only the
	// runtime, as the one that preempts the others, gave up on its own
	// stack, which then starts at runtime.mstart: the goroutine after it
	// may have nothing to do with the crash.
	if len(stacks) > 1 && stacks[0].id == "runtime" {
		if f := stacks[0].frames; len(f) == 0 || f[0].Module != "runtime" || f[0].Function != "mstart" {
			stacks = append(stacks[1:], stacks[0])
		}
	}
	return summary, stacks
}

// maxStacks keeps a report with thousands of goroutines under Sentry's
// size limit; the attachment has them all.
const maxStacks = 50

// stack is one stack of a crash trace: the runtime's own, or a goroutine's.
type stack struct {
	// id is the goroutine's number, or "runtime".
	id string
	// name is the goroutine's number and state, such as
	// "goroutine 1 [syscall, locked to thread]".
	name   string
	frames []sentry.Frame
}

// goroutine reads a goroutine's header, such as "goroutine 1 gp=0x9064e80c000
// m=0 mp=0x7ff78ccabb40 [syscall, locked to thread]:".
func goroutine(head string) stack {
	id := "?"
	if fields := strings.Fields(head); len(fields) > 1 {
		id = fields[1]
	}
	name := "goroutine " + id
	if i, j := strings.Index(head, "["), strings.LastIndex(head, "]"); i >= 0 && j > i {
		name += " " + head[i:j+1]
	}
	return stack{id: id, name: name}
}

// framesFrom reads the frames of the stack starting at lines[i], oldest
// first as Sentry wants them, and returns the line after them.
func framesFrom(lines []string, i int) (frames []sentry.Frame, end int) {
	for ; i+1 < len(lines); i += 2 {
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
	slices.Reverse(frames)
	return frames, i
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
