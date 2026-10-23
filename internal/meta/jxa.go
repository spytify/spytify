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
function run(argv) {
  const spec = argv[0];
  const interval = parseFloat(argv[1] || "0.25");
  let app = null;
  while (true) {
    // Application() throws while the player is not installed; try again.
    app = app || get(() => Application(spec));
    if (!app || !get(() => app.running())) { out({running: false}); delay(interval); continue; }
    let track = null;
    const props = get(() => app.currentTrack.properties());
    if (props) {
      track = {
        id: props.id === undefined ? null : String(props.id),
        name: props.name || "",
        artist: props.artist || "",
        album: props.album || "",
        albumArtist: props.albumArtist || "",
        duration: typeof props.duration === "number" ? props.duration : null,
        trackNumber: props.trackNumber || 0,
        discNumber: props.discNumber || 0,
        year: props.year || 0,
        artworkUrl: props.artworkUrl || "",
        url: props.spotifyUrl || props.url || ""
      };
    }
    out({running: true, state: String(get(() => app.playerState()) || ""), position: get(() => app.playerPosition()), track: track});
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

type jxaTrack struct {
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

type jxaPoll struct {
	Running  bool      `json:"running"`
	State    string    `json:"state"`
	Position *float64  `json:"position"`
	Track    *jxaTrack `json:"track"`
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

func (p jxaPoll) info(unit string) TrackInfo {
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
	if !strings.HasPrefix(info.ArtURL, "http://") && !strings.HasPrefix(info.ArtURL, "https://") {
		info.ArtURL = ""
	}
	return info
}

func (p jxaPoll) status() Status {
	switch strings.ToLower(p.State) {
	case "playing", "fast forwarding", "rewinding":
		return Playing
	case "paused":
		return Paused
	}
	return Stopped
}

// jxaTracker turns the stream of polls into change events.
type jxaTracker struct {
	unit    string
	started bool
	running bool
	last    TrackInfo
	status  Status
}

func (k *jxaTracker) next(p jxaPoll) []Event {
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
		k.last, k.status = p.info(k.unit), p.status()
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
	tracker := &jxaTracker{unit: t.DurationUnit}
	go func() {
		defer cmd.Wait()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		for sc.Scan() {
			var p jxaPoll
			if json.Unmarshal(sc.Bytes(), &p) != nil {
				continue
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
	method := map[string]string{"play": "play", "pause": "pause", "next": "nextTrack", "previous": "previousTrack"}[cmd]
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
