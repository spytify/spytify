package link

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// fakeCore answers the calls the UI makes on connect.
func fakeCore(t *testing.T, protocol int) (*bus.Server, string, *atomic.Int32) {
	t.Helper()
	dir, err := os.MkdirTemp("", "spyui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return fakeCoreAt(t, protocol, filepath.Join(dir, "s.sock"))
}

// fakeCoreAt serves a scripted core on the socket at path.
func fakeCoreAt(t *testing.T, protocol int, path string) (*bus.Server, string, *atomic.Int32) {
	t.Helper()
	starts := &atomic.Int32{}
	recent := func() []proto.Event {
		raw, _ := json.Marshal(proto.TrackStartedEvent{Track: proto.Track{ID: "s-001", Title: "Replayed"}})
		state, _ := json.Marshal(proto.SessionStateEvent{State: proto.StateRecording})
		return []proto.Event{{Method: proto.EventSessionState, Params: state}, {Method: proto.EventTrackStarted, Params: raw}}
	}
	srv := bus.NewServer(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case proto.MethodCoreInfo:
			return proto.CoreInfoResult{Version: "test", Protocol: protocol}, nil
		case proto.MethodSettingsGet:
			return proto.SettingsValues{Values: map[string]any{"encode.format": "flac"}}, nil
		case proto.MethodLogTail:
			return proto.LogTailResult{Lines: []proto.LogEvent{{Level: "info", Msg: "hello"}}}, nil
		case proto.MethodEventsRecent:
			return proto.EventsRecentResult{Events: recent()}, nil
		case proto.MethodSessionStatus:
			return proto.SessionStatusResult{State: proto.StateRecording, SessionID: "s", Track: &proto.Track{ID: "s-001", Title: "Replayed"}}, nil
		case proto.MethodSessionStart:
			starts.Add(1)
			return proto.SessionStartResult{SessionID: "s"}, nil
		}
		return nil, proto.Errorf(proto.CodeMethodNotFound, "unknown")
	}, nil)
	if err := srv.ListenUnix(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv, path, starts
}

func waitFor(t *testing.T, m *model.Model, what string, ok func(model.State) bool) model.State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Snapshot(); ok(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %+v", what, m.Snapshot())
	return model.State{}
}

func TestConnectSyncAndEvents(t *testing.T) {
	srv, path, starts := fakeCore(t, proto.Protocol)
	m := model.New()
	l := New(path, m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)

	s := waitFor(t, m, "connected", func(s model.State) bool { return s.Conn == model.Connected })
	if s.Info.Version != "test" || s.Settings["encode.format"] != "flac" || len(s.Logs) != 1 {
		t.Fatalf("sync: %+v", s)
	}
	if s.Current == nil || s.Current.Title != "Replayed" || len(s.Rows) != 0 {
		t.Fatalf("replay plus status gave current %+v rows %d", s.Current, len(s.Rows))
	}

	if err := l.Do(ctx, proto.MethodSessionStart, nil, nil); err != nil || starts.Load() != 1 {
		t.Fatalf("Do: %v (%d starts)", err, starts.Load())
	}

	srv.Broadcast(proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-002", Title: "Live"}})
	waitFor(t, m, "live event", func(s model.State) bool { return s.Current != nil && s.Current.Title == "Live" })

	srv.Close()
	waitFor(t, m, "disconnect", func(s model.State) bool { return s.Conn != model.Connected })
	if err := l.Do(ctx, proto.MethodSessionStart, nil, nil); err != ErrNotConnected {
		t.Fatalf("Do while disconnected: %v", err)
	}
}

func TestReconnectsWhenTheDaemonComesBack(t *testing.T) {
	_, path, _ := fakeCore(t, proto.Protocol)
	// Point the link at a socket that does not exist yet, then create it.
	missing := filepath.Join(filepath.Dir(path), "later.sock")
	m := model.New()
	l := New(missing, m)
	l.MaxBackoff = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	// Nothing answers: no reason, the window says the recorder is not
	// running.
	waitFor(t, m, "disconnected", func(s model.State) bool { return s.Conn == model.Disconnected && s.ConnError == "" })
	fakeCoreAt(t, proto.Protocol, missing)
	l.Retry()
	waitFor(t, m, "reconnected", func(s model.State) bool { return s.Conn == model.Connected })
}

func TestRefusesAnotherProtocol(t *testing.T) {
	_, path, _ := fakeCore(t, proto.Protocol+1)
	m := model.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go New(path, m).Run(ctx)
	s := waitFor(t, m, "refusal", func(s model.State) bool { return strings.Contains(s.ConnError, "protocol") })
	if s.Conn == model.Connected {
		t.Fatal("connected to a daemon with another protocol")
	}
	// The reason stays while the link keeps retrying, and it does not
	// hammer the daemon.
	time.Sleep(600 * time.Millisecond)
	if s := m.Snapshot(); !strings.Contains(s.ConnError, "protocol") {
		t.Fatalf("reason lost while retrying: %+v", s)
	}
}

// Started from inside its archive, the app finds itself alone in the
// temporary folder: it says to extract the archive, not that the recorder
// is missing.
func TestFindDaemonTellsAnArchiveFromAMissingRecorder(t *testing.T) {
	base := t.TempDir()
	tmp := filepath.Join(base, "tmp")
	for _, env := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(env, tmp)
	}
	t.Setenv("PATH", "")
	app := filepath.Join(base, "Spytify")
	unpacked := filepath.Join(tmp, "Temp1_spytify-windows-amd64.zip", "spytify-windows-amd64")
	for _, dir := range []string{app, unpacked} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ui := "Spytify" + exeSuffix
	if _, found, err := findDaemon(filepath.Join(unpacked, ui)); err != ErrInArchive || found != "none" {
		t.Errorf("unpacked alone: %v found %q, want ErrInArchive", err, found)
	}
	if _, found, err := findDaemon(filepath.Join(app, ui)); err != ErrNoDaemon || found != "none" {
		t.Errorf("recorder missing: %v found %q, want ErrNoDaemon", err, found)
	}
	daemon := filepath.Join(app, "spytifyd"+exeSuffix)
	if err := os.WriteFile(daemon, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if p, found, err := findDaemon(filepath.Join(app, ui)); err != nil || p != daemon || found != "next to the app" {
		t.Errorf("next to the app: %q found %q, %v", p, found, err)
	}
}

// TestMain plays spytifyd when a test starts this binary as one: see
// fakeDaemon.
func TestMain(m *testing.M) {
	switch os.Getenv("SPYTIFY_FAKE_DAEMON") {
	case "exit":
		fmt.Fprintln(os.Stderr, "spytifyd: reading settings")
		fmt.Fprintln(os.Stderr, "spytifyd: listen: address already in use")
		os.Exit(3)
	case "slow":
		fmt.Fprintln(os.Stderr, "spytifyd: starting")
		time.Sleep(1800 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "spytifyd: still starting")
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeDaemon puts this test binary on PATH as spytifyd, which plays the
// given part, and nothing else.
func fakeDaemon(t *testing.T, part string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spytifyd"+exeSuffix), b, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SPYTIFY_FAKE_DAEMON", part)
}

// waitExited waits, as the test ends, for the recorder l started to exit:
// Windows does not delete a program still running, and the test's folder
// goes with its copy of the fake in it. Its cleanup comes after
// fakeDaemon's, so it runs first.
func waitExited(t *testing.T, l *Link) {
	t.Cleanup(func() {
		l.mu.Lock()
		st := l.start
		l.mu.Unlock()
		if st == nil {
			return
		}
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			st.mu.Lock()
			done := st.facts.Exited || !st.facts.Started
			st.mu.Unlock()
			if done {
				return
			}
			if time.Now().After(deadline) {
				t.Error("the fake recorder never exited")
				return
			}
		}
	})
}

// A recorder found nowhere is said so, and was never started.
func TestAStartSaysTheRecorderWasNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	l := New(filepath.Join(t.TempDir(), "absent.sock"), model.New())
	if err := l.StartDaemon(); err != ErrNoDaemon && err != ErrInArchive {
		t.Fatalf("StartDaemon: %v", err)
	}
	s := l.EndStart()
	if s.Found != "none" || s.Started || s.ExeBytes != 0 || s.At.IsZero() {
		t.Errorf("start %+v", s)
	}
}

// A recorder that stops at once says how, and what it wrote last.
func TestAStartSaysHowTheRecorderStopped(t *testing.T) {
	fakeDaemon(t, "exit")
	l := New(filepath.Join(t.TempDir(), "absent.sock"), model.New())
	waitExited(t, l)
	err := l.StartDaemon()
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("StartDaemon: %v", err)
	}
	s := l.EndStart()
	if s.Found != "PATH" || s.ExeBytes == 0 || !s.Started || !s.Exited || s.ExitCode != 3 || s.Err != "exit status 3" {
		t.Errorf("start %+v", s)
	}
	if !strings.Contains(s.Stderr, "reading settings") || !strings.HasSuffix(strings.TrimSpace(s.Stderr), "address already in use") {
		t.Errorf("stderr %q", s.Stderr)
	}
}

// A recorder still running at 1.5 s that does not answer keeps what it
// wrote since, until its start is reported; then it is dropped.
func TestAStartThatGetsNoAnswerKeepsTheRecorderOutput(t *testing.T) {
	fakeDaemon(t, "slow")
	sock := filepath.Join(t.TempDir(), "absent.sock")
	l := New(sock, model.New())
	waitExited(t, l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	if err := l.StartDaemon(); err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}
	l.mu.Lock()
	st := l.start
	l.mu.Unlock()
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(st.stderr.String(), "still starting"); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("stderr %q, without what came after 1.5 s", st.stderr.String())
		}
	}
	s := l.EndStart()
	if !s.Started || s.Exited || s.SocketExists {
		t.Errorf("start %+v", s)
	}
	if !strings.Contains(s.Stderr, "spytifyd: starting") || !strings.Contains(s.Stderr, "still starting") {
		t.Errorf("stderr %q", s.Stderr)
	}
	if s.DialErr == "" || strings.Contains(s.DialErr, sock) {
		t.Errorf("dial error %q, want one without the socket's path", s.DialErr)
	}
	if st.stderr.String() != "" {
		t.Error("the recorder's output is still kept once its start was reported")
	}
}

// A recorder that answers is no longer heard on stderr.
func TestAStartThatGetsAnAnswerDropsTheRecorderOutput(t *testing.T) {
	_, sock, _ := fakeCore(t, proto.Protocol)
	fakeDaemon(t, "slow")
	m := model.New()
	l := New(sock, m)
	waitExited(t, l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	if err := l.StartDaemon(); err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}
	waitFor(t, m, "connected", func(s model.State) bool { return s.Conn == model.Connected })
	l.mu.Lock()
	st := l.start
	l.mu.Unlock()
	time.Sleep(500 * time.Millisecond)
	if s := st.stderr.String(); s != "" {
		t.Errorf("kept %q once it answered", s)
	}
}

// Its stderr keeps its end: the last lines say why it stopped.
func TestTheDaemonOutputKeepsItsEnd(t *testing.T) {
	var b lockedBuffer
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if s := b.String(); len(s) > 64<<10 || lastLine(s) != "line 9999" {
		t.Fatalf("kept %d bytes ending %q", len(s), lastLine(s))
	}
}

// Once the recorder answers, what it writes to stderr is not kept in the
// app: its log file has it.
func TestTheDaemonOutputIsNotKeptAfterStart(t *testing.T) {
	var b lockedBuffer
	b.Write([]byte("spytifyd: starting\n"))
	if lastLine(b.String()) != "spytifyd: starting" {
		t.Fatalf("before start: %q", b.String())
	}
	b.stop()
	for i := 0; i < 1000; i++ {
		b.Write([]byte("a log line\n"))
	}
	if b.String() != "" {
		t.Fatalf("kept %d bytes after start", len(b.String()))
	}
}
