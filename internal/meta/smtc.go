package meta

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// smtcSession is one read of the target's media session.
type smtcSession struct {
	// Status is playing, paused or stopped.
	Status string
	// Position is where the player was at Updated, its last report.
	Position time.Duration
	Updated  time.Time
	Length   time.Duration

	Title, Artist, Album, AlbumArtist string
	TrackNumber                       int
	// Thumbnail reads the cover; nil when the session has none. It reads
	// this session's cover only until the next read.
	Thumbnail func() ([]byte, error)
}

// smtcAPI is Windows' media session API (SMTC), which the player
// publishes what it plays to. Tests replace it.
type smtcAPI interface {
	// session reads the first session whose app id, which holds the
	// executable or package name, contains one of names; nil when none does.
	session(names []string) (*smtcSession, error)
	// control sends cmd to that session: ErrNoPlayer when there is none,
	// errRefused when the player said no.
	control(names []string, cmd proto.PlayerCmd) error
	// answers says whether the API gives its session manager.
	answers() error
}

// SMTCAnswers says whether Windows' media session API answers: without
// it, nothing tells what plays.
func SMTCAnswers() error {
	api, err := openSMTC()
	if err != nil {
		return err
	}
	return api.answers()
}

var errRefused = errors.New("the player refused")

// openSMTC gives the media session API; tests replace it.
var openSMTC = newSMTCAPI

// SMTC reads what plays on Windows through the media session API.
type SMTC struct {
	Interval time.Duration
	Logf     func(format string, args ...any)
	// Mixer is the player's volume in the Windows audio sessions; the
	// media session API has none of its own.
	Mixer interface {
		Volume() (float64, error)
		SetVolume(v float64) error
	}

	mu    sync.Mutex
	names []string
	ask   restartAsk
}

func NewSMTC() *SMTC { return &SMTC{} }

// Target sets the player; its process names find its media session.
func (m *SMTC) Target(t target.Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.names = nil
	for _, n := range t.ProcessNames {
		if n = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(n)), ".exe"); n != "" {
			m.names = append(m.names, n)
		}
	}
}

func (m *SMTC) current() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.names...)
}

// Watch reads the session at each interval and reports changes.
func (m *SMTC) Watch(ctx context.Context, t target.Target) (<-chan Event, error) {
	m.Target(t)
	names := m.current()
	if len(names) == 0 {
		return nil, ErrNoPlayer
	}
	api, err := openSMTC()
	if err != nil {
		return nil, err
	}
	interval := m.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	out := make(chan Event, 64)
	if m.Mixer != nil {
		go watchMixer(ctx, m.Mixer, mixerInterval, out)
	}
	// SMTC lengths are whole milliseconds.
	tracker := &pollTracker{unit: "ms", ask: &m.ask}
	go func() {
		var p smtcPoller
		tick := time.NewTicker(interval)
		defer tick.Stop()
		lastErr := ""
		for {
			poll := p.poll(api, names, time.Now())
			if poll.Error != lastErr {
				lastErr = poll.Error
				if poll.Error != "" && m.Logf != nil {
					m.Logf("now playing: %s", poll.Error)
				}
			}
			for _, e := range tracker.next(poll) {
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
			select {
			case <-tick.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// smtcPoller turns session reads into polls, in the shape the macOS poller
// gives, so pollTracker turns both into events. SMTC has no track id and
// reports the position as of its last update; the cover is a thumbnail,
// saved to a temporary file once per track.
type smtcPoller struct {
	lastKey, cover, coverFile, coverKey string
}

func (p *smtcPoller) poll(api smtcAPI, names []string, now time.Time) playerPoll {
	out := playerPoll{at: now}
	// A read that fails after finding the session still gives what it read.
	s, err := api.session(names)
	if err != nil {
		out.Error = err.Error()
	}
	if s == nil {
		return out
	}
	out.Running = true
	out.State = s.Status
	pos := s.Position
	if s.Status == "playing" && !s.Updated.IsZero() {
		pos += now.Sub(s.Updated)
	}
	secs := max(0, pos.Seconds())
	out.Position = &secs
	if s.Title == "" {
		return out
	}
	key := s.Artist + "|" + s.Album + "|" + s.Title
	if key != p.lastKey {
		p.lastKey, p.cover, p.coverKey = key, "", ""
	}
	// The thumbnail often comes a moment after the title; a cover that
	// cannot be read is tried once per track and never costs the track.
	if p.cover == "" && s.Thumbnail != nil && p.coverKey != key {
		p.coverKey = key
		p.removeCover()
		if u, err := p.saveCover(s.Thumbnail); err != nil {
			out.Error = "cover: " + err.Error()
		} else {
			p.cover = u
		}
	}
	length := math.Round(float64(s.Length) / float64(time.Millisecond))
	out.Track = &polledTrack{
		Name: s.Title, Artist: s.Artist, Album: s.Album, AlbumArtist: s.AlbumArtist,
		Duration: &length, TrackNumber: s.TrackNumber, ArtworkURL: p.cover,
	}
	return out
}

func (p *smtcPoller) saveCover(read func() ([]byte, error)) (string, error) {
	data, err := read()
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "spytify-cover-")
	if err != nil {
		return "", err
	}
	p.coverFile = f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		p.removeCover()
		return "", err
	}
	if err := f.Close(); err != nil {
		p.removeCover()
		return "", err
	}
	return fileURL(p.coverFile), nil
}

func (p *smtcPoller) removeCover() {
	if p.coverFile != "" {
		os.Remove(p.coverFile)
		p.coverFile = ""
	}
}

// fileURL is path as a file URL: file:///C:/Users/... on Windows.
func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// mixerInterval is how often the player's Volume Mixer slider is read: each
// read runs the capture helper once.
var mixerInterval = 2 * time.Second

// watchMixer reports the player's Volume Mixer slider, as the media session
// API has no volume: when first read and each time it changes.
func watchMixer(ctx context.Context, mixer interface{ Volume() (float64, error) }, every time.Duration, out chan<- Event) {
	known, last := false, 0.0
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if v, err := mixer.Volume(); err == nil && (!known || v != last) {
			known, last = true, v
			select {
			case out <- Event{Kind: VolumeChanged, Volume: v, PositionMs: -1}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}

func (m *SMTC) Control(cmd proto.PlayerCmd) error {
	switch cmd {
	case proto.CmdPlay, proto.CmdPause, proto.CmdNext, proto.CmdPrevious, proto.CmdRestart:
	default:
		return ErrUnsupported
	}
	names := m.current()
	if len(names) == 0 {
		return ErrNoPlayer
	}
	api, err := openSMTC()
	if err != nil {
		return err
	}
	if cmd == proto.CmdRestart {
		// Armed first: a poll may see the restart before the call
		// returns.
		m.ask.arm(time.Now())
	}
	err = api.control(names, cmd)
	if err != nil && cmd == proto.CmdRestart {
		m.ask.disarm()
	}
	switch {
	case errors.Is(err, errRefused):
		return fmt.Errorf("the player refused %q", cmd)
	case err != nil:
		return err
	}
	return nil
}

func (m *SMTC) Volume() (float64, error) {
	if m.Mixer == nil {
		return 0, ErrUnsupported
	}
	return m.Mixer.Volume()
}

func (m *SMTC) SetVolume(v float64) error {
	if m.Mixer == nil {
		return ErrUnsupported
	}
	return m.Mixer.SetVolume(v)
}
