// Package link keeps the UI connected to spytifyd: it dials with backoff,
// fills the model when it connects, feeds it every event, and starts the
// daemon when asked.
package link

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// ErrNotConnected is returned by Do while no daemon answers.
var ErrNotConnected = errors.New("not connected to spytifyd")

var (
	// ErrInArchive is the app started from inside its download: the file
	// manager unpacked it alone into a temporary folder, without the
	// recorder.
	ErrInArchive = errors.New("Spytify runs from inside the downloaded archive, without the recorder next to it. Extract the whole archive to a folder, then start Spytify from there.")
	// ErrNoDaemon is the recorder found neither next to the app nor on
	// PATH.
	ErrNoDaemon = errors.New("spytifyd" + exeSuffix + ", the recorder, is not next to Spytify. Extract the whole download again and start Spytify from there; if it is still missing, an antivirus may have removed it.")
)

// Link is the UI's connection to the core.
type Link struct {
	Addr  string
	Model *model.Model
	// MaxBackoff caps the wait between dial attempts.
	MaxBackoff time.Duration

	mu   sync.Mutex
	c    *bus.Client
	wake chan struct{}
}

// New prepares a link to addr (a socket path or tcp://host:port).
func New(addr string, m *model.Model) *Link {
	return &Link{Addr: addr, Model: m, MaxBackoff: 5 * time.Second, wake: make(chan struct{}, 1)}
}

// Run keeps the link up until ctx ends.
func (l *Link) Run(ctx context.Context) {
	const first = 250 * time.Millisecond
	wait := first
	// pause waits out the backoff, or less when Retry is called.
	pause := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-l.wake:
			wait = first
		case <-time.After(wait):
			if wait *= 2; wait > l.MaxBackoff {
				wait = l.MaxBackoff
			}
		}
		return true
	}
	lastErr := ""
	for ctx.Err() == nil {
		// The reason of the last failure stays on screen while retrying.
		l.Model.SetConn(model.Connecting, lastErr)
		c, err := bus.Dial(ctx, l.Addr)
		if err != nil {
			lastErr = "spytifyd is not running"
			l.Model.SetConn(model.Disconnected, lastErr)
			if !pause() {
				return
			}
			continue
		}
		l.set(c)
		if err := l.sync(ctx, c); err != nil {
			c.Close()
			l.set(nil)
			lastErr = err.Error()
			l.Model.SetConn(model.Disconnected, lastErr)
			if !pause() {
				return
			}
			continue
		}
		wait, lastErr = first, ""
		l.Model.SetConn(model.Connected, "")
		for loop := true; loop; {
			select {
			case <-ctx.Done():
				c.Close()
				return
			case ev, ok := <-c.Subscribe():
				if !ok {
					loop = false
					break
				}
				l.Model.Apply(ev)
			}
		}
		l.set(nil)
		l.Model.SetConn(model.Disconnected, "spytifyd closed the connection")
	}
}

func (l *Link) set(c *bus.Client) {
	l.mu.Lock()
	l.c = c
	l.mu.Unlock()
}

// sync fills the model in the order that leaves it right: what the core is,
// its settings and logs, what happened lately, then where the session is.
func (l *Link) sync(ctx context.Context, c *bus.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var info proto.CoreInfoResult
	if err := c.Call(ctx, proto.MethodCoreInfo, nil, &info); err != nil {
		return err
	}
	if info.Protocol != proto.Protocol {
		return errors.New("spytifyd speaks another protocol version; update the UI or the daemon")
	}
	l.Model.SetInfo(info)
	var settings proto.SettingsValues
	if err := c.Call(ctx, proto.MethodSettingsGet, proto.SettingsGetParams{}, &settings); err != nil {
		return err
	}
	l.Model.SetSettings(settings.Values)
	var logs proto.LogTailResult
	if err := c.Call(ctx, proto.MethodLogTail, proto.LogTailParams{Lines: 500}, &logs); err != nil {
		return err
	}
	l.Model.SetLogs(logs.Lines)
	var recent proto.EventsRecentResult
	if err := c.Call(ctx, proto.MethodEventsRecent, nil, &recent); err != nil {
		return err
	}
	l.Model.Replay(recent.Events)
	var status proto.SessionStatusResult
	if err := c.Call(ctx, proto.MethodSessionStatus, nil, &status); err != nil {
		return err
	}
	l.Model.SetStatus(status)
	return nil
}

// Do sends an action and decodes its result into out (which may be nil).
func (l *Link) Do(ctx context.Context, method string, params, out any) error {
	l.mu.Lock()
	c := l.c
	l.mu.Unlock()
	if c == nil {
		return ErrNotConnected
	}
	return c.Call(ctx, method, params, out)
}

// RefreshInfo reloads core.info, for the checklist after a change.
func (l *Link) RefreshInfo(ctx context.Context) error {
	var info proto.CoreInfoResult
	if err := l.Do(ctx, proto.MethodCoreInfo, nil, &info); err != nil {
		return err
	}
	l.Model.SetInfo(info)
	return nil
}

// Retry makes Run dial again now instead of waiting out its backoff.
func (l *Link) Retry() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// FindDaemon locates spytifyd: next to the running binary, then on PATH.
func FindDaemon() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return findDaemon(exe)
}

func findDaemon(exe string) (string, error) {
	if exe != "" {
		p := filepath.Join(filepath.Dir(exe), "spytifyd"+exeSuffix)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("spytifyd"); err == nil {
		return p, nil
	}
	if exe != "" && inTempDir(filepath.Dir(exe)) {
		return "", ErrInArchive
	}
	return "", ErrNoDaemon
}

// inTempDir says whether dir is in the temporary folder, where Windows'
// Explorer and Linux archive managers unpack a program started from
// inside an archive.
func inTempDir(dir string) bool {
	tmp := os.TempDir()
	// Both resolved: macOS's /var is a link, and Windows may name the
	// temporary folder in 8.3 form.
	if p, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = p
	}
	if p, err := filepath.EvalSymlinks(dir); err == nil {
		dir = p
	}
	rel, err := filepath.Rel(tmp, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// StartDaemon launches spytifyd detached from the UI, so closing the window
// does not stop a recording, then asks Run to connect. A daemon that exits
// at once (no player configured, socket taken) returns its message.
func (l *Link) StartDaemon() error {
	path, err := FindDaemon()
	if err != nil {
		return err
	}
	cmd := exec.Command(path)
	detach(cmd)
	var stderr lockedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(250 * time.Millisecond)
			l.Retry()
		}
	}()
	select {
	case err := <-exited:
		msg := lastLine(stderr.String())
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return errors.New("spytifyd stopped: " + msg)
	case <-time.After(1500 * time.Millisecond):
		// Still running: its log file has the rest. The pipe stays open
		// until the app quits, so what comes through is dropped.
		stderr.stop()
		return nil
	}
}

type lockedBuffer struct {
	mu      sync.Mutex
	b       []byte
	stopped bool
}

func (w *lockedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped && len(w.b) < 64<<10 {
		w.b = append(w.b, p...)
	}
	return len(p), nil
}

// stop drops what is written from now on and what was kept.
func (w *lockedBuffer) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped, w.b = true, nil
}

func (w *lockedBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.b)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
