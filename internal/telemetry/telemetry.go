// Package telemetry sends the developers what the user agreed to send: a
// crash report or the checks that fail, each on a click, and, with
// privacy.anonymous_stats on, anonymous counts, timings and the problems
// the app runs into. It goes to the developers' Sentry project, set at
// build time; a build without one sends nothing.
//
// Nothing sent identifies the user: there is no install ID and no host
// name, the IP address only lets Sentry tell the country (the project
// keeps no IP), and the home folder, the user name and the names of the
// tracks are replaced before anything leaves.
package telemetry

import (
	"context"
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

// Prefs keeps the little this package remembers between runs: the day of
// the last count, the steps and problems already reported, the crashes
// already handled. Fyne's preferences fit.
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
	// TracesSampleRate is the share of timings sent, 0.1 when zero.
	TracesSampleRate float64
	// ProblemSampleRate is the share of installs that send a problem's
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
	dailyMu sync.Mutex
	stats   *sentry.Hub // set while anonymous stats are on
	log     sentry.Logger
	stop    chan struct{}
	seen    map[string]bool
}

// New makes a client; anonymous stats stay off until SetStats(true).
func New(opts Options) *Client {
	dsn := opts.DSN
	if dsn == "" {
		dsn = os.Getenv("SPYTIFY_SENTRY_DSN")
	}
	if dsn == "" {
		dsn = DSN
	}
	if opts.TracesSampleRate == 0 {
		opts.TracesSampleRate = 0.1
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
	return &Client{opts: opts, dsn: dsn, scrub: NewScrubber(opts.Names), seen: map[string]bool{}}
}

// Available says this build can send anything at all.
func (c *Client) Available() bool { return c != nil && c.dsn != "" }

// Stats says anonymous stats are on.
func (c *Client) Stats() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats != nil
}

// SetStats turns anonymous stats on or off. Off, the Sentry client is
// closed: nothing is collected, not even breadcrumbs.
func (c *Client) SetStats(on bool) {
	if !c.Available() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case on && c.stats == nil:
		hub, err := c.newHub()
		if err != nil {
			return
		}
		c.stats = hub
		c.log = sentry.NewLogger(sentry.SetHubOnContext(context.Background(), hub))
		c.stop = make(chan struct{})
		go c.daily(c.stop)
	case !on && c.stats != nil:
		close(c.stop)
		c.stats.Flush(2 * time.Second)
		c.stats.Client().Close()
		c.stats, c.log, c.stop = nil, nil, nil
	}
}

// Flush waits for what is queued to leave.
func (c *Client) Flush(timeout time.Duration) bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	hub := c.stats
	c.mu.Unlock()
	if hub == nil {
		return true
	}
	return hub.Flush(timeout)
}

// Close sends what is queued and stops.
func (c *Client) Close() { c.SetStats(false) }

func (c *Client) newHub() (*sentry.Hub, error) {
	cl, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:                   c.dsn,
		Release:               "spytify@" + strings.TrimPrefix(proto.Version, "v"),
		Environment:           environment(proto.Version),
		SampleRate:            1,
		EnableTracing:         true,
		TracesSampleRate:      c.opts.TracesSampleRate,
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
	// "{{auto}}" lets Sentry find the country from the connection; the
	// project is set to keep no IP address.
	scope.SetUser(sentry.User{IPAddress: "{{auto}}"})
	return sentry.NewHub(cl, scope), nil
}

// environment tells development builds, such as CI's 2.0.0-alpha+3f2c1d,
// apart from releases.
func environment(version string) string {
	if strings.Contains(version, "dev") || strings.Contains(version, "+") {
		return "development"
	}
	return "production"
}

// hub is the stats hub, or nil while anonymous stats are off.
func (c *Client) hub() *sentry.Hub {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// oneShot is the hub for something the user clicked to send: the stats
// hub when it runs, with its breadcrumbs, otherwise a client for this one
// send, closed by done.
func (c *Client) oneShot() (hub *sentry.Hub, done func() bool, err error) {
	if hub := c.hub(); hub != nil {
		return hub, func() bool { return hub.Flush(10 * time.Second) }, nil
	}
	hub, err = c.newHub()
	if err != nil {
		return nil, nil, err
	}
	return hub, func() bool {
		ok := hub.Flush(10 * time.Second)
		hub.Client().Close()
		return ok
	}, nil
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
