// Package bus carries the messaging contract between the core and its
// clients: newline-delimited JSON-RPC 2.0 over a Unix socket (or TCP for a
// web client). Actions are requests; events are notifications broadcast to
// every connected client.
package bus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

const (
	maxLine     = 1 << 20
	sendBacklog = 512
)

// Handler answers one request. An error of type *proto.Error is sent as is;
// any other error becomes an internal error.
type Handler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// Server accepts clients on one or more listeners.
type Server struct {
	handle Handler
	logf   func(format string, args ...any)

	mu        sync.Mutex
	listeners []net.Listener
	conns     map[*conn]struct{}
	closed    bool
	wg        sync.WaitGroup
}

// NewServer serves handle; logf may be nil.
func NewServer(handle Handler, logf func(format string, args ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{handle: handle, logf: logf, conns: map[*conn]struct{}{}}
}

// ListenUnix listens on a socket path, replacing a stale socket left by a
// crashed daemon but refusing to steal one a live daemon answers on.
func (s *Server) ListenUnix(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, 500*time.Millisecond); err == nil {
			c.Close()
			return fmt.Errorf("another spytifyd is listening on %s", path)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}
	s.add(ln)
	return nil
}

// ListenTCP listens on a TCP address, for a web UI on the same machine.
func (s *Server) ListenTCP(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.add(ln)
	return nil
}

func (s *Server) add(ln net.Listener) {
	s.mu.Lock()
	s.listeners = append(s.listeners, ln)
	s.mu.Unlock()
	s.wg.Add(1)
	go s.accept(ln)
}

// Addrs lists the listening addresses.
func (s *Server) Addrs() []net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []net.Addr
	for _, ln := range s.listeners {
		out = append(out, ln.Addr())
	}
	return out
}

func (s *Server) accept(ln net.Listener) {
	defer s.wg.Done()
	for {
		nc, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.logf("bus: accept: %v", err)
			}
			return
		}
		c := &conn{srv: s, nc: nc, out: make(chan []byte, sendBacklog), done: make(chan struct{})}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			nc.Close()
			return
		}
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		go c.writeLoop()
		go c.readLoop()
	}
}

// Broadcast sends a notification to every client. A client that cannot
// keep up is disconnected rather than allowed to stall the core.
func (s *Server) Broadcast(method string, params any) {
	raw, err := json.Marshal(params)
	if err != nil {
		s.logf("bus: event %s: %v", method, err)
		return
	}
	line, err := json.Marshal(proto.Message{JSONRPC: proto.JSONRPCVersion, Method: method, Params: raw})
	if err != nil {
		return
	}
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.send(line, true)
	}
}

// Close stops accepting and disconnects every client.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	listeners := s.listeners
	s.listeners = nil
	conns := s.conns
	s.conns = map[*conn]struct{}{}
	s.mu.Unlock()
	for _, ln := range listeners {
		ln.Close()
		if ln.Addr().Network() == "unix" {
			os.Remove(ln.Addr().String())
		}
	}
	for c := range conns {
		c.close()
	}
	s.wg.Wait()
	return nil
}

type conn struct {
	srv  *Server
	nc   net.Conn
	out  chan []byte
	once sync.Once
	done chan struct{}
}

func (c *conn) close() {
	c.once.Do(func() {
		close(c.done)
		c.nc.Close()
		c.srv.mu.Lock()
		delete(c.srv.conns, c)
		c.srv.mu.Unlock()
	})
}

// send queues a line; a full backlog drops the client when dropSlow is set.
func (c *conn) send(line []byte, dropSlow bool) {
	select {
	case <-c.done:
	case c.out <- line:
	default:
		if dropSlow {
			c.srv.logf("bus: client %s too slow, disconnected", c.nc.RemoteAddr())
			go c.close()
			return
		}
		select {
		case c.out <- line:
		case <-c.done:
		}
	}
}

func (c *conn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case line := <-c.out:
			if _, err := c.nc.Write(line); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *conn) readLoop() {
	defer c.close()
	r := bufio.NewReaderSize(c.nc, 64<<10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		line, err := readLine(r)
		if err != nil {
			return
		}
		if len(line) == 0 {
			continue
		}
		var m proto.Message
		if err := json.Unmarshal(line, &m); err != nil {
			c.reply(proto.Message{JSONRPC: proto.JSONRPCVersion, ID: new(int64), Error: proto.Errorf(proto.CodeParseError, "parse error: %v", err)})
			continue
		}
		if m.JSONRPC != proto.JSONRPCVersion || !m.IsRequest() {
			if m.ID != nil {
				c.reply(proto.Message{JSONRPC: proto.JSONRPCVersion, ID: m.ID, Error: proto.Errorf(proto.CodeInvalidRequest, "expected a JSON-RPC 2.0 request")})
			}
			continue
		}
		go c.serve(ctx, m)
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, fmt.Errorf("bus: line longer than %d bytes", maxLine)
		}
		if !isPrefix {
			return line, nil
		}
	}
}

func (c *conn) serve(ctx context.Context, m proto.Message) {
	result, err := c.srv.handle(ctx, m.Method, m.Params)
	resp := proto.Message{JSONRPC: proto.JSONRPCVersion, ID: m.ID}
	if err != nil {
		var perr *proto.Error
		if !errors.As(err, &perr) {
			perr = &proto.Error{Code: proto.CodeInternalError, Message: err.Error()}
		}
		resp.Error = perr
	} else {
		raw, merr := json.Marshal(result)
		if merr != nil {
			resp.Error = &proto.Error{Code: proto.CodeInternalError, Message: merr.Error()}
		} else {
			resp.Result = raw
		}
	}
	c.reply(resp)
}

func (c *conn) reply(m proto.Message) {
	line, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.send(append(line, '\n'), false)
}
