// Package link keeps the UI connected to spytifyd: it dials with backoff,
// fills the model when it connects, feeds it every event, and starts the
// daemon when asked.
package link

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// ErrNotConnected is returned by Do while no daemon answers.
var ErrNotConnected = errors.New(lang.NotConnectedToRecorder)

var (
	// ErrInArchive is the app started from inside its download: the file
	// manager unpacked it alone into a temporary folder, without the
	// recorder.
	//lint:ignore ST1005 the app shows it to the user as written
	ErrInArchive = errors.New(lang.InArchive)
	// ErrNoDaemon is the recorder found neither next to the app nor on
	// PATH.
	//lint:ignore ST1005 the app shows it to the user as written
	ErrNoDaemon = fmt.Errorf(lang.NoRecorder, "spytifyd"+exeSuffix)
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
	// start is the last start of spytifyd; dialErr, why the last dial
	// failed.
	start   *starting
	dialErr string
}

// Start is what a start of spytifyd found and saw, for the bug tracker:
// nothing in it names the user or a folder of theirs.
type Start struct {
	// At is when the start began.
	At time.Time
	// Found is where spytifyd was: "next to the app", "PATH" or "none";
	// ExeBytes is its size.
	Found    string
	ExeBytes int64
	// InTemp is the app run from inside the temporary folder.
	InTemp bool
	// Started is spytifyd launched; Exited, that it stopped since, with
	// ExitCode.
	Started, Exited bool
	ExitCode        int
	// Err is why it did not start or how it stopped, without the path it
	// may hold, and Errno the system's error number in it, 0 for none.
	Err   string
	Errno uintptr
	// Stderr is what spytifyd wrote to its stderr, its end kept.
	Stderr string
	// SocketExists is the socket's file there as the start ends; DialErr,
	// why the last dial failed, without the path.
	SocketExists bool
	DialErr      string
}

// starting is a start of spytifyd: what StartDaemon found, then what the
// process does once it returned. settled is StartDaemon returned with it
// running: until then its stderr is its message if it stops.
type starting struct {
	mu      sync.Mutex
	facts   Start
	settled bool
	stderr  lockedBuffer
}

func (s *starting) note(f func(*Start)) {
	s.mu.Lock()
	f(&s.facts)
	s.mu.Unlock()
}

// failed notes err, without the path it may hold.
func (f *Start) failed(err error) {
	f.Err = pathless(err)
	var errno syscall.Errno
	if errors.As(err, &errno) {
		f.Errno = uintptr(errno)
	}
}

// pathless is err's message without the path it may hold: the bug tracker
// gets what went wrong, not where.
func pathless(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	var ee *exec.Error
	if errors.As(err, &ee) {
		return ee.Err.Error()
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return oe.Op + ": " + oe.Err.Error()
	}
	return err.Error()
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
			// Nothing answers: the window says the recorder is not
			// running, with no reason to add.
			l.mu.Lock()
			l.dialErr = pathless(err)
			l.mu.Unlock()
			lastErr = ""
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
		l.answered()
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
		l.Model.SetConn(model.Disconnected, lang.RecorderClosed)
	}
}

func (l *Link) set(c *bus.Client) {
	l.mu.Lock()
	l.c = c
	l.mu.Unlock()
}

// answered ends the start under way: the recorder answers, so what it
// writes to stderr is no longer kept, its log file has it. A start not
// settled yet stops keeping it as it settles. It comes after the model
// says connected, which StartDaemon reads as it settles.
func (l *Link) answered() {
	l.mu.Lock()
	l.dialErr = ""
	st := l.start
	l.mu.Unlock()
	if st == nil {
		return
	}
	st.mu.Lock()
	settled := st.settled
	st.mu.Unlock()
	if settled {
		st.stderr.stop()
	}
}

// EndStart returns what the last start of spytifyd found and saw, and
// stops keeping its output: the start failed and is being reported. It is
// the zero Start when no start was made.
func (l *Link) EndStart() Start {
	l.mu.Lock()
	st, dialErr := l.start, l.dialErr
	l.mu.Unlock()
	if st == nil {
		return Start{}
	}
	st.mu.Lock()
	s := st.facts
	st.mu.Unlock()
	s.Stderr = st.stderr.String()
	st.stderr.stop()
	s.DialErr = dialErr
	if !strings.HasPrefix(l.Addr, "tcp://") {
		_, err := os.Lstat(l.Addr)
		s.SocketExists = err == nil
	}
	return s
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
		return errors.New(lang.RecorderOtherProtocol)
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
	p, _, err := findDaemon(exe)
	return p, err
}

// findDaemon also says where spytifyd was found: "next to the app",
// "PATH" or "none".
func findDaemon(exe string) (string, string, error) {
	if exe != "" {
		p := filepath.Join(filepath.Dir(exe), "spytifyd"+exeSuffix)
		if _, err := os.Stat(p); err == nil {
			return p, "next to the app", nil
		}
	}
	if p, err := exec.LookPath("spytifyd"); err == nil {
		return p, "PATH", nil
	}
	if exe != "" && inTempDir(filepath.Dir(exe)) {
		return "", "none", ErrInArchive
	}
	return "", "none", ErrNoDaemon
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
// at once (no player configured, socket taken) returns its message. What
// it found and saw waits for EndStart.
func (l *Link) StartDaemon() error {
	st := &starting{facts: Start{At: time.Now(), Found: "none"}}
	l.mu.Lock()
	if l.start != nil {
		l.start.stderr.stop()
	}
	l.start = st
	l.mu.Unlock()
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	inTemp := exe != "" && inTempDir(filepath.Dir(exe))
	path, found, err := findDaemon(exe)
	st.note(func(f *Start) { f.Found, f.InTemp = found, inTemp })
	if err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		st.note(func(f *Start) { f.ExeBytes = fi.Size() })
	}
	cmd := exec.Command(path)
	detach(cmd)
	cmd.Stderr = &st.stderr
	if err := cmd.Start(); err != nil {
		st.note(func(f *Start) { f.failed(err) })
		return err
	}
	st.note(func(f *Start) { f.Started = true })
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		st.note(func(f *Start) {
			f.Exited, f.ExitCode = true, cmd.ProcessState.ExitCode()
			if err != nil {
				f.failed(err)
			}
		})
		exited <- err
	}()
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(250 * time.Millisecond)
			l.Retry()
		}
	}()
	select {
	case err := <-exited:
		msg := lastLine(st.stderr.String())
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return fmt.Errorf(lang.RecorderStopped, msg)
	case <-time.After(1500 * time.Millisecond):
		// Still running: its log file has the rest. The pipe stays open
		// until the app quits; what comes through is kept until it answers
		// or EndStart reports it did not, then dropped.
		st.mu.Lock()
		st.settled = true
		st.mu.Unlock()
		if l.Model.Snapshot().Conn == model.Connected {
			st.stderr.stop()
		}
		return nil
	}
}

// lockedBuffer keeps the last 64 KiB written to it: the end of what
// spytifyd said says why it stopped.
type lockedBuffer struct {
	mu      sync.Mutex
	b       []byte
	stopped bool
}

func (w *lockedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.b = append(w.b, p...)
		if over := len(w.b) - 64<<10; over > 0 {
			w.b = append(w.b[:0], w.b[over:]...)
		}
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
