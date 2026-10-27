package fake

import (
	"context"
	"math/rand"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/target"
)

// span is a run of frames in one format: part of an item's audio, a gap, or
// a pause. A span with noData takes time but delivers no frames, like a
// player whose stream goes idle while paused.
type span struct {
	format pcm.Format
	frames int64
	owner  int   // item the span belongs to, -1 for the lead-in
	audio  bool  // item audio rather than silence
	from   int64 // first frame of the item's audio this span plays
	noData bool
}

type scheduled struct {
	pos int64
	seq int
	ev  meta.Event
}

type hook struct {
	pos int64
	fn  func()
}

// Sink receives what the player produces. The in-process backends turn it
// into channels; cmd/fakeplayer turns it into a PipeWire stream and MPRIS.
type Sink interface {
	Chunk(ctx context.Context, c capture.Chunk) error
	Event(ctx context.Context, e meta.Event) error
}

// Player plays a timeline. It is driven by Run; Control may be called from
// any goroutine.
type Player struct {
	tl       *Timeline
	realtime bool
	chunkMs  int64
	// Lead lets a real-time player deliver audio this far ahead of the
	// wall clock, to keep an output buffer filled. The sink is expected to
	// delay events by the same amount.
	Lead time.Duration

	mu      sync.Mutex
	audio   map[int][]byte
	formats map[int]pcm.Format
	spans   []span
	off     int64 // frames already played in spans[0]
	pos     int64 // frames delivered so far
	events  []scheduled
	hooks   []hook
	seq     int
	cmds    chan string
	sent    []string
	volume  float64
	paused  bool
	status  meta.Status
	current meta.TrackInfo
	rng     *rand.Rand
}

// NewPlayer prepares tl. Realtime paces delivery with the wall clock;
// otherwise frames flow as fast as the sink takes them.
func NewPlayer(tl *Timeline, realtime bool) (*Player, error) {
	p := &Player{
		tl:       tl,
		realtime: realtime,
		chunkMs:  10,
		audio:    map[int][]byte{},
		formats:  map[int]pcm.Format{},
		cmds:     make(chan string, 8),
		volume:   1,
		status:   meta.Stopped,
		rng:      rand.New(rand.NewSource(tl.Seed + 7)),
	}
	if tl.Volume != nil {
		p.volume = *tl.Volume
	}
	for i := range tl.Items {
		f, data, err := tl.ItemAudio(i)
		if err != nil {
			return nil, err
		}
		p.audio[i], p.formats[i] = data, f
		if tl.Items[i].LengthMs == 0 {
			tl.Items[i].LengthMs = f.MsFromFrames(int64(len(data) / f.FrameBytes()))
		}
	}
	p.push(0, meta.Event{Kind: meta.TrackChanged, PositionMs: -1})
	p.push(0, meta.Event{Kind: meta.StatusChanged, Status: meta.Stopped, PositionMs: -1})
	p.schedule(0, 0, true)
	return p, nil
}

// Info returns the metadata the player exposes for item i.
func (p *Player) Info(i int) meta.TrackInfo {
	it := p.tl.Items[i]
	return meta.TrackInfo{
		ID:          it.ID,
		Title:       it.Title,
		Artists:     append([]string(nil), it.Artists...),
		Album:       it.Album,
		TrackNumber: it.TrackNumber,
		Year:        it.Year,
		LengthMs:    it.LengthMs,
		ArtURL:      fileURL(it.Art),
	}
}

// fileURL is a file's URL, file:///C:/... on Windows.
func fileURL(path string) string {
	if path == "" {
		return ""
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func (p *Player) push(pos int64, ev meta.Event) {
	p.seq++
	p.events = append(p.events, scheduled{pos: pos, seq: p.seq, ev: ev})
	sort.SliceStable(p.events, func(a, b int) bool {
		if p.events[a].pos != p.events[b].pos {
			return p.events[a].pos < p.events[b].pos
		}
		return p.events[a].seq < p.events[b].seq
	})
}

// schedule appends the spans and events of items[from:] starting at stream
// position base. Notifications never land before base: a skip happens now.
func (p *Player) schedule(from int, base int64, first bool) {
	p.scheduleFrom(from, base, first, false)
}

// scheduleFrom is schedule; restart marks the first item's TrackChanged as
// the same item started over.
func (p *Player) scheduleFrom(from int, base int64, first, restart bool) {
	items := p.tl.Items
	pos := base
	if first && p.tl.LeadInMs > 0 {
		f := p.formats[0]
		n := f.FramesFromMs(p.tl.LeadInMs)
		p.spans = append(p.spans, span{format: f, frames: n, owner: -1})
		pos += n
	}
	at := func(start int64, f pcm.Format, ms int64) int64 {
		x := start + f.FramesFromMs(ms)
		if x < base {
			x = base
		}
		return x
	}
	playing := !first && p.status == meta.Playing
	for i := from; i < len(items); i++ {
		it := items[i]
		f := p.formats[i]
		total := int64(len(p.audio[i]) / f.FrameBytes())
		offset := it.NotifyOffsetMs
		if it.JitterMs > 0 {
			offset += p.rng.Int63n(2*it.JitterMs+1) - it.JitterMs
		}
		notify := at(pos, f, offset)
		if !playing {
			p.push(notify, meta.Event{Kind: meta.StatusChanged, Status: meta.Playing, PositionMs: -1})
			playing = true
		}
		p.push(notify, meta.Event{Kind: meta.TrackChanged, Track: p.Info(i), PositionMs: 0, Restart: restart && i == from})
		pauseAt := f.FramesFromMs(it.PauseAtMs)
		if it.PauseMs > 0 && pauseAt > 0 && pauseAt < total {
			p.spans = append(p.spans, span{format: f, frames: pauseAt, owner: i, audio: true})
			pos += pauseAt
			p.push(at(pos, f, it.PauseNotifyMs), meta.Event{Kind: meta.StatusChanged, Status: meta.Paused, PositionMs: it.PauseAtMs})
			pf := f.FramesFromMs(it.PauseMs)
			if p.tl.PauseSilence {
				p.spans = append(p.spans, span{format: f, frames: pf, owner: i})
				pos += pf
			} else {
				p.spans = append(p.spans, span{format: f, frames: pf, owner: i, noData: true})
			}
			p.push(at(pos, f, it.DesyncMs), meta.Event{Kind: meta.StatusChanged, Status: meta.Playing, PositionMs: it.PauseAtMs})
			p.spans = append(p.spans, span{format: f, frames: total - pauseAt, owner: i, audio: true, from: pauseAt})
			pos += total - pauseAt
		} else if seekAt := f.FramesFromMs(it.SeekAtMs); seekAt > 0 && seekAt < total {
			seekTo := min(f.FramesFromMs(it.SeekToMs), total)
			p.spans = append(p.spans, span{format: f, frames: seekAt, owner: i, audio: true})
			pos += seekAt
			p.push(pos, meta.Event{Kind: meta.Seeked, Track: p.Info(i), PositionMs: it.SeekToMs})
			p.spans = append(p.spans, span{format: f, frames: total - seekTo, owner: i, audio: true, from: seekTo})
			pos += total - seekTo
		} else {
			p.spans = append(p.spans, span{format: f, frames: total, owner: i, audio: true})
			pos += total
		}
		if it.GapMs > 0 {
			// A player opens the next item's stream when this one ends, so
			// the gap is already in the next item's format.
			gf := f
			if i+1 < len(items) {
				gf = p.formats[i+1]
			}
			n := gf.FramesFromMs(it.GapMs)
			p.spans = append(p.spans, span{format: gf, frames: n, owner: i})
			pos += n
		}
	}
	last := p.formats[len(items)-1]
	p.push(pos, meta.Event{Kind: meta.StatusChanged, Status: meta.Stopped, PositionMs: -1})
	p.push(pos, meta.Event{Kind: meta.TrackChanged, PositionMs: -1})
	if p.tl.TailMs > 0 {
		p.spans = append(p.spans, span{format: last, frames: last.FramesFromMs(p.tl.TailMs), owner: len(items) - 1})
	}
}

// At runs fn once the stream reaches ms (in the base format), before the
// events due there and the next chunk, so a command it sends lands where
// the timeline puts it however the goroutines are scheduled. Tests use it
// to send player.control at a given moment; fn must not wait on the player
// (Control only queues).
func (p *Player) At(ms int64, fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hooks = append(p.hooks, hook{pos: p.tl.Format().FramesFromMs(ms), fn: fn})
}

// Control queues a transport command: play, pause, next, previous.
func (p *Player) Control(cmd string) error {
	switch cmd {
	case "play", "pause", "playpause", "next", "previous", "restart":
	default:
		return meta.ErrUnsupported
	}
	p.mu.Lock()
	p.sent = append(p.sent, cmd)
	p.mu.Unlock()
	p.cmds <- cmd
	return nil
}

// Commands lists the transport commands the player was sent, in order,
// whether or not it played on long enough to act on them.
func (p *Player) Commands() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.sent...)
}

func (p *Player) Volume() (float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume, nil
}

func (p *Player) SetVolume(v float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volume = v
	return nil
}

// State is the player's current item and status, for an MPRIS server.
func (p *Player) State() (meta.TrackInfo, meta.Status, float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current, p.status, p.volume
}

func (p *Player) apply(ctx context.Context, sink Sink, cmd string) error {
	p.mu.Lock()
	owner := -1
	if len(p.spans) > 0 {
		owner = p.spans[0].owner
	}
	var immediate []meta.Event
	switch cmd {
	case "next", "previous", "restart":
		next := owner + 1
		if cmd != "next" {
			next = owner
		}
		if next < 0 {
			next = 0
		}
		p.spans, p.off = nil, 0
		p.events = nil
		p.paused = false
		if next < len(p.tl.Items) {
			p.scheduleFrom(next, p.pos, false, next == owner)
		} else {
			p.push(p.pos, meta.Event{Kind: meta.StatusChanged, Status: meta.Stopped, PositionMs: -1})
		}
	case "pause":
		if !p.paused {
			p.paused = true
			immediate = append(immediate, meta.Event{Kind: meta.StatusChanged, Status: meta.Paused, PositionMs: -1})
		}
	case "play":
		if p.paused {
			p.paused = false
			immediate = append(immediate, meta.Event{Kind: meta.StatusChanged, Status: meta.Playing, PositionMs: -1})
		}
	case "playpause":
		p.mu.Unlock()
		if p.isPaused() {
			return p.apply(ctx, sink, "play")
		}
		return p.apply(ctx, sink, "pause")
	}
	p.mu.Unlock()
	for _, ev := range immediate {
		if err := p.deliver(ctx, sink, ev); err != nil {
			return err
		}
	}
	return nil
}

func (p *Player) isPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

func (p *Player) deliver(ctx context.Context, sink Sink, ev meta.Event) error {
	p.mu.Lock()
	switch ev.Kind {
	case meta.TrackChanged:
		p.current = ev.Track
	case meta.StatusChanged:
		p.status = ev.Status
	}
	p.mu.Unlock()
	return sink.Event(ctx, ev)
}

// Run plays the timeline into sink until it ends or ctx is done.
func (p *Player) Run(ctx context.Context, sink Sink) error {
	start := time.Now()
	var played time.Duration
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cmd := <-p.cmds:
			if err := p.apply(ctx, sink, cmd); err != nil {
				return err
			}
			continue
		default:
		}

		p.mu.Lock()
		var due []meta.Event
		for len(p.events) > 0 && p.events[0].pos <= p.pos {
			due = append(due, p.events[0].ev)
			p.events = p.events[1:]
		}
		var fire []func()
		kept := p.hooks[:0]
		for _, h := range p.hooks {
			if h.pos <= p.pos {
				fire = append(fire, h.fn)
			} else {
				kept = append(kept, h)
			}
		}
		p.hooks = kept
		p.mu.Unlock()
		for _, fn := range fire {
			fn()
		}
		for _, ev := range due {
			if err := p.deliver(ctx, sink, ev); err != nil {
				return err
			}
		}

		p.mu.Lock()
		if p.paused {
			f := p.tl.Format()
			if len(p.spans) > 0 {
				f = p.spans[0].format
			}
			p.mu.Unlock()
			if !p.tl.PauseSilence {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case cmd := <-p.cmds:
					if err := p.apply(ctx, sink, cmd); err != nil {
						return err
					}
				}
				start, played = time.Now(), 0
				continue
			}
			n := f.FramesFromMs(p.chunkMs)
			if err := sink.Chunk(ctx, capture.Chunk{Data: make([]byte, n*int64(f.FrameBytes())), Format: f}); err != nil {
				return err
			}
			p.mu.Lock()
			p.pos += n
			for i := range p.events {
				p.events[i].pos += n
			}
			p.mu.Unlock()
			played += time.Duration(p.chunkMs) * time.Millisecond
			p.pace(ctx, start, played)
			continue
		}
		if len(p.spans) == 0 {
			rest := make([]meta.Event, 0, len(p.events))
			for _, e := range p.events {
				rest = append(rest, e.ev)
			}
			p.events = nil
			p.mu.Unlock()
			for _, ev := range rest {
				if err := p.deliver(ctx, sink, ev); err != nil {
					return err
				}
			}
			return nil
		}
		sp := p.spans[0]
		n := sp.format.FramesFromMs(p.chunkMs)
		if left := sp.frames - p.off; n > left {
			n = left
		}
		var data []byte
		if !sp.noData {
			fb := int64(sp.format.FrameBytes())
			if sp.audio {
				a := p.audio[sp.owner]
				from := (sp.from + p.off) * fb
				data = a[from : from+n*fb]
			} else {
				data = make([]byte, n*fb)
			}
		}
		p.off += n
		if p.off >= sp.frames {
			p.spans, p.off = p.spans[1:], 0
		}
		p.mu.Unlock()

		played += time.Duration(sp.format.MsFromFrames(n)) * time.Millisecond
		if data != nil {
			if err := sink.Chunk(ctx, capture.Chunk{Data: data, Format: sp.format}); err != nil {
				return err
			}
			p.mu.Lock()
			p.pos += n
			p.mu.Unlock()
		}
		p.pace(ctx, start, played)
	}
}

func (p *Player) pace(ctx context.Context, start time.Time, played time.Duration) {
	if !p.realtime {
		return
	}
	if wait := time.Until(start.Add(played - p.Lead)); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// Backend adapts a Player to the capture and metadata interfaces, in the
// same process. Frames and events share one goroutine and unbuffered
// channels, so every event arrives exactly between the chunks it sits
// between in the timeline.
type Backend struct {
	p      *Player
	frames chan capture.Chunk
	events chan meta.Event

	mu      sync.Mutex
	opened  bool
	watched bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewBackend wraps p.
func NewBackend(p *Player) *Backend {
	return &Backend{p: p, frames: make(chan capture.Chunk), events: make(chan meta.Event), done: make(chan struct{})}
}

// Player is the wrapped player.
func (b *Backend) Player() *Player { return b.p }

func (b *Backend) Open(ctx context.Context, _ target.Target) (pcm.Format, error) {
	b.mu.Lock()
	b.opened = true
	b.mu.Unlock()
	b.maybeStart()
	return b.p.tl.Format(), nil
}

func (b *Backend) Watch(ctx context.Context, _ target.Target) (<-chan meta.Event, error) {
	b.mu.Lock()
	b.watched = true
	b.mu.Unlock()
	b.maybeStart()
	return b.events, nil
}

func (b *Backend) maybeStart() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.opened || !b.watched || b.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go func() {
		defer close(b.done)
		defer close(b.frames)
		_ = b.p.Run(ctx, chanSink{b})
	}()
}

func (b *Backend) Frames() <-chan capture.Chunk { return b.frames }
func (b *Backend) Overruns() int64              { return 0 }
func (b *Backend) Control(cmd string) error     { return b.p.Control(cmd) }
func (b *Backend) Volume() (float64, error)     { return b.p.Volume() }
func (b *Backend) SetVolume(v float64) error    { return b.p.SetVolume(v) }

func (b *Backend) Close() error {
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		<-b.done
	}
	return nil
}

type chanSink struct{ b *Backend }

func (s chanSink) Chunk(ctx context.Context, c capture.Chunk) error {
	select {
	case s.b.frames <- c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s chanSink) Event(ctx context.Context, e meta.Event) error {
	select {
	case s.b.events <- e:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
