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
	path := filepath.Join(dir, "s.sock")
	if err := srv.ListenUnix(path); err != nil {
		t.Fatal(err)
	}
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
	if err := os.Symlink(path, missing); err != nil {
		t.Fatal(err)
	}
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
