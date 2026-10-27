package link

import (
	"context"
	"encoding/json"
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
	waitFor(t, m, "disconnected", func(s model.State) bool { return s.Conn == model.Disconnected && s.ConnError != "" })
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
	if _, err := findDaemon(filepath.Join(unpacked, ui)); err != ErrInArchive {
		t.Errorf("unpacked alone: %v, want ErrInArchive", err)
	}
	if _, err := findDaemon(filepath.Join(app, ui)); err != ErrNoDaemon {
		t.Errorf("recorder missing: %v, want ErrNoDaemon", err)
	}
	daemon := filepath.Join(app, "spytifyd"+exeSuffix)
	if err := os.WriteFile(daemon, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := findDaemon(filepath.Join(app, ui)); err != nil || p != daemon {
		t.Errorf("next to the app: %q, %v", p, err)
	}
}

// Once the recorder is up, what it writes to stderr is not kept in the
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
