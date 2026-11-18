package safenet

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// brokenWrites panics as macOS's write makes Go panic once it returns its
// own system call number, while broken is set.
type brokenWrites struct {
	net.Conn
	broken *atomic.Bool
}

func (c brokenWrites) Write(p []byte) (int, error) {
	if c.broken.Load() {
		panic("invalid return from write: got 33554436 from a write of 44")
	}
	return c.Conn.Write(p)
}

// A write that panics under an HTTP/2 request fails that request, and the
// test program, like the window, keeps running.
func TestAPanickingWriteFailsTheRequest(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	var broken atomic.Bool
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := newTransport(func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return brokenWrites{c, &broken}, nil
	})
	tr.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("the request went over %s, want HTTP/2", resp.Proto)
	}

	broken.Store(true)
	resp, err = client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("the request succeeded over a connection whose writes panic")
	}

	broken.Store(false)
	resp, err = client.Get(srv.URL)
	if err != nil {
		t.Fatalf("the next request failed: %v", err)
	}
	resp.Body.Close()
}

type panics struct {
	net.Conn
	value any
}

func (c panics) Write([]byte) (int, error) { panic(c.value) }

// Only the impossible count is caught: any other panic in a write is a bug
// and still ends the program.
func TestOtherPanicsGoOn(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	if _, err := (conn{panics{client, "invalid return from write: got 48 from a write of 45"}}).Write([]byte("x")); err == nil {
		t.Fatal("the impossible count did not fail the write")
	}
	defer func() {
		if r := recover(); r != "something else" {
			t.Fatalf("recovered %v, want the other panic to go on", r)
		}
	}()
	conn{panics{client, "something else"}}.Write([]byte("x"))
	t.Fatal("the other panic was swallowed")
}
