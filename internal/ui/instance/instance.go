// Package instance keeps the desktop app to one copy: opened again while
// it runs, Spytify shows the window of the copy already running, hidden to
// the tray or not, instead of starting a second one.
package instance

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ErrRunning says another copy of the app runs; it was asked to show its
// window.
var ErrRunning = errors.New("spytify is already running")

const (
	showRequest = "show\n"
	shownReply  = "shown\n"
	// answerTimeout bounds a request and its answer, both ways.
	answerTimeout = 2 * time.Second
	// A launch at the same moment may hold the socket a moment before it
	// answers there.
	retries    = 3
	retryDelay = 50 * time.Millisecond
)

// Instance is the copy of the app that runs, listening for the next
// launches.
type Instance struct {
	ln net.Listener
}

// Claim makes this process the running copy, listening on the socket at
// path. When a copy already answers there, Claim asks it to show its
// window and returns ErrRunning.
func Claim(path string) (*Instance, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if askToShow(path) == nil {
		return nil, ErrRunning
	}
	ln, err := net.Listen("unix", path)
	for try := 0; err != nil && try < retries; try++ {
		// The address is taken: by a launch at the same moment, about to
		// answer, or by the socket file of a copy that crashed.
		time.Sleep(retryDelay)
		if askToShow(path) == nil {
			return nil, ErrRunning
		}
		_ = os.Remove(path)
		ln, err = net.Listen("unix", path)
	}
	if err != nil {
		return nil, err
	}
	return &Instance{ln: ln}, nil
}

// askToShow asks the copy listening at path to show its window. It fails
// only when none answers the call.
func askToShow(path string) error {
	c, err := net.DialTimeout("unix", path, answerTimeout)
	if err != nil {
		return err
	}
	defer c.Close()
	// The user just started this process, so it may bring a window to the
	// front; the running copy, in the background, may not unless allowed.
	allowForeground()
	_ = c.SetDeadline(time.Now().Add(answerTimeout))
	if _, err := io.WriteString(c, showRequest); err == nil {
		// Stay until the running copy has taken the request.
		_, _ = bufio.NewReader(c).ReadString('\n')
	}
	return nil
}

// Serve calls show for each later launch, until Close.
func (i *Instance) Serve(show func()) {
	for {
		c, err := i.ln.Accept()
		if err != nil {
			return
		}
		answer(c, show)
	}
}

func answer(c net.Conn, show func()) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(answerTimeout))
	if req, err := bufio.NewReader(c).ReadString('\n'); err != nil || req != showRequest {
		return
	}
	show()
	_, _ = io.WriteString(c, shownReply)
}

// Close stops listening and removes the socket file.
func (i *Instance) Close() error {
	return i.ln.Close()
}
