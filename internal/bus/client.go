package bus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

// Client is one connection to the core. Do sends an action and waits for
// its result; Subscribe delivers every event the core broadcasts.
type Client struct {
	conn net.Conn

	wmu  sync.Mutex
	enc  *json.Encoder
	next int64

	mu      sync.Mutex
	pending map[int64]chan proto.Message
	events  chan proto.Event
	err     error
	done    chan struct{}
}

// ErrClosed is returned once the connection is gone.
var ErrClosed = errors.New("bus: connection closed")

// Dial connects to a socket path, or to host:port when addr has the form
// "tcp://host:port".
func Dial(ctx context.Context, addr string) (*Client, error) {
	network := "unix"
	if strings.HasPrefix(addr, "tcp://") {
		network, addr = "tcp", strings.TrimPrefix(addr, "tcp://")
	}
	var d net.Dialer
	nc, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:    nc,
		enc:     json.NewEncoder(nc),
		pending: map[int64]chan proto.Message{},
		events:  make(chan proto.Event, 1024),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *Client) readLoop() {
	r := bufio.NewReaderSize(c.conn, 64<<10)
	var err error
	for {
		var line []byte
		line, err = readLine(r)
		if err != nil {
			break
		}
		var m proto.Message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch {
		case m.IsNotification():
			select {
			case c.events <- proto.Event{Method: m.Method, Params: m.Params}:
			default:
				// A subscriber that does not read loses events, never the
				// connection.
			}
		case m.ID != nil:
			c.mu.Lock()
			ch := c.pending[*m.ID]
			delete(c.pending, *m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	c.mu.Lock()
	c.err = ErrClosed
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.events)
	close(c.done)
}

// Do sends method with params and returns the raw result.
func (c *Client) Do(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	ch := make(chan proto.Message, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return nil, c.err
	}
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()

	c.wmu.Lock()
	err := c.enc.Encode(proto.Message{JSONRPC: proto.JSONRPCVersion, ID: &id, Method: method, Params: raw})
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Call is Do with the result decoded into out (which may be nil).
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	raw, err := c.Do(ctx, method, params)
	if err != nil || out == nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// Subscribe returns the event stream; it is closed with the connection.
func (c *Client) Subscribe() <-chan proto.Event { return c.events }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// DialRetry dials until it succeeds, backing off up to max between tries,
// or ctx ends. The UI uses it to reconnect after starting the daemon.
func DialRetry(ctx context.Context, addr string, max time.Duration) (*Client, error) {
	wait := 100 * time.Millisecond
	for {
		c, err := Dial(ctx, addr)
		if err == nil {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(wait):
		}
		if wait *= 2; wait > max {
			wait = max
		}
	}
}
