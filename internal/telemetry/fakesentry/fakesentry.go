// Package fakesentry is a Sentry server for tests: it keeps the envelopes
// it receives, item by item, and fails the test on one Sentry would
// refuse.
package fakesentry

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
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
	t        testing.TB
	srv      *httptest.Server
	mu       sync.Mutex
	requests int
	raw      [][]byte
	items    []Item
}

// New starts a server, stopped when the test ends.
func New(t testing.TB) *Server {
	s := &Server{t: t}
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
	header, items := parse(body)
	s.mu.Lock()
	s.requests++
	s.raw = append(s.raw, body)
	s.items = append(s.items, items...)
	s.mu.Unlock()
	if problems := check(r, header, items); len(problems) > 0 {
		for _, p := range problems {
			s.t.Errorf("Sentry would refuse this envelope: %s", p)
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// check holds an envelope to what Sentry takes: the project's key, and a
// feedback as develop.sentry.dev/sdk/telemetry/feedbacks describes it,
// since the app builds that one by hand.
func check(r *http.Request, header map[string]any, items []Item) []string {
	var problems []string
	if !strings.Contains(r.Header.Get("X-Sentry-Auth"), "sentry_key=public") {
		problems = append(problems, "no sentry_key in X-Sentry-Auth")
	}
	count := map[string]int{}
	for _, it := range items {
		count[it.Type]++
	}
	if count["feedback"] == 0 {
		return problems
	}
	if count["feedback"] > 1 || count["event"] > 0 || count["transaction"] > 0 {
		problems = append(problems, fmt.Sprintf("a feedback shares its envelope: %v", count))
	}
	id, _ := header["event_id"].(string)
	if !isUUID4(id) {
		problems = append(problems, fmt.Sprintf("envelope event_id %q is not a version 4 UUID", id))
	}
	for _, it := range items {
		if it.Type != "feedback" {
			continue
		}
		var p struct {
			EventID   string `json:"event_id"`
			Timestamp any    `json:"timestamp"`
			Platform  string `json:"platform"`
			Contexts  struct {
				Feedback *struct {
					Message           *string `json:"message"`
					AssociatedEventID string  `json:"associated_event_id"`
				} `json:"feedback"`
			} `json:"contexts"`
		}
		if err := json.Unmarshal(it.Payload, &p); err != nil {
			problems = append(problems, "feedback payload: "+err.Error())
			continue
		}
		fb := p.Contexts.Feedback
		switch {
		case p.EventID != id:
			problems = append(problems, fmt.Sprintf("feedback event_id %q is not the envelope's %q", p.EventID, id))
		case p.Timestamp == nil:
			problems = append(problems, "feedback without a timestamp")
		case p.Platform == "":
			problems = append(problems, "feedback without a platform")
		case fb == nil || fb.Message == nil:
			problems = append(problems, "feedback without contexts.feedback.message")
		case strings.TrimSpace(*fb.Message) == "":
			problems = append(problems, "feedback with a blank message")
		case utf8.RuneCountInString(*fb.Message) > 4096:
			problems = append(problems, fmt.Sprintf("feedback message of %d characters, over 4096", utf8.RuneCountInString(*fb.Message)))
		case fb.AssociatedEventID != "" && !isHex32(fb.AssociatedEventID):
			problems = append(problems, fmt.Sprintf("associated_event_id %q is not an event ID", fb.AssociatedEventID))
		}
	}
	return problems
}

func isHex32(s string) bool {
	_, err := hex.DecodeString(s)
	return len(s) == 32 && err == nil
}

func isUUID4(s string) bool {
	return isHex32(s) && s[12] == '4' && strings.ContainsRune("89ab", rune(s[16]))
}

// parse splits an envelope into its header and items.
func parse(body []byte) (map[string]any, []Item) {
	r := bufio.NewReader(bytes.NewReader(body))
	first, err := r.ReadBytes('\n')
	if err != nil {
		return nil, nil
	}
	var header map[string]any
	_ = json.Unmarshal(first, &header)
	var items []Item
	for {
		line, err := r.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if err != nil {
				return header, items
			}
			continue
		}
		var h map[string]any
		if json.Unmarshal(line, &h) != nil {
			return header, items
		}
		var payload []byte
		if n, ok := h["length"].(float64); ok {
			payload = make([]byte, int(n))
			if _, err := io.ReadFull(r, payload); err != nil {
				return header, items
			}
			_, _ = r.ReadBytes('\n')
		} else {
			payload, _ = r.ReadBytes('\n')
			payload = bytes.TrimSpace(payload)
		}
		typ, _ := h["type"].(string)
		items = append(items, Item{Type: typ, Header: h, Payload: payload})
		if err != nil {
			return header, items
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

// Metrics are the metric samples received, each with its attributes'
// values.
func (s *Server) Metrics() []Metric {
	var out []Metric
	for _, it := range s.Items("trace_metric") {
		var batch struct {
			Items []struct {
				Name       string                    `json:"name"`
				Type       string                    `json:"type"`
				Value      float64                   `json:"value"`
				Unit       string                    `json:"unit"`
				Attributes map[string]map[string]any `json:"attributes"`
			} `json:"items"`
		}
		_ = json.Unmarshal(it.Payload, &batch)
		for _, m := range batch.Items {
			attrs := map[string]string{}
			for k, v := range m.Attributes {
				attrs[k] = valueString(v["value"])
			}
			out = append(out, Metric{Name: m.Name, Type: m.Type, Unit: m.Unit, Value: m.Value, Attrs: attrs})
		}
	}
	return out
}

// Metric is one metric sample.
type Metric struct {
	Name, Type, Unit string
	Value            float64
	Attrs            map[string]string
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
