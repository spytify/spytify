package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/spytify/spytify/pkg/proto"
)

// What the bug tracker sends while it is on.

// Breadcrumb notes a step on the way to a problem or a crash: it leaves
// only with one of them.
func (c *Client) Breadcrumb(category, message string, data map[string]any) {
	defer quietly()
	hub := c.hub()
	if hub == nil {
		return
	}
	hub.AddBreadcrumb(&sentry.Breadcrumb{Category: category, Message: message, Data: data, Level: sentry.LevelInfo}, nil)
}

// Log sends one log line, counted in Sentry's Explore > Logs.
func (c *Client) Log(name string, attrs map[string]any) {
	defer quietly()
	if c == nil {
		return
	}
	c.mu.Lock()
	log := c.log
	c.mu.Unlock()
	if log == nil {
		return
	}
	e := log.Info()
	for k, v := range attrs {
		switch v := v.(type) {
		case string:
			e = e.String(k, v)
		case bool:
			e = e.Bool(k, v)
		case int:
			e = e.Int(k, v)
		case int64:
			e = e.Int64(k, v)
		case float64:
			e = e.Float64(k, v)
		case []string:
			e = e.StringSlice(k, v)
		}
	}
	e.Emit(name)
}

const (
	keyStep    = "telemetry.step."
	keyProblem = "telemetry.problem."
)

// Step reports, once per install, that the user got this far: the app
// launched, the recorder ready, a first recording, its first sound, its
// first saved track.
func (c *Client) Step(name string) {
	defer quietly()
	if c.hub() == nil || c.once(keyStep+name) {
		return
	}
	if c.opts.Prefs.String(keyStep+name) != "" {
		return
	}
	c.opts.Prefs.SetString(keyStep+name, c.opts.Now().UTC().Format(time.RFC3339))
	c.Log("step", map[string]any{"step": name})
}

// once says key was already seen in this run.
func (c *Client) once(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[key] {
		return true
	}
	c.seen[key] = true
	return false
}

// Problem reports a problem that is not a crash, such as no sound reaching
// the recorder. Every one is counted in the logs; its details, with the
// breadcrumbs that led to it, go as a Sentry issue once per install and
// version, from a share of the installs.
func (c *Client) Problem(reason, detail string) {
	defer quietly()
	hub := c.hub()
	if hub == nil {
		return
	}
	c.Log("problem", map[string]any{"reason": reason})
	if c.opts.Prefs.String(keyProblem+reason) == proto.Version {
		return
	}
	c.opts.Prefs.SetString(keyProblem+reason, proto.Version)
	if c.opts.Rand() >= c.opts.ProblemSampleRate {
		return
	}
	ev := sentry.NewEvent()
	ev.Level = sentry.LevelWarning
	ev.Message = "blocked: " + reason
	ev.Fingerprint = []string{"blocked", reason}
	ev.Tags["reason"] = reason
	if detail != "" {
		ev.Contexts["problem"] = sentry.Context{"detail": detail}
	}
	hub.CaptureEvent(ev)
}

// Span is one timed step of a Measure.
type Span struct {
	Op         string
	Start, End time.Time
	Data       map[string]any
}

// Measure sends how long something took, from start to end, with its
// steps. A share of them leaves (TracesSampleRate).
func (c *Client) Measure(name string, start, end time.Time, tags map[string]string, spans []Span) {
	defer quietly()
	hub := c.hub()
	if hub == nil {
		return
	}
	ctx := sentry.SetHubOnContext(context.Background(), hub)
	tx := sentry.StartTransaction(ctx, name, sentry.WithOpName(name), sentry.WithTransactionSource(sentry.SourceTask))
	tx.StartTime = start
	for k, v := range tags {
		tx.SetTag(k, v)
	}
	for _, sp := range spans {
		child := tx.StartChild(sp.Op)
		child.StartTime, child.EndTime = sp.Start, sp.End
		for k, v := range sp.Data {
			child.SetData(k, v)
		}
		child.Finish()
	}
	tx.EndTime = end
	tx.Finish()
}

func randFloat() float64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return float64(binary.BigEndian.Uint64(b[:])>>11) / (1 << 53)
}
