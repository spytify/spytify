package meta

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// pollScript runs inside osascript as JavaScript for Automation. It polls
// the player's scripting interface (current track, player state, player
// position) and prints one JSON line per poll. Asking whether the player
// runs never launches it; nothing else is asked unless it runs.
const pollScript = `
ObjC.import('Foundation');
function out(o) {
  $.NSFileHandle.fileHandleWithStandardOutput.writeData($(JSON.stringify(o) + "\n").dataUsingEncoding($.NSUTF8StringEncoding));
}
function get(f) { try { return f(); } catch (e) { return null; } }
function track(p) {
  return {
    id: p.id === undefined || p.id === null ? null : String(p.id),
    name: p.name || "",
    artist: p.artist || "",
    album: p.album || "",
    albumArtist: p.albumArtist || "",
    duration: typeof p.duration === "number" ? p.duration : null,
    trackNumber: p.trackNumber || 0,
    discNumber: p.discNumber || 0,
    year: typeof p.year === "number" ? p.year : 0,
    artworkUrl: p.artworkUrl || "",
    url: p.url || ""
  };
}
// fields reads the track one property at a time, for players whose
// dictionary does not answer "properties" of the current track.
function fields(t) {
  const p = {};
  ["id", "name", "artist", "album", "albumArtist", "duration", "trackNumber", "discNumber", "year", "artworkUrl", "url"].forEach(function (k) {
    const v = get(() => t[k]());
    if (v !== null) { p[k] = v; }
  });
  return p;
}
function run(argv) {
  const spec = argv[0];
  const interval = parseFloat(argv[1] || "0.25");
  let app = null;
  let wholeRecord = true;
  let lastID = null, last = null;
  while (true) {
    // Application() throws while the player is not installed; try again.
    app = app || get(() => Application(spec));
    if (!app || !get(() => app.running())) { out({running: false}); delay(interval); continue; }
    let state = null, error = "";
    try { state = String(app.playerState()); } catch (e) { error = String(e); }
    let current = null;
    if (wholeRecord) {
      const props = get(() => app.currentTrack.properties());
      if (props) { current = track(props); } else { wholeRecord = false; }
    }
    if (!wholeRecord) {
      // One event per poll for the id; the rest only when it changes.
      const id = get(() => app.currentTrack.id());
      if (id === null) {
        lastID = null; last = null;
      } else if (String(id) !== lastID || !last) {
        last = track(fields(app.currentTrack));
        lastID = String(id);
      }
      current = last;
    }
    out({running: true, state: state || "", position: get(() => app.playerPosition()), track: current, error: error});
    delay(interval);
  }
}
`

// controlScript runs one command against the player.
const controlScript = `
function run(argv) {
  const app = Application(argv[0]);
  if (!app.running()) { throw new Error("player not running"); }
  switch (argv[1]) {
    case "volume": return String(app.soundVolume());
    case "restart": app.playerPosition = 0; return "";
    case "setvolume": app.soundVolume = parseInt(argv[2], 10); return "";
    default: app[argv[1]](); return "";
  }
}
`

// JXA reads now-playing from the player's AppleScript dictionary on macOS.
// Players that are scriptable expose the same vocabulary: current track,
// player state, player position, next track, pause, sound volume. The
// player is named by its bundle id (or process name), never in code.
type JXA struct {
	Interval time.Duration
	// Logf receives scripting errors, once per distinct message.
	Logf func(format string, args ...any)

	mu   sync.Mutex
	spec string
	unit string
}

// NewJXA returns a backend polling every 250 ms.
func NewJXA() *JXA { return &JXA{Interval: 250 * time.Millisecond} }

// Target sets the player for Control and Volume without watching it.
func (j *JXA) Target(t target.Target) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.spec, j.unit = t.AppSpecifier(), t.DurationUnit
}

func (j *JXA) specifier() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.spec
}

type polledTrack struct {
	ID          *string  `json:"id"`
	Name        string   `json:"name"`
	Artist      string   `json:"artist"`
	Album       string   `json:"album"`
	AlbumArtist string   `json:"albumArtist"`
	Duration    *float64 `json:"duration"`
	TrackNumber int      `json:"trackNumber"`
	DiscNumber  int      `json:"discNumber"`
	Year        int      `json:"year"`
	ArtworkURL  string   `json:"artworkUrl"`
	URL         string   `json:"url"`
}

type playerPoll struct {
	Running  bool         `json:"running"`
	State    string       `json:"state"`
	Position *float64     `json:"position"`
	Track    *polledTrack `json:"track"`
	Error    string       `json:"error"`
	// at is when the poll was read; zero leaves seeks undetected.
	at time.Time
}

// explain turns a scripting error into what the user can do about it.
func explain(err string) string {
	if strings.Contains(err, "-1743") || strings.Contains(err, "Not authorized") || strings.Contains(err, "not allowed") {
		return "macOS does not let spytify read the player: allow it in System Settings > Privacy & Security > Automation (" + err + ")"
	}
	return "the player's AppleScript interface failed: " + err
}

// durationMs converts the dictionary's track length: some players report
// milliseconds (an integer), others seconds (a real).
func durationMs(d float64, unit string) int64 {
	switch unit {
	case "ms":
		return int64(math.Round(d))
	case "s":
		return int64(math.Round(d * 1000))
	}
	if d == math.Trunc(d) {
		return int64(d)
	}
	return int64(math.Round(d * 1000))
}

func (p playerPoll) info(unit string) TrackInfo {
	if p.Track == nil {
		return TrackInfo{}
	}
	t := p.Track
	info := TrackInfo{
		Title:       strings.TrimSpace(t.Name),
		Album:       strings.TrimSpace(t.Album),
		TrackNumber: t.TrackNumber,
		DiscNumber:  t.DiscNumber,
		Year:        t.Year,
		ArtURL:      t.ArtworkURL,
		URL:         t.URL,
	}
	if t.ID != nil {
		info.ID = *t.ID
	}
	if a := strings.TrimSpace(t.Artist); a != "" {
		info.Artists = []string{a}
	}
	if a := strings.TrimSpace(t.AlbumArtist); a != "" {
		info.AlbumArtists = []string{a}
	}
	if t.Duration != nil {
		info.LengthMs = durationMs(*t.Duration, unit)
	}
	// The Windows poller saves the cover to a temporary file.
	if !strings.HasPrefix(info.ArtURL, "http://") && !strings.HasPrefix(info.ArtURL, "https://") && !strings.HasPrefix(info.ArtURL, "file:///") {
		info.ArtURL = ""
	}
	return info
}

func (p playerPoll) status() Status {
	switch strings.ToLower(p.State) {
	case "playing", "fast forwarding", "rewinding":
		return Playing
	case "paused":
		return Paused
	}
	return Stopped
}

// pollTracker turns the stream of polls into change events.
type pollTracker struct {
	unit    string
	started bool
	running bool
	last    TrackInfo
	status  Status
	lastPos int64
	lastAt  time.Time
}

// A position that jumps back near zero on the same item is a restart.
const (
	restartFromMs = 3000
	// endResetMs: a player that reports 0:00 within this much of an item's
	// end is between two items, not starting this one over.
	endResetMs = 3000
	// seekSlackMs is how far the position may drift from the wall clock
	// between two polls before it counts as a seek.
	seekSlackMs = 2000
	restartToMs = 1500
)

func (k *pollTracker) next(p playerPoll) []Event {
	pos := int64(-1)
	if p.Position != nil {
		pos = int64(math.Round(*p.Position * 1000))
	}
	if !k.started {
		k.started = true
		k.running = p.Running
		if !p.Running {
			return []Event{{Kind: TrackChanged, PositionMs: -1}, {Kind: StatusChanged, Status: Stopped, PositionMs: -1}}
		}
		k.last, k.status, k.lastPos = p.info(k.unit), p.status(), pos
		return []Event{{Kind: TrackChanged, Track: k.last, PositionMs: pos}, {Kind: StatusChanged, Status: k.status, PositionMs: pos}}
	}
	if !p.Running {
		if k.running {
			k.running, k.last, k.status = false, TrackInfo{}, Stopped
			return []Event{{Kind: PlayerGone, PositionMs: -1}}
		}
		return nil
	}
	var out []Event
	if !k.running {
		k.running = true
		out = append(out, Event{Kind: PlayerAppeared, PositionMs: -1})
	}
	info := p.info(k.unit)
	if !equalInfo(info, k.last) {
		k.last = info
		out = append(out, Event{Kind: TrackChanged, Track: info, PositionMs: pos})
	} else if !info.Empty() && pos >= 0 && pos < restartToMs && info.LengthMs > 0 && k.lastPos >= info.LengthMs-endResetMs {
		// The item ended; the next one's metadata follows.
	} else if !info.Empty() && pos >= 0 && pos < restartToMs && k.lastPos-pos > restartFromMs {
		out = append(out, Event{Kind: TrackChanged, Track: info, PositionMs: pos, Restart: true})
	} else if !info.Empty() && pos >= 0 && k.lastPos >= 0 && !p.at.IsZero() && !k.lastAt.IsZero() {
		expected := k.lastPos
		if k.status == Playing {
			expected += p.at.Sub(k.lastAt).Milliseconds()
		}
		if d := pos - expected; d > seekSlackMs || d < -seekSlackMs {
			out = append(out, Event{Kind: Seeked, Track: info, PositionMs: pos})
		}
	}
	if pos >= 0 {
		k.lastPos, k.lastAt = pos, p.at
	}
	if st := p.status(); st != k.status {
		k.status = st
		out = append(out, Event{Kind: StatusChanged, Status: st, PositionMs: pos})
	}
	return out
}

// Watch starts the poller and reports changes.
func (j *JXA) Watch(ctx context.Context, t target.Target) (<-chan Event, error) {
	j.Target(t)
	spec := j.specifier()
	if spec == "" {
		return nil, ErrNoPlayer
	}
	interval := j.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	cmd := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", pollScript, spec, strconv.FormatFloat(interval.Seconds(), 'f', 3, 64))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("osascript: %w", err)
	}
	out := make(chan Event, 64)
	tracker := &pollTracker{unit: t.DurationUnit}
	go func() {
		defer cmd.Wait()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		lastErr := ""
		for sc.Scan() {
			var p playerPoll
			if json.Unmarshal(sc.Bytes(), &p) != nil {
				continue
			}
			p.at = time.Now()
			if p.Error != lastErr {
				lastErr = p.Error
				if p.Error != "" && j.Logf != nil {
					j.Logf("now playing: %s", explain(p.Error))
				}
			}
			for _, e := range tracker.next(p) {
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (j *JXA) run(args ...string) (string, error) {
	spec := j.specifier()
	if spec == "" {
		return "", ErrNoPlayer
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmdArgs := append([]string{"-l", "JavaScript", "-e", controlScript, spec}, args...)
	b, err := exec.CommandContext(ctx, "osascript", cmdArgs...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(b))
		if strings.Contains(msg, "not running") || strings.Contains(msg, "(-2700)") || strings.Contains(msg, "can't be found") {
			return "", ErrNoPlayer
		}
		if strings.Contains(msg, "-1743") || strings.Contains(msg, "Not authorized") {
			return "", errors.New("macOS refused to let spytify control the player: allow it in System Settings > Privacy & Security > Automation")
		}
		return "", fmt.Errorf("%w: %s", ErrUnsupported, msg)
	}
	return strings.TrimSpace(string(b)), nil
}

// Control sends a transport command through the player's dictionary.
func (j *JXA) Control(cmd string) error {
	method := map[string]string{"play": "play", "pause": "pause", "next": "nextTrack", "previous": "previousTrack", "restart": "restart"}[cmd]
	if method == "" {
		return ErrUnsupported
	}
	_, err := j.run(method)
	return err
}

func (j *JXA) Volume() (float64, error) {
	s, err := j.run("volume")
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, ErrUnsupported
	}
	return v / 100, nil
}

func (j *JXA) SetVolume(v float64) error {
	_, err := j.run("setvolume", strconv.Itoa(int(math.Round(v*100))))
	return err
}
