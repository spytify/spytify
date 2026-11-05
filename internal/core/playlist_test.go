package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

// sessionStart is a session's start in the playlist tests.
var sessionStart = time.Date(2026, 11, 3, 14, 20, 5, 0, time.Local)

// listening feeds a playlist the events of a session, as the core sends
// them. root is output.dir and on is output.playlist, on unless a test
// turns it off.
type listening struct {
	t    *testing.T
	root string
	on   bool
	p    *playlist
}

func newListening(t *testing.T) *listening {
	t.Helper()
	log, _ := NewLogger("", testWriter{t})
	l := &listening{t: t, root: filepath.Join(t.TempDir(), "out"), on: true}
	l.p = newPlaylist(func() string { return l.root }, sessionStart, func() bool { return l.on }, log)
	return l
}

func (l *listening) started(id, artist, title string, lengthMs int64) {
	l.p.observe(proto.TrackStartedEvent{Track: proto.Track{ID: id, Artist: artist, Title: title, LengthMs: lengthMs}})
}

func (l *listening) saved(id, rel string, ms int64) {
	l.p.observe(proto.TrackFinishedEvent{ID: id, Path: l.at(rel), DurationMs: ms})
}

func (l *listening) skipped(id string, reason proto.SkipReason) {
	l.p.observe(proto.TrackSkippedEvent{ID: id, Reason: reason})
}

// at is rel's path in the output folder.
func (l *listening) at(rel string) string {
	return filepath.Join(l.root, filepath.FromSlash(rel))
}

// file is the playlist's text, "" when there is none.
func (l *listening) file(name string) string {
	l.t.Helper()
	data, err := os.ReadFile(filepath.Join(l.root, "Playlists", name))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		l.t.Fatal(err)
	}
	return string(data)
}

// fromList is a file's line as the playlist writes it: from the Playlists
// folder, with the system's separator.
func fromList(rel string) string {
	return filepath.Join("..", filepath.FromSlash(rel))
}

// A session's playlist has its tracks in the order they played: each one
// saved, marks included; one already recorded, from its file; one kept with
// track.keep, in its place. Ads, items too short and takes cut short and
// not kept are left out.
func TestThePlaylistOfASession(t *testing.T) {
	l := newListening(t)
	l.started("s-001", "M83", "Intro", 134000)
	l.saved("s-001", "M83/Hurry Up/M83 - Intro.flac", 133700)
	l.started("s-002", "Sponsor", "Sponsored", 30000)
	l.skipped("s-002", proto.SkipAd)
	l.started("s-003", "M83", "Reunion", 236000)
	l.saved("s-003", "M83/Hurry Up/M83 - Reunion (pause fade).flac", 236000)
	l.started("s-004", "M83", "Wait", 174000)
	l.p.observe(proto.TrackSkippedEvent{ID: "s-004", Reason: proto.SkipDuplicate, Path: l.at("Old/M83 - Wait.flac")})
	l.started("s-005", "", "Jingle", 9000)
	l.skipped("s-005", proto.SkipTooShort)
	l.started("s-006", "M83", "Raconte-moi une histoire", 250000)
	l.skipped("s-006", proto.SkipIncomplete)
	l.started("s-007", "M83", "Midnight City", 243000)
	l.saved("s-007", "M83/Hurry Up/M83 - Midnight City.flac", 243000)
	l.started("s-008", "M83", "Outro", 247000)
	l.skipped("s-008", proto.SkipIncomplete)
	// Outro, cut short, is kept after all.
	l.saved("s-008", "M83/Hurry Up/M83 - Outro (partial).flac", 61000)
	want := "#EXTM3U\n#PLAYLIST:2026-11-03 14.20\n" +
		"#EXTINF:134,M83 - Intro\n" + fromList("M83/Hurry Up/M83 - Intro.flac") + "\n" +
		"#EXTINF:236,M83 - Reunion\n" + fromList("M83/Hurry Up/M83 - Reunion (pause fade).flac") + "\n" +
		"#EXTINF:174,M83 - Wait\n" + fromList("Old/M83 - Wait.flac") + "\n" +
		"#EXTINF:243,M83 - Midnight City\n" + fromList("M83/Hurry Up/M83 - Midnight City.flac") + "\n" +
		"#EXTINF:61,M83 - Outro\n" + fromList("M83/Hurry Up/M83 - Outro (partial).flac") + "\n"
	if got := l.file("2026-11-03 14.20.m3u8"); got != want {
		t.Errorf("playlist\n%s\nwant\n%s", got, want)
	}
	if got, want := l.p.name(), l.at("Playlists/2026-11-03 14.20.m3u8"); got != want {
		t.Errorf("name %q, want %q", got, want)
	}
}

// A track played again is in again, from the file of its first take:
// saved before the second one is skipped, or after.
func TestATrackPlayedTwiceIsInTwice(t *testing.T) {
	l := newListening(t)
	l.started("s-001", "M83", "Wait", 174000)
	l.saved("s-001", "M83 - Wait.flac", 174000)
	l.started("s-002", "M83", "Wait", 174000)
	l.p.observe(proto.TrackSkippedEvent{ID: "s-002", Reason: proto.SkipDuplicate, Of: "s-001"})
	l.started("s-003", "M83", "Intro", 134000)
	l.started("s-004", "M83", "Intro", 134000)
	l.p.observe(proto.TrackSkippedEvent{ID: "s-004", Reason: proto.SkipDuplicate, Of: "s-003"})
	// Intro's first take is still being saved: the second waits for it.
	if got := l.file("2026-11-03 14.20.m3u8"); got != "#EXTM3U\n#PLAYLIST:2026-11-03 14.20\n#EXTINF:174,M83 - Wait\n"+fromList("M83 - Wait.flac")+"\n#EXTINF:174,M83 - Wait\n"+fromList("M83 - Wait.flac")+"\n" {
		t.Errorf("before Intro is saved:\n%s", got)
	}
	l.saved("s-003", "M83 - Intro.flac", 134000)
	want := "#EXTM3U\n#PLAYLIST:2026-11-03 14.20\n" +
		"#EXTINF:174,M83 - Wait\n" + fromList("M83 - Wait.flac") + "\n" +
		"#EXTINF:174,M83 - Wait\n" + fromList("M83 - Wait.flac") + "\n" +
		"#EXTINF:134,M83 - Intro\n" + fromList("M83 - Intro.flac") + "\n" +
		"#EXTINF:134,M83 - Intro\n" + fromList("M83 - Intro.flac") + "\n"
	if got := l.file("2026-11-03 14.20.m3u8"); got != want {
		t.Errorf("playlist\n%s\nwant\n%s", got, want)
	}
}

// A track still being saved when the next one is keeps its place: the
// playlist is written in the order the tracks played, not the order their
// files came.
func TestATrackStillBeingSavedKeepsItsPlace(t *testing.T) {
	l := newListening(t)
	l.started("s-001", "M83", "Intro", 134000)
	l.started("s-002", "M83", "Reunion", 236000)
	l.saved("s-002", "M83 - Reunion.flac", 236000)
	if got := l.file("2026-11-03 14.20.m3u8"); got != "#EXTM3U\n#PLAYLIST:2026-11-03 14.20\n#EXTINF:236,M83 - Reunion\n"+fromList("M83 - Reunion.flac")+"\n" {
		t.Errorf("before Intro is saved:\n%s", got)
	}
	// The session ends while Intro is being saved: its playlist is named.
	if got, want := l.p.name(), l.at("Playlists/2026-11-03 14.20.m3u8"); got != want {
		t.Errorf("name %q, want %q", got, want)
	}
	l.saved("s-001", "M83 - Intro.flac", 134000)
	want := "#EXTM3U\n#PLAYLIST:2026-11-03 14.20\n" +
		"#EXTINF:134,M83 - Intro\n" + fromList("M83 - Intro.flac") + "\n" +
		"#EXTINF:236,M83 - Reunion\n" + fromList("M83 - Reunion.flac") + "\n"
	if got := l.file("2026-11-03 14.20.m3u8"); got != want {
		t.Errorf("playlist\n%s\nwant\n%s", got, want)
	}
}

// A session whose tracks all stay out writes no playlist, and names none.
func TestASessionThatSavesNothingWritesNoPlaylist(t *testing.T) {
	l := newListening(t)
	l.started("s-001", "Sponsor", "Sponsored", 30000)
	l.skipped("s-001", proto.SkipAd)
	l.started("s-002", "M83", "Intro", 134000)
	l.skipped("s-002", proto.SkipIncomplete)
	if got := l.p.name(); got != "" {
		t.Errorf("name %q", got)
	}
	if _, err := os.Stat(filepath.Join(l.root, "Playlists")); !os.IsNotExist(err) {
		t.Errorf("Playlists made: %v", err)
	}
}

// output.playlist turned on mid-session writes the session's playlist with
// the tracks saved before it too, at the next track saved or, when none
// is, as the session ends. Turned off, it is no longer written or named.
func TestThePlaylistTurnedOnMidSession(t *testing.T) {
	want := "#EXTM3U\n#PLAYLIST:2026-11-03 14.20\n" +
		"#EXTINF:134,M83 - Intro\n" + fromList("M83/Hurry Up/M83 - Intro.flac") + "\n" +
		"#EXTINF:236,M83 - Reunion\n" + fromList("M83/Hurry Up/M83 - Reunion.flac") + "\n"
	t.Run("then a track saved", func(t *testing.T) {
		l := newListening(t)
		l.on = false
		l.started("s-001", "M83", "Intro", 134000)
		l.saved("s-001", "M83/Hurry Up/M83 - Intro.flac", 133700)
		if got := l.file("2026-11-03 14.20.m3u8"); got != "" {
			t.Fatalf("written while off:\n%s", got)
		}
		l.on = true
		l.started("s-002", "M83", "Reunion", 236000)
		l.saved("s-002", "M83/Hurry Up/M83 - Reunion.flac", 236000)
		if got := l.file("2026-11-03 14.20.m3u8"); got != want {
			t.Errorf("playlist\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("then the session ends", func(t *testing.T) {
		l := newListening(t)
		l.on = false
		l.started("s-001", "M83", "Intro", 134000)
		l.saved("s-001", "M83/Hurry Up/M83 - Intro.flac", 133700)
		l.started("s-002", "M83", "Reunion", 236000)
		l.saved("s-002", "M83/Hurry Up/M83 - Reunion.flac", 236000)
		if got := l.p.name(); got != "" {
			t.Errorf("named %q while off", got)
		}
		l.on = true
		if got, want := l.p.name(), l.at("Playlists/2026-11-03 14.20.m3u8"); got != want {
			t.Errorf("name %q, want %q", got, want)
		}
		if got := l.file("2026-11-03 14.20.m3u8"); got != want {
			t.Errorf("playlist\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("after the output folder changed", func(t *testing.T) {
		l := newListening(t)
		l.on = false
		l.started("s-001", "M83", "Intro", 134000)
		l.saved("s-001", "M83/Hurry Up/M83 - Intro.flac", 133700)
		before := l.at("M83/Hurry Up/M83 - Intro.flac")
		l.root = filepath.Join(t.TempDir(), "elsewhere")
		l.on = true
		l.started("s-002", "M83", "Reunion", 236000)
		l.saved("s-002", "M83/Hurry Up/M83 - Reunion.flac", 236000)
		want := "#EXTM3U\n#PLAYLIST:2026-11-03 14.20\n" +
			"#EXTINF:134,M83 - Intro\n" + before + "\n" +
			"#EXTINF:236,M83 - Reunion\n" + fromList("M83/Hurry Up/M83 - Reunion.flac") + "\n"
		if got := l.file("2026-11-03 14.20.m3u8"); got != want {
			t.Errorf("playlist\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("turned off", func(t *testing.T) {
		l := newListening(t)
		l.started("s-001", "M83", "Intro", 134000)
		l.saved("s-001", "M83/Hurry Up/M83 - Intro.flac", 133700)
		l.on = false
		l.started("s-002", "M83", "Reunion", 236000)
		l.saved("s-002", "M83/Hurry Up/M83 - Reunion.flac", 236000)
		if got := l.file("2026-11-03 14.20.m3u8"); strings.Contains(got, "Reunion") {
			t.Errorf("written while off:\n%s", got)
		}
		if got := l.p.name(); got != "" {
			t.Errorf("named %q while off", got)
		}
	})
}

// Another session of the same minute wrote its playlist: this one takes
// " (2)". A file out of the output folder keeps its full path.
func TestThePlaylistsNameAndAFileElsewhere(t *testing.T) {
	l := newListening(t)
	if err := os.MkdirAll(filepath.Join(l.root, "Playlists"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.root, "Playlists", "2026-11-03 14.20.m3u8"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "Music", "M83 - Wait.flac")
	l.started("s-001", "M83", "Wait", 174000)
	l.p.observe(proto.TrackSkippedEvent{ID: "s-001", Reason: proto.SkipDuplicate, Path: elsewhere})
	if got := l.file("2026-11-03 14.20.m3u8"); got != "before" {
		t.Errorf("the other session's playlist became %q", got)
	}
	want := "#EXTM3U\n#PLAYLIST:2026-11-03 14.20 (2)\n#EXTINF:174,M83 - Wait\n" + elsewhere + "\n"
	if got := l.file("2026-11-03 14.20 (2).m3u8"); got != want {
		t.Errorf("playlist\n%s\nwant\n%s", got, want)
	}
}

// A session with output.playlist on writes its playlist in the output
// folder's Playlists and names it as it ends: the duplicate timeline's
// track, saved then played again, is in twice, from its one file. Off, no
// playlist is written or named.
func TestASessionWritesItsPlaylist(t *testing.T) {
	for _, on := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[on], func(t *testing.T) {
			tl := loadTimeline(t, "duplicate")
			s := newScenario(t, tl, map[string]any{"recorder.duplicates": "skip", "output.playlist": on})
			runWith(t, s, SessionOptions{EndOnStop: true})
			var named string
			for _, ev := range s.events.all() {
				var st proto.SessionStateEvent
				if ev.Method == proto.EventSessionState && ev.Decode(&st) == nil && st.State == proto.StateIdle {
					named = st.Playlist
				}
			}
			files, _ := filepath.Glob(filepath.Join(s.out, "Playlists", "*.m3u8"))
			if !on {
				if len(files) != 0 || named != "" {
					t.Fatalf("off: files %v, named %q", files, named)
				}
				return
			}
			if len(files) != 1 || files[0] != named {
				t.Fatalf("files %v, named %q", files, named)
			}
			if base := filepath.Base(named); !regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}\.\d{2}\.m3u8$`).MatchString(base) {
				t.Errorf("named %q, want the session's start", base)
			}
			data, err := os.ReadFile(named)
			if err != nil {
				t.Fatal(err)
			}
			got := regexp.MustCompile(`#EXTINF:\d+,`).ReplaceAllString(string(data), "#EXTINF:,")
			file := fromList("Copper Lanterns - Hollow Moon.wav")
			want := "#EXTM3U\n#PLAYLIST:" + strings.TrimSuffix(filepath.Base(named), ".m3u8") + "\n" +
				"#EXTINF:,Copper Lanterns - Hollow Moon\n" + file + "\n" +
				"#EXTINF:,Copper Lanterns - Hollow Moon\n" + file + "\n"
			if got != want {
				t.Errorf("playlist\n%s\nwant\n%s", got, want)
			}
		})
	}
}
