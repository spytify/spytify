package core

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"hash/crc32"
	"maps"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/pkg/proto"
)

// The TestPinned tests write down what a session does today, sample by
// sample and event by event, and compare it with their golden files under
// testdata/session. They pin behavior, odd parts included, so that moving
// code around cannot change a cut unnoticed. A change meant to move the
// cuts rewrites them:
//
//	go test ./internal/core -run TestPinned -update
//
// and the diff of testdata/session is then the change in behavior.
var update = flag.Bool("update", false, "rewrite the golden files of the TestPinned tests")

// f44 is the stream format of the scripted cases.
var f44 = pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}

var errRigEnded = errors.New("the session ended")

// rig runs a Session as Core does, with Run on its own goroutine and frames
// and events on unbuffered channels, but hands it one input at a time:
// after each chunk or event it sends an empty chunk, which Run only takes
// once it is done with the input before. What the session does in between
// is written down in order, so a trace is the same on every run. The
// finisher's goroutine is not started: the rig takes each job the session
// hands over and writes it down instead of saving a file.
type rig struct {
	t      *testing.T
	s      *Session
	fin    *finisher
	out    string
	f      pcm.Format // the format of scripted audio
	chunk  int64      // frames per scripted chunk
	frames chan capture.Chunk
	events chan meta.Event
	cancel context.CancelFunc
	done   chan struct{}
	ended  bool
	stuck  bool // Run neither took an input nor ended in time
	header []string
	// played is how many ms of each seed's music the script played.
	played map[int64]int64

	mu       sync.Mutex
	fed      float64 // ms of audio handed to the session so far
	trace    []string
	jobs     []string
	controls []string
	levels   []proto.LevelEvent
	take     string
}

func newRig(t *testing.T, extra map[string]any, opts SessionOptions) *rig {
	t.Helper()
	r := bareRig(t, extra, opts)
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		r.s.Run(ctx, r.frames, r.events)
	}()
	t.Cleanup(func() {
		r.cancel()
		r.wait()
	})
	r.settle()
	return r
}

// bareRig is a rig whose session does not run: a test calls its methods
// itself, for what the wall clock paces.
func bareRig(t *testing.T, extra map[string]any, opts SessionOptions) *rig {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	store, err := settings.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	overrides := map[string]any{"output.dir": out}
	for _, m := range []map[string]any{baseSettings, extra} {
		for k, v := range m {
			overrides[k] = v
		}
	}
	if rej := store.Override(overrides); len(rej) > 0 {
		t.Fatalf("settings rejected: %v", rej)
	}
	if o, ok := extra["output.dir"].(string); ok {
		out = o
		extra = maps.Clone(extra)
		delete(extra, "output.dir")
	}
	r := &rig{t: t, out: out, f: f44, chunk: f44.FramesFromMs(10), frames: make(chan capture.Chunk), events: make(chan meta.Event), done: make(chan struct{})}
	if len(extra) > 0 {
		keys := make([]string, 0, len(extra))
		for k := range extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			r.header = append(r.header, fmt.Sprintf("setting %s = %v", k, extra[k]))
		}
	}
	if opts.EndOnStop {
		r.header = append(r.header, "option EndOnStop")
	}
	if opts.SingleTrack {
		r.header = append(r.header, "option SingleTrack")
	}
	log, _ := NewLogger("", nil)
	log.SetEmitter(r.emit)
	r.fin = &finisher{values: store.Values, emit: r.emit, log: log, jobs: make(chan finishJob, 256), claims: map[string]bool{},
		index: openIndex(filepath.Join(out, ".spytify", "recorded.jsonl"))}
	r.s = newSession("t", sessionDeps{values: store.Values, emit: r.emit, log: log, fin: r.fin, control: r.control, mute: r.mute}, opts)
	return r
}

// lines is the trace so far, without the stamps.
func (r *rig) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, l := range r.trace {
		out = append(out, strings.TrimSpace(l[strings.Index(l, "ms  ")+4:]))
	}
	return out
}

// describe adds a line to the golden file's header.
func (r *rig) describe(format string, args ...any) {
	r.header = append(r.header, fmt.Sprintf(format, args...))
}

func (r *rig) emit(method string, payload any) {
	var line string
	switch p := payload.(type) {
	case proto.LevelEvent:
		// Paced by the wall clock, so kept out of the trace: TestPinnedMeter
		// pins the meter.
		r.mu.Lock()
		r.levels = append(r.levels, p)
		r.mu.Unlock()
		return
	case proto.SessionStateEvent:
		line = "state " + string(p.State)
	case proto.TrackStartedEvent:
		line = fmt.Sprintf("started %s %q artist=%q album=%q length=%dms", p.ID, p.Title, p.Artist, p.Album, p.LengthMs)
		if p.Cut != nil {
			line += fmt.Sprintf(" cut=%s/%s offset=%dms", p.Cut.Kind, p.Cut.By, p.Cut.OffsetMs)
		}
		if p.Source != nil {
			line += fmt.Sprintf(" source=%d/%d", p.Source.SampleRate, p.Source.Channels)
		}
		if p.CoverURL != "" {
			line += " cover"
		}
	case proto.TrackSkippedEvent:
		line = fmt.Sprintf("skipped %s %s", p.ID, p.Reason)
	case proto.ErrorEvent:
		msg := p.Msg
		switch p.Code {
		case "capture_low_quality", "capture_stalled":
			// The advice after the first sentence depends on the system.
			if i := strings.Index(msg, ". "); i >= 0 {
				msg = msg[:i+1] + " …"
			}
		case "output_dir", "temp_file", "write":
			// So does the system's own wording of a file error.
			if i := strings.LastIndex(msg, ": "); i >= 0 {
				msg = msg[:i+2] + "…"
			}
		}
		line = fmt.Sprintf("error %s %q", p.Code, r.clean(msg))
		if p.Fatal {
			line += " fatal"
		}
	case proto.LogEvent:
		line = fmt.Sprintf("log %s: %s", p.Level, r.clean(p.Msg))
	default:
		line = fmt.Sprintf("%s %+v", method, payload)
	}
	r.mu.Lock()
	r.trace = append(r.trace, r.stamp(line))
	r.mu.Unlock()
}

// clean takes the test's folders out of a message.
func (r *rig) clean(s string) string {
	for _, p := range []string{r.out, filepath.ToSlash(r.out)} {
		s = strings.ReplaceAll(s, p, "$OUT")
	}
	return strings.ReplaceAll(s, `\`, "/")
}

// stamp prefixes a trace line with how much audio the session was handed;
// the caller holds mu.
func (r *rig) stamp(line string) string {
	return fmt.Sprintf("%10s  %s", msText(r.fed)+"ms", line)
}

func (r *rig) note(format string, args ...any) {
	r.mu.Lock()
	r.trace = append(r.trace, r.stamp("> "+fmt.Sprintf(format, args...)))
	r.mu.Unlock()
}

func (r *rig) control(cmd string) error {
	r.mu.Lock()
	r.controls = append(r.controls, cmd)
	r.mu.Unlock()
	return nil
}

// sent is the player.control commands the session sent, in order.
func (r *rig) sent() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.controls...)
}

func (r *rig) mute(on bool) error {
	r.mu.Lock()
	r.trace = append(r.trace, r.stamp(fmt.Sprintf("mute %v", on)))
	r.mu.Unlock()
	return nil
}

// rigTimeout is how long the rig waits on the session: far more than any
// case takes, only so that a broken session fails the test instead of
// hanging it.
const rigTimeout = time.Minute

// deliver hands v to Run on ch, taking the jobs meanwhile. It is false
// once Run has returned, or took nothing for rigTimeout.
func deliver[T any](r *rig, ch chan<- T, v T) bool {
	timeout := time.After(rigTimeout)
	for {
		select {
		case ch <- v:
			return true
		case <-r.done:
			r.ended = true
			return false
		case j := <-r.fin.jobs:
			r.addJob(j)
		case <-timeout:
			r.giveUp()
			return false
		}
	}
}

func (r *rig) giveUp() {
	r.mu.Lock()
	trace := strings.Join(r.trace, "\n")
	r.mu.Unlock()
	r.t.Errorf("the session neither took an input nor ended for %s; trace so far:\n%s", rigTimeout, trace)
	r.ended, r.stuck = true, true
}

// settle waits until Run is done with the last input, and writes down the
// take when it changed.
func (r *rig) settle() bool {
	ok := deliver(r, r.frames, capture.Chunk{})
	r.look()
	return ok
}

// wait takes the jobs until Run returns.
func (r *rig) wait() {
	timeout := time.After(rigTimeout)
	for {
		select {
		case <-r.done:
			r.ended = true
			return
		case j := <-r.fin.jobs:
			r.addJob(j)
		case <-timeout:
			r.giveUp()
			return
		}
	}
}

// look writes the current take down when it changed. Run is then waiting
// for the next input, or has returned.
func (r *rig) look() {
	if r.stuck {
		return
	}
	line := takeLine(r.s.cur)
	r.mu.Lock()
	defer r.mu.Unlock()
	if line != r.take {
		r.take = line
		r.trace = append(r.trace, r.stamp(line))
	}
}

func takeLine(c *segment) string {
	if c == nil {
		return "take none"
	}
	line := fmt.Sprintf("take %s %q start=%s", c.id, c.info.Title, frameText(c.format, c.start))
	if c.origin != c.start {
		line += " origin=" + frameText(c.format, c.origin)
	}
	line += " length=" + frameText(c.format, c.length)
	for _, fl := range []struct {
		on   bool
		name string
	}{
		{c.ghost, "ghost"},
		{c.provisional, "provisional"},
		{c.awaitStart, "awaiting-sound"},
		{c.partial, "partial"},
		{c.w == nil && !c.ghost && !c.provisional, "no-file"},
	} {
		if fl.on {
			line += " " + fl.name
		}
	}
	if c.reason != "" {
		line += " reason=" + string(c.reason)
	}
	if c.format != f44 {
		line += fmt.Sprintf(" format=%d/%d", c.format.Rate, c.format.Channels)
	}
	return line
}

func (r *rig) addJob(j finishJob) {
	defer r.fin.wg.Done()
	wav := "no file"
	if f, data, err := pcm.ReadWAV(j.tmp); err == nil {
		wav = fmt.Sprintf("wav=%d crc=%08x", int64(len(data)/f.FrameBytes()), crc32.ChecksumIEEE(data))
	}
	line := fmt.Sprintf("job %s %q played=%s length=%s %s", j.id, j.info.Title, frameText(j.format, j.played), frameText(j.format, j.length), wav)
	if j.partial {
		line += " partial"
	}
	if j.rejected != "" {
		line += " rejected=" + string(j.rejected)
	}
	if j.format != f44 {
		line += fmt.Sprintf(" format=%d/%d", j.format.Rate, j.format.Channels)
	}
	if j.info.ArtURL != "" {
		line += " cover=" + path.Base(j.info.ArtURL)
	}
	r.mu.Lock()
	r.jobs = append(r.jobs, line)
	r.mu.Unlock()
}

// feed hands one chunk to the session.
func (r *rig) feed(c capture.Chunk) bool {
	if r.ended {
		return false
	}
	if c.Format.Valid() {
		r.mu.Lock()
		r.fed += float64(len(c.Data)/c.Format.FrameBytes()) * 1000 / float64(c.Format.Rate)
		r.mu.Unlock()
	}
	return deliver(r, r.frames, c) && r.settle()
}

// send hands one player event to the session.
func (r *rig) send(ev meta.Event) bool {
	if r.ended {
		return false
	}
	ring := "-"
	if r.s.ring != nil {
		ring = strconv.FormatInt(r.s.ring.End(), 10)
	}
	r.note("player %s (ring at %s)", eventText(ev), ring)
	return deliver(r, r.events, ev) && r.settle()
}

func eventText(ev meta.Event) string {
	switch ev.Kind {
	case meta.TrackChanged:
		if ev.Track.Empty() {
			return fmt.Sprintf("TrackChanged none pos=%d", ev.PositionMs)
		}
		s := fmt.Sprintf("TrackChanged %q id=%s length=%dms pos=%d", ev.Track.Title, ev.Track.ID, ev.Track.LengthMs, ev.PositionMs)
		if ev.Restart {
			s += " restart"
		}
		return s
	case meta.StatusChanged:
		return fmt.Sprintf("%s pos=%d", ev.Status, ev.PositionMs)
	case meta.PlayerGone:
		return "gone"
	case meta.PlayerAppeared:
		return "appeared"
	case meta.Seeked:
		return fmt.Sprintf("Seeked %q pos=%d", ev.Track.Title, ev.PositionMs)
	}
	return fmt.Sprintf("kind %d", ev.Kind)
}

// play feeds data, in the rig's format, in chunks of the rig's size.
func (r *rig) play(what string, data []byte) {
	if r.ended {
		return
	}
	fb := int64(r.f.FrameBytes())
	n := int64(len(data)) / fb
	r.note("audio %s, %s", what, frameText(r.f, n))
	for i := int64(0); i < n; i += r.chunk {
		if !r.feed(capture.Chunk{Data: data[i*fb : min(i+r.chunk, n)*fb], Format: r.f}) {
			return
		}
	}
}

// setFormat makes the scripted audio from now on come in format f, as a
// stream made again by the player.
func (r *rig) setFormat(f pcm.Format) {
	r.note("the stream is now %s", f)
	r.f, r.chunk = f, f.FramesFromMs(10)
}

// music is a scripted track's audio: the synth of seed, 30 ms fades.
func music(seed int64) pcm.Synth { return pcm.Synth{Seed: seed, FadeInMs: 30, FadeOut: 30} }

// sound plays the next ms of the music of seed, from where the last call
// for that seed stopped: it fades in at its start, and stops dead where
// the script stops playing it.
func (r *rig) sound(seed, ms int64) {
	if r.played == nil {
		r.played = map[int64]int64{}
	}
	from := r.played[seed]
	r.played[seed] = from + ms
	data := pcm.Synth{Seed: seed, FadeInMs: 30}.Render(r.f, r.f.FramesFromMs(from+ms))
	r.play(fmt.Sprintf("music %d at %dms", seed, from), data[r.f.FramesFromMs(from)*int64(r.f.FrameBytes()):])
}

// rewind makes the music of seed start over, as a track played again.
func (r *rig) rewind(seed int64) { delete(r.played, seed) }

func (r *rig) quiet(ms int64) {
	r.play("silence", make([]byte, r.f.FramesFromMs(ms)*int64(r.f.FrameBytes())))
}

func (r *rig) track(info meta.TrackInfo, posMs int64) {
	r.send(meta.Event{Kind: meta.TrackChanged, Track: info, PositionMs: posMs})
}

func (r *rig) status(st meta.Status) {
	r.send(meta.Event{Kind: meta.StatusChanged, Status: st, PositionMs: -1})
}

// stop asks the session to stop and waits until it has taken the request.
func (r *rig) stop(now bool) {
	if r.ended {
		return
	}
	if now {
		r.note("session.stop now")
	} else {
		r.note("session.stop")
	}
	r.s.Stop(now)
	timeout := time.After(rigTimeout)
	for len(r.s.stopCh) > 0 {
		select {
		case <-r.done:
			r.ended = true
			return
		case j := <-r.fin.jobs:
			r.addJob(j)
		case <-timeout:
			r.giveUp()
			return
		case <-time.After(time.Millisecond):
		}
	}
	r.settle()
}

// endCapture closes the capture stream, which ends the session.
func (r *rig) endCapture() {
	if r.ended {
		return
	}
	r.note("capture stream closed")
	close(r.frames)
	r.wait()
}

// endEvents closes the player's event stream.
func (r *rig) endEvents() {
	if r.ended {
		return
	}
	r.note("player event stream closed")
	close(r.events)
	r.settle()
}

// cancelRun cancels Run's context, as Core's shutdown does.
func (r *rig) cancelRun() {
	if r.ended {
		return
	}
	r.note("context canceled")
	r.cancel()
	r.wait()
}

type rigSink struct{ r *rig }

func (k rigSink) Chunk(_ context.Context, c capture.Chunk) error {
	if !k.r.feed(c) {
		return errRigEnded
	}
	return nil
}

func (k rigSink) Event(_ context.Context, e meta.Event) error {
	if !k.r.send(e) {
		return errRigEnded
	}
	return nil
}

// playTimeline plays tl into the session as the fake player does, actions
// included.
func (r *rig) playTimeline(tl *fake.Timeline) {
	r.t.Helper()
	p, err := fake.NewPlayer(tl, false)
	if err != nil {
		r.t.Fatal(err)
	}
	r.describeTimeline(tl)
	for _, a := range tl.Actions {
		cmd := a.Cmd
		p.At(a.AtMs, func() {
			r.note("player.control %s", cmd)
			_ = p.Control(cmd)
		})
	}
	if err := p.Run(context.Background(), rigSink{r}); err != nil && !errors.Is(err, errRigEnded) {
		r.t.Fatal(err)
	}
}

// describeTimeline writes where each item's audio is in the stream.
func (r *rig) describeTimeline(tl *fake.Timeline) {
	pos := float64(tl.LeadInMs)
	if tl.PauseSilence {
		r.describe("the stream carries silence while paused")
	}
	for i, it := range tl.Items {
		f, data, err := tl.ItemAudio(i)
		if err != nil {
			r.t.Fatal(err)
		}
		ms := float64(len(data)/f.FrameBytes()) * 1000 / float64(f.Rate)
		var extra []string
		if it.Rate != 0 || it.Channels != 0 {
			extra = append(extra, fmt.Sprintf("format %d/%d", f.Rate, f.Channels))
		}
		if it.NotifyOffsetMs != 0 || it.JitterMs != 0 {
			s := fmt.Sprintf("notified %+dms", it.NotifyOffsetMs)
			if it.JitterMs != 0 {
				s += fmt.Sprintf(" ±%dms", it.JitterMs)
			}
			extra = append(extra, s)
		}
		if it.PauseMs > 0 {
			extra = append(extra, fmt.Sprintf("paused at %dms for %dms (notified %+dms, resumed %+dms)", it.PauseAtMs, it.PauseMs, it.PauseNotifyMs, it.DesyncMs))
			if tl.PauseSilence {
				ms += float64(it.PauseMs)
			}
		}
		if it.SeekAtMs > 0 {
			extra = append(extra, fmt.Sprintf("jumps at %dms to %dms", it.SeekAtMs, it.SeekToMs))
			ms -= float64(it.SeekToMs - it.SeekAtMs)
		}
		line := fmt.Sprintf("item %d %q (%s, declared %dms): audio from %sms to %sms", i, it.Title, it.Kind, it.LengthMs, msText(pos), msText(pos+ms))
		pos += ms
		if len(extra) > 0 {
			line += ", " + strings.Join(extra, ", ")
		}
		if it.GapMs > 0 {
			line += fmt.Sprintf(", then %dms of silence", it.GapMs)
			pos += float64(it.GapMs)
		}
		r.describe("%s", line)
	}
	for _, a := range tl.Actions {
		r.describe("player.control %s at %dms", a.Cmd, a.AtMs)
	}
}

// result ends the session if it still runs, and returns the golden text.
func (r *rig) result(name string) string {
	r.endCapture()
	r.look()
	st := r.s.Status()
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", name)
	for _, h := range r.header {
		fmt.Fprintf(&b, "# %s\n", h)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b.WriteString("--- trace\n")
	for _, l := range r.trace {
		b.WriteString(strings.TrimRight(l, " ") + "\n")
	}
	b.WriteString("--- jobs\n")
	for _, l := range r.jobs {
		b.WriteString(l + "\n")
	}
	c := st.Counters
	fmt.Fprintf(&b, "--- end: %s, %d skipped, %d ads, %d overruns\n", st.State, c.Skipped, c.Ads, c.Overruns)
	return b.String()
}

// check compares the rig's result with its golden file.
func (r *rig) check(name string) {
	r.t.Helper()
	checkGolden(r.t, name, r.result(name))
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "session", filepath.FromSlash(name)+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run the test with -update to write it", err)
	}
	// Git may check text out with CRLF on Windows.
	want := strings.ReplaceAll(string(b), "\r\n", "\n")
	if want != got {
		t.Errorf("the session no longer does what %s pins:\n%s", path, lineDiff(want, got))
	}
}

// lineDiff shows the lines around the first one that differs.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	i := 0
	for i < len(w) && i < len(g) && w[i] == g[i] {
		i++
	}
	from := max(0, i-3)
	var b strings.Builder
	fmt.Fprintf(&b, "first difference at line %d\n", i+1)
	for j := from; j < i; j++ {
		fmt.Fprintf(&b, "  %s\n", w[j])
	}
	for j := i; j < min(len(w), i+8); j++ {
		fmt.Fprintf(&b, "- %s\n", w[j])
	}
	for j := i; j < min(len(g), i+8); j++ {
		fmt.Fprintf(&b, "+ %s\n", g[j])
	}
	return b.String()
}

func msText(ms float64) string {
	s := strconv.FormatFloat(ms, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// frameText is a frame count with its length in ms.
func frameText(f pcm.Format, n int64) string {
	if f.Rate == 0 {
		return strconv.FormatInt(n, 10)
	}
	return fmt.Sprintf("%d(%sms)", n, msText(float64(n)*1000/float64(f.Rate)))
}

// info is a scripted track's metadata.
func info(id, title string, lengthMs int64) meta.TrackInfo {
	return meta.TrackInfo{ID: id, Title: title, Artists: []string{"Artist"}, Album: "Album", LengthMs: lengthMs}
}

// alreadyRecorded makes the finisher take a track for one saved before.
func (r *rig) alreadyRecorded(artist, title string) {
	r.fin.mu.Lock()
	r.fin.claims[trackKey(artist, title)] = true
	r.fin.mu.Unlock()
	r.describe("%s - %s is already recorded", artist, title)
}

// waitFor waits for what a goroutine of the session does, such as a
// player.control it sends.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no %s", what)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
