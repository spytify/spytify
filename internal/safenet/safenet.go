// Package safenet is the HTTP transport every outbound request uses: a
// panic in a connection's write becomes that request's error.
//
// On macOS, write can return its own system call number instead of a byte
// count when a VPN or the packet filter changes the routes under an open
// connection (golang/go#61060). Go panics on it, in a goroutine of its own
// HTTP/2 client that no recover in the program reaches, and the program
// closes. Each connection is opened here, so its Write is on that
// goroutine's stack.
package safenet

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

var transport = newTransport((&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext)

// Transport is the round tripper for every HTTP client; it is
// http.DefaultTransport otherwise.
func Transport() http.RoundTripper { return transport }

// Client is an HTTP client on Transport that gives up after timeout.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: transport, Timeout: timeout}
}

func newTransport(dial dialFunc) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return conn{c}, nil
	}
	return t
}

type conn struct{ net.Conn }

// badWrite starts the panic internal/poll raises when write returns more
// than it was given; any other panic is a bug and goes on.
const badWrite = "invalid return from write"

// Write closes the connection when write returned an impossible count: how
// much of p left is unknown, so nothing more can go on it.
func (c conn) Write(p []byte) (n int, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if s, ok := r.(string); !ok || !strings.HasPrefix(s, badWrite) {
			panic(r)
		}
		c.Conn.Close()
		n, err = 0, fmt.Errorf("the connection broke: %v", r)
	}()
	return c.Conn.Write(p)
}
