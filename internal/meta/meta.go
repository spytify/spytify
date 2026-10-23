// Package meta reads what the target player says it is playing through the
// OS-level now-playing interface (MPRIS on Linux), and sends it transport
// commands. It never decides where a track ends; the core does that from
// the audio, using the declared length as a hint.
package meta

import (
	"context"
	"errors"
	"strings"

	"github.com/spytify/spytify/internal/target"
)

// Status is the player's playback status.
type Status string

const (
	Playing Status = "Playing"
	Paused  Status = "Paused"
	Stopped Status = "Stopped"
)

// TrackInfo is the metadata the player exposes for the current item.
type TrackInfo struct {
	ID           string
	Title        string
	Artists      []string
	Album        string
	AlbumArtists []string
	TrackNumber  int
	DiscNumber   int
	Year         int
	LengthMs     int64
	ArtURL       string
	URL          string
}

// Empty reports whether no item is loaded.
func (t TrackInfo) Empty() bool { return t.ID == "" && t.Title == "" }

// Artist is the first artist, or "".
func (t TrackInfo) Artist() string {
	if len(t.Artists) == 0 {
		return ""
	}
	return t.Artists[0]
}

// IsAdvert applies the generic advert rule: the item id or URL carries an
// "ad" path or URI segment, as players expose for sponsored items
// (/<vendor>/ad/<n>, <vendor>:ad:<n>). No player name is involved.
func (t TrackInfo) IsAdvert() bool {
	for _, s := range []string{t.ID, t.URL} {
		s = strings.ToLower(s)
		if s == "" {
			continue
		}
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == ':' }) {
			if part == "ad" || part == "ads" {
				return true
			}
		}
	}
	return false
}

// Kind of a metadata event.
type Kind int

const (
	// TrackChanged carries the metadata of the item now loaded. The same
	// ID again means the metadata of the current item was refreshed.
	TrackChanged Kind = iota
	// StatusChanged carries a new playback status.
	StatusChanged
	// PlayerGone means the player's interface disappeared.
	PlayerGone
	// PlayerAppeared means the player's interface is (back) on the bus.
	PlayerAppeared
)

// Event is one change reported by a backend. The core stamps it with the
// capture sample count when it arrives.
type Event struct {
	Kind   Kind
	Track  TrackInfo
	Status Status
	// PositionMs is the playback position when known, or -1.
	PositionMs int64
}

// NowPlaying is one OS metadata backend.
type NowPlaying interface {
	// Watch starts watching the target. The first events describe the
	// current state: a TrackChanged (possibly empty) then a StatusChanged.
	Watch(ctx context.Context, t target.Target) (<-chan Event, error)
	Control(cmd string) error
	Volume() (float64, error)
	SetVolume(v float64) error
}

var (
	ErrUnsupported = errors.New("not supported by this backend")
	ErrNoPlayer    = errors.New("player interface not found")
)
