// Package fake replays a scripted timeline as a player would: audio frames
// for the capture side and now-playing events for the metadata side, with
// the notification offsets, gaps, adverts, pauses and format changes the
// scenarios need. It replaces v1's fake player.
package fake

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spytify/spytify/internal/pcm"
)

// Timeline is the JSON scenario format of testdata/timelines.
type Timeline struct {
	Name     string `json:"name"`
	Rate     int    `json:"rate"`
	Channels int    `json:"channels"`
	Sample   string `json:"sample"` // s16 (default) or f32
	LeadInMs int64  `json:"lead_in_ms"`
	TailMs   int64  `json:"tail_ms"`
	Seed     int64  `json:"seed"`
	// PauseSilence makes the stream carry silence while paused, as some
	// players do; otherwise no frames flow during a pause.
	PauseSilence bool           `json:"pause_silence"`
	Items        []Item         `json:"items"`
	Actions      []Action       `json:"actions"`
	Settings     map[string]any `json:"settings"`
	Expect       []Expect       `json:"expect"`
	// Variants run the same timeline again with other settings and
	// expectations, such as each recorder.duplicates policy.
	Variants []Variant `json:"variants"`

	dir string
}

// Item is one track, advert or podcast in the play queue.
type Item struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // track (default), ad, podcast
	Artist      string   `json:"artist"`
	Artists     []string `json:"artists"`
	Title       string   `json:"title"`
	Album       string   `json:"album"`
	TrackNumber int      `json:"track_number"`
	Year        int      `json:"year"`
	LengthMs    int64    `json:"length_ms"`
	GapMs       int64    `json:"gap_ms"`
	// NotifyOffsetMs moves the now-playing notification relative to the
	// first sample of the item: negative is early, positive is late.
	NotifyOffsetMs int64 `json:"notify_offset_ms"`
	// JitterMs adds a seeded random offset in [-JitterMs, +JitterMs].
	JitterMs  int64  `json:"jitter_ms"`
	Rate      int    `json:"rate"`
	FadeMs    *int64 `json:"fade_ms"`
	Seed      int64  `json:"seed"`
	WAV       string `json:"wav"`
	PauseAtMs int64  `json:"pause_at_ms"`
	PauseMs   int64  `json:"pause_ms"`
	// PauseNotifyMs moves the Paused notification relative to the moment
	// the audio stops; DesyncMs moves the Playing notification relative to
	// the moment it resumes. Negative is early.
	PauseNotifyMs int64 `json:"pause_notify_ms"`
	DesyncMs      int64 `json:"desync_ms"`
	// SeekAtMs and SeekToMs drag the track bar: SeekAtMs into the item,
	// playback jumps to SeekToMs, forward or back.
	SeekAtMs int64 `json:"seek_at_ms"`
	SeekToMs int64 `json:"seek_to_ms"`
}

// Action is a command a test sends to the core while the timeline plays.
type Action struct {
	AtMs int64  `json:"at_ms"`
	Cmd  string `json:"cmd"`
}

// Expect is the manifest entry for one item: saved as a file whose audio
// matches the item, or skipped with a reason.
type Expect struct {
	Item   int    `json:"item"`
	Saved  bool   `json:"saved"`
	Reason string `json:"reason,omitempty"`
}

// Variant overrides settings and the manifest for one more run.
type Variant struct {
	Name     string         `json:"name"`
	Settings map[string]any `json:"settings"`
	Expect   []Expect       `json:"expect"`
	// Files is the number of audio files the run must leave, when the
	// manifest alone cannot say (an overwrite leaves one file for two
	// saved items).
	Files *int `json:"files"`
}

// Load reads a timeline file.
func Load(path string) (*Timeline, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tl Timeline
	if err := json.Unmarshal(b, &tl); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	tl.dir = filepath.Dir(path)
	return &tl, tl.validate()
}

func (tl *Timeline) validate() error {
	if tl.Rate == 0 {
		tl.Rate = 44100
	}
	if tl.Channels == 0 {
		tl.Channels = 2
	}
	if len(tl.Items) == 0 {
		return fmt.Errorf("timeline %q has no items", tl.Name)
	}
	for i := range tl.Items {
		it := &tl.Items[i]
		if it.Kind == "" {
			it.Kind = "track"
		}
		if it.WAV == "" && it.LengthMs <= 0 {
			return fmt.Errorf("item %d: length_ms is required without a wav", i)
		}
		if it.ID == "" {
			kind := "track"
			if it.Kind == "ad" {
				kind = "ad"
			} else if it.Kind == "podcast" {
				kind = "episode"
			}
			it.ID = fmt.Sprintf("/com/fake/%s/%d", kind, i+1)
		}
		if it.Seed == 0 {
			it.Seed = tl.Seed*1000 + int64(i) + 1
		}
		if len(it.Artists) == 0 && it.Artist != "" {
			it.Artists = []string{it.Artist}
		}
	}
	return nil
}

// Format is the base stream format.
func (tl *Timeline) Format() pcm.Format {
	f := pcm.Format{Rate: tl.Rate, Channels: tl.Channels, Sample: pcm.S16LE}
	if tl.Sample == "f32" {
		f.Sample = pcm.F32LE
	}
	return f
}

// ItemAudio is the full audio of item i, as a recording of it should be.
func (tl *Timeline) ItemAudio(i int) (pcm.Format, []byte, error) {
	it := tl.Items[i]
	if it.WAV != "" {
		path := it.WAV
		if !filepath.IsAbs(path) {
			path = filepath.Join(tl.dir, path)
		}
		return pcm.ReadWAV(path)
	}
	f := tl.Format()
	if it.Rate != 0 {
		f.Rate = it.Rate
	}
	fade := int64(30)
	if it.FadeMs != nil {
		fade = *it.FadeMs
	}
	frames := f.FramesFromMs(it.LengthMs)
	return f, pcm.Synth{Seed: it.Seed, FadeInMs: fade, FadeOut: fade}.Render(f, frames), nil
}
