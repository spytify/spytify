// Package telemetry is the Sentry bug tracker. With privacy.bug_tracker
// on, the app sends its crashes, the problems it runs into with the steps
// that led there, its launches and timings, and, on a click, the checks
// that fail. Off, nothing is sent: the Sentry client is not even made. It
// goes to the developers' Sentry project, set at build time; a build
// without one sends nothing.
//
// Reports name the computer only by a hash of its machine ID made for this
// app. There is no host name, the IP address only lets Sentry tell the
// country (the project keeps no IP), and the home folder, the user name
// and the names of the tracks the app knows are replaced before anything
// leaves.
package telemetry

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/spytify/spytify/pkg/proto"
)

// DSN is the developers' Sentry project, set at build time with
// -ldflags "-X github.com/spytify/spytify/internal/telemetry.DSN=…".
// SPYTIFY_SENTRY_DSN replaces it, to try another project.
var DSN string

// Prefs keeps the little this package remembers between runs: the steps
// and problems already reported, the crashes already handled. Fyne's
// preferences fit.
type Prefs interface {
	String(key string) string
	SetString(key, value string)
}

// Options configure a Client; the zero values are what ships.
type Options struct {
	// DSN replaces the build's project (tests).
	DSN   string
	Prefs Prefs
	// Names lists the artists, titles and albums the app knows: they are
	// replaced in everything sent.
	Names func() []string
	// TracesSampleRate is the share of timings sent, 0.01 when zero; every
	// launch is sent.
	TracesSampleRate float64
	// ProblemSampleRate is the share of computers that send a problem's
	// details, 0.1 when zero; the logs count every problem.
	ProblemSampleRate float64
	// Now and Rand replace the clock and the dice (tests).
	Now  func() time.Time
	Rand func() float64
}

// Client sends to Sentry. A nil Client, or one without a DSN, sends
// nothing.
type Client struct {
	opts  Options
	dsn   string
	scrub *Scrubber

	mu      sync.Mutex
	tracker *sentry.Hub // set while the bug tracker is on
	log     sentry.Logger
	seen    map[string]bool
}

var (
	// errOff answers a report made while the bug tracker is off.
	errOff     = errors.New("the Sentry bug tracker is off")
	errNotSent = errors.New("the report did not reach the developers; try again later")
)

// New makes a client; the bug tracker stays off until SetEnabled(true).
func New(opts Options) *Client {
	dsn := opts.DSN
	if dsn == "" {
		dsn = os.Getenv("SPYTIFY_SENTRY_DSN")
	}
	if dsn == "" {
		dsn = DSN
	}
	if opts.TracesSampleRate == 0 {
		opts.TracesSampleRate = 0.01
	}
	if opts.ProblemSampleRate == 0 {
		opts.ProblemSampleRate = 0.1
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rand == nil {
		opts.Rand = randFloat
	}
	if opts.Prefs == nil {
		opts.Prefs = &memPrefs{m: map[string]string{}}
	}
	c := &Client{opts: opts, dsn: dsn, scrub: NewScrubber(opts.Names), seen: map[string]bool{}}
	if c.Available() {
		// Both may run a command: not on the UI's thread when it turns on.
		go func() {
			defer quietly()
			computerID()
			Region()
		}()
	}
	return c
}

// Available says this build can send anything at all.
func (c *Client) Available() bool { return c != nil && c.dsn != "" }

// Enabled says the bug tracker is on.
func (c *Client) Enabled() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tracker != nil
}

// SetEnabled turns the bug tracker on or off. Off, the Sentry client is
// closed: nothing is collected, not even breadcrumbs.
func (c *Client) SetEnabled(on bool) {
	defer quietly()
	if !c.Available() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case on && c.tracker == nil:
		hub, err := c.newHub()
		if err != nil {
			return
		}
		c.tracker = hub
		c.log = sentry.NewLogger(sentry.SetHubOnContext(context.Background(), hub))
	case !on && c.tracker != nil:
		c.tracker.Flush(2 * time.Second)
		c.tracker.Client().Close()
		c.tracker, c.log = nil, nil
	}
}

// Flush waits for what is queued to leave.
func (c *Client) Flush(timeout time.Duration) bool {
	defer quietly()
	hub := c.hub()
	if hub == nil {
		return true
	}
	return hub.Flush(timeout)
}

// Close sends what is queued and stops.
func (c *Client) Close() { c.SetEnabled(false) }

func (c *Client) newHub() (*sentry.Hub, error) {
	cl, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:                   c.dsn,
		Release:               "spytify@" + strings.TrimPrefix(proto.Version, "v"),
		Environment:           environment(proto.Version),
		SampleRate:            1,
		EnableTracing:         true,
		TracesSampler:         c.sample,
		MaxBreadcrumbs:        100,
		BeforeSend:            c.beforeSend,
		BeforeSendTransaction: c.beforeSend,
		BeforeSendLog:         c.beforeSendLog,
		BeforeBreadcrumb:      c.beforeBreadcrumb,
		Tags:                  map[string]string{"os_region": Region()},
	})
	if err != nil {
		return nil, err
	}
	scope := sentry.NewScope()
	scope.SetUser(computerUser())
	return sentry.NewHub(cl, scope), nil
}

// computerUser is who every report comes from: this computer, by its hash, and
// "{{auto}}" so that Sentry tells the country from the connection; the
// project is set to keep no IP address.
func computerUser() sentry.User {
	return sentry.User{ID: computerID(), IPAddress: "{{auto}}"}
}

// sample sends every launch, each one counting the computer with its
// system, version and country, and a share of the other timings.
func (c *Client) sample(ctx sentry.SamplingContext) float64 {
	if ctx.Span.Name == "launch" {
		return 1
	}
	return c.opts.TracesSampleRate
}

// environment tells development builds, such as CI's 2.0.0-alpha+3f2c1d,
// apart from releases.
func environment(version string) string {
	if strings.Contains(version, "dev") || strings.Contains(version, "+") {
		return "development"
	}
	return "production"
}

// quietly lets a call to Sentry fail without the app noticing: what
// cannot be collected or sent is dropped, and the app goes on.
func quietly() { _ = recover() }

// hub is the bug tracker's hub, or nil while it is off.
func (c *Client) hub() *sentry.Hub {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tracker
}

// memPrefs forget everything at exit: for a client made without
// preferences.
type memPrefs struct {
	mu sync.Mutex
	m  map[string]string
}

func (p *memPrefs) String(key string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[key]
}

func (p *memPrefs) SetString(key, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[key] = value
}
