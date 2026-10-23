// Command fakeplayer plays a timeline through the OS as a real player would:
// audio through PipeWire (pw-play) and now-playing through MPRIS on the
// session bus, under the name of its own binary. CI records it instead of a
// real player. Linux only.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
)

func main() {
	name := flag.String("name", strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"), "MPRIS name (org.mpris.MediaPlayer2.<name>) and stream application name")
	startDelay := flag.Duration("start-delay", 2*time.Second, "time on the bus, stopped, before playing")
	leadIn := flag.Duration("lead-in", 2*time.Second, "minimum silence before the first item, so a recorder can attach to the stream")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [flags] timeline.json\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*name, flag.Arg(0), *startDelay, *leadIn); err != nil {
		fmt.Fprintln(os.Stderr, "fakeplayer:", err)
		os.Exit(1)
	}
}

func run(name, path string, startDelay, leadIn time.Duration) error {
	tl, err := fake.Load(path)
	if err != nil {
		return err
	}
	if ms := leadIn.Milliseconds(); tl.LeadInMs < ms {
		tl.LeadInMs = ms
	}
	p, err := fake.NewPlayer(tl, true)
	if err != nil {
		return err
	}
	p.Lead = outputLead
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("session bus: %w", err)
	}
	defer conn.Close()
	srv, err := fake.ServeMPRIS(conn, name, p)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(startDelay):
	}
	sink := &pwSink{name: name, srv: srv, events: make(chan timedEvent, 256), published: make(chan struct{})}
	go sink.publish()
	err = p.Run(ctx, sink)
	sink.close()
	close(sink.events)
	<-sink.published
	// Stay on the bus a moment so the recorder sees the final state.
	time.Sleep(500 * time.Millisecond)
	if err == context.Canceled {
		return nil
	}
	return err
}

// outputLead is how far audio is written ahead of the wall clock, so pw-play
// never runs dry; MPRIS updates are delayed by the same amount so they still
// describe what is being heard.
const outputLead = 300 * time.Millisecond

type timedEvent struct {
	at time.Time
	ev meta.Event
}

// pwSink plays chunks through pw-play, one process per stream format, so a
// format change recreates the stream as a real player does.
type pwSink struct {
	name      string
	srv       *fake.MPRISServer
	format    pcm.Format
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	events    chan timedEvent
	published chan struct{}
}

func (s *pwSink) publish() {
	defer close(s.published)
	for te := range s.events {
		time.Sleep(time.Until(te.at))
		s.srv.Update(te.ev)
	}
}

func (s *pwSink) Chunk(ctx context.Context, c capture.Chunk) error {
	if s.cmd == nil || c.Format != s.format {
		s.close()
		if err := s.open(c.Format); err != nil {
			return err
		}
	}
	_, err := s.stdin.Write(c.Data)
	return err
}

func (s *pwSink) Event(ctx context.Context, e meta.Event) error {
	s.events <- timedEvent{at: time.Now().Add(outputLead), ev: e}
	return nil
}

func (s *pwSink) open(f pcm.Format) error {
	format := "s16"
	if f.Sample == pcm.F32LE {
		format = "f32"
	}
	cmd := exec.Command("pw-play",
		"--raw",
		"--rate", strconv.Itoa(f.Rate),
		"--channels", strconv.Itoa(f.Channels),
		"--format", format,
		"--latency", "100ms",
		"-P", fmt.Sprintf("{ application.name = %q media.name = %q }", s.name, s.name),
		"-")
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("pw-play: %w", err)
	}
	s.cmd, s.stdin, s.format = cmd, stdin, f
	return nil
}

func (s *pwSink) close() {
	if s.cmd == nil {
		return
	}
	s.stdin.Close()
	_ = s.cmd.Wait()
	s.cmd, s.stdin = nil, nil
}
