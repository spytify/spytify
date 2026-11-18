package core

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

const link = "https://example.com/album/harbor-lights"

func TestASessionThatPlaysALinkRecordsWhatThePlayerPlays(t *testing.T) {
	// In real time: the request waits for the player's first state, and a
	// fake player that is not paced would be done with the list by then.
	tl := loadTimeline(t, "basic")
	for i := range tl.Items {
		tl.Items[i].LengthMs, tl.Items[i].GapMs = 2000, 300
	}
	tl.Expect = nil
	s := newScenarioAt(t, tl, nil, true)
	runWith(t, s, SessionOptions{EndOnStop: true, PlayURL: link})
	if got := s.player.Commands(); !slices.Equal(got, []string{"play_url " + link}) {
		t.Fatalf("the player was sent %q, want the link once", got)
	}
	saved, _ := outcome(t, s)
	if !slices.Equal(saved, []string{"Midnight Ferry", "Paper Lanterns", "Golden Hour - 2011 Remaster"}) {
		t.Fatalf("saved %q", saved)
	}
}

type noLinkBackends struct{ fakeBackends }

// noLinkPlayer is a player that cannot be asked to play a link, as on
// Windows.
type noLinkPlayer struct{ meta.NowPlaying }

func (b noLinkBackends) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	return b.b, noLinkPlayer{b.b}, nil
}

func TestALinkThePlayerCannotBeAskedToPlayStartsNoSession(t *testing.T) {
	s := newScenario(t, loadTimeline(t, "basic"), nil)
	s.core.backends = noLinkBackends{s.core.backends.(fakeBackends)}
	_, err := s.core.StartSession(SessionOptions{PlayURL: link})
	var perr *proto.Error
	if !errors.As(err, &perr) || perr.Code != proto.CodeUnsupported {
		t.Fatalf("got %v, want CodeUnsupported", err)
	}
	if st := s.core.Status(); st.State != proto.StateIdle {
		t.Fatalf("a session runs: %+v", st)
	}
}

// player stands for a player asked to play a link: it plays once it was
// asked at least after times, and each request fails with fail until then.
type player struct {
	after int
	fail  error
	sent  int
	plays atomic.Uint64
}

func (p *player) send() error {
	p.sent++
	if p.sent < p.after {
		return p.fail
	}
	p.plays.Add(1)
	return nil
}

func always() bool { return true }

// The watch's first state may already show the link playing: it does not
// count as a move, so a request sent before it would be sent again and
// start the first track over.
func TestAPlayRequestWaitsForThePlayersFirstState(t *testing.T) {
	p := &player{after: 1}
	var watching atomic.Bool
	time.AfterFunc(300*time.Millisecond, func() { watching.Store(true) })
	start := time.Now()
	sent := func() error {
		if !watching.Load() {
			t.Error("asked before the player's first state")
		}
		return p.send()
	}
	if err := askToPlay(sent, watching.Load, p.plays.Load, nil, 100*time.Millisecond, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if p.sent != 1 || time.Since(start) < 300*time.Millisecond {
		t.Fatalf("asked %d times after %s", p.sent, time.Since(start))
	}
	never := &player{after: 1}
	err := askToPlay(never.send, func() bool { return false }, never.plays.Load, nil, 100*time.Millisecond, 200*time.Millisecond)
	if err == nil || never.sent != 0 {
		t.Fatalf("got %v after %d requests with no state", err, never.sent)
	}
}

func TestAPlayRequestIsSentAgainUntilThePlayerPlays(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    *player
		want int
	}{
		{"plays at once", &player{after: 1}, 1},
		{"a player just opened ignores the first", &player{after: 2}, 2},
		{"a player that does not answer yet", &player{after: 3, fail: meta.ErrNoPlayer}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := askToPlay(tc.p.send, always, tc.p.plays.Load, nil, 100*time.Millisecond, 2*time.Second); err != nil {
				t.Fatal(err)
			}
			if tc.p.sent != tc.want {
				t.Fatalf("asked %d times, want %d", tc.p.sent, tc.want)
			}
		})
	}
}

func TestAPlayRequestTheLinkNeverAnswersSaysWhy(t *testing.T) {
	ignored := &player{after: 1 << 30}
	err := askToPlay(ignored.send, always, ignored.plays.Load, nil, 100*time.Millisecond, 300*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "went on as it was") || ignored.sent < 3 {
		t.Fatalf("got %v after %d requests", err, ignored.sent)
	}
	refused := &player{after: 1 << 30, fail: errors.New("not a link the player knows")}
	if err := askToPlay(refused.send, always, refused.plays.Load, nil, 100*time.Millisecond, 300*time.Millisecond); err != refused.fail {
		t.Fatalf("got %v, want the player's last answer", err)
	}
	done := make(chan struct{})
	close(done)
	if err := askToPlay(ignored.send, always, ignored.plays.Load, done, 100*time.Millisecond, time.Minute); err != errSessionEnded {
		t.Fatalf("got %v once the session ended", err)
	}
}
