package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

const (
	levelError = iota
	levelInfo
	levelDebug
)

const (
	logRingSize  = 1000
	logMaxBytes  = 5 << 20
	logKeepFiles = 3
	// logMaxAge is how long a log nobody writes to stays on the computer.
	logMaxAge = 30 * 24 * time.Hour
)

// Logger writes a rotating log file, keeps the last lines for log.tail and
// broadcasts every line as a log event; the Logs tab filters by level.
type Logger struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	size    int64
	console io.Writer
	ring    []proto.LogEvent
	emit    func(method string, payload any)
}

// NewLogger opens path (empty for no file) and mirrors lines to console.
func NewLogger(path string, console io.Writer) (*Logger, error) {
	l := &Logger{path: path, console: console}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		removeStale(path, time.Now())
		if err := l.open(); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *Logger) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.file, l.size = f, info.Size()
	return nil
}

// removeStale deletes the log and its rotated copies when nothing was
// written to them for logMaxAge: rotation alone keeps them forever once
// the app is no longer used.
func removeStale(path string, now time.Time) {
	for i := 0; i <= logKeepFiles; i++ {
		p := path
		if i > 0 {
			p = fmt.Sprintf("%s.%d", path, i)
		}
		if st, err := os.Stat(p); err == nil && now.Sub(st.ModTime()) > logMaxAge {
			_ = os.Remove(p)
		}
	}
}

func (l *Logger) rotate() {
	l.file.Close()
	for i := logKeepFiles - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
	if err := l.open(); err != nil {
		l.file = nil
	}
}

// SetEmitter connects the logger to the event broadcaster.
func (l *Logger) SetEmitter(emit func(method string, payload any)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.emit = emit
}

func (l *Logger) Errorf(format string, args ...any) { l.write(levelError, format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.write(levelInfo, format, args...) }
func (l *Logger) Debugf(format string, args ...any) { l.write(levelDebug, format, args...) }

func (l *Logger) write(level int, format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	ev := proto.LogEvent{
		Level: [...]string{"error", "info", "debug"}[level],
		Msg:   strings.TrimSpace(fmt.Sprintf(format, args...)),
		TS:    time.Now().Format(time.RFC3339Nano),
	}
	line := fmt.Sprintf("%s %-5s %s\n", ev.TS, ev.Level, ev.Msg)
	if l.file != nil {
		n, _ := l.file.WriteString(line)
		l.size += int64(n)
		if l.size > logMaxBytes {
			l.rotate()
		}
	}
	if l.console != nil {
		_, _ = io.WriteString(l.console, line)
	}
	l.ring = append(l.ring, ev)
	if len(l.ring) > logRingSize {
		l.ring = l.ring[len(l.ring)-logRingSize:]
	}
	emit := l.emit
	l.mu.Unlock()
	if emit != nil {
		emit(proto.EventLog, ev)
	}
}

// Tail returns the last n lines.
func (l *Logger) Tail(n int) []proto.LogEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.ring) {
		n = len(l.ring)
	}
	return append([]proto.LogEvent(nil), l.ring[len(l.ring)-n:]...)
}

// Close closes the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
