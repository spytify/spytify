// Package crash keeps the trace of a fatal panic in a file, so the next run
// of a program, or the app, can report it.
package crash

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// Keep copies a fatal panic's trace to path as well as stderr, so a
// crash leaves a trace whoever started the program: the app drops
// spytifyd's stderr once it is up. It returns the summary of the trace the
// previous run left, which moves to path.1.
func Keep(path string) (previous string) {
	// Let go of a file set before, which Windows would not rename.
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		previous = Summary(string(b))
		_ = os.Rename(path, path+".1")
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
