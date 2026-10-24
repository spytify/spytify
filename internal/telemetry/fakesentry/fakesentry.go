// Package fakesentry is a Sentry server for tests: it keeps the envelopes
// it receives, item by item.
package fakesentry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Item is one item of an envelope: an event, a transaction, logs,
// an attachment, a feedback.
type Item struct {
	Type    string
	Header  map[string]any
	Payload []byte
}

// JSON decodes the payload.
func (i Item) JSON() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(i.Payload, &m)
	return m
}

// Server records what reaches it.
type Server struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests int
	raw      [][]byte
	items    []Item
}

// New starts a server, stopped when the test ends.
func New(t testing.TB) *Server {
	s := &Server{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

// DSN points a client at this server.
func (s *Server) DSN() string {
	return "http://public@" + strings.TrimPrefix(s.srv.URL, "http://") + "/1"
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	items := parse(body)
	s.mu.Lock()
	s.requests++
	s.raw = append(s.raw, body)
	s.items = append(s.items, items...)
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// parse splits an envelope into its items.
func parse(body []byte) []Item {
	r := bufio.NewReader(bytes.NewReader(body))
	if _, err := r.ReadBytes('\n'); err != nil {
		return nil
	}
	var items []Item
	for {
		line, err := r.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if err != nil {
				return items
			}
			continue
		}
		var h map[string]any
		if json.Unmarshal(line, &h) != nil {
			return items
		}
		var payload []byte
		if n, ok := h["length"].(float64); ok {
			payload = make([]byte, int(n))
			if _, err := io.ReadFull(r, payload); err != nil {
				return items
			}
			_, _ = r.ReadBytes('\n')
		} else {
			payload, _ = r.ReadBytes('\n')
			payload = bytes.TrimSpace(payload)
		}
		typ, _ := h["type"].(string)
		items = append(items, Item{Type: typ, Header: h, Payload: payload})
		if err != nil {
			return items
		}
	}
}

// Requests counts the requests received.
func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// Raw is everything received, as sent.
func (s *Server) Raw() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(bytes.Join(s.raw, []byte("\n")))
}

// Items are the items of one type received so far.
func (s *Server) Items(typ string) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Item
	for _, it := range s.items {
		if it.Type == typ {
			out = append(out, it)
		}
	}
	return out
}

// Wait waits for n items of a type, and fails the test after 5 s.
func (s *Server) Wait(t testing.TB, typ string, n int) []Item {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if items := s.Items(typ); len(items) >= n {
			return items
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d %s items, want %d", len(s.Items(typ)), typ, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Logs are the log lines received, each with its attributes' values.
func (s *Server) Logs() []Log {
	var out []Log
	for _, it := range s.Items("log") {
		var batch struct {
			Items []struct {
				Body       string                    `json:"body"`
				Attributes map[string]map[string]any `json:"attributes"`
			} `json:"items"`
		}
		_ = json.Unmarshal(it.Payload, &batch)
		for _, l := range batch.Items {
			attrs := map[string]string{}
			for k, v := range l.Attributes {
				attrs[k] = valueString(v["value"])
			}
			out = append(out, Log{Body: l.Body, Attrs: attrs})
		}
	}
	return out
}

// Log is one log line.
type Log struct {
	Body  string
	Attrs map[string]string
}

func valueString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
