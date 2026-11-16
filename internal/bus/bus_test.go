package bus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

func socketPath(t *testing.T) string {
	// macOS caps socket paths at 104 bytes, shorter than t.TempDir().
	dir, err := os.MkdirTemp("", "spy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func echoServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv := NewServer(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case "echo":
			var v any
			_ = json.Unmarshal(params, &v)
			return map[string]any{"echo": v}, nil
		case "fail":
			return nil, proto.Errorf(proto.CodeSessionActive, "busy")
		case "boom":
			return nil, errors.New("plain error")
		case "slow":
			time.Sleep(200 * time.Millisecond)
			return "slow", nil
		}
		return nil, proto.Errorf(proto.CodeMethodNotFound, "unknown method %q", method)
	}, t.Logf)
	path := socketPath(t)
	if err := srv.ListenUnix(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv, path
}

func TestCallAndErrors(t *testing.T) {
	_, path := echoServer(t)
	ctx := context.Background()
	c, err := Dial(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var out struct{ Echo map[string]any }
	if err := c.Call(ctx, "echo", map[string]any{"a": 1}, &out); err != nil || out.Echo["a"] != 1.0 {
		t.Fatalf("echo: %v %v", out, err)
	}
	var perr *proto.Error
	if err := c.Call(ctx, "fail", nil, nil); !errors.As(err, &perr) || perr.Code != proto.CodeSessionActive {
		t.Fatalf("fail: %v", err)
	}
	if err := c.Call(ctx, "boom", nil, nil); !errors.As(err, &perr) || perr.Code != proto.CodeInternalError {
		t.Fatalf("boom: %v", err)
	}
	if err := c.Call(ctx, "nope", nil, nil); !errors.As(err, &perr) || perr.Code != proto.CodeMethodNotFound {
		t.Fatalf("nope: %v", err)
	}
}

func TestConcurrentCallsAreMatchedByID(t *testing.T) {
	_, path := echoServer(t)
	ctx := context.Background()
	c, err := Dial(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	slowDone := make(chan string, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		var s string
		_ = c.Call(ctx, "slow", nil, &s)
		slowDone <- s
	}()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var out struct{ Echo float64 }
			if err := c.Call(ctx, "echo", i, &out); err != nil || int(out.Echo) != i {
				t.Errorf("echo %d = %v %v", i, out.Echo, err)
			}
		}(i)
	}
	wg.Wait()
	if s := <-slowDone; s != "slow" {
		t.Fatalf("slow = %q", s)
	}
}

func TestBroadcastReachesEveryClient(t *testing.T) {
	srv, path := echoServer(t)
	ctx := context.Background()
	var clients []*Client
	for i := 0; i < 3; i++ {
		c, err := Dial(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		// A round trip guarantees the server registered the connection.
		if err := c.Call(ctx, "echo", 0, nil); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	srv.Broadcast(proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording})
	for i, c := range clients {
		select {
		case ev := <-c.Subscribe():
			var st proto.SessionStateEvent
			if ev.Method != proto.EventSessionState || ev.Decode(&st) != nil || st.State != proto.StateRecording {
				t.Fatalf("client %d got %+v", i, ev)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("client %d got no event", i)
		}
	}
}

func TestRawWireFormat(t *testing.T) {
	_, path := echoServer(t)
	nc, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	r := bufio.NewReader(nc)
	send := func(s string) string {
		if _, err := nc.Write([]byte(s + "\n")); err != nil {
			t.Fatal(err)
		}
		nc.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(line)
	}
	if got := send(`{"jsonrpc":"2.0","id":7,"method":"echo","params":{"x":"y"}}`); got != `{"jsonrpc":"2.0","id":7,"result":{"echo":{"x":"y"}}}` {
		t.Fatalf("got %s", got)
	}
	if got := send(`not json`); !strings.Contains(got, `"code":-32700`) {
		t.Fatalf("parse error: %s", got)
	}
	if got := send(`{"jsonrpc":"1.0","id":3,"method":"echo"}`); !strings.Contains(got, `"code":-32600`) {
		t.Fatalf("invalid request: %s", got)
	}
}

func TestStaleSocketIsReplacedButLiveOneIsNot(t *testing.T) {
	path := socketPath(t)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewServer(nil, nil)
	if err := a.ListenUnix(path); err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	defer a.Close()
	info, err := os.Stat(path)
	// Windows has no mode bits; the socket's folder ACL protects it.
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("socket mode: %v %v", info.Mode(), err)
	}
	b := NewServer(nil, nil)
	if err := b.ListenUnix(path); err == nil {
		b.Close()
		t.Fatal("second daemon took over a live socket")
	}
}

func TestClientSeesClose(t *testing.T) {
	srv, path := echoServer(t)
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), "echo", 1, nil); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("client did not notice the server closing")
	}
	if err := c.Call(context.Background(), "echo", 1, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("call after close: %v", err)
	}
}

// A handler that closes the server, as core.shutdown does, still answers.
func TestReplyBeforeCloseIsDelivered(t *testing.T) {
	for i := 0; i < 50; i++ {
		var srv *Server
		srv = NewServer(func(context.Context, string, json.RawMessage) (any, error) {
			go srv.Close()
			return "bye", nil
		}, t.Logf)
		path := socketPath(t)
		if err := srv.ListenUnix(path); err != nil {
			t.Fatal(err)
		}
		c, err := Dial(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := c.Call(context.Background(), "shutdown", nil, &got); err != nil || got != "bye" {
			t.Fatalf("run %d: %q, %v", i, got, err)
		}
		c.Close()
		srv.Close()
	}
}

// --listen serves the same protocol over TCP, next to the socket.
func TestTCPServesTheSameProtocol(t *testing.T) {
	srv, path := echoServer(t)
	if err := srv.ListenTCP("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	var tcp string
	for _, a := range srv.Addrs() {
		if a.Network() == "tcp" {
			tcp = a.String()
		} else if a.String() != path {
			t.Errorf("unix address %s, want %s", a, path)
		}
	}
	if tcp == "" {
		t.Fatalf("no TCP address in %v", srv.Addrs())
	}
	ctx := context.Background()
	c, err := Dial(ctx, "tcp://"+tcp)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var out struct{ Echo string }
	if err := c.Call(ctx, "echo", "over tcp", &out); err != nil || out.Echo != "over tcp" {
		t.Fatalf("echo: %v %v", out, err)
	}
	if err := srv.ListenTCP(tcp); err == nil {
		t.Fatal("a second listener took a busy port")
	}
}

// The bus has no authentication: --listen takes a loopback address only,
// never one that other computers reach.
func TestTCPListensOnLoopbackOnly(t *testing.T) {
	for _, addr := range []string{":9000", "0.0.0.0:9000", ":0", "0.0.0.0:0", "[::]:0", "192.168.1.10:0", "example.com:0"} {
		srv, _ := echoServer(t)
		err := srv.ListenTCP(addr)
		if err == nil {
			t.Errorf("%s: listening on %v", addr, srv.Addrs())
		} else if !strings.Contains(err.Error(), "not a loopback address") {
			t.Errorf("%s: %v, want a refusal that says why", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0", "localhost:0"} {
		srv, _ := echoServer(t)
		if err := srv.ListenTCP(addr); err != nil {
			if addr == "[::1]:0" && !strings.Contains(err.Error(), "loopback") {
				t.Logf("%s: %v (no IPv6 here)", addr, err)
				continue
			}
			t.Errorf("%s: %v", addr, err)
		}
	}
}

// A client that stops reading is dropped once its backlog is full, so
// events never wait for it.
func TestAClientThatStopsReadingIsDropped(t *testing.T) {
	srv, path := echoServer(t)
	nc, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	conns := func() int {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.conns)
	}
	deadline := time.Now().Add(5 * time.Second)
	for conns() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the client never connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	big := strings.Repeat("x", 16<<10)
	for n := 0; conns() > 0; n++ {
		begin := time.Now()
		srv.Broadcast("log", map[string]string{"msg": big})
		if took := time.Since(begin); took > time.Second {
			t.Fatalf("broadcast %d waited %s for a client that does not read", n, took)
		}
		if time.Now().After(deadline.Add(20 * time.Second)) {
			t.Fatalf("the slow client is still connected after %d events", n)
		}
	}
}

// Params the core already encoded go out byte for byte: the bus does not
// encode them a second time, nor wrap them through a second Marshal.
func TestEncodedParamsAreSentAsTheyAre(t *testing.T) {
	srv, path := echoServer(t)
	nc, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	r := bufio.NewReader(nc)
	if _, err := nc.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"echo"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	// Spaces a second encode would compact away.
	srv.Broadcast("log", json.RawMessage(`{"level": "debug", "msg": "a <b> & c"}`))
	srv.Broadcast(proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording})
	srv.Broadcast("empty", json.RawMessage(nil))
	for _, want := range []string{
		`{"jsonrpc":"2.0","method":"log","params":{"level": "debug", "msg": "a <b> & c"}}`,
		`{"jsonrpc":"2.0","method":"session.state","params":{"state":"recording"}}`,
		`{"jsonrpc":"2.0","method":"empty","params":null}`,
	} {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSuffix(line, "\n"); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	}
}

// A subscriber that falls behind loses the events past the 1024 that wait
// for it, while the client keeps reading, and is told how many it lost. The
// events come straight down the socket, as the server writes them: a
// server's own queue drops a client too slow for it, which on Windows'
// slower sockets would come first.
func TestASubscriberThatFallsBehindCountsWhatItLost(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line := []byte(`{"jsonrpc":"2.0","method":"session.state","params":{"state":"recording"}}` + "\n")
		for range 1100 {
			if _, err := conn.Write(line); err != nil {
				return
			}
		}
		time.Sleep(5 * time.Second)
	}()
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for c.Dropped() != 76 {
		if time.Now().After(deadline) {
			t.Fatalf("%d events lost, want 1100 - 1024 = 76", c.Dropped())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(c.Subscribe()); got != 1024 {
		t.Fatalf("%d events wait for the subscriber, want 1024", got)
	}
}
