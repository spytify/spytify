package meta

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// The Windows poller gives the macOS poller's shape; the tracker reads it
// with lengths in milliseconds and keeps the cover saved to a file.
func TestSMTCPollLines(t *testing.T) {
	k := &pollTracker{unit: "ms"}
	evs := k.next(poll(t, `{"running":true,"state":"playing","position":12.5,"track":{"id":null,"name":"Song","artist":"Artist","album":"Album","albumArtist":"Artist","duration":184864,"trackNumber":3,"artworkUrl":"file:///C:/Users/u/AppData/Local/Temp/spytify-cover-1"}}`))
	if len(evs) != 2 || evs[0].Kind != TrackChanged {
		t.Fatalf("first poll = %+v", evs)
	}
	got := evs[0].Track
	if got.Title != "Song" || got.LengthMs != 184864 || got.TrackNumber != 3 || got.ArtURL != "file:///C:/Users/u/AppData/Local/Temp/spytify-cover-1" || got.ID != "" {
		t.Fatalf("track = %+v", got)
	}
	if evs := k.next(poll(t, `{"running":false}`)); len(evs) != 1 || evs[0].Kind != PlayerGone {
		t.Fatalf("session gone = %+v", evs)
	}
}

// fakeSMTC answers each read with the next session, then the last one,
// and each control with the next answer.
type fakeSMTC struct {
	mu       sync.Mutex
	reads    []fakeRead
	replies  []error
	names    [][]string
	controls []proto.PlayerCmd
	answer   error
}

type fakeRead struct {
	s   *smtcSession
	err error
}

func (f *fakeSMTC) session(names []string) (*smtcSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, names)
	r := f.reads[0]
	if len(f.reads) > 1 {
		f.reads = f.reads[1:]
	}
	return r.s, r.err
}

func (f *fakeSMTC) control(names []string, cmd proto.PlayerCmd) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, names)
	f.controls = append(f.controls, cmd)
	err := f.replies[0]
	f.replies = f.replies[1:]
	return err
}

func (f *fakeSMTC) answers() error { return f.answer }

func useSMTC(t *testing.T, f *fakeSMTC) {
	t.Helper()
	old := openSMTC
	openSMTC = func() (smtcAPI, error) { return f, nil }
	t.Cleanup(func() { openSMTC = old })
}

// thumbnail counts its reads.
type thumbnail struct {
	data  []byte
	err   error
	reads int
}

func (th *thumbnail) read() ([]byte, error) {
	th.reads++
	return th.data, th.err
}

// coverPath is the file a cover URL names: file:///C:/... on Windows.
func coverPath(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" {
		t.Fatalf("cover URL %q: %v", raw, err)
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
	}
	return filepath.FromSlash(p)
}

// A read gives the poll the script gave: the state, the position moved on
// since the player's last report while it plays, the track, and its
// thumbnail saved to a file once per track.
func TestSMTCPollerReadsTheSession(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	first := &thumbnail{data: []byte("cover one")}
	song := func(title string, th *thumbnail) *smtcSession {
		s := &smtcSession{
			Status: "playing", Position: 1500 * time.Millisecond, Updated: now.Add(-500 * time.Millisecond),
			Length: 184864*time.Millisecond + 400*time.Microsecond, Title: title, Artist: "Artist", Album: "Album", AlbumArtist: "Artist", TrackNumber: 3,
		}
		if th != nil {
			s.Thumbnail = th.read
		}
		return s
	}
	var p smtcPoller
	api := &fakeSMTC{reads: []fakeRead{{s: song("Song", first)}}}
	got := p.poll(api, []string{"player"}, now)
	if !got.Running || got.State != "playing" || got.Position == nil || *got.Position != 2 || got.Error != "" {
		t.Fatalf("poll = %+v", got)
	}
	tr := got.Track
	if tr == nil || tr.Name != "Song" || tr.Artist != "Artist" || tr.Album != "Album" || tr.AlbumArtist != "Artist" || tr.TrackNumber != 3 || tr.Duration == nil || *tr.Duration != 184864 {
		t.Fatalf("track = %+v", tr)
	}
	firstFile := coverPath(t, tr.ArtworkURL)
	t.Cleanup(func() { os.Remove(firstFile) })
	if data, err := os.ReadFile(firstFile); err != nil || string(data) != "cover one" {
		t.Fatalf("cover file holds %q: %v", data, err)
	}
	if got := p.poll(api, []string{"player"}, now); got.Track.ArtworkURL != tr.ArtworkURL || first.reads != 1 {
		t.Fatalf("second poll read the cover %d times, URL %q", first.reads, got.Track.ArtworkURL)
	}

	// The next track's cover replaces the file; one that cannot be read
	// is said once and not tried again for that track.
	broken := &thumbnail{err: errors.New("access denied")}
	api.reads = []fakeRead{{s: song("Next", broken)}}
	got = p.poll(api, []string{"player"}, now)
	if got.Error != "cover: access denied" || got.Track.ArtworkURL != "" || got.Track.Name != "Next" {
		t.Fatalf("broken cover poll = %+v", got)
	}
	if _, err := os.Stat(firstFile); !os.IsNotExist(err) {
		t.Errorf("the first cover is still there: %v", err)
	}
	if got = p.poll(api, []string{"player"}, now); got.Error != "" || broken.reads != 1 {
		t.Fatalf("the broken cover was read %d times; poll %+v", broken.reads, got)
	}

	// Paused, the position is the player's own; no title, no track.
	api.reads = []fakeRead{{s: &smtcSession{Status: "paused", Position: 3 * time.Second, Updated: now.Add(-time.Hour)}}}
	if got = p.poll(api, []string{"player"}, now); got.State != "paused" || *got.Position != 3 || got.Track != nil {
		t.Fatalf("paused poll = %+v", got)
	}
	// A read that fails after finding the session keeps what it read.
	api.reads = []fakeRead{{s: &smtcSession{Status: "paused"}, err: errors.New("properties: busy")}}
	if got = p.poll(api, []string{"player"}, now); !got.Running || got.Error != "properties: busy" {
		t.Fatalf("partial poll = %+v", got)
	}
	api.reads = []fakeRead{{err: errors.New("no manager")}}
	if got = p.poll(api, []string{"player"}, now); got.Running || got.Error != "no manager" {
		t.Fatalf("failed poll = %+v", got)
	}
	api.reads = []fakeRead{{}}
	if got = p.poll(api, []string{"player"}, now); got.Running || got.Error != "" {
		t.Fatalf("no session poll = %+v", got)
	}
}

// Watch turns the reads into events and logs a read's error once.
func TestSMTCWatchReadsTheSession(t *testing.T) {
	song := func(status string, err error) fakeRead {
		return fakeRead{s: &smtcSession{Status: status, Position: time.Second, Length: 3 * time.Minute, Title: "Song", Artist: "Artist", Album: "Album"}, err: err}
	}
	api := &fakeSMTC{reads: []fakeRead{
		song("playing", nil),
		song("playing", errors.New("cover: access denied")),
		song("playing", errors.New("cover: access denied")),
		song("paused", nil),
		{},
	}}
	useSMTC(t, api)
	m := NewSMTC()
	m.Interval = time.Millisecond
	var mu sync.Mutex
	var logged []string
	m.Logf = func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, fmt.Sprintf(f, a...))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := m.Watch(ctx, target.Target{ProcessNames: []string{"Player.exe"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	timeout := time.After(10 * time.Second)
	for done := false; !done; {
		select {
		case e := <-events:
			got = append(got, e)
			done = e.Kind == PlayerGone
		case <-timeout:
			t.Fatalf("events so far: %+v", got)
		}
	}
	paused := slices.ContainsFunc(got, func(e Event) bool { return e.Kind == StatusChanged && e.Status == Paused })
	if got[0].Kind != TrackChanged || got[0].Track.Title != "Song" || got[0].Track.LengthMs != 180000 || !paused {
		t.Fatalf("events %+v", got)
	}
	mu.Lock()
	if !slices.Equal(logged, []string{"now playing: cover: access denied"}) {
		t.Errorf("logged %q", logged)
	}
	mu.Unlock()
	api.mu.Lock()
	if !slices.Equal(api.names[0], []string{"player"}) {
		t.Errorf("read the sessions of %q, want the process name without .exe", api.names[0])
	}
	api.mu.Unlock()
	if _, err := NewSMTC().Watch(context.Background(), target.Target{}); !errors.Is(err, ErrNoPlayer) {
		t.Fatalf("no player: %v", err)
	}
}

// Control says what went wrong: no session, or the player refused.
func TestSMTCControlSaysTheAnswer(t *testing.T) {
	api := &fakeSMTC{replies: []error{nil, ErrNoPlayer, errRefused, errors.New("no answer in 10s")}}
	useSMTC(t, api)
	m := NewSMTC()
	m.Target(target.Target{ProcessNames: []string{"player"}})
	for _, want := range []string{"", ErrNoPlayer.Error(), `the player refused "next"`, "no answer"} {
		err := m.Control(proto.CmdNext)
		if (want == "" && err != nil) || (want != "" && (err == nil || !strings.Contains(err.Error(), want))) {
			t.Errorf("want %q: %v", want, err)
		}
	}
	if err := m.Control("rewind"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("rewind: %v", err)
	}
	if err := NewSMTC().Control(proto.CmdNext); !errors.Is(err, ErrNoPlayer) {
		t.Errorf("no player: %v", err)
	}
	if len(api.controls) != 4 {
		t.Errorf("sent %v", api.controls)
	}
}

// mixerFeed is the capture helper's side of the mixer: Volumes gives what
// the running helper reports, and each direct read is counted, as each one
// starts the helper once more.
type mixerFeed struct {
	volumes chan float64
	reads   atomic.Int32
}

func (m *mixerFeed) Volume() (float64, error) {
	m.reads.Add(1)
	return 1, nil
}

func (m *mixerFeed) SetVolume(float64) error { return nil }
func (m *mixerFeed) Volumes() <-chan float64 { return m.volumes }

// During a session the watch reports the Volume Mixer slider the capture
// helper reads as it records, when first given and each time it changes. It
// never reads the slider itself: each read would start the helper once more,
// every two seconds of the session (#834).
func TestTheMixerWatchStartsNoHelper(t *testing.T) {
	useSMTC(t, &fakeSMTC{reads: []fakeRead{{}}})
	mixer := &mixerFeed{volumes: make(chan float64)}
	m := NewSMTC()
	m.Interval = time.Millisecond
	m.Mixer = mixer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := m.Watch(ctx, target.Target{ProcessNames: []string{"Player.exe"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for _, v := range []float64{1, 1, 0.4, 0.4, 1} {
			select {
			case mixer.volumes <- v:
			case <-ctx.Done():
				return
			}
		}
	}()
	var got []float64
	timeout := time.After(5 * time.Second)
	for len(got) < 3 {
		select {
		case e := <-events:
			if e.Kind == VolumeChanged {
				got = append(got, e.Volume)
			}
		case <-timeout:
			t.Fatalf("volumes %v, want three; the watch read the slider %d times", got, mixer.reads.Load())
		}
	}
	if want := []float64{1, 0.4, 1}; !slices.Equal(got, want) {
		t.Fatalf("volumes %v, want %v", got, want)
	}
	if n := mixer.reads.Load(); n != 0 {
		t.Fatalf("the watch read the slider %d times, starting the helper as often", n)
	}
}

// The checks ask the API whether it answers.
func TestSMTCAnswersSaysTheAPIsAnswer(t *testing.T) {
	api := &fakeSMTC{}
	useSMTC(t, api)
	if err := SMTCAnswers(); err != nil {
		t.Fatalf("answering API: %v", err)
	}
	api.answer = errors.New("Class not registered")
	if err := SMTCAnswers(); err == nil || err.Error() != "Class not registered" {
		t.Fatalf("silent API: %v", err)
	}
}

// A call is looked at after 1, 2, 4, 8 then every 16 ms: one that ends in
// 10 ms is looked at 5 times, where every millisecond made it 11.
func TestACallIsLookedAtLessAndLessOften(t *testing.T) {
	var waits []time.Duration
	looks, waited := 1, time.Duration(0)
	for w := awaitFirst; waited < 10*time.Millisecond; w = nextAwait(w) {
		waits = append(waits, w)
		waited += w
		looks++
	}
	if want := []time.Duration{1, 2, 4, 8}; !slices.Equal(waits, scale(want, time.Millisecond)) || looks != 5 {
		t.Fatalf("waits %v, %d looks", waits, looks)
	}
	if got := nextAwait(awaitLast); got != awaitLast {
		t.Fatalf("after %v the wait is %v", awaitLast, got)
	}
}

func scale(ds []time.Duration, unit time.Duration) []time.Duration {
	out := make([]time.Duration, len(ds))
	for i, d := range ds {
		out[i] = d * unit
	}
	return out
}
