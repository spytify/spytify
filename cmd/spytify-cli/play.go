package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/proc"
	"github.com/spytify/spytify/pkg/proto"
)

// playStartTimeout covers opening the player (up to 15 s) and asking it to
// play the link (up to 20 s), which session.start waits for.
const playStartTimeout = 60 * time.Second

// play starts a session that plays a link. A spytifyd that answers runs it,
// in the window too when the app is open, and the command follows it to its
// end unless wait is false. With none, spytifyd --once runs it here.
func (a *app) play(p proto.SessionStartParams, wait bool) error {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	c, err := bus.Dial(ctx, a.socket)
	cancel()
	if err != nil {
		return a.playOnce(p, sigs)
	}
	defer c.Close()
	// Read from before the start, so the session's end is not missed.
	events := c.Subscribe()
	ctx, cancel = context.WithTimeout(context.Background(), playStartTimeout)
	raw, err := c.Do(ctx, proto.MethodSessionStart, p)
	cancel()
	if err != nil {
		return err
	}
	var res proto.SessionStartResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "session %s started: the player plays %s\n", res.SessionID, p.PlayURL)
	if !wait {
		return nil
	}
	stops := 0
	for {
		select {
		case <-sigs:
			stops++
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := c.Call(ctx, proto.MethodSessionStop, proto.SessionStopParams{Now: stops > 1}, nil)
			cancel()
			if err != nil {
				return err
			}
			if stops == 1 {
				fmt.Fprintln(a.out, "stopping after the current track (again to stop now)")
			}
		case ev, ok := <-events:
			if !ok {
				return errors.New("spytifyd closed the connection")
			}
			if a.follow(ev) {
				return nil
			}
		}
	}
}

// follow prints what a session that plays a link does, and says when it
// has ended.
func (a *app) follow(ev proto.Event) bool {
	switch ev.Method {
	case proto.EventSessionState:
		var e proto.SessionStateEvent
		if ev.Decode(&e) == nil && e.State == proto.StateIdle {
			fmt.Fprintln(a.out, "session ended")
			return true
		}
	case proto.EventTrackStarted:
		var e proto.TrackStartedEvent
		if ev.Decode(&e) == nil {
			fmt.Fprintf(a.out, "track    %s - %s\n", e.Artist, e.Title)
		}
	case proto.EventTrackFinished:
		var e proto.TrackFinishedEvent
		if ev.Decode(&e) == nil {
			fmt.Fprintf(a.out, "saved    %s\n", e.Path)
		}
	case proto.EventTrackSkipped:
		var e proto.TrackSkippedEvent
		if ev.Decode(&e) == nil {
			fmt.Fprintf(a.out, "skipped  %s\n", e.Reason)
		}
	case proto.EventError:
		var e proto.ErrorEvent
		if ev.Decode(&e) == nil && e.Msg != "" {
			fmt.Fprintf(os.Stderr, "spytifyd: %s\n", e.Msg)
		}
	}
	return false
}

// playOnce runs spytifyd --once for the link, with its output here. Ctrl-C
// reaches it from the terminal; a SIGTERM sent to this command alone, as a
// scheduler does, is passed on.
func (a *app) playOnce(p proto.SessionStartParams, sigs <-chan os.Signal) error {
	path, err := findDaemon("")
	if err != nil {
		return fmt.Errorf("no spytifyd answers at %s, and none can be started: %w", a.socket, err)
	}
	args := []string{"--once", "--play-url", p.PlayURL}
	if p.SingleTrack != nil {
		args = append(args, fmt.Sprintf("--single-track=%t", *p.SingleTrack))
	}
	fmt.Fprintf(a.out, "no spytifyd answers: running %s %s\n", path, strings.Join(args, " "))
	cmd := proc.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, a.out, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case err := <-done:
			return err
		case s := <-sigs:
			if s == syscall.SIGTERM {
				_ = cmd.Process.Signal(s)
			}
		}
	}
}
