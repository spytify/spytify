// Package crash keeps the trace of a fatal panic in a file, so the next run
// of a program, or the app, can report it.
package crash

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

// MaxAge is how long the previous run's trace stays, as for logs.
const MaxAge = 30 * 24 * time.Hour

// Keep copies a fatal panic's trace to path as well as stderr, so a
// crash leaves a trace whoever started the program: the app drops
// spytifyd's stderr once it is up. It returns the summary of the trace the
// previous run left, which moves to path.1.
func Keep(path string) (previous string) {
	// Let go of a file set before, which Windows would not rename.
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	if st, err := os.Stat(path + ".1"); err == nil && time.Since(st.ModTime()) > MaxAge {
		_ = os.Remove(path + ".1")
		_ = os.Remove(StderrOf(path + ".1"))
	}
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		previous = Summary(WithLost(path, string(b)))
		_ = os.Rename(path, path+".1")
		// An older crash's stderr must not stay beside this trace.
		_ = os.Remove(StderrOf(path + ".1"))
		_ = os.Rename(StderrOf(path), StderrOf(path+".1"))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return previous
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return previous
	}
	defer f.Close()
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	return previous
}

// Summary is the panic message and where it happened: the first line,
// and the first source line of the goroutine that panicked.
func Summary(trace string) string {
	lines := strings.Split(strings.TrimSpace(trace), "\n")
	summary := strings.TrimSpace(lines[0])
	if headline := Headline(trace); headline != summary {
		// It names where already.
		return headline
	}
	for i, l := range lines {
		if strings.HasPrefix(l, "goroutine ") && strings.HasSuffix(strings.TrimSpace(l), "[running]:") {
			for _, frame := range lines[i+1:] {
				if f := strings.TrimSpace(frame); strings.Contains(f, ".go:") && !strings.Contains(f, "/runtime/") {
					return summary + " at " + strings.Fields(f)[0]
				}
			}
		}
	}
	return summary
}

// Headline is the trace's first line, the panic message, unless the
// runtime's own message is missing: a fatal error prints it to stderr only,
// before what the runtime copies to the crash output (as of Go 1.27). Such
// a trace starts with a stack, the runtime's ("runtime stack:") or a
// goroutine's, and its headline names the function that gave up, the first
// one out of runtime/panic.go, where throw and fatal live.
func Headline(trace string) string {
	if header := Header(trace); len(header) > 0 {
		for _, l := range header {
			if strings.HasPrefix(l, "panic: ") || strings.HasPrefix(l, "fatal error: ") {
				return l
			}
		}
		return header[0]
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(trace), "\r\n", "\n"), "\n")
	for i := 1; i+1 < len(lines) && strings.HasPrefix(lines[i+1], "\t"); i += 2 {
		file, line := location(lines[i+1])
		if file == "runtime/panic.go" || strings.HasSuffix(file, "/runtime/panic.go") {
			continue
		}
		fn := strings.TrimSpace(lines[i])
		if j := strings.LastIndex(fn, "("); j > 0 && strings.HasSuffix(fn, ")") {
			fn = fn[:j]
		}
		return fmt.Sprintf("fatal error: thrown in %s (%s:%s)", fn, path.Base(file), line)
	}
	return strings.TrimSpace(lines[0])
}

// Header is what a trace says before its first stack: the message of the
// panic, the fatal error or the signal, with the lines printed along with
// it, such as an errno or where a signal hit.
func Header(trace string) []string {
	var header []string
	for _, l := range strings.Split(strings.ReplaceAll(trace, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "runtime stack:" || strings.HasPrefix(l, "goroutine ") && strings.HasSuffix(l, ":") {
			break
		}
		if l != "" {
			header = append(header, l)
		}
	}
	return header
}

// StderrOf is the file CaptureStderr writes beside the crash log at path,
// which Keep moves along with it: "spytify-ui.crash.log" has
// "spytify-ui.crash.stderr", and "spytify-ui.crash.log.1"
// "spytify-ui.crash.stderr.1".
func StderrOf(path string) string {
	if i := strings.LastIndex(path, ".log"); i >= 0 {
		return path[:i] + ".stderr" + path[i+len(".log"):]
	}
	return path + ".stderr"
}

// maxStderr is how much of the end of a stderr file WithLost reads.
const maxStderr = 8 << 20

// WithLost is trace, from the crash log at path, with the lines the
// runtime printed to stderr just before it, which the crash output misses
// (see Headline): a fatal error's message, and what the failing call
// printed first, such as an errno. They are there when CaptureStderr kept
// that run's stderr.
func WithLost(path, trace string) string {
	f, err := os.Open(StderrOf(path))
	if err != nil {
		return trace
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > maxStderr {
		_, _ = f.Seek(-maxStderr, io.SeekEnd)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return trace
	}
	lost := lostBefore(string(b), trace)
	if len(lost) == 0 {
		return trace
	}
	return strings.Join(lost, "\n") + "\n\n" + strings.TrimSpace(trace)
}

// lostBefore is the block of lines, up to ten, that stderr has right
// before trace.
func lostBefore(stderr, trace string) []string {
	stderr = strings.ReplaceAll(stderr, "\r\n", "\n")
	head := strings.TrimSpace(strings.ReplaceAll(trace, "\r\n", "\n"))
	if len(head) > 200 {
		head = head[:200]
	}
	i := strings.LastIndex(stderr, head)
	if head == "" || i < 0 {
		return nil
	}
	before := strings.Split(strings.TrimRight(stderr[:i], " \t\n"), "\n")
	var lost []string
	for j := len(before) - 1; j >= 0 && len(lost) < 10; j-- {
		l := strings.TrimSpace(before[j])
		if l == "" {
			break
		}
		lost = append(lost, l)
	}
	slices.Reverse(lost)
	return lost
}

// location splits a trace's source line, such as
// "\truntime/panic.go:1229 +0x4d fp=0xe56a7ff720", into its file and line.
func location(s string) (file, line string) {
	file = strings.TrimSpace(s)
	if j := strings.LastIndex(file, " +0x"); j >= 0 {
		file = file[:j]
	}
	if j := strings.LastIndex(file, ":"); j >= 0 {
		return file[:j], file[j+1:]
	}
	return file, ""
}
